import SwiftUI

/// The colors of the night palette of the deck (deck/internal/ui/theme.go).
enum Theme {
    static let orange = Color(rgb: 0xff5a1f)
    static let lcd = Color(rgb: 0x080808)
    static let plate = Color(rgb: 0x1c1c1a)
    static let digits = Color(rgb: 0xff8c4a)
    static let digitsDim = Color(rgb: 0xd98a55)
    static let text = Color(rgb: 0xeee9df)
    static let dim = Color(rgb: 0xc2bcb0)
    static let red = Color(rgb: 0x9c2216)

    /// A small uppercase label, as printed on the panel of the deck.
    static func label(_ size: CGFloat = 11) -> Font { .system(size: size, weight: .semibold, design: .monospaced) }
    static func digits(_ size: CGFloat) -> Font { .system(size: size, weight: .medium, design: .monospaced) }
}

extension Color {
    init(rgb: UInt32) {
        self.init(red: Double(rgb >> 16 & 0xff) / 255, green: Double(rgb >> 8 & 0xff) / 255, blue: Double(rgb & 0xff) / 255)
    }

    /// A color from #rrggbb, or nil.
    init?(hex: String) {
        var s = hex
        if s.hasPrefix("#") { s.removeFirst() }
        guard s.count == 6, let v = UInt32(s, radix: 16) else { return nil }
        self.init(rgb: v)
    }
}

enum Format {
    /// A duration as "2h 05m", "14m" or "<1m".
    static func duration(_ seconds: Double) -> String {
        let m = Int(seconds / 60)
        if m < 1 { return "<1m" }
        if m < 60 { return "\(m)m" }
        return String(format: "%dh %02dm", m / 60, m % 60)
    }

    /// A short duration for a small complication: "2h05" or "14m".
    static func shortDuration(_ seconds: Double) -> String {
        let m = Int(seconds / 60)
        if m < 60 { return "\(max(m, 0))m" }
        return String(format: "%dh%02d", m / 60, m % 60)
    }

    /// The time of a day, with the weekday when it is not today.
    static func clock(_ date: Date) -> String {
        let time = date.formatted(date: .omitted, time: .shortened)
        if Calendar.current.isDateInToday(date) { return time }
        return date.formatted(.dateTime.weekday(.abbreviated)) + " " + time
    }

    static func percent(_ progress: Double) -> String { "\(Int((progress * 100).rounded(.down)))%" }

    static func temp(_ t: Temp?) -> String {
        guard let t else { return "--" }
        let actual = Int(t.actual.rounded())
        if t.target <= 0 { return "\(actual)°" }
        return "\(actual)/\(Int(t.target.rounded()))°"
    }
}
