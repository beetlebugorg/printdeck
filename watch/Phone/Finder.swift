import Foundation
import Network
import Observation

/// A deck on the local network.
struct FoundDeck: Identifiable, Hashable {
    let name: String
    let endpoint: NWEndpoint
    var id: String { name }
}

/// Finds the decks on the local network. A deck announces itself as an OctoPrint server
/// (_octoprint._tcp) with Bonjour.
@MainActor @Observable
final class DeckFinder {
    private(set) var decks: [FoundDeck] = []
    private var browser: NWBrowser?

    func start() {
        guard browser == nil else { return }
        let b = NWBrowser(for: .bonjour(type: "_octoprint._tcp", domain: nil), using: .tcp)
        b.browseResultsChangedHandler = { [weak self] results, _ in
            let found = results.compactMap { r -> FoundDeck? in
                guard case let .service(name, _, _, _) = r.endpoint else { return nil }
                return FoundDeck(name: name, endpoint: r.endpoint)
            }
            Task { @MainActor in self?.decks = found.sorted { $0.name < $1.name } }
        }
        b.start(queue: .main)
        browser = b
    }

    func stop() {
        browser?.cancel()
        browser = nil
    }

    /// The IPv4 address and the port of a deck, as "192.168.1.50" or "192.168.1.50:8080". The
    /// watch may not resolve the .local name of the deck, so the settings have the address.
    static func address(of deck: FoundDeck) async throws -> String {
        let params = NWParameters.tcp
        if let ip = params.defaultProtocolStack.internetProtocol as? NWProtocolIP.Options {
            ip.version = .v4
        }
        let conn = NWConnection(to: deck.endpoint, using: params)
        defer { conn.cancel() }
        return try await withCheckedThrowingContinuation { cont in
            var done = false
            func finish(_ result: Result<String, Error>) {
                guard !done else { return }
                done = true
                cont.resume(with: result)
            }
            conn.stateUpdateHandler = { state in
                switch state {
                case .ready:
                    if case let .hostPort(host, port)? = conn.currentPath?.remoteEndpoint {
                        var h = "\(host)"
                        if let i = h.firstIndex(of: "%") { h = String(h[..<i]) }
                        finish(.success(port.rawValue == 80 ? h : "\(h):\(port.rawValue)"))
                    } else {
                        finish(.failure(URLError(.cannotFindHost)))
                    }
                case .failed(let e), .waiting(let e):
                    finish(.failure(e))
                default:
                    break
                }
            }
            conn.start(queue: .main)
            DispatchQueue.main.asyncAfter(deadline: .now() + 10) { finish(.failure(URLError(.timedOut))) }
        }
    }
}
