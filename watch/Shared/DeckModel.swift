// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import CoreGraphics
import Foundation
import Observation
import WidgetKit
#if os(watchOS)
import WatchConnectivity
#endif

/// The state of the app: the settings and the last status of the deck. The watch gets the settings
/// from the iPhone.
@MainActor @Observable
final class DeckModel {
    private(set) var settings = Store.settings
    private(set) var status = Store.lastStatus
    private(set) var error: String?
    /// The status came from the deck since the start of the app, and the last read worked. A
    /// status from the store, or after a failed read, is old.
    private(set) var live = false
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
    #if os(watchOS)
    private var link: PhoneLink?
    #endif

    init() {
        #if DEBUG
        // For screenshots in the simulator: PRINTDECK_ADDRESS and PRINTDECK_KEY set the deck.
        let env = ProcessInfo.processInfo.environment
        if let a = env["PRINTDECK_ADDRESS"], let k = env["PRINTDECK_KEY"] {
            settings = DeckSettings(address: a, key: k)
        }
        #endif
        #if os(watchOS)
        link = PhoneLink { [weak self] settings in
            Task { @MainActor in self?.apply(settings) }
        }
        #endif
    }

    /// Uses new settings: from the iPhone on the watch, from the setup on the iPhone.
    func apply(_ new: DeckSettings) {
        guard new != settings else { return }
        settings = new
        Store.settings = new
        generation += 1
        status = nil
        live = false
        error = nil
        model = nil
        modelImage = nil
        modelLayer = nil
        modelTime = .distantPast
        Store.lastStatus = nil
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
        Store.lastStatus = new
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

    func send(_ command: Command) async {
        guard !sending else { return }
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

#if os(watchOS)
/// The WatchConnectivity session that receives the settings from the iPhone.
final class PhoneLink: NSObject, WCSessionDelegate {
    private let received: (DeckSettings) -> Void

    init(received: @escaping (DeckSettings) -> Void) {
        self.received = received
        super.init()
        guard WCSession.isSupported() else { return }
        WCSession.default.delegate = self
        WCSession.default.activate()
    }

    func session(_ session: WCSession, activationDidCompleteWith state: WCSessionActivationState, error: Error?) {
        handle(session.receivedApplicationContext)
    }

    func session(_ session: WCSession, didReceiveApplicationContext context: [String: Any]) {
        handle(context)
    }

    private func handle(_ context: [String: Any]) {
        guard let address = context["address"] as? String, let key = context["key"] as? String else { return }
        received(DeckSettings(address: address, key: key))
    }
}
#endif
