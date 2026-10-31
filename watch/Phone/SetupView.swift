// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

/// The setup of a deck. With no model it adds a deck: the app finds the decks on the network and
/// asks one for a key, and the touch screen of the deck asks to allow the app. With a model it is
/// the Menu of that deck: the name, the watch, the notifications, the address and key, and Remove.
struct SetupView: View {
    @Environment(Fleet.self) private var fleet
    @Environment(\.palette) private var p
    let link: WatchLink
    /// The deck of the Menu, or nil to add a deck.
    let model: DeckModel?
    /// Closes the setup, or nil when the setup is the main screen.
    var close: (() -> Void)?
    /// Opens the deck screen of a new deck.
    var added: ((String) -> Void)?
    /// Closes the deck screen after Remove.
    var removed: (() -> Void)?

    @State private var finder = DeckFinder()
    @State private var push = PushPairing.shared
    @State private var busy = false
    @State private var step: String?
    @State private var result: String?
    @State private var manual = false
    @State private var address = ""
    @State private var key = ""
    @State private var name = ""
    @State private var confirmRemove = false

    var body: some View {
        ScrollView {
            VStack(spacing: 14) {
                Heading(title: heading) {
                    if let close {
                        Button("Back", action: close)
                            .buttonStyle(KeyStyle())
                            .frame(width: 100)
                            .environment(\.panelScale, 0.75)
                    }
                }
                .padding(.top, 8)
                if let model {
                    deckPlate(model)
                    watchPlate
                    notificationsPlate(model.deck)
                    handPlate(label: "04 By hand")
                    removePlate
                } else {
                    findPlate
                    handPlate(label: "02 By hand")
                }
            }
            .padding(16)
        }
        .background(p.bg.ignoresSafeArea())
        .onAppear {
            if model == nil { finder.start() }
            address = model?.settings.address ?? ""
            key = model?.settings.key ?? ""
            name = model?.deck.name ?? ""
        }
        .onDisappear { finder.stop() }
        .overlay {
            if confirmRemove, let model {
                ConfirmPanel(question: "Remove \(model.deck.title)?", yes: "Remove", danger: true,
                             onYes: {
                                 confirmRemove = false
                                 Task { await remove(model) }
                             },
                             onNo: { confirmRemove = false })
            }
        }
    }

    private var heading: String {
        if model != nil { return "Setup" }
        return close == nil ? "Printdeck" : "Add a deck"
    }

    // MARK: Add a deck

    private var findPlate: some View {
        Plate(label: "01 Deck") {
            DisplayWindow {
                Text((step ?? "Not connected").uppercased())
                    .font(Theme.plex(20))
                    .lineLimit(1)
                    .minimumScaleFactor(0.6)
                Text(busy ? "WAITING" : step != nil ? "TRY AGAIN"
                     : "\(finder.decks.count) DECK\(finder.decks.count == 1 ? "" : "S") FOUND")
                    .font(Theme.plex(14, bold: false))
                    .opacity(0.8)
            }
            if finder.decks.isEmpty && !busy {
                Text("LOOKING FOR DECKS ON THIS NETWORK")
                    .font(Theme.plex(12))
                    .foregroundStyle(p.dim)
                    .padding(.horizontal, 4)
            }
            ForEach(finder.decks) { deck in
                let known = fleet.models.contains { $0.deck.name == deck.name }
                Button(known ? "Connect \(deck.name) again" : "Connect \(deck.name)") {
                    Task { await connect(deck) }
                }
                .buttonStyle(KeyStyle(cap: known ? .raised : .orange))
                .disabled(busy)
            }
            Text("Tap the key, then tap Allow on the touch screen of the deck.")
                .font(Theme.plex(12, bold: false))
                .foregroundStyle(p.dim)
                .padding(.horizontal, 4)
        }
    }

    // MARK: The Menu of a deck

    private func deckPlate(_ model: DeckModel) -> some View {
        Plate(label: "01 Deck") {
            DisplayWindow {
                Text((step ?? model.deck.title).uppercased())
                    .font(Theme.plex(20))
                    .lineLimit(1)
                    .minimumScaleFactor(0.6)
                Text(deckLine(model))
                    .font(Theme.plex(14, bold: false))
                    .opacity(0.8)
            }
            PanelField(label: "Name", prompt: model.settings.address, text: $name)
            Button("Save the name") { model.rename(name) }
                .buttonStyle(KeyStyle())
                .disabled(name.trimmingCharacters(in: .whitespaces) == model.deck.name)
            Button("Connect again") { Task { await connect(address: model.settings.address, name: model.deck.name) } }
                .buttonStyle(KeyStyle())
                .disabled(busy)
            Text("Connect again asks the deck for a new key. Tap Allow on the touch screen of the deck.")
                .font(Theme.plex(12, bold: false))
                .foregroundStyle(p.dim)
                .padding(.horizontal, 4)
        }
    }

    /// The address, the process and the state: "192.168.76.45 · RESIN · CONNECTED".
    private func deckLine(_ model: DeckModel) -> String {
        if busy { return "WAITING" }
        if step != nil { return "TRY AGAIN" }
        var parts = [model.settings.address]
        if let pr = model.deck.process { parts.append(pr == .resin ? "RESIN" : "FDM") }
        parts.append(result ?? (model.live ? "CONNECTED" : model.error == nil ? "READING" : "NO CONNECTION"))
        return parts.joined(separator: " · ").uppercased()
    }

    private var watchPlate: some View {
        Plate(label: "02 Watch") {
            DisplayWindow {
                Text(link.state.isEmpty ? "WAITING FOR THE WATCH" : link.state.uppercased())
                    .font(Theme.plex(14))
            }
            Button("Send to the watch") { link.send(fleet.decks) }
                .buttonStyle(KeyStyle())
            Text("The watch shows the deck that prints, or the first deck.")
                .font(Theme.plex(12, bold: false))
                .foregroundStyle(p.dim)
                .padding(.horizontal, 4)
        }
    }

    private func notificationsPlate(_ deck: SavedDeck) -> some View {
        let paired = push.pairing(of: deck) != nil
        return Plate(label: "03 Notifications") {
            DisplayWindow {
                Text(paired ? "ON" : "OFF")
                    .font(Theme.plex(20))
                Text(push.message?.uppercased()
                     ?? (paired ? "THIS IPHONE GETS THE MESSAGES" : "THE DECK SENDS NO MESSAGES HERE"))
                    .font(Theme.plex(14, bold: false))
                    .opacity(0.8)
            }
            if paired {
                Button("Unpair") { Task { await push.unpair(with: deck) } }
                    .buttonStyle(KeyStyle(cap: .danger))
                    .disabled(push.busy)
            } else {
                Button("Pair for notifications") { Task { await push.pair(with: deck) } }
                    .buttonStyle(KeyStyle(cap: .orange))
                    .disabled(push.busy)
            }
            Text("Print done, print failed, paused, first layer done, and a roll that runs short. The messages are encrypted from the deck to this iPhone. The watch shows them too.")
                .font(Theme.plex(12, bold: false))
                .foregroundStyle(p.dim)
                .padding(.horizontal, 4)
        }
    }

    private var removePlate: some View {
        Plate(label: "05 Remove") {
            Button("Remove this deck") { confirmRemove = true }
                .buttonStyle(KeyStyle(cap: .danger))
                .disabled(busy)
            Text("The app forgets the deck and its key, and unpairs its notifications.")
                .font(Theme.plex(12, bold: false))
                .foregroundStyle(p.dim)
                .padding(.horizontal, 4)
        }
    }

    // MARK: By hand

    private func handPlate(label: String) -> some View {
        Plate(label: label) {
            Button("Enter the address") { withAnimation { manual.toggle() } }
                .buttonStyle(KeyStyle(led: manual))
            if manual {
                PanelField(label: "Address", prompt: "192.168.1.50", text: $address, keyboard: .URL)
                Button("Ask the deck for a key") {
                    Task { await connect(address: address, name: model?.deck.name ?? "") }
                }
                .buttonStyle(KeyStyle(cap: .orange))
                .disabled(busy || DeckSettings(address: address).baseURL == nil)
                PanelField(label: "API key", prompt: "From printdeck.local/system", text: $key)
                Button("Save the address and key") {
                    Task { await save(DeckSettings(address: address, key: key), name: model?.deck.name ?? "") }
                }
                .buttonStyle(KeyStyle())
                .disabled(busy || !DeckSettings(address: address, key: key).isComplete)
                Text("Machine > Wi-Fi and help on the deck shows its address.")
                    .font(Theme.plex(12, bold: false))
                    .foregroundStyle(p.dim)
                    .padding(.horizontal, 4)
            }
        }
    }

    // MARK: Connect

    private func connect(_ deck: FoundDeck) async {
        busy = true
        step = "Finding the deck"
        do {
            let a = try await DeckFinder.address(of: deck)
            await connect(address: a, name: deck.name)
        } catch {
            step = errorText(error)
            busy = false
        }
    }

    private func connect(address: String, name: String) async {
        let a = address.trimmingCharacters(in: .whitespaces)
        guard let base = DeckSettings(address: a).baseURL else { return }
        busy = true
        defer { busy = false }
        step = "Tap Allow on the deck"
        do {
            let k = try await DeckClient.requestKey(base: base, app: "Printdeck on iPhone")
            step = nil
            await save(DeckSettings(address: a, key: k), name: name)
        } catch {
            step = errorText(error)
        }
    }

    /// Saves the address and the key: to the deck of the Menu, or to a new deck of the list.
    private func save(_ new: DeckSettings, name: String) async {
        let s = DeckSettings(address: new.address.trimmingCharacters(in: .whitespaces),
                             key: new.key.trimmingCharacters(in: .whitespaces))
        let m: DeckModel
        if let model {
            model.apply(s)
            m = model
        } else {
            m = fleet.add(SavedDeck(name: name, settings: s))
        }
        address = s.address
        key = s.key
        manual = false
        step = nil
        result = "READING THE DECK"
        await m.refresh()
        result = m.error?.uppercased()
        if model == nil, m.error == nil { added?(m.id) }
    }

    private func remove(_ model: DeckModel) async {
        if push.pairing(of: model.deck) != nil { await push.unpair(with: model.deck) }
        fleet.remove(model.id)
        removed?()
    }
}
