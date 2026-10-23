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

/// The settings of the deck on the iPhone. Save keeps them, sends them to the watch and reads the
/// deck with them.
struct SettingsView: View {
    @State private var address = Store.settings.address
    @State private var key = Store.settings.key
    @State private var result: String?
    @State private var testing = false
    @State private var link = WatchLink()

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("192.168.1.50", text: $address)
                        .keyboardType(.URL)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .font(.system(.body, design: .monospaced))
                } header: {
                    Text("DECK ADDRESS")
                } footer: {
                    Text("The IP address of the deck. The watch may not find printdeck.local, so use the IP address. Machine > Wi-Fi and help on the deck shows it.")
                }
                Section {
                    TextField("API key", text: $key)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .font(.system(.body, design: .monospaced))
                } header: {
                    Text("API KEY")
                } footer: {
                    Text("The key is on the System page of the deck: http://printdeck.local/system")
                }
                Section {
                    Button(testing ? "Reading the deck..." : "Save and send to the watch") {
                        Task { await save() }
                    }
                    .disabled(testing)
                    if let result {
                        Text(result)
                            .font(.system(.footnote, design: .monospaced))
                    }
                } footer: {
                    Text(link.state)
                }
            }
            .navigationTitle("Printdeck")
        }
        .tint(Theme.orange)
    }

    private func save() async {
        let settings = DeckSettings(address: address.trimmingCharacters(in: .whitespaces),
                                    key: key.trimmingCharacters(in: .whitespaces))
        Store.settings = settings
        link.send(settings)
        testing = true
        defer { testing = false }
        do {
            let st = try await DeckClient(settings: settings).status()
            if st.busy {
                result = "OK: \(st.state) \(st.file ?? ""), \(Format.percent(st.progress ?? 0))"
            } else {
                result = "OK: \(st.state)"
            }
        } catch {
            result = errorText(error)
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
