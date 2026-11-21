// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

/// The short label of a printer of a deck with many printers, on its color. The color never shows
/// without the label. A deck with one printer has no label, and shows no chip.
struct LabelChip: View {
    let deck: SavedDeck
    var size: CGFloat = 12

    var body: some View {
        if let label = deck.label, !label.isEmpty {
            let fill = deck.color.flatMap(Color.init(hex:)) ?? .gray
            Text(label)
                .font(.system(size: size, weight: .bold, design: .rounded))
                .foregroundStyle(Self.light(deck.color) ? Color.black : Color.white)
                .padding(.horizontal, size * 0.55)
                .padding(.vertical, size * 0.2)
                .background(Capsule().fill(fill))
                .accessibilityLabel("Printer \(label)")
        }
    }

    /// The color is light: black text reads better on it.
    static func light(_ hex: String?) -> Bool {
        guard var s = hex else { return true }
        if s.hasPrefix("#") { s.removeFirst() }
        guard s.count == 6, let v = UInt32(s, radix: 16) else { return true }
        let r = Double(v >> 16 & 0xff), g = Double(v >> 8 & 0xff), b = Double(v & 0xff)
        return (0.299 * r + 0.587 * g + 0.114 * b) / 255 > 0.6
    }
}

/// The kind of a printer as a glyph: a printer for FDM, a drop for resin.
struct KindIcon: View {
    let process: DeckProcess?

    var body: some View {
        Image(systemName: process == .resin ? "drop.fill" : "printer.fill")
            .accessibilityLabel(process == .resin ? "Resin" : "FDM")
    }
}

/// The chip, the name and the kind of a printer, for the top of its screen.
struct PrinterTitle: View {
    let deck: SavedDeck

    var body: some View {
        HStack(spacing: 6) {
            LabelChip(deck: deck, size: 11)
            Text(deck.title)
                .font(.headline)
                .lineLimit(1)
            KindIcon(process: deck.process)
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }
}

/// A thin bar of the progress of a print.
struct ProgressLine: View {
    let value: Double
    var tint: Color = .accentColor

    var body: some View {
        GeometryReader { g in
            ZStack(alignment: .leading) {
                Capsule().fill(.quaternary)
                Capsule().fill(tint).frame(width: max(4, g.size.width * min(1, max(0, value))))
            }
        }
        .frame(height: 6)
        .accessibilityElement()
        .accessibilityLabel("Progress")
        .accessibilityValue(Format.percent(value))
    }
}

/// The label of a row of a list: a symbol in the accent color and a title in the color of text,
/// with a chevron for a row that opens a page.
struct RowLabel: View {
    let title: String
    let symbol: String
    var chevron = false
    @Environment(\.isEnabled) private var enabled

    init(_ title: String, symbol: String, chevron: Bool = false) {
        self.title = title
        self.symbol = symbol
        self.chevron = chevron
    }

    var body: some View {
        HStack {
            Label {
                Text(title).foregroundStyle(enabled ? Color.primary : Color.secondary)
            } icon: {
                Image(systemName: symbol).foregroundStyle(enabled ? Color.accentColor : Color.secondary)
            }
            if chevron {
                Spacer()
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(.tertiary)
            }
        }
    }
}
