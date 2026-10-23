// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

/// The setup: the connection to the deck, the watch, and the notifications. The app finds the deck
/// on the network and asks it for a key: the touch screen of the deck asks to allow the app.
struct SetupView: View {
    @Environment(DeckModel.self) private var model
    @Environment(\.palette) private var p
    let link: WatchLink
    /// Closes the setup, or nil when the setup is the main screen.
    var close: (() -> Void)?

    @State private var finder = DeckFinder()
    @State private var push = PushPairing.shared
    @State private var busy = false
    @State private var step: String?
    @State private var result: String?
    @State private var manual = false
    @State private var address = Store.settings.address
    @State private var key = Store.settings.key

    private var settings: DeckSettings { model.settings }

    var body: some View {
        ScrollView {
            VStack(spacing: 14) {
                Heading(title: close == nil ? "Printdeck" : "Setup") {
                    if let close {
                        Button("Back", action: close)
                            .buttonStyle(KeyStyle())
                            .frame(width: 100)
                            .environment(\.panelScale, 0.75)
                    }
                }
                .padding(.top, 8)
                deckPlate
                if settings.isComplete {
                    watchPlate
                    notificationsPlate
                }
                handPlate
            }
            .padding(16)
        }
        .background(p.bg.ignoresSafeArea())
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
                    .foregroundStyle(p.dim)
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
                .foregroundStyle(p.dim)
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

    private var notificationsPlate: some View {
        Plate(label: "03 Notifications") {
            DisplayWindow {
                Text(push.pairing == nil ? "OFF" : "ON")
                    .font(Theme.plex(20))
                Text(push.message?.uppercased()
                     ?? (push.pairing == nil ? "THE DECK SENDS NO MESSAGES HERE" : "THIS IPHONE GETS THE MESSAGES"))
                    .font(Theme.plex(14, bold: false))
                    .opacity(0.8)
            }
            if push.pairing == nil {
                Button("Pair for notifications") { Task { await push.pair(with: settings) } }
                    .buttonStyle(KeyStyle(cap: .orange))
                    .disabled(push.busy)
            } else {
                Button("Unpair") { Task { await push.unpair(with: settings) } }
                    .buttonStyle(KeyStyle(cap: .danger))
                    .disabled(push.busy)
            }
            Text("Print done, print failed, paused, first layer done, and a roll that runs short. The messages are encrypted from the deck to this iPhone. The watch shows them too.")
                .font(Theme.plex(12, bold: false))
                .foregroundStyle(p.dim)
                .padding(.horizontal, 4)
        }
    }

    private var handPlate: some View {
        Plate(label: settings.isComplete ? "04 By hand" : "02 By hand") {
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
                    .foregroundStyle(p.dim)
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
        return result ?? (settings.isComplete ? "CONNECTED"
                          : "\(finder.decks.count) DECK\(finder.decks.count == 1 ? "" : "S") FOUND")
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
        model.apply(s)
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
