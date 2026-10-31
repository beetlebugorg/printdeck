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
    /// The projects of the session, and the view of each, from the last read.
    private(set) var projects: [ServiceProject] = []
    private(set) var details: [String: ProjectDetail] = [:]
    /// When the app read the live prints, or nil.
    private(set) var liveTime: Date?
    private(set) var busy = false
    private(set) var message: String?

    var signedIn: Bool { session.map { !$0.expired } ?? false }

    #if DEBUG
    /// For screenshots in the simulator: PRINTDECK_SAMPLE_PROJECTS=1 shows sample projects, with
    /// no service.
    private var sample = false

    init() {
        guard ProcessInfo.processInfo.environment["PRINTDECK_SAMPLE_PROJECTS"] == "1",
              let v = try? ServiceClient.decodeSample(Self.sampleJSON) else { return }
        sample = true
        session = ServiceSession(token: "", kind: "owner", name: "Sample", expires: .distantFuture)
        projects = [ServiceProject(id: "pj1", name: v.name), ServiceProject(id: "pj2", name: "Pod bay")]
        details = ["pj1": v]
        liveTime = Date()
    }

    private static let sampleJSON = """
    {"id": "pj1", "name": "Bittlex", "slug": "bittlex", "is_owner": true,
     "prints": [
       {"id": "a", "n": 1, "name": "Shoulder pair", "result": "complete", "ended": "2026-10-31T12:00:00Z",
        "seconds": 3600, "printer": "Phenom", "ready": "2026-10-31T13:00:00Z", "ready_note": "On the shelf"},
       {"id": "b", "n": 2, "name": "Hip", "result": "complete", "ended": "2026-10-31T14:00:00Z", "printer": "TAZ"},
       {"id": "d", "n": 3, "name": "Elbow", "result": "complete", "ended": "2026-10-30T08:00:00Z",
        "discarded": "2026-10-31T19:00:00Z", "discard_note": "Warped corner"}],
     "plans": [{"id": "pl1", "commit": "abcdef1", "kind": "test", "pr": 7, "created": "2026-10-30T08:00:00Z",
       "builds": [{"id": "bd1", "created": "2026-10-30T09:00:00Z", "state": "ready", "plates": [
         {"printer": "Phenom", "process": "resin", "file": "plate-1.ctb", "state": "waiting", "seconds": 7200,
          "material_name": "Grey 80/20"},
         {"printer": "TAZ", "process": "fdm", "file": "plate-2.gcode", "state": "approved", "seconds": 2700}]}]}],
     "live": [{"name": "Pod door", "percent": 41, "layer": 120, "layers": 300, "left": 2400, "printer": "TAZ"}],
     "queue": [{"printer": "TAZ", "name": "Lid", "place": 1, "seconds": 2700}]
    }
    """
    #endif

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
        projects = []
        details = [:]
        liveTime = nil
        message = nil
    }

    /// Reads the live prints of all projects of the session, and for the owner the live records of
    /// the printers, for a print with no project. A 401 ends the session: the user signs in again.
    func refresh() async {
        #if DEBUG
        if sample { return }
        #endif
        guard signedIn else {
            if session != nil { message = ServiceError.signedOut.errorDescription }
            return
        }
        let c = client
        do {
            let projects = try await c.projects()
            var all: [LivePrint] = []
            var views: [String: ProjectDetail] = [:]
            let owner = session?.kind == "owner"
            try await withThrowingTaskGroup(of: (String?, ProjectDetail?, [LivePrint]).self) { group in
                for p in projects {
                    group.addTask {
                        let v = try await c.project(p.id)
                        return (p.id, v, (v.live ?? []).map { var l = $0; l.project = p.name; return l })
                    }
                }
                if owner { group.addTask { (nil, nil, try await c.printers()) } }
                for try await (id, v, l) in group {
                    all += l
                    if let id, let v { views[id] = v }
                }
            }
            self.projects = projects
            details = views
            live = all
            liveTime = Date()
            message = nil
        } catch ServiceError.signedOut {
            session = nil
            Store.serviceSession = nil
            live = []
            projects = []
            details = [:]
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
