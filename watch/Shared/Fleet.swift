// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import Foundation
import Observation
import WidgetKit
#if os(watchOS)
import WatchConnectivity
#endif

/// The decks of the app, in the order of the home screen, each with its model. The iPhone keeps
/// the list in the keychain group and sends it to the watch.
@MainActor @Observable
final class Fleet {
    private(set) var models: [DeckModel]
    /// The model of the watch and the widgets with no deck: no settings.
    let empty = DeckModel(deck: SavedDeck(id: ""))
    /// Runs after each round of reads of all decks.
    @ObservationIgnored var onRound: (() -> Void)?
    #if os(watchOS)
    @ObservationIgnored private var link: PhoneLink?
    #endif

    init() {
        var list = Store.decks
        #if DEBUG
        // For screenshots in the simulator: PRINTDECK_ADDRESS and PRINTDECK_KEY set one deck, and
        // PRINTDECK_DECKS sets several as "name|address|key,name|address|key". A printer of a deck
        // with many printers has its label and color too: "name|address|key|label|#rrggbb".
        let env = ProcessInfo.processInfo.environment
        if let a = env["PRINTDECK_ADDRESS"], let k = env["PRINTDECK_KEY"] {
            list = [SavedDeck(id: "debug", name: "Printdeck", settings: DeckSettings(address: a, key: k))]
        }
        if let d = env["PRINTDECK_DECKS"] {
            list = d.split(separator: ",").enumerated().compactMap { i, part in
                let f = part.split(separator: "|", omittingEmptySubsequences: false).map(String.init)
                guard f.count == 3 || f.count == 5 else { return nil }
                var d = SavedDeck(id: "debug\(i)", name: f[0], settings: DeckSettings(address: f[1], key: f[2]))
                // A printer of a deck with many printers: its label and its color.
                if f.count == 5 {
                    d.printer = "debug\(i)"
                    d.label = f[3]
                    d.color = f[4]
                }
                return d
            }
        }
        #endif
        models = list.map { DeckModel(deck: $0) }
        #if DEBUG && os(iOS)
        // PRINTDECK_DECK_KEY connects to a deck with a deck key, as "address|key", and adds its
        // printers.
        if let d = env["PRINTDECK_DECK_KEY"], case let f = d.split(separator: "|").map(String.init), f.count == 2 {
            models = []
            Task { await connect(address: f[0], grant: KeyGrant(key: f[1], deck: true), name: "") }
        }
        #endif
        #if os(watchOS)
        link = PhoneLink { [weak self] decks in
            Task { @MainActor in self?.set(decks) }
        }
        #endif
    }

    /// The list entries of the decks, in order.
    var decks: [SavedDeck] { models.map(\.deck) }

    func model(_ id: String) -> DeckModel? { models.first { $0.id == id } }

    /// The deck that prints. With two prints, the print that ends first.
    var printing: DeckModel? {
        Status.endsFirst(models.map(\.status)).map { models[$0] }
    }

    /// The deck of the watch: the deck that prints, or else the first deck.
    var shown: DeckModel { printing ?? models.first ?? empty }

    /// Adds a deck, or uses the new key of the deck at the same address, and returns its model.
    @discardableResult
    func add(_ deck: SavedDeck) -> DeckModel {
        if let m = models.first(where: { $0.settings.address == deck.address }) {
            m.apply(deck.settings)
            if m.deck.name.isEmpty { m.rename(deck.name) }
            return m
        }
        let m = DeckModel(deck: deck)
        models.append(m)
        Store.decks = decks
        WidgetCenter.shared.reloadAllTimelines()
        Task { await m.refresh() }
        return m
    }

    /// Adds the printers of a deck, or uses the new entries for the printers that the list has (by
    /// address). It returns the model of the first printer.
    @discardableResult
    func add(_ entries: [SavedDeck]) -> DeckModel? {
        var first: DeckModel?
        var added = false
        for d in entries {
            let m: DeckModel
            if let old = models.first(where: { $0.id == d.id || $0.settings.address == d.address }) {
                old.merge(d)
                m = old
            } else {
                m = DeckModel(deck: d)
                models.append(m)
                added = true
                Task { await m.refresh() }
            }
            if first == nil { first = m }
        }
        if added {
            Store.decks = decks
            WidgetCenter.shared.reloadAllTimelines()
        }
        return first
    }

    #if os(iOS)
    /// Adds the printers of the deck at the address with a new key. A deck key gives each printer
    /// of the deck an entry of its own. It returns the model of the first printer.
    @discardableResult
    func connect(address: String, grant: KeyGrant, name: String) async -> DeckModel? {
        if grant.deck {
            let root = SavedDeck.root(address)
            let client = DeckClient(settings: DeckSettings(address: root, key: grant.key))
            let first = (try? await client.printers()).flatMap {
                add($0.entries(address: root, key: grant.key, name: name, existing: decks))
            }
            // Each other entry of the deck with a deck key gets the new key too: a removed deck
            // key failed for all of them.
            add(SavedDeck.rekeyed(decks, root: root, key: grant.key))
            if let first { return first }
            if let m = models.first(where: { $0.deck.deckRoot == root && $0.deck.deckKey == true }) { return m }
        }
        var d = SavedDeck(name: name, settings: DeckSettings(address: address, key: grant.key))
        if let old = models.first(where: { $0.settings.address == address }) { d = old.deck; d.key = grant.key }
        return add([d])
    }

    private var lastSync = Date.distantPast

    /// Reads the printers of each deck with a deck key, at most every 5 minutes, and adds the
    /// printers that are new. A printer that the deck no longer lists stays until the user removes
    /// it.
    func syncPrinters() async {
        guard Date().timeIntervalSince(lastSync) > 300 else { return }
        lastSync = Date()
        var seen = Set<String>()
        for d in decks where d.deckKey == true && seen.insert(d.deckRoot).inserted {
            let client = DeckClient(settings: DeckSettings(address: d.deckRoot, key: d.key))
            guard let list = try? await client.printers() else { continue }
            add(list.entries(address: d.deckRoot, key: d.key, name: d.deckName ?? "", existing: decks))
        }
    }
    #endif

    /// The printers of each deck, in the order of the list: the first printer of a deck sets its
    /// place. A deck with one printer is a group of one.
    var groups: [[DeckModel]] {
        var out: [[DeckModel]] = []
        for m in models {
            if let i = out.firstIndex(where: { $0[0].deck.deckRoot == m.deck.deckRoot }) {
                out[i].append(m)
            } else {
                out.append([m])
            }
        }
        return out
    }

    /// The decks that refused their deck key, once each: the app shows one Connect again for each.
    var refusedDecks: [String] {
        SavedDeck.refusedDecks(decks) { model($0.id)?.problem == .key }
    }

    func remove(_ id: String) {
        models.removeAll { $0.id == id }
        Store.decks = decks
        Store.setStatus(nil, of: id)
        WidgetCenter.shared.reloadAllTimelines()
    }

    /// Uses the list of the iPhone, on the watch. A deck with the same ID keeps its model.
    func set(_ list: [SavedDeck]) {
        guard list != decks else { return }
        var next: [DeckModel] = []
        for d in list {
            if let m = model(d.id) {
                m.update(d)
                next.append(m)
            } else {
                next.append(DeckModel(deck: d))
            }
        }
        for m in models where !list.contains(where: { $0.id == m.id }) {
            Store.setStatus(nil, of: m.id)
        }
        models = next
        Store.decks = list
        WidgetCenter.shared.reloadAllTimelines()
    }

    /// Reads all decks at the same time, every 5 seconds, until the task is cancelled.
    func poll() async {
        while !Task.isCancelled {
            await refresh()
            try? await Task.sleep(for: .seconds(5))
        }
    }

    func refresh() async {
        #if os(iOS)
        await syncPrinters()
        #endif
        await withTaskGroup(of: Void.self) { group in
            for m in models where m.settings.isComplete {
                group.addTask { await m.refresh() }
            }
        }
        onRound?()
    }
}

#if os(watchOS)
/// The WatchConnectivity session that receives the list of decks from the iPhone.
final class PhoneLink: NSObject, WCSessionDelegate {
    private let received: ([SavedDeck]) -> Void

    init(received: @escaping ([SavedDeck]) -> Void) {
        self.received = received
        super.init()
        guard WCSession.isSupported() else { return }
        WCSession.default.delegate = self
        WCSession.default.activate()
    }

    func session(_ session: WCSession, activationDidCompleteWith state: WCSessionActivationState, error: Error?) {
        handle(session.receivedApplicationContext)
    }

    func session(_ session: WCSession, didReceiveApplicationContext context: [String: Any]) {
        handle(context)
    }

    private func handle(_ context: [String: Any]) {
        guard let data = context["decks"] as? Data,
              let decks = try? JSONDecoder().decode([SavedDeck].self, from: data) else { return }
        received(decks)
    }
}
#endif
