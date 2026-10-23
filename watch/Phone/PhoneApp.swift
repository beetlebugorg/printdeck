import SwiftUI
import WatchConnectivity

@main
struct PhoneApp: App {
    var body: some Scene {
        WindowGroup {
            SettingsView()
        }
    }
}

/// The connection to the deck. The app finds the deck on the network and asks it for a key: the
/// touch screen of the deck asks to allow the app. The address and the key go to the watch.
struct SettingsView: View {
    @Environment(\.colorScheme) private var scheme
    @State private var settings = Store.settings
    @State private var finder = DeckFinder()
    @State private var link = WatchLink()
    @State private var busy = false
    @State private var step: String?
    @State private var result: String?
    @State private var manual = false
    @State private var address = Store.settings.address
    @State private var key = Store.settings.key

    var body: some View {
        let p = Palette.of(scheme)
        ScrollView {
            VStack(spacing: 14) {
                Heading(title: "Printdeck")
                    .padding(.top, 8)
                deckPlate
                if settings.isComplete { watchPlate }
                handPlate
            }
            .padding(16)
        }
        .background(p.bg.ignoresSafeArea())
        .environment(\.palette, p)
        .onAppear { finder.start() }
        .onDisappear { finder.stop() }
    }

    private var deckPlate: some View {
        Plate(label: "01 Deck") {
            DisplayWindow {
                Text(windowTitle)
                    .font(Theme.plex(20))
                    .lineLimit(1)
                    .minimumScaleFactor(0.6)
                Text(windowLine)
                    .font(Theme.plex(14, bold: false))
                    .opacity(0.8)
            }
            if finder.decks.isEmpty && !busy {
                Text("LOOKING FOR THE DECK ON THIS NETWORK")
                    .font(Theme.plex(12))
                    .foregroundStyle(Palette.of(scheme).dim)
                    .padding(.horizontal, 4)
            }
            ForEach(finder.decks) { deck in
                Button(settings.isComplete ? "Connect \(deck.name) again" : "Connect \(deck.name)") {
                    Task { await connect(deck) }
                }
                .buttonStyle(KeyStyle(cap: settings.isComplete ? .raised : .orange))
                .disabled(busy)
            }
            Text("Tap the key, then tap Allow on the touch screen of the deck.")
                .font(Theme.plex(12, bold: false))
                .foregroundStyle(Palette.of(scheme).dim)
                .padding(.horizontal, 4)
        }
    }

    private var watchPlate: some View {
        Plate(label: "02 Watch") {
            DisplayWindow {
                Text(link.state.isEmpty ? "WAITING FOR THE WATCH" : link.state.uppercased())
                    .font(Theme.plex(14))
            }
            Button("Send to the watch") { link.send(settings) }
                .buttonStyle(KeyStyle())
        }
    }

    private var handPlate: some View {
        Plate(label: settings.isComplete ? "03 By hand" : "02 By hand") {
            Button("Enter the address") { withAnimation { manual.toggle() } }
                .buttonStyle(KeyStyle(led: manual))
            if manual {
                PanelField(label: "Address", prompt: "192.168.1.50", text: $address, keyboard: .URL)
                Button("Ask the deck for a key") { Task { await connect(address: address) } }
                    .buttonStyle(KeyStyle(cap: .orange))
                    .disabled(busy || DeckSettings(address: address).baseURL == nil)
                PanelField(label: "API key", prompt: "From printdeck.local/system", text: $key)
                Button("Save the address and key") { Task { await save(DeckSettings(address: address, key: key)) } }
                    .buttonStyle(KeyStyle())
                    .disabled(busy || !DeckSettings(address: address, key: key).isComplete)
                Text("Machine > Wi-Fi and help on the deck shows its address.")
                    .font(Theme.plex(12, bold: false))
                    .foregroundStyle(Palette.of(scheme).dim)
                    .padding(.horizontal, 4)
            }
        }
    }

    private var windowTitle: String {
        if let step { return step.uppercased() }
        return settings.isComplete ? settings.address : "NOT CONNECTED"
    }

    private var windowLine: String {
        if busy { return "WAITING" }
        if step != nil { return "TRY AGAIN" }
        return result ?? (settings.isComplete ? "CONNECTED" : "\(finder.decks.count) DECK\(finder.decks.count == 1 ? "" : "S") FOUND")
    }

    private func connect(_ deck: FoundDeck) async {
        busy = true
        step = "Finding the deck"
        do {
            let a = try await DeckFinder.address(of: deck)
            await connect(address: a)
        } catch {
            step = errorText(error)
            busy = false
        }
    }

    private func connect(address: String) async {
        let a = address.trimmingCharacters(in: .whitespaces)
        guard let base = DeckSettings(address: a).baseURL else { return }
        busy = true
        defer { busy = false }
        step = "Tap Allow on the deck"
        do {
            let k = try await DeckClient.requestKey(base: base, app: "Printdeck on iPhone")
            step = nil
            await save(DeckSettings(address: a, key: k))
        } catch {
            step = errorText(error)
        }
    }

    private func save(_ new: DeckSettings) async {
        let s = DeckSettings(address: new.address.trimmingCharacters(in: .whitespaces),
                             key: new.key.trimmingCharacters(in: .whitespaces))
        Store.settings = s
        settings = s
        address = s.address
        key = s.key
        manual = false
        step = nil
        link.send(s)
        result = "READING THE DECK"
        do {
            let st = try await DeckClient(settings: s).status()
            result = (st.busy ? "\(st.state) \(Format.percent(st.progress ?? 0))" : st.state).uppercased()
        } catch {
            result = errorText(error).uppercased()
        }
    }
}

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
