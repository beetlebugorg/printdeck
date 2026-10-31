// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import Foundation
import Security

/// The settings and the last status, in a keychain group. The watch app and the complication
/// share the group. The group is in the KeychainGroup key of the Info.plist.
enum Store {
    private static let service = "org.beetlebug.printdeck"

    /// The ID of the deck of an older app, which had one deck in "settings".
    static let firstDeckID = "1"

    /// The decks, in the order of the home screen. On the first read after an update from the
    /// app with one deck, the deck of "settings" becomes the first deck of the list, with its
    /// status, its pairing and its camera.
    static var decks: [SavedDeck] {
        get {
            if let d: [SavedDeck] = read("decks") { return d }
            return moveSettings()
        }
        set { write(newValue, "decks") }
    }

    /// Replaces the deck with the same ID in the list. A deck that is not in the list stays out.
    static func save(_ deck: SavedDeck) {
        var list = decks
        guard let i = list.firstIndex(where: { $0.id == deck.id }), list[i] != deck else { return }
        list[i] = deck
        decks = list
    }

    /// The last status of a deck, or nil.
    static func status(of id: String) -> Status? {
        read("status." + id)
    }

    static func setStatus(_ st: Status?, of id: String) {
        write(st, "status." + id)
    }

    /// The pairings of the iPhone with the decks for push messages. The notification service
    /// extension reads the message keys from them.
    static var pairings: [Pairing] {
        get {
            _ = decks
            return read("pairings") ?? []
        }
        set { write(newValue, "pairings") }
    }

    /// Makes the list from the items of the app with one deck. It writes the list only when it
    /// finds a deck: a read of the keychain fails before the first unlock, and an empty list in the
    /// keychain stops the move. The old items stay, so that an app and a widget can both make the
    /// same list, with the same ID, at the same time.
    private static func moveSettings() -> [SavedDeck] {
        guard let old: DeckSettings = read("settings"), old.isComplete else { return [] }
        let camera = UserDefaults.standard.string(forKey: "camera." + old.address)
        let list = [SavedDeck(id: firstDeckID, settings: old, camera: camera)]
        if let st: Status = read("status") { setStatus(st, of: firstDeckID) }
        if var p: Pairing = read("pairing") {
            p.deck = firstDeckID
            write([p], "pairings")
        }
        decks = list
        return list
    }

    private static var group: String? {
        guard let g = Bundle.main.object(forInfoDictionaryKey: "KeychainGroup") as? String,
              !g.isEmpty, !g.hasPrefix(".") else { return nil }
        return g
    }

    /// The query of an item. A build with no team has no group prefix, and then the item is in
    /// the keychain of the app only.
    private static func query(_ account: String, group: String?) -> [String: Any] {
        var q: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
        if let group { q[kSecAttrAccessGroup as String] = group }
        return q
    }

    private static func read<T: Decodable>(_ account: String) -> T? {
        var q = query(account, group: group)
        q[kSecReturnData as String] = true
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var out: CFTypeRef?
        var status = SecItemCopyMatching(q as CFDictionary, &out)
        if status == errSecMissingEntitlement {
            q[kSecAttrAccessGroup as String] = nil
            status = SecItemCopyMatching(q as CFDictionary, &out)
        }
        guard status == errSecSuccess, let data = out as? Data else { return nil }
        return try? JSONDecoder().decode(T.self, from: data)
    }

    private static func write<T: Encodable>(_ value: T?, _ account: String) {
        var g = group
        var status = SecItemDelete(query(account, group: g) as CFDictionary)
        if status == errSecMissingEntitlement {
            g = nil
            SecItemDelete(query(account, group: nil) as CFDictionary)
        }
        guard let value, let data = try? JSONEncoder().encode(value) else { return }
        var q = query(account, group: g)
        q[kSecValueData as String] = data
        q[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
        status = SecItemAdd(q as CFDictionary, nil)
        if status == errSecMissingEntitlement, g != nil {
            q[kSecAttrAccessGroup as String] = nil
            SecItemAdd(q as CFDictionary, nil)
        }
    }
}

/// A pairing with the deck for push messages.
struct Pairing: Codable, Equatable {
    /// The ID of the phone on the deck.
    var id: String
    /// The APNs device token, in hex.
    var device: String
    /// "sandbox" or "production".
    var env: String
    /// The message key, 32 bytes.
    var key: Data
    /// The address of the deck at the pairing.
    var address: String
    /// The ID of the deck (SavedDeck.id). Nil only in a pairing of an older app before the move to
    /// the list of decks.
    var deck: String?
}
