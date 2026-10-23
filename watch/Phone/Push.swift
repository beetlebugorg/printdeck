// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import CryptoKit
import Foundation
import Observation
import UIKit
import UserNotifications

/// The pairing of the iPhone with the deck for push messages (relay/README.md):
///
///  1. iOS gives the app a device token.
///  2. The relay gives a send token for the device token (POST /register).
///  3. The app and the deck agree on a message key with X25519
///     (POST /api/plugin/printdeck/push/pair).
///
/// The key is in the keychain group, for the notification service extension. No token or key goes
/// to a log.
@MainActor @Observable
final class PushPairing {
    static let shared = PushPairing()

    private(set) var pairing = Store.pairing
    private(set) var busy = false
    private(set) var message: String?

    private var tokenWaiter: CheckedContinuation<Data, Error>?

    /// The relay, from the PushRelay key of the Info.plist.
    private var relay: URL? {
        (Bundle.main.object(forInfoDictionaryKey: "PushRelay") as? String).flatMap(URL.init(string:))
    }

    /// The APNs environment of the build: sandbox for a build that Xcode installs, production for
    /// TestFlight and the App Store. The provisioning profile of the build has it; an App Store
    /// build has no profile.
    nonisolated static var environment: String {
        guard let url = Bundle.main.url(forResource: "embedded", withExtension: "mobileprovision"),
              let data = try? Data(contentsOf: url),
              let text = String(data: data, encoding: .isoLatin1),
              let r = text.range(of: "<key>aps-environment</key>") else {
            return "production"
        }
        let rest = text[r.upperBound...].prefix(80)
        return rest.contains("development") ? "sandbox" : "production"
    }

    func pair(with settings: DeckSettings) async {
        guard settings.isComplete else {
            message = "Connect to the deck first."
            return
        }
        busy = true
        defer { busy = false }
        message = "Asking for notifications"
        do {
            let center = UNUserNotificationCenter.current()
            guard try await center.requestAuthorization(options: [.alert, .sound, .badge]) else {
                message = "Notifications are off. Turn them on for Printdeck in Settings."
                return
            }
            message = "Registering this iPhone"
            let token = try await deviceToken()
            try await pair(settings: settings, token: token)
            message = nil
        } catch {
            message = errorText(error)
        }
    }

    /// Pairs with a device token: the send token of the relay, then the key exchange with the deck.
    private func pair(settings: DeckSettings, token: Data) async throws {
        guard let relay else { throw DeckError.refused("The app has no relay address.") }
        let env = Self.environment
        let sendToken = try await register(relay: relay, token: token, env: env)
        message = "Pairing with the deck"
        let priv = Curve25519.KeyAgreement.PrivateKey()
        let reply = try await DeckClient(settings: settings).pairPush([
            "name": UIDevice.current.name,
            "device": token.hex,
            "env": env,
            "send_token": sendToken,
            "relay": relay.absoluteString,
            "public_key": priv.publicKey.rawRepresentation.base64EncodedString(),
        ])
        guard let deckKey = Data(base64Encoded: reply.publicKey) else { throw DeckError.badReply }
        let key = try PushCrypto.messageKey(privateKey: priv, deckPublicKey: deckKey, deviceToken: token)
        let p = Pairing(id: reply.id, device: token.hex, env: env, key: key, address: settings.address)
        Store.pairing = p
        pairing = p
    }

    func unpair(with settings: DeckSettings) async {
        guard let p = pairing else { return }
        busy = true
        defer { busy = false }
        do {
            try await DeckClient(settings: settings).unpairPush(id: p.id)
            message = nil
        } catch {
            // The pairing goes on the iPhone in any case. The deck lists it under Settings > Phones.
            message = "The deck did not answer. Remove the phone under Settings > Phones on the deck."
        }
        Store.pairing = nil
        pairing = nil
        UIApplication.shared.unregisterForRemoteNotifications()
    }

    /// The device token of iOS. It waits up to 20 seconds.
    private func deviceToken() async throws -> Data {
        try await withThrowingTaskGroup(of: Data.self) { group in
            group.addTask { @MainActor in
                try await withCheckedThrowingContinuation { c in
                    self.tokenWaiter?.resume(throwing: CancellationError())
                    self.tokenWaiter = c
                    UIApplication.shared.registerForRemoteNotifications()
                }
            }
            group.addTask {
                try await Task.sleep(for: .seconds(20))
                throw DeckError.refused("iOS gave no device token. Check the network and try again.")
            }
            defer { group.cancelAll() }
            let token = try await group.next()!
            return token
        }
    }

    private func register(relay: URL, token: Data, env: String) async throws -> String {
        var req = URLRequest(url: relay.appending(path: "register"))
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try JSONEncoder().encode(["device": token.hex, "env": env])
        req.timeoutInterval = 15
        let (data, resp) = try await URLSession.shared.data(for: req)
        let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard code == 200, let t = (try? JSONDecoder().decode([String: String].self, from: data))?["send_token"] else {
            throw DeckError.refused("The push relay answered \(code).")
        }
        return t
    }

    // The callbacks of the app delegate.

    func received(token: Data) {
        if let w = tokenWaiter {
            tokenWaiter = nil
            w.resume(returning: token)
            return
        }
        // iOS can give a new token, for example after a restore. Pair again with it.
        guard let p = pairing, p.device != token.hex, !busy else { return }
        let settings = Store.settings
        Task {
            busy = true
            defer { busy = false }
            do {
                try await pair(settings: settings, token: token)
            } catch {
                message = "Pair again: iOS gave a new device token, and the deck did not answer."
            }
        }
    }

    func failed(_ error: Error) {
        tokenWaiter?.resume(throwing: error)
        tokenWaiter = nil
    }
}

final class AppDelegate: NSObject, UIApplicationDelegate {
    func application(_ application: UIApplication,
                     didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        // A paired iPhone registers at each start, so that a new device token pairs again.
        if Store.pairing != nil {
            application.registerForRemoteNotifications()
        }
        return true
    }

    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        Task { @MainActor in PushPairing.shared.received(token: deviceToken) }
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
        Task { @MainActor in PushPairing.shared.failed(error) }
    }
}
