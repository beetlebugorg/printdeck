// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

/// The status panel of the deck (drawStatusBox in deck/internal/ui/statusbox.go), on the dark
/// window of the watch: three readouts, the state and the time, the message, the details, and the
/// progress bar, in the amber of the deck and IBM Plex Mono.
struct PanelView: View {
    let text: PanelText
    /// The printer: a printer of a deck with many printers shows its label and its name.
    var deck: SavedDeck?
    private let p = Palette.night

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            if let deck, deck.onSharedDeck, let label = deck.label {
                HStack(spacing: 5) {
                    Text(label)
                        .font(Theme.plex(10))
                        .foregroundStyle(Self.light(deck.color) ? Color.black : Color.white)
                        .padding(.horizontal, 5)
                        .padding(.vertical, 1)
                        .background(Capsule().fill(deck.color.flatMap(Color.init(hex:)) ?? p.lcdCell))
                    Text(deck.title)
                        .font(Theme.plex(11))
                        .foregroundStyle(p.lcdInk)
                        .lineLimit(1)
                }
                .accessibilityElement(children: .combine)
            }
            // Five readouts are narrow: the nozzle, the aux nozzle and the bed, then the chamber
            // and the fan, as the deck puts them in two rows.
            let rows = text.readouts.count > 3 ? [Array(text.readouts.prefix(3)), Array(text.readouts.dropFirst(3))]
                : [text.readouts]
            ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                HStack(spacing: 4) {
                    // Two rows are smaller, and a row with no targets has no line for them.
                    let targets = row.contains { !$0.target.isEmpty }
                    ForEach(row, id: \.name) { ReadoutCell(readout: $0, compact: rows.count > 1, targetLine: targets) }
                }
            }
            .padding(.bottom, 1)
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(text.head)
                    .font(Theme.plex(16))
                    .lineLimit(1)
                    .minimumScaleFactor(0.6)
                Spacer(minLength: 0)
                // The deck drops "Ends" when the line has no room for it.
                ViewThatFits(in: .horizontal) {
                    Text(text.side)
                    if !text.short.isEmpty { Text(text.short) }
                }
                .font(Theme.plex(12))
                .lineLimit(1)
            }
            .foregroundStyle(p.lcdInk)
            if !text.msg.isEmpty {
                Text(text.msg)
                    .font(Theme.plex(13, bold: false))
                    .foregroundStyle(p.lcdInk)
                    .lineLimit(2)
                    .minimumScaleFactor(0.8)
            }
            if !text.detail.isEmpty {
                Text(text.detail)
                    .font(Theme.plex(12, bold: false))
                    .foregroundStyle(p.lcdDim)
                    .lineLimit(2)
                    .minimumScaleFactor(0.8)
            }
            if !text.rolls.isEmpty {
                RollsRow(rolls: text.rolls)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 2)
            }
            if text.bar {
                GeometryReader { g in
                    ZStack(alignment: .leading) {
                        RoundedRectangle(cornerRadius: 3).fill(p.lcdCell)
                        RoundedRectangle(cornerRadius: 3).fill(p.lcdInk)
                            .frame(width: g.size.width * min(1, max(0, text.progress)))
                    }
                }
                .frame(height: 7)
                .padding(.top, 3)
                .accessibilityLabel("Progress")
                .accessibilityValue(Format.percent(text.progress))
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

extension PanelView {
    /// The color is light: black text reads better on it.
    static func light(_ hex: String?) -> Bool {
        guard var s = hex else { return false }
        if s.hasPrefix("#") { s.removeFirst() }
        guard s.count == 6, let v = UInt32(s, radix: 16) else { return false }
        let r = Double(v >> 16 & 0xff), g = Double(v >> 8 & 0xff), b = Double(v & 0xff)
        return (0.299 * r + 0.587 * g + 0.114 * b) / 255 > 0.6
    }
}

/// The loaded spools as little rolls in the middle of the panel (drawLoaded in
/// deck/internal/ui/statusbox.go): each roll in its color, as thick as the part that is left, with
/// the part left after it when the row has room. A ring marks the roll that feeds now, and a line
/// comes before the external spools.
struct RollsRow: View {
    let rolls: [PanelRoll]
    private let p = Palette.night

    var body: some View {
        ViewThatFits(in: .horizontal) {
            row(labels: true)
            row(labels: false)
        }
        .accessibilityElement()
        .accessibilityLabel(rolls.map { r in
            r.material + (r.remain.map { " " + Format.percent($0) } ?? "") + (r.active ? ", feeding" : "")
        }.joined(separator: ", "))
    }

    private func row(labels: Bool) -> some View {
        HStack(spacing: 6) {
            ForEach(Array(rolls.enumerated()), id: \.offset) { i, r in
                if r.external, i > 0, !rolls[i - 1].external {
                    Rectangle().fill(p.lcdDim).frame(width: 1, height: 12)
                }
                HStack(spacing: 2) {
                    RollIcon(roll: r)
                    if labels, let rem = r.remain {
                        Text(Format.percent(rem)).font(Theme.plex(10)).foregroundStyle(p.lcdDim).fixedSize()
                    }
                }
            }
        }
    }
}

/// A roll seen from the side: the flange, the filament in its color as thick as the part left,
/// and the hub. A ring marks the roll that feeds now.
private struct RollIcon: View {
    let roll: PanelRoll
    private let p = Palette.night
    private let d: CGFloat = 16

    var body: some View {
        let f = min(1, max(0, roll.remain ?? 1))
        let hub = d * 0.4
        ZStack {
            if roll.active { Circle().stroke(p.orange, lineWidth: 1.5).frame(width: d + 4, height: d + 4) }
            Circle().fill(p.lcdDim).frame(width: d, height: d)
            Circle().fill(p.lcdCell).frame(width: d - 2, height: d - 2)
            let size = hub + (d - 2 - hub) * f
            Circle().fill(p.lcdDim).frame(width: size, height: size)
            Circle().fill(roll.color.flatMap(Color.init(hex:)) ?? p.lcdDim).frame(width: size - 1.5, height: size - 1.5)
            Circle().fill(p.lcdDim).frame(width: hub, height: hub)
            Circle().fill(p.lcd).frame(width: hub - 3, height: hub - 3)
        }
        .frame(width: d + 4, height: d + 4)
    }
}

/// A readout of the panel: the name at the top, the value, and the target under it, as the deck
/// shows a narrow readout. A heater that is on has a bar of its power at the bottom.
struct ReadoutCell: View {
    let readout: PanelReadout
    /// A readout of two rows: a smaller value.
    var compact = false
    /// The row has a target: a line for it under the value.
    var targetLine = true
    private let p = Palette.night

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(readout.name)
                .font(Theme.plex(10))
                .foregroundStyle(p.lcdDim)
                .lineLimit(1)
                .minimumScaleFactor(0.7)
            Text(readout.value)
                .font(Theme.plex(compact ? 16 : 19))
                .foregroundStyle(p.lcdInk)
                .lineLimit(1)
                .minimumScaleFactor(0.6)
            if targetLine {
                Text(readout.target.isEmpty ? " " : readout.target)
                    .font(Theme.plex(11))
                    .foregroundStyle(p.lcdDim)
                    .lineLimit(1)
            }
        }
        .padding(.horizontal, 5)
        .padding(.top, compact ? 2 : 3)
        .padding(.bottom, compact ? 3 : 5)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 6).fill(p.lcdCell))
        .overlay(alignment: .bottom) {
            if readout.power > 0 {
                GeometryReader { g in
                    Rectangle().fill(p.orange).frame(width: (g.size.width - 8) * readout.power, height: 2)
                        .offset(x: 4)
                }
                .frame(height: 2)
                .padding(.bottom, 2)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// A part of the scroll under the panel: a name, and its rows of a name and a value.
struct PanelSection<Content: View>: View {
    let title: String
    @ViewBuilder let content: Content
    private let p = Palette.night

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title.uppercased())
                .font(Theme.plex(10))
                .foregroundStyle(p.lcdDim)
            content
        }
        .padding(8)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 8).fill(p.lcdCell))
    }
}

/// A row of a section: the name at the left, the value at the right.
struct PanelRow: View {
    let name: String
    var value = ""
    private let p = Palette.night

    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            Text(name).font(Theme.plex(12, bold: value.isEmpty)).lineLimit(2)
            Spacer(minLength: 4)
            Text(value).font(Theme.plex(12))
        }
        .foregroundStyle(p.lcdInk)
    }
}

/// A picture in dots, in amber on the dark window: the model of the print as the deck draws it.
struct DotPicture: View {
    let picture: LCDPicture
    private let p = Palette.night

    var body: some View {
        Canvas { ctx, size in
            let d = min(size.width / CGFloat(picture.width), size.height / CGFloat(picture.height))
            let ox = (size.width - d * CGFloat(picture.width)) / 2
            for y in 0..<picture.height {
                for x in 0..<picture.width where picture.bits[y * picture.width + x] {
                    ctx.fill(Path(CGRect(x: ox + CGFloat(x) * d, y: CGFloat(y) * d, width: d * 0.85, height: d * 0.85)),
                             with: .color(p.lcdInk))
                }
            }
        }
        .aspectRatio(CGFloat(picture.width) / CGFloat(max(1, picture.height)), contentMode: .fit)
        .accessibilityLabel("The model of the print")
    }
}

/// The menu of the knob over the scroll: the title, and the rows. The crown moves the lit row, and
/// a tap selects it.
struct PanelMenu: View {
    let title: String
    let items: [String]
    let selected: Int
    private let p = Palette.night

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title)
                .font(Theme.plex(15))
                .foregroundStyle(p.lcdInk)
                .padding(.horizontal, 6)
                .padding(.bottom, 4)
            ForEach(Array(items.enumerated()), id: \.offset) { i, label in
                let on = i == selected
                Text(label)
                    .font(Theme.plex(16))
                    .foregroundStyle(on ? p.lcd : p.lcdInk)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 7)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(RoundedRectangle(cornerRadius: 6).fill(on ? p.lcdInk : p.lcdCell))
                    .accessibilityAddTraits(on ? .isSelected : [])
            }
        }
    }
}

/// A bar of a part, 0 to 1, in amber on the color of a cell: the filament left on a roll.
struct PanelBar: View {
    let value: Double
    private let p = Palette.night

    var body: some View {
        GeometryReader { g in
            ZStack(alignment: .leading) {
                RoundedRectangle(cornerRadius: 3).fill(p.lcd)
                RoundedRectangle(cornerRadius: 3).fill(p.lcdInk).frame(width: g.size.width * min(1, max(0, value)))
            }
        }
        .frame(height: 6)
    }
}
