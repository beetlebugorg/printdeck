// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import AuthenticationServices
import Observation
import UIKit

/// The sign-in to the service with a passkey, and the live prints of the projects for the cards of
/// the decks that the iPhone cannot reach. The token is in the keychain group (Store.serviceSession).
@MainActor @Observable
final class Account {
    static let shared = Account()

    private(set) var session = Store.serviceSession
    /// The live prints of all projects of the session, from the last read.
    private(set) var live: [LivePrint] = []
    /// When the app read the live prints, or nil.
    private(set) var liveTime: Date?
    private(set) var busy = false
    private(set) var message: String?

    var signedIn: Bool { session.map { !$0.expired } ?? false }

    private var client: ServiceClient { ServiceClient(token: session?.token) }

    /// Signs in with a passkey of printdeck.beetlebug.org: the begin, the assertion of iOS, and
    /// the finish. iOS offers the passkeys of the iPhone and a security key (USB, Lightning or
    /// NFC).
    func signIn() async {
        guard !busy else { return }
        busy = true
        defer { busy = false }
        message = "Asking for the passkey"
        do {
            let c = ServiceClient()
            let (challenge, state) = try await c.loginBegin()
            let a = try await Passkey.assert(challenge: challenge, relyingParty: ServiceClient.relyingParty)
            message = "Signing in"
            let s: ServiceSession
            do {
                s = try await c.loginFinish(state: state, credential: a.json)
            } catch ServiceError.signedOut where !a.hasUser {
                // A security key with no discoverable credential sends no user handle, so the
                // service finds no passkey.
                throw ServiceError.http(401, "This key has no passkey for Printdeck.")
            }
            session = s
            Store.serviceSession = s
            message = nil
            await refresh()
        } catch let e as ASAuthorizationError where e.code == .canceled {
            message = nil
        } catch {
            message = errorText(error)
        }
    }

    func signOut() {
        session = nil
        Store.serviceSession = nil
        live = []
        liveTime = nil
        message = nil
    }

    /// Reads the live prints of all projects of the session, and for the owner the live records of
    /// the printers, for a print with no project. A 401 ends the session: the user signs in again.
    func refresh() async {
        guard signedIn else {
            if session != nil { message = ServiceError.signedOut.errorDescription }
            return
        }
        let c = client
        do {
            let projects = try await c.projects()
            var all: [LivePrint] = []
            let owner = session?.kind == "owner"
            try await withThrowingTaskGroup(of: [LivePrint].self) { group in
                for p in projects { group.addTask { try await c.live(p) } }
                if owner { group.addTask { try await c.printers() } }
                for try await l in group { all += l }
            }
            live = all
            liveTime = Date()
            message = nil
        } catch ServiceError.signedOut {
            session = nil
            Store.serviceSession = nil
            live = []
            message = ServiceError.signedOut.errorDescription
        } catch {
            message = errorText(error)
        }
    }

    /// Reads the live prints every 15 seconds while some deck does not answer, until the task is
    /// cancelled. At home every deck answers, and the app reads only the decks.
    func poll(_ fleet: Fleet) async {
        while !Task.isCancelled {
            if signedIn, fleet.models.contains(where: { $0.settings.isComplete && !$0.live && $0.error != nil }) {
                await refresh()
            }
            try? await Task.sleep(for: .seconds(15))
        }
    }

    /// The live print of a deck at the service, or nil.
    func live(of deck: SavedDeck) -> LivePrint? {
        LivePrint.of(deck, in: live)
    }
}

/// The assertion of a passkey of the iPhone or of a security key, as the JSON of a
/// PublicKeyCredential with base64url fields.
@MainActor
private final class Passkey: NSObject, ASAuthorizationControllerDelegate, ASAuthorizationControllerPresentationContextProviding {
    struct Assertion {
        var json: [String: Any]
        /// The assertion has a user handle. A security key with no discoverable credential has none.
        var hasUser: Bool
    }

    private var done: CheckedContinuation<Assertion, Error>?
    private var controller: ASAuthorizationController?
    /// The ceremony that runs now. The controller holds its delegate weakly, so this holds it
    /// until the end.
    private static var current: Passkey?

    /// Asks for a passkey of the iPhone or a security key, with the same challenge. The login is
    /// discoverable: the requests list no credentials.
    static func assert(challenge: Data, relyingParty: String) async throws -> Assertion {
        let p = Passkey()
        let platform = ASAuthorizationPlatformPublicKeyCredentialProvider(relyingPartyIdentifier: relyingParty)
            .createCredentialAssertionRequest(challenge: challenge)
        let key = ASAuthorizationSecurityKeyPublicKeyCredentialProvider(relyingPartyIdentifier: relyingParty)
            .createCredentialAssertionRequest(challenge: challenge)
        // The service asks for user verification with "preferred".
        key.userVerificationPreference = .preferred
        let ctl = ASAuthorizationController(authorizationRequests: [platform, key])
        ctl.delegate = p
        ctl.presentationContextProvider = p
        current?.finish(.failure(CancellationError()))
        current = p
        p.controller = ctl
        return try await withCheckedThrowingContinuation { c in
            p.done = c
            ctl.performRequests()
        }
    }

    func presentationAnchor(for controller: ASAuthorizationController) -> ASPresentationAnchor {
        let scene = UIApplication.shared.connectedScenes.first { $0.activationState == .foregroundActive } as? UIWindowScene
        return scene?.keyWindow ?? ASPresentationAnchor()
    }

    func authorizationController(controller: ASAuthorizationController, didCompleteWithAuthorization a: ASAuthorization) {
        // The assertion of the iPhone and of a security key have the same fields.
        guard let cred = a.credential as? ASAuthorizationPublicKeyCredentialAssertion else {
            finish(.failure(ServiceError.badReply))
            return
        }
        var response: [String: Any] = [
            "clientDataJSON": cred.rawClientDataJSON.base64URL,
            "authenticatorData": cred.rawAuthenticatorData.base64URL,
            "signature": cred.signature.base64URL,
        ]
        let user: Data? = cred.userID
        let hasUser = !(user ?? Data()).isEmpty
        if hasUser, let user { response["userHandle"] = user.base64URL }
        finish(.success(Assertion(json: [
            "id": cred.credentialID.base64URL,
            "rawId": cred.credentialID.base64URL,
            "type": "public-key",
            "response": response,
        ], hasUser: hasUser)))
    }

    func authorizationController(controller: ASAuthorizationController, didCompleteWithError error: Error) {
        finish(.failure(error))
    }

    private func finish(_ r: Result<Assertion, Error>) {
        done?.resume(with: r)
        done = nil
        controller = nil
        if Self.current === self { Self.current = nil }
    }
}
