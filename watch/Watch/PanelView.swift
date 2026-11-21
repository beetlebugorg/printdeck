// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

/// The status panel of the deck (drawStatusBox in deck/internal/ui/statusbox.go), on the dark
/// window of the watch: three readouts, the state and the time, the message, the details, and the
/// progress bar, in the amber of the deck and IBM Plex Mono.
struct PanelView: View {
    let text: PanelText
    private let p = Palette.night

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack(spacing: 4) {
                ForEach(text.readouts, id: \.name) { ReadoutCell(readout: $0) }
            }
            .padding(.bottom, 3)
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(text.head)
                    .font(Theme.plex(15))
                    .lineLimit(1)
                    .minimumScaleFactor(0.6)
                Spacer(minLength: 0)
                // The deck drops "Ends" when the line has no room for it.
                ViewThatFits(in: .horizontal) {
                    Text(text.side)
                    if !text.short.isEmpty { Text(text.short) }
                }
                .font(Theme.plex(11))
                .lineLimit(1)
            }
            .foregroundStyle(p.lcdInk)
            if !text.msg.isEmpty {
                Text(text.msg)
                    .font(Theme.plex(12, bold: false))
                    .foregroundStyle(p.lcdInk)
                    .lineLimit(2)
                    .minimumScaleFactor(0.8)
            }
            if !text.detail.isEmpty {
                Text(text.detail)
                    .font(Theme.plex(11, bold: false))
                    .foregroundStyle(p.lcdDim)
                    .lineLimit(2)
                    .minimumScaleFactor(0.8)
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

/// A readout of the panel: the name at the top, the value, and the target under it, as the deck
/// shows a narrow readout. A heater that is on has a bar of its power at the bottom.
struct ReadoutCell: View {
    let readout: PanelReadout
    private let p = Palette.night

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(readout.name)
                .font(Theme.plex(9))
                .foregroundStyle(p.lcdDim)
                .lineLimit(1)
                .minimumScaleFactor(0.7)
            Text(readout.value)
                .font(Theme.plex(19))
                .foregroundStyle(p.lcdInk)
                .lineLimit(1)
                .minimumScaleFactor(0.6)
            Text(readout.target.isEmpty ? " " : readout.target)
                .font(Theme.plex(10))
                .foregroundStyle(p.lcdDim)
                .lineLimit(1)
        }
        .padding(.horizontal, 5)
        .padding(.top, 3)
        .padding(.bottom, 5)
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
