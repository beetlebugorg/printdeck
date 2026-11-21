// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

/// The setup of a deck, as a form. With no model it adds a deck: the app finds the decks on the
/// network and asks one for a key, and the touch screen of the deck asks to allow the app. On a
/// deck with many printers, the key works for each printer, and the app adds each one. With a
/// model it is the Settings of that printer: the name, the watch, the notifications, the address
/// and key, and Remove.
struct SetupView: View {
    @Environment(Fleet.self) private var fleet
    let link: WatchLink
    /// The printer of the Settings, or nil to add a deck.
    let model: DeckModel?
    /// Closes the setup, or nil when the setup is the main screen.
    var close: (() -> Void)?
    /// Opens the screen of a new printer.
    var added: ((String) -> Void)?
    /// Closes the screen of the printer after Remove.
    var removed: (() -> Void)?

    @State private var finder = DeckFinder()
    @State private var push = PushPairing.shared
    @State private var busy = false
    @State private var step: String?
    @State private var result: String?
    @State private var address = ""
    @State private var key = ""
    @State private var name = ""
    @State private var confirmRemove = false

    var body: some View {
        Form {
            if let model {
                printerSection(model)
                notificationsSection(model.deck)
                watchSection
                connectionSection(model)
                handSection
                removeSection(model)
            } else {
                findSection
                handSection
            }
        }
        .navigationTitle(model.map { "Settings · \($0.deck.label ?? $0.deck.title)" } ?? "Add a deck")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if let close { Button("Done", action: close) }
        }
        .onAppear {
            if model == nil { finder.start() }
            address = model?.settings.address ?? ""
            key = model?.settings.key ?? ""
            name = model?.deck.name ?? ""
        }
        .onDisappear { finder.stop() }
        .confirmationDialog(model.map { "Remove \($0.deck.title) from this iPhone?" } ?? "", isPresented: $confirmRemove,
                            titleVisibility: .visible) {
            Button("Remove", role: .destructive) {
                if let model { Task { await remove(model) } }
            }
            Button("Back", role: .cancel) {}
        } message: {
            Text("The app forgets the printer and its key, and unpairs its notifications. The deck keeps the printer.")
        }
    }

    // MARK: Add a deck

    private var findSection: some View {
        Section {
            if let step {
                HStack {
                    if busy { ProgressView().padding(.trailing, 4) }
                    Text(step).foregroundStyle(busy ? .primary : Color.red)
                }
            }
            if finder.decks.isEmpty && !busy {
                HStack {
                    ProgressView().padding(.trailing, 4)
                    Text("Looking for decks on this network").foregroundStyle(.secondary)
                }
            }
            ForEach(finder.decks) { deck in
                let known = fleet.models.contains { $0.deck.name == deck.name || $0.deck.deckName == deck.name }
                Button {
                    Task { await connect(deck) }
                } label: {
                    HStack {
                        Label(deck.name, systemImage: "printer")
                        Spacer()
                        Text(known ? "Connect again" : "Connect").foregroundStyle(.tint)
                    }
                }
                .foregroundStyle(.primary)
                .disabled(busy)
            }
        } header: {
            Text("Decks on this network")
        } footer: {
            Text("Tap a deck, then tap Allow on the touch screen of the deck. On a deck with many printers, the app adds each printer.")
        }
    }

    // MARK: The Settings of a printer

    private func printerSection(_ model: DeckModel) -> some View {
        Section {
            HStack {
                TextField("Name", text: $name, prompt: Text(model.settings.address))
                    .submitLabel(.done)
                    .onSubmit { model.rename(name) }
                    .disabled(model.deck.onSharedDeck)
                LabelChip(deck: model.deck)
            }
            LabeledContent("State", value: result ?? (model.live ? "Connected" : model.error ?? "Reading"))
            LabeledContent("Kind", value: model.deck.process == .resin ? "Resin" : "FDM")
        } header: {
            Text("Printer")
        } footer: {
            if model.deck.onSharedDeck {
                Text("The name of a printer of this deck comes from the deck.")
            }
        }
    }

    private func notificationsSection(_ deck: SavedDeck) -> some View {
        let paired = push.pairing(of: deck) != nil
        return Section {
            if paired {
                Button("Unpair", role: .destructive) { Task { await push.unpair(with: deck) } }
                    .disabled(push.busy)
            } else {
                Button("Pair for notifications") { Task { await push.pair(with: deck) } }
                    .disabled(push.busy)
            }
            if let m = push.message { Text(m).font(.footnote).foregroundStyle(.secondary) }
        } header: {
            Text("Notifications · " + (paired ? "On" : "Off"))
        } footer: {
            Text("Print done, print failed, paused, first layer done, and a roll that runs short. The messages are encrypted from the deck to this iPhone. The watch shows them too.")
        }
    }

    private var watchSection: some View {
        Section {
            LabeledContent("Watch", value: link.state.isEmpty ? "Waiting for the watch" : link.state)
            Button("Send to the watch") { link.send(fleet.decks) }
        } footer: {
            Text("The watch shows the printer that prints, or the first printer.")
        }
    }

    private func connectionSection(_ model: DeckModel) -> some View {
        Section {
            LabeledContent("Address", value: model.settings.address)
            if model.deck.onSharedDeck { LabeledContent("Deck", value: model.deck.deckName ?? model.deck.deckRoot) }
            Button {
                Task { await connect(address: model.deck.deckRoot, name: model.deck.deckName ?? model.deck.name) }
            } label: {
                HStack {
                    Text("Connect again")
                    if busy { Spacer(); ProgressView() }
                }
            }
            .disabled(busy)
            if let step { Text(step).font(.footnote).foregroundStyle(busy ? .secondary : Color.red) }
        } header: {
            Text("Connection")
        } footer: {
            Text("Connect again asks the deck for a new key. Tap Allow on the touch screen of the deck.")
        }
    }

    private func removeSection(_ model: DeckModel) -> some View {
        Section {
            Button("Remove from this iPhone", role: .destructive) { confirmRemove = true }
                .disabled(busy)
        }
    }

    // MARK: By hand

    private var handSection: some View {
        Section {
            TextField("Address", text: $address, prompt: Text("192.168.1.50"))
                .keyboardType(.URL)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            Button("Ask the deck for a key") {
                Task { await connect(address: address, name: model?.deck.name ?? "") }
            }
            .disabled(busy || DeckSettings(address: address).baseURL == nil)
            TextField("API key", text: $key, prompt: Text("From printdeck.local/system"))
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            Button("Save the address and key") {
                Task { await save(DeckSettings(address: address, key: key), name: model?.deck.name ?? "") }
            }
            .disabled(busy || !DeckSettings(address: address, key: key).isComplete)
        } header: {
            Text("By hand")
        } footer: {
            Text("Machine > Wi-Fi and help on the deck shows its address.")
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
            let g = try await DeckClient.requestKey(base: base, app: "Printdeck on iPhone")
            step = "Reading the printers"
            let m = await fleet.connect(address: a, grant: g, name: name)
            step = nil
            if let m { await read(m) }
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
        await read(m)
    }

    /// Reads the deck after a new key, and opens the deck screen of a new deck.
    private func read(_ m: DeckModel) async {
        address = m.settings.address
        key = m.settings.key
        step = nil
        result = "Reading the deck"
        await m.refresh()
        result = m.error
        if model == nil, m.error == nil { added?(m.id) }
    }

    private func remove(_ model: DeckModel) async {
        if push.pairing(of: model.deck) != nil { await push.unpair(with: model.deck) }
        fleet.remove(model.id)
        removed?()
    }
}
