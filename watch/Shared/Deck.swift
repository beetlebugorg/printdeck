import Foundation

/// The address and the API key of the deck.
struct DeckSettings: Codable, Equatable {
    var address = ""
    var key = ""

    /// The address as a URL. An address with no scheme gets http://.
    var baseURL: URL? {
        var a = address.trimmingCharacters(in: .whitespacesAndNewlines)
        if a.isEmpty { return nil }
        if !a.contains("://") { a = "http://" + a }
        while a.hasSuffix("/") { a.removeLast() }
        guard let url = URL(string: a), url.host != nil else { return nil }
        return url
    }

    var isComplete: Bool { baseURL != nil && !key.trimmingCharacters(in: .whitespaces).isEmpty }
}

/// A temperature and its target, in °C.
struct Temp: Codable, Equatable {
    var actual: Double
    var target: Double
}

/// The roll of filament in the printer.
struct Roll: Codable, Equatable {
    var name: String
    var material: String
    /// Grams of filament on the roll.
    var left: Double
    /// Grams of filament on a full roll.
    var full: Double
    /// The color as #rrggbb, or nil.
    var color: String?
}

/// The state of the deck at one time.
struct Status: Codable, Equatable {
    /// The state text of /api/job: "Printing", "Paused", "Operational", "Offline" and so on.
    var state: String
    /// A print runs or is paused.
    var busy: Bool
    var paused: Bool
    var file: String?
    /// The progress of the print from 0 to 1.
    var progress: Double?
    /// Seconds left in the print, or nil when the deck has no estimate.
    var left: Double?
    var layer: Int?
    var layers: Int?
    var tool: Temp?
    var bed: Temp?
    var roll: Roll?
    /// When the app read the status.
    var time: Date

    /// The end time of the print, or nil when the print is paused.
    var end: Date? {
        guard busy, !paused, let left else { return nil }
        return time.addingTimeInterval(left)
    }

    /// The progress at a later time. It goes up in a straight line to 1 at the end time, so a
    /// complication can show it between two reads of the deck.
    func progress(at date: Date) -> Double? {
        guard let progress else { return nil }
        guard busy, !paused, let left, left > 0 else { return progress }
        let f = min(1, max(0, date.timeIntervalSince(time) / left))
        return progress + (1 - progress) * f
    }

    /// Seconds left at a later time.
    func left(at date: Date) -> Double? {
        guard let left else { return nil }
        if paused { return left }
        return max(0, left - date.timeIntervalSince(time))
    }
}

enum DeckError: LocalizedError {
    case noSettings
    case badKey
    case refused(String)
    case http(Int)
    case badReply
    case denied

    var errorDescription: String? {
        switch self {
        case .noSettings: "Connect to the deck in Printdeck on the iPhone."
        case .badKey: "The deck does not accept the API key."
        case .refused(let msg): msg
        case .http(let code): "The deck sent HTTP \(code)."
        case .badReply: "The reply of the deck has an unknown format."
        case .denied: "The deck denied the app, or nobody tapped Allow within 2 minutes."
        }
    }
}

/// A short message for an error of a request.
func errorText(_ error: Error) -> String {
    if let e = error as? URLError {
        switch e.code {
        case .timedOut, .cannotConnectToHost, .cannotFindHost, .networkConnectionLost, .notConnectedToInternet:
            return "No connection to the deck."
        default:
            return e.localizedDescription
        }
    }
    return error.localizedDescription
}

/// The client of the OctoPrint API of the deck.
struct DeckClient {
    let settings: DeckSettings

    private static let session: URLSession = {
        let c = URLSessionConfiguration.ephemeral
        c.timeoutIntervalForRequest = 8
        c.timeoutIntervalForResource = 15
        c.waitsForConnectivity = false
        return URLSession(configuration: c)
    }()

    private func send(_ path: String, method: String = "GET", body: [String: String]? = nil) async throws -> Data {
        guard settings.isComplete, let base = settings.baseURL else { throw DeckError.noSettings }
        var req = URLRequest(url: base.appending(path: path))
        req.httpMethod = method
        req.setValue(settings.key.trimmingCharacters(in: .whitespaces), forHTTPHeaderField: "X-Api-Key")
        if let body {
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = try JSONEncoder().encode(body)
        }
        let (data, resp) = try await Self.session.data(for: req)
        let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
        switch code {
        case 200..<300:
            return data
        case 403:
            throw DeckError.badKey
        case 409:
            let msg = (try? JSONDecoder().decode([String: String].self, from: data))?["error"]
            throw DeckError.refused(msg ?? "The printer refused the command.")
        default:
            throw DeckError.http(code)
        }
    }

    /// Asks the deck for a key of its own with the App Keys flow of OctoPrint. The touch screen of
    /// the deck asks to allow the app, and the request waits up to 2 minutes for the answer.
    static func requestKey(base: URL, app: String) async throws -> String {
        var req = URLRequest(url: base.appending(path: "/plugin/appkeys/request"))
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try JSONEncoder().encode(["app": app])
        let (data, resp) = try await session.data(for: req)
        let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard code == 201, let token = (try? JSONDecoder().decode([String: String].self, from: data))?["app_token"] else {
            throw code == 404 ? DeckError.refused("The deck has app keys off.") : DeckError.http(code)
        }
        let poll = base.appending(path: "/plugin/appkeys/request/" + token)
        let end = Date().addingTimeInterval(150)
        while Date() < end {
            try await Task.sleep(for: .seconds(1))
            let (data, resp) = try await session.data(from: poll)
            switch (resp as? HTTPURLResponse)?.statusCode ?? 0 {
            case 202:
                continue
            case 200:
                guard let key = (try? JSONDecoder().decode([String: String].self, from: data))?["api_key"] else {
                    throw DeckError.badReply
                }
                return key
            case 404:
                throw DeckError.denied
            case let c:
                throw DeckError.http(c)
            }
        }
        throw DeckError.denied
    }

    private func get<T: Decodable>(_ path: String) async throws -> T {
        let data = try await send(path)
        do {
            return try JSONDecoder().decode(T.self, from: data)
        } catch {
            throw DeckError.badReply
        }
    }

    /// /api/printer, or nil when the printer is not connected (HTTP 409).
    private func printer() async throws -> PrinterReply? {
        do {
            return try await get("/api/printer")
        } catch DeckError.refused {
            return nil
        }
    }

    /// Reads the job, the printer and the Printdeck plugin at the same time.
    func status() async throws -> Status {
        async let job: JobReply = get("/api/job")
        async let plugin: PluginReply = get("/api/plugin/printdeck")
        async let printer = printer()
        let (j, p, pr) = try await (job, plugin, printer)

        var st = Status(state: j.state ?? "Unknown", busy: p.job != nil, paused: p.job?.state == "paused", time: Date())
        if let pj = p.job {
            st.file = j.job?.file?.name ?? pj.file.split(separator: "/").last.map(String.init)
            st.progress = pj.progress
            st.left = pj.left >= 0 ? pj.left : j.progress?.printTimeLeft
            if pj.layers > 0 {
                st.layer = pj.layer
                st.layers = pj.layers
            }
        }
        if let t = pr?.temperature {
            st.tool = t["tool0"].map { Temp(actual: $0.actual, target: $0.target) }
            st.bed = t["bed"].map { Temp(actual: $0.actual, target: $0.target) }
        }
        if let s = p.spool {
            st.roll = Roll(name: s.name, material: s.material, left: s.left, full: s.full,
                           color: s.color?.isEmpty == false ? s.color : nil)
        }
        return st
    }

    func pause() async throws { _ = try await send("/api/job", method: "POST", body: ["command": "pause", "action": "pause"]) }
    func resume() async throws { _ = try await send("/api/job", method: "POST", body: ["command": "pause", "action": "resume"]) }
    func cancel() async throws { _ = try await send("/api/job", method: "POST", body: ["command": "cancel"]) }
}

// The replies of the deck. The fields are in deck/internal/octoapi.

private struct JobReply: Decodable {
    struct File: Decodable { var name: String? }
    struct Job: Decodable { var file: File? }
    struct Progress: Decodable { var printTimeLeft: Double? }
    var state: String?
    var job: Job?
    var progress: Progress?
}

private struct PrinterReply: Decodable {
    struct Temp: Decodable {
        var actual: Double
        var target: Double
    }
    var temperature: [String: Temp]?
}

private struct PluginReply: Decodable {
    struct Spool: Decodable {
        var name: String
        var material: String
        var left: Double
        var full: Double
        var color: String?
    }
    struct Job: Decodable {
        var file: String
        var state: String
        var layer: Int
        var layers: Int
        var progress: Double
        var left: Double
    }
    var spool: Spool?
    var job: Job?
}
