import Foundation
import Security

/// The settings and the last status, in a keychain group. The watch app and the complication
/// share the group. The group is in the KeychainGroup key of the Info.plist.
enum Store {
    private static let service = "org.beetlebug.printdeck"

    static var settings: DeckSettings {
        get { read("settings") ?? DeckSettings() }
        set { write(newValue, "settings") }
    }

    static var lastStatus: Status? {
        get { read("status") }
        set { write(newValue, "status") }
    }

    /// The pairing of the iPhone with the deck for push messages, or nil. The notification
    /// service extension reads the message key from it.
    static var pairing: Pairing? {
        get { read("pairing") }
        set { write(newValue, "pairing") }
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
}
