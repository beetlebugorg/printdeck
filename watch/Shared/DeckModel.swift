// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import CoreGraphics
import Foundation
import Observation
import WidgetKit

/// The state of one deck: its place in the list, and its last status. The watch gets the list
/// from the iPhone (Fleet).
@MainActor @Observable
final class DeckModel: Identifiable {
    /// The deck in the list: the address, the key, the name, the process and the camera.
    private(set) var deck: SavedDeck
    private(set) var status: Status?
    /// The ID of the deck (SavedDeck.id). It stays the same for the life of the model.
    nonisolated let id: String
    var settings: DeckSettings { deck.settings }
    private(set) var error: String?
    /// The status came from the deck since the start of the app, and the last read worked. A
    /// status from the store, or after a failed read, is old.
    private(set) var live = false
    /// The camera view that follows the print now (DeckCamera.view).
    var cameraView: String { DeckCamera.view(printing: status.map { $0.busy && !$0.paused } ?? false) }
    /// The camera that the user chose for this deck (DeckCamera.id), or nil for the main camera.
    private(set) var cameraID: String?
    /// The camera to show: the chosen camera when the deck has it on, or the main camera.
    var camera: DeckCamera? { DeckCamera.pick(status?.cameras ?? [], id: cameraID, main: status?.camera) }
    /// The printer has a camera for a picture: the status reports a camera that is on, or, before
    /// the first status, the list of the deck names a camera of the printer.
    var hasCameraPicture: Bool {
        if status != nil { return camera?.on == true }
        return deck.cameraPath != nil
    }

    /// The cameras of the deck that are on, in the order of the deck.
    var cameras: [DeckCamera] { (status?.cameras ?? []).filter { $0.on && $0.id != nil } }
    /// The reason of the last failed read, or nil.
    private(set) var problem: DeckProblem?

    /// The problem of the printer now: the deck list or a read says that the printer is gone.
    var problemNow: DeckProblem? { deck.gone == true ? .gone : problem }

    /// A command is on its way to the deck.
    private(set) var sending = false
    /// The result of the last command, for a few seconds: "Pausing" or the error of the deck.
    private(set) var notice: String?
    private var noticeEnd = Date.distantPast
    /// The model of the print in dots, or nil.
    private(set) var model: LCDPicture?
    /// The model of the print in the colors of the deck, for the iPhone, or nil.
    private(set) var modelImage: CGImage?
    /// The size of the picture in color in pixels, or nil for none.
    var modelImageSize: (w: Int, h: Int)?
    /// The size of the picture of the model that the screen has room for, in dots, or nil for no
    /// picture.
    var modelSize: (w: Int, h: Int)?
    private var modelTime = Date.distantPast
    private var modelLayer: Int?

    private var lastReload = Date.distantPast
    /// The last status in the store: the app writes it again only when it changes.
    private var stored: Status?
    /// Goes up at new settings and at each command, so that an older read does not overwrite a
    /// newer status.
    private var generation = 0

    init(deck: SavedDeck) {
        self.deck = deck
        id = deck.id
        status = Store.status(of: deck.id)
        cameraID = deck.camera
    }

    /// Shows the camera id of the deck (DeckCamera.id), or the main camera with nil, and keeps the
    /// choice for this deck.
    func chooseCamera(_ id: String?) {
        guard id != cameraID else { return }
        cameraID = id
        deck.camera = id
        Store.save(deck)
    }

    /// Changes the name of the deck.
    func rename(_ name: String) {
        let n = name.trimmingCharacters(in: .whitespaces)
        guard n != deck.name else { return }
        deck.name = n
        Store.save(deck)
    }

    /// Uses a new list entry for this deck, from the iPhone on the watch. New settings start the
    /// reads again.
    func update(_ new: SavedDeck) {
        guard new != deck else { return }
        let settings = new.settings
        deck.name = new.name
        deck.process = new.process ?? deck.process
        deck.printerID = new.printerID ?? deck.printerID
        identify(new)
        chooseCamera(new.camera)
        apply(settings)
    }

    /// Uses the entry of the printer from the list of its deck: the key, and the name, the label,
    /// the color and the kind of the printer. A printer of a deck with one printer keeps the name
    /// that the user gave it. The chosen camera stays.
    func merge(_ new: SavedDeck) {
        if new.printer != nil || deck.name.isEmpty { deck.name = new.name }
        deck.process = new.process ?? deck.process
        identify(new)
        Store.save(deck)
        apply(new.settings)
    }

    /// Copies the fields of the printer and of its deck.
    private func identify(_ new: SavedDeck) {
        deck.deckAddress = new.deckAddress
        deck.deckName = new.deckName
        deck.printer = new.printer
        deck.label = new.label
        deck.color = new.color
        deck.model = new.model
        deck.deckKey = new.deckKey
        deck.cameraPath = new.cameraPath
        deck.gone = new.gone
    }

    /// Uses a new address or key: from the iPhone on the watch, from the setup on the iPhone.
    func apply(_ new: DeckSettings) {
        guard new != settings else { return }
        deck.settings = new
        Store.save(deck)
        generation += 1
        status = nil
        live = false
        error = nil
        problem = nil
        model = nil
        modelImage = nil
        modelLayer = nil
        modelTime = .distantPast
        Store.setStatus(nil, of: deck.id)
        stored = nil
        WidgetCenter.shared.reloadAllTimelines()
        Task { await refresh() }
    }

    /// Reads the deck every 5 seconds until the task is cancelled.
    func poll() async {
        while !Task.isCancelled {
            await refresh()
            try? await Task.sleep(for: .seconds(5))
        }
    }

    func refresh() async {
        guard settings.isComplete else {
            error = DeckError.noSettings.errorDescription
            return
        }
        let gen = generation
        do {
            let new = try await DeckClient(settings: settings).status()
            // New settings or a command came during the read: this status is older than theirs.
            guard gen == generation else { return }
            let old = status
            status = new
            live = true
            error = nil
            problem = nil
            if let p = new.process, p != deck.process {
                deck.process = p
                Store.save(deck)
            }
            if let id = new.printerID, id != deck.printerID {
                deck.printerID = id
                Store.save(deck)
            }
            store(new)
            reloadComplication(old: old, new: new)
            await refreshModel(new)
        } catch {
            guard gen == generation, !isCancel(error) else { return }
            live = false
            self.error = errorText(error)
            problem = DeckProblem.of(error, sharedDeck: deck.onSharedDeck)
        }
    }

    /// The text of the command, or nil when it has ended.
    var noticeNow: String? { Date() < noticeEnd ? notice : nil }

    private func show(_ text: String, for seconds: Double) {
        notice = text
        noticeEnd = Date().addingTimeInterval(seconds)
    }

    /// A read that stopped because the task ended: the wrist went down, or the app left.
    private func isCancel(_ error: Error) -> Bool {
        error is CancellationError || (error as? URLError)?.code == .cancelled
    }

    /// Writes the status to the store for the widgets when the print changes, and at most every
    /// minute otherwise. A keychain write is slow on the watch.
    private func store(_ new: Status) {
        if let s = stored, s.busy == new.busy, s.paused == new.paused, s.file == new.file, s.layer == new.layer,
           s.roll == new.roll, new.time.timeIntervalSince(s.time) < 60 {
            return
        }
        stored = new
        Store.setStatus(new, of: deck.id)
    }

    /// Reloads the complication when the print starts, pauses or ends, and at most every 10
    /// minutes otherwise. The system limits the reloads of a complication.
    private func reloadComplication(old: Status?, new: Status) {
        let changed = old?.busy != new.busy || old?.paused != new.paused || old?.file != new.file
        if changed || Date().timeIntervalSince(lastReload) > 600 {
            lastReload = Date()
            WidgetCenter.shared.reloadAllTimelines()
        }
    }

    /// Reads the picture of the model at a new layer, or every 20 seconds. The picture changes
    /// with the layers.
    private func refreshModel(_ st: Status) async {
        guard st.busy, let size = modelSize else {
            model = nil
            modelImage = nil
            return
        }
        guard st.layer != modelLayer || Date().timeIntervalSince(modelTime) > 20 else { return }
        let gen = generation
        let client = DeckClient(settings: settings)
        // A failed read keeps the last picture, and the next reading tries again.
        guard let pic = await client.model(width: size.w, height: size.h), gen == generation else { return }
        model = pic
        modelTime = Date()
        modelLayer = st.layer
        if let c = modelImageSize, let img = await client.modelImage(width: c.w, height: c.h), gen == generation {
            modelImage = img
        }
    }

    /// The queue of the printer, while its screen reads it, or nil.
    var queue: DeckQueue?

    /// Reads the queue. A failed read keeps the last queue: the screen shows the error of the deck.
    func refreshQueue() async {
        guard settings.isComplete, let q = try? await DeckClient(settings: settings).queue() else { return }
        queue = q
    }

    /// The controls that the app shows for this printer now.
    var controls: Controls { Controls(deck: deck, status: status, live: live) }

    /// Runs a control on the deck, and checks its effect: after the deck accepts it, the app reads
    /// the status until it shows the change, and shows the change. When the status does not show
    /// it in time, or cannot show it, the text says so. taken runs when the deck accepts the control.
    /// It reports that the deck took the control.
    @discardableResult
    func run(_ doing: String, effect: Effect, taken: (() -> Void)? = nil,
             _ f: @escaping (DeckClient) async throws -> Void) async -> Bool {
        guard !sending else { return false }
        sending = true
        generation += 1
        checks += 1
        let token = checks
        let before = status
        show(doing, for: 60)
        do {
            try await f(DeckClient(settings: settings))
        } catch {
            sending = false
            show(errorText(error), for: 8)
            await refresh()
            return false
        }
        // The next control can go now, and the page of this one can close: the screen of the
        // printer shows the check.
        sending = false
        taken?()
        await check(effect, before: before, token: token)
        return true
    }

    /// The nozzle heats for a load, an unload or an extrusion that waits for it.
    private(set) var heating = false
    private var heatStop = false

    /// Stops the wait for a hot nozzle. The nozzle keeps its target.
    func stopHeating() { heatStop = true }

    /// Heats the nozzle to temp, waits until it is hot (3 °C under the target, as the swap of the
    /// deck), and then runs the control. A Klipper printer refuses to extrude with a cold nozzle.
    /// The wait ends after 8 minutes, or with stopHeating.
    func heatThen(_ temp: Int, doing: String, effect: Effect, _ f: @escaping (DeckClient) async throws -> Void) async {
        guard !heating else { return }
        heating = true
        heatStop = false
        defer { heating = false }
        let target = Int((status?.lcd?.tool_target ?? status?.tool?.target ?? 0).rounded())
        if target < temp - 3 {
            guard await run("Heating the nozzle", effect: .nozzleTarget(temp), { try await $0.setTool(temp) }) else { return }
        }
        let end = Date().addingTimeInterval(480)
        while true {
            let actual = status?.lcd?.tool ?? status?.tool?.actual ?? 0
            if SwapMaterial.hot(actual, for: temp) { break }
            if heatStop || Task.isCancelled {
                show("Stopped the wait. The nozzle stays at \(temp) °C.", for: 8)
                return
            }
            if Date() > end {
                show("The nozzle did not reach \(temp) °C in 8 minutes.", for: 10)
                return
            }
            show("Heating the nozzle: \(Int(actual.rounded())) of \(temp) °C", for: 5)
            try? await Task.sleep(for: .seconds(2))
            await refresh()
        }
        await run(doing, effect: effect, f)
    }

    /// The number of the last control: only its check shows its text.
    private var checks = 0

    /// Reads the status about once a second until it shows the effect, or until the timeout.
    private func check(_ effect: Effect, before: Status?, token: Int) async {
        func say(_ text: String, _ seconds: Double) {
            if token == checks { show(text, for: seconds) }
        }
        if let text = effect.unobservable(before: before) {
            await refresh()
            say(text, 6)
            return
        }
        let end = Date().addingTimeInterval(effect.timeout)
        repeat {
            await refresh()
            if token != checks { return }
            if live, let st = status, let text = effect.confirmed(before: before, after: st) {
                say(text, 5)
                return
            }
            try? await Task.sleep(for: .seconds(1))
        } while Date() < end && !Task.isCancelled
        say(effect.unconfirmed(before: before), 8)
    }

    enum Command {
        case pause, resume, cancel

        var doing: String {
            switch self {
            case .pause: "Pausing"
            case .resume: "Resuming"
            case .cancel: "Cancelling"
            }
        }
    }

    /// The app shows Resume for this deck. A resin deck resumes only from its touch screen.
    var resumes: Bool { deck.resumesFromApp }

    func send(_ command: Command) async {
        guard !sending else { return }
        // The keys for Resume are off on a resin deck, and the deck refuses a resume there too.
        if command == .resume && !resumes {
            show("Resume on the touch screen of the deck", for: 8)
            return
        }
        let effect: Effect = switch command {
        case .pause: .pause
        case .resume: .resume
        case .cancel: .cancel
        }
        await run(command.doing, effect: effect) { client in
            switch command {
            case .pause: try await client.pause()
            case .resume: try await client.resume()
            case .cancel: try await client.cancel()
            }
        }
    }
}
