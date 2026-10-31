// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import Foundation

/// The session at the service (printdeck.beetlebug.org): the reply to the finish of the passkey
/// sign-in (wire.SignInReply in deck/internal/cloud/wire). It is in the keychain group.
struct ServiceSession: Codable, Equatable {
    var token: String
    /// "owner" or "member".
    var kind: String
    /// The project of a member with one project, or nil.
    var project: String?
    var name: String?
    var expires: Date

    var expired: Bool { expires <= Date() }
}

/// A project in the list of /api/v1/projects (wire.ProjectSummary).
struct ServiceProject: Decodable, Identifiable, Equatable {
    var id: String
    var name: String
}

/// A print of a project that prints now, or that ended a short time ago (wire.ProjectLive). The
/// live record of a printer (wire.PrinterLive) becomes a LivePrint too, with no project and no
/// model.
struct LivePrint: Decodable, Equatable {
    var name: String
    /// The picture of the model, or nil. It works for one hour.
    var model: URL?
    /// From 0 to 100.
    var percent: Double
    var layer: Int
    var layers: Int
    /// Seconds left, or 0 for no estimate.
    var left: Int
    var paused: Bool
    /// "" while it prints, or "complete", "cancelled" or "error".
    var result: String
    var updated: Date?
    var printer: String
    var printerID: String
    /// The project of the print, from the view that had it.
    var project = ""
    /// The print came from the live record of the printer, which has no model and no project.
    var fromPrinter = false
    /// The printer has no print and no result: a record from the printer only.
    var idle = false

    var printing: Bool { result.isEmpty && !idle }

    enum CodingKeys: String, CodingKey {
        case name, model, percent, layer, layers, left, paused, result, updated, printer
        case printerID = "printer_id"
    }

    init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        model = try c.decodeIfPresent(String.self, forKey: .model).flatMap { URL(string: $0, relativeTo: ServiceClient.base)?.absoluteURL }
        percent = try c.decodeIfPresent(Double.self, forKey: .percent) ?? 0
        layer = try c.decodeIfPresent(Int.self, forKey: .layer) ?? 0
        layers = try c.decodeIfPresent(Int.self, forKey: .layers) ?? 0
        left = try c.decodeIfPresent(Int.self, forKey: .left) ?? 0
        paused = try c.decodeIfPresent(Bool.self, forKey: .paused) ?? false
        result = try c.decodeIfPresent(String.self, forKey: .result) ?? ""
        updated = try c.decodeIfPresent(String.self, forKey: .updated).flatMap(ServiceClient.date)
        printer = try c.decodeIfPresent(String.self, forKey: .printer) ?? ""
        printerID = try c.decodeIfPresent(String.self, forKey: .printerID) ?? ""
    }

    /// The live record of a printer (wire.PrinterLive in GET /api/v1/printers).
    init(printer: ServicePrinter, live r: ServicePrinter.Live) {
        name = r.name ?? ""
        percent = r.percent ?? 0
        layer = r.layer ?? 0
        layers = r.layers ?? 0
        left = r.left ?? 0
        paused = r.paused ?? false
        result = r.printing ? "" : (r.result ?? "")
        idle = !r.printing && (r.result ?? "").isEmpty
        updated = r.updated.flatMap(ServiceClient.date)
        self.printer = printer.name
        printerID = printer.id
        fromPrinter = true
    }

    /// The live print of a deck: a print that runs, then a print of a project (it has the picture
    /// of the model), then the newest. A deck matches by the ID of its printer at the service, or
    /// by its name when the app has no ID for it.
    static func of(_ deck: SavedDeck, in list: [LivePrint]) -> LivePrint? {
        let mine = list.filter { l in
            if let id = deck.printerID, !id.isEmpty { return l.printerID == id }
            return !deck.name.isEmpty && l.printer.caseInsensitiveCompare(deck.name) == .orderedSame
        }
        return mine.max { a, b in
            if a.printing != b.printing { return !a.printing }
            if a.fromPrinter != b.fromPrinter { return a.fromPrinter }
            return (a.updated ?? .distantPast) < (b.updated ?? .distantPast)
        }
    }
}

/// A printer at the service (wire.Printer), with what it does now for the owner.
struct ServicePrinter: Decodable {
    struct Live: Decodable {
        var printing: Bool
        var paused: Bool?
        var name: String?
        var percent: Double?
        var layer: Int?
        var layers: Int?
        var left: Int?
        var result: String?
        var updated: String?
    }
    var id: String
    var name: String
    var live: Live?
}

enum ServiceError: LocalizedError {
    case signedOut
    case http(Int, String?)
    case badReply

    var errorDescription: String? {
        switch self {
        case .signedOut: "Sign in to Printdeck again."
        case .http(let code, let msg): msg ?? "The service sent HTTP \(code)."
        case .badReply: "The reply of the service has an unknown format."
        }
    }
}

/// The client of the API of the service.
struct ServiceClient {
    /// The service, from the ServiceURL key of the Info.plist.
    static let base: URL = (Bundle.main.object(forInfoDictionaryKey: "ServiceURL") as? String)
        .flatMap(URL.init(string:)) ?? URL(string: "https://printdeck.beetlebug.org")!

    /// The relying party of the passkeys: the host of the service.
    static var relyingParty: String { base.host ?? "printdeck.beetlebug.org" }

    /// The token of the session, or nil before the sign-in.
    var token: String?

    private static let session: URLSession = {
        let c = URLSessionConfiguration.ephemeral
        c.timeoutIntervalForRequest = 10
        c.timeoutIntervalForResource = 20
        return URLSession(configuration: c)
    }()

    /// A time of the service: RFC 3339, with or without the fraction of a second.
    static func date(_ s: String) -> Date? {
        if let d = try? Date(s, strategy: .iso8601) { return d }
        return try? Date(s, strategy: Date.ISO8601FormatStyle(includingFractionalSeconds: true))
    }

    private static let decoder: JSONDecoder = {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .custom { dec in
            let s = try dec.singleValueContainer().decode(String.self)
            guard let date = ServiceClient.date(s) else {
                throw DecodingError.dataCorrupted(.init(codingPath: dec.codingPath, debugDescription: "bad time \(s)"))
            }
            return date
        }
        return d
    }()

    /// Sends a request to /api/v1/<path>. A 401 means the session ended.
    func send(_ path: String, method: String = "GET", body: Data? = nil) async throws -> Data {
        var req = URLRequest(url: Self.base.appending(path: "/api/v1/" + path))
        req.httpMethod = method
        if let body {
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = body
        }
        if let token { req.setValue("Bearer " + token, forHTTPHeaderField: "Authorization") }
        let (data, resp) = try await Self.session.data(for: req)
        let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
        switch code {
        case 200..<300: return data
        case 401: throw ServiceError.signedOut
        default:
            let msg = (try? JSONDecoder().decode([String: String].self, from: data))?["error"]
            throw ServiceError.http(code, msg)
        }
    }

    func get<T: Decodable>(_ path: String) async throws -> T {
        let data = try await send(path)
        do {
            return try Self.decoder.decode(T.self, from: data)
        } catch {
            throw ServiceError.badReply
        }
    }

    /// The begin of the passkey sign-in: the options (PublicKeyCredentialRequestOptions under
    /// "publicKey") and the sealed state for the finish.
    func loginBegin() async throws -> (challenge: Data, state: String) {
        let data = try await send("auth/login/begin", method: "POST", body: Data("{}".utf8))
        struct Begin: Decodable {
            struct Options: Decodable {
                struct Key: Decodable { var challenge: String }
                var publicKey: Key
            }
            var options: Options
            var state: String
        }
        guard let b = try? JSONDecoder().decode(Begin.self, from: data),
              let challenge = Data(base64URL: b.options.publicKey.challenge) else { throw ServiceError.badReply }
        return (challenge, b.state)
    }

    /// The finish of the passkey sign-in, with the assertion as the JSON of a PublicKeyCredential.
    func loginFinish(state: String, credential: [String: Any]) async throws -> ServiceSession {
        let body = try JSONSerialization.data(withJSONObject: ["state": state, "credential": credential])
        let data = try await send("auth/login/finish", method: "POST", body: body)
        guard let s = try? Self.decoder.decode(ServiceSession.self, from: data) else { throw ServiceError.badReply }
        return s
    }

    func projects() async throws -> [ServiceProject] {
        try await get("projects")
    }

    /// The live records of the printers, for the owner. A member gets 401.
    func printers() async throws -> [LivePrint] {
        let list: [ServicePrinter] = try await get("printers")
        return list.compactMap { p in p.live.map { LivePrint(printer: p, live: $0) } }
    }

    /// The live prints of a project, each with the name of the project.
    func live(_ project: ServiceProject) async throws -> [LivePrint] {
        struct View: Decodable { var live: [LivePrint]? }
        let v: View = try await get("projects/" + project.id)
        return (v.live ?? []).map {
            var l = $0
            l.project = project.name
            return l
        }
    }
}

extension Data {
    /// Base64url with no padding, as WebAuthn has it.
    init?(base64URL s: String) {
        var b = s.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        while b.count % 4 != 0 { b += "=" }
        self.init(base64Encoded: b)
    }

    var base64URL: String {
        base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }
}
