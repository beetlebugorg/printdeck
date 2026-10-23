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
            try s.updateApplicationContext(["address": settings.address, "key": settings.key])
            state = "Sent to the watch."
        } catch {
            state = "The watch did not get the settings: \(error.localizedDescription)"
        }
    }

    func session(_ session: WCSession, activationDidCompleteWith state: WCSessionActivationState, error: Error?) {
        let s = Store.settings
        if state == .activated, s.isComplete, session.isWatchAppInstalled,
           session.applicationContext["key"] as? String != s.key || session.applicationContext["address"] as? String != s.address {
            try? session.updateApplicationContext(["address": s.address, "key": s.key])
        }
    }

    func sessionDidBecomeInactive(_ session: WCSession) {}

    func sessionDidDeactivate(_ session: WCSession) {
        session.activate()
    }
}
