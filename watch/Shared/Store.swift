import Foundation

/// The settings and the last status, in the defaults of the app group. The watch app and the
/// complication share them.
enum Store {
    static var defaults: UserDefaults {
        let group = Bundle.main.object(forInfoDictionaryKey: "AppGroup") as? String ?? ""
        return UserDefaults(suiteName: group) ?? .standard
    }

    static var settings: DeckSettings {
        get { read("settings") ?? DeckSettings() }
        set { write(newValue, "settings") }
    }

    static var lastStatus: Status? {
        get { read("status") }
        set { write(newValue, "status") }
    }

    private static func read<T: Decodable>(_ key: String) -> T? {
        guard let data = defaults.data(forKey: key) else { return nil }
        return try? JSONDecoder().decode(T.self, from: data)
    }

    private static func write<T: Encodable>(_ value: T?, _ key: String) {
        if let value, let data = try? JSONEncoder().encode(value) {
            defaults.set(data, forKey: key)
        } else {
            defaults.removeObject(forKey: key)
        }
    }
}
