// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

/// The colors of the night palette of the deck (deck/internal/ui/theme.go). The watch uses them.
enum Theme {
    static let orange = Color(rgb: 0xff5a1f)
    static let lcd = Color(rgb: 0x080808)
    static let plate = Color(rgb: 0x1c1c1a)
    static let digits = Color(rgb: 0xff8c4a)
    static let digitsDim = Color(rgb: 0xd98a55)
    static let text = Color(rgb: 0xeee9df)
    static let dim = Color(rgb: 0xc2bcb0)
    static let red = Color(rgb: 0x9c2216)

    /// IBM Plex Mono, the type of the deck (Shared/Fonts, SIL Open Font License).
    static func plex(_ size: CGFloat, bold: Bool = true) -> Font {
        .custom(bold ? "IBMPlexMono-SmBld" : "IBMPlexMono", fixedSize: size)
    }

    /// A small uppercase label, as printed on the panel of the deck.
    static func label(_ size: CGFloat = 11) -> Font { plex(size) }
    static func digits(_ size: CGFloat) -> Font { plex(size, bold: false) }
}

/// The day and night palettes of the deck. The watch has the night palette.
struct Palette {
    var bg, surface, raised, edge, shine, text, dim, orange, onAcc, lcd, lcdInk, lcdDim, ledOff: Color
    var redCap, onRed: Color
    /// Red text, for a key that stops or deletes.
    var red: Color
    /// The blue character display: the backlight, a dot that is off, and a dot that is on. Both
    /// palettes have the bright blue of the day palette of the deck, as the app icon.
    var glcd = Color(rgb: 0x1f4bd1), glcdCell = Color(rgb: 0x2a58e0), glcdInk = Color(rgb: 0xf2f6ff)

    static let day = Palette(
        bg: Color(rgb: 0xd3d0c8), surface: Color(rgb: 0xe6e3dc), raised: Color(rgb: 0xf3f1ec),
        edge: Color(rgb: 0xa9a59c), shine: Color(rgb: 0xfbfaf7), text: Color(rgb: 0x171717),
        dim: Color(rgb: 0x4a4742), orange: Color(rgb: 0xff5a1f), onAcc: Color(rgb: 0x121212),
        lcd: Color(rgb: 0x1b1c1a), lcdInk: Color(rgb: 0xff7a30), lcdDim: Color(rgb: 0xc27a48),
        ledOff: Color(rgb: 0x8f8a80), redCap: Color(rgb: 0xc0291b), onRed: Color(rgb: 0xfbfaf7),
        red: Color(rgb: 0xd12b1d))

    static let night = Palette(
        bg: Color(rgb: 0x121211), surface: Color(rgb: 0x1c1c1a), raised: Color(rgb: 0x292825),
        edge: Color(rgb: 0x060606), shine: Color(rgb: 0x34332f), text: Color(rgb: 0xeee9df),
        dim: Color(rgb: 0xc2bcb0), orange: Color(rgb: 0xff7a3a), onAcc: Color(rgb: 0x0e0e0d),
        lcd: Color(rgb: 0x080808), lcdInk: Color(rgb: 0xff8c4a), lcdDim: Color(rgb: 0xd98a55),
        ledOff: Color(rgb: 0x3a3834), redCap: Color(rgb: 0x9c2216), onRed: Color(rgb: 0xf3efe8),
        red: Color(rgb: 0xff7a6a))

    static func of(_ scheme: ColorScheme) -> Palette { scheme == .dark ? night : day }
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
        let m = Int(min(max(seconds, 0), 1e8) / 60)
        if m < 1 { return "<1m" }
        if m < 60 { return "\(m)m" }
        return String(format: "%dh %02dm", m / 60, m % 60)
    }

    /// A short duration for a small complication: "2h05" or "14m".
    static func shortDuration(_ seconds: Double) -> String {
        let m = Int(min(max(seconds, 0), 1e8) / 60)
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
