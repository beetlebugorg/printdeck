// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import Foundation
import Observation
import WatchConnectivity

/// The WatchConnectivity session that sends the settings to the watch as the application context.
/// The watch gets the last context when it starts, so the watch app does not need to run.
@Observable
final class WatchLink: NSObject, WCSessionDelegate {
    private(set) var state = ""

    override init() {
        super.init()
        guard WCSession.isSupported() else {
            state = "This iPhone has no watch."
            return
        }
        WCSession.default.delegate = self
        WCSession.default.activate()
    }

    func send(_ settings: DeckSettings) {
        let s = WCSession.default
        guard s.activationState == .activated else {
            state = "The watch session is not active."
            return
        }
        guard s.isPaired, s.isWatchAppInstalled else {
            state = "Printdeck is not on the watch."
            return
        }
        do {
            try s.updateApplicationContext(Self.context(settings))
            state = "Sent to the watch."
        } catch {
            state = "The watch did not get the settings: \(error.localizedDescription)"
        }
    }

    /// The context for the watch. The time makes each context new, so that WatchConnectivity
    /// sends it again to a watch app that was installed again.
    private static func context(_ s: DeckSettings) -> [String: Any] {
        ["address": s.address, "key": s.key, "sent": Date().timeIntervalSince1970]
    }

    /// Sends the settings when the watch app is on the watch.
    private func sendStored(_ session: WCSession) {
        let s = Store.settings
        guard session.activationState == .activated, s.isComplete, session.isPaired, session.isWatchAppInstalled
        else { return }
        try? session.updateApplicationContext(Self.context(s))
    }

    func session(_ session: WCSession, activationDidCompleteWith state: WCSessionActivationState, error: Error?) {
        sendStored(session)
    }

    /// A new watch, or the watch app installed after the setup: the watch gets the settings now.
    func sessionWatchStateDidChange(_ session: WCSession) {
        sendStored(session)
    }

    func sessionDidBecomeInactive(_ session: WCSession) {}

    func sessionDidDeactivate(_ session: WCSession) {
        session.activate()
    }
}
