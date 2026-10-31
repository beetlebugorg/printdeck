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
    /// The cameras of the deck that are on, in the order of the deck.
    var cameras: [DeckCamera] { (status?.cameras ?? []).filter { $0.on && $0.id != nil } }
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
        chooseCamera(new.camera)
        apply(settings)
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
        sending = true
        defer { sending = false }
        // A read that started before the command has the old state.
        generation += 1
        show(command.doing, for: 30)
        let client = DeckClient(settings: settings)
        do {
            switch command {
            case .pause: try await client.pause()
            case .resume: try await client.resume()
            case .cancel: try await client.cancel()
            }
            show(command.doing, for: 4)
        } catch {
            show(errorText(error), for: 8)
        }
        await refresh()
    }
}
