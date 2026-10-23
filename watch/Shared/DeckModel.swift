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
    /// A command is on its way to the deck.
    private(set) var sending = false

    private var lastReload = Date.distantPast
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
        status = nil
        Store.lastStatus = nil
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
        do {
            let new = try await DeckClient(settings: settings).status()
            let old = status
            status = new
            error = nil
            Store.lastStatus = new
            reloadComplication(old: old, new: new)
        } catch {
            self.error = errorText(error)
        }
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

    enum Command { case pause, resume, cancel }

    func send(_ command: Command) async {
        sending = true
        defer { sending = false }
        let client = DeckClient(settings: settings)
        do {
            switch command {
            case .pause: try await client.pause()
            case .resume: try await client.resume()
            case .cancel: try await client.cancel()
            }
            error = nil
        } catch {
            self.error = errorText(error)
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
