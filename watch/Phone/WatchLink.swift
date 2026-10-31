// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import Foundation
import Observation
import WatchConnectivity

/// The WatchConnectivity session that sends the list of decks to the watch as the application
/// context. The watch gets the last context when it starts, so the watch app does not need to run.
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

    func send(_ decks: [SavedDeck]) {
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
            try s.updateApplicationContext(Self.context(decks))
            state = "Sent to the watch."
        } catch {
            state = "The watch did not get the decks: \(error.localizedDescription)"
        }
    }

    /// The context for the watch. The time makes each context new, so that WatchConnectivity
    /// sends it again to a watch app that was installed again.
    private static func context(_ decks: [SavedDeck]) throws -> [String: Any] {
        ["decks": try JSONEncoder().encode(decks), "sent": Date().timeIntervalSince1970]
    }

    /// Sends the decks when the watch app is on the watch.
    private func sendStored(_ session: WCSession) {
        guard session.activationState == .activated, session.isPaired, session.isWatchAppInstalled,
              let c = try? Self.context(Store.decks) else { return }
        try? session.updateApplicationContext(c)
    }

    func session(_ session: WCSession, activationDidCompleteWith state: WCSessionActivationState, error: Error?) {
        sendStored(session)
    }

    /// A new watch, or the watch app installed after the setup: the watch gets the decks now.
    func sessionWatchStateDidChange(_ session: WCSession) {
        sendStored(session)
    }

    func sessionDidBecomeInactive(_ session: WCSession) {}

    func sessionDidDeactivate(_ session: WCSession) {
        session.activate()
    }
}
