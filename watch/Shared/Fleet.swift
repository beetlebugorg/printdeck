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
        // PRINTDECK_DECKS sets several as "name|address|key,name|address|key".
        let env = ProcessInfo.processInfo.environment
        if let a = env["PRINTDECK_ADDRESS"], let k = env["PRINTDECK_KEY"] {
            list = [SavedDeck(id: "debug", name: "Printdeck", settings: DeckSettings(address: a, key: k))]
        }
        if let d = env["PRINTDECK_DECKS"] {
            list = d.split(separator: ",").enumerated().compactMap { i, part in
                let f = part.split(separator: "|", omittingEmptySubsequences: false).map(String.init)
                guard f.count == 3 else { return nil }
                return SavedDeck(id: "debug\(i)", name: f[0], settings: DeckSettings(address: f[1], key: f[2]))
            }
        }
        #endif
        models = list.map { DeckModel(deck: $0) }
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
