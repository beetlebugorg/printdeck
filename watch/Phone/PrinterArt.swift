// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

/// The picture of a printer with no camera picture and no print: its color, its label or name, its
/// model, and its nozzle and bed. A printer of a deck with one printer has the accent color.
struct PrinterArt: View {
    @Environment(DeckModel.self) private var model
    /// A large picture for the screen of the printer, or a card.
    var large = false

    var body: some View {
        let d = model.deck
        let base = d.color.flatMap(Color.init(hex:)) ?? Color.accentColor
        let light = LabelChip.light(d.color)
        let ink: Color = light && d.color != nil ? .black : .white
        ZStack(alignment: .bottomLeading) {
            LinearGradient(colors: [base, base.opacity(0.72)], startPoint: .topLeading, endPoint: .bottomTrailing)
            // The kind of the printer, large and faint at the right.
            KindIcon(process: d.process)
                .font(.system(size: large ? 120 : 96, weight: .regular))
                .foregroundStyle(ink.opacity(0.14))
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .trailing)
                .padding(.trailing, 18)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 4) {
                Text(d.label ?? initials(d.title))
                    .font(.system(size: large ? 52 : 44, weight: .bold, design: .rounded))
                    .lineLimit(1)
                    .minimumScaleFactor(0.5)
                Text(d.model ?? (d.process == .resin ? "Resin printer" : "FDM printer"))
                    .font(.subheadline.weight(.semibold))
                    .lineLimit(1)
                if let line = heaters {
                    Text(line)
                        .font(.footnote.monospacedDigit())
                        .opacity(0.85)
                }
            }
            .foregroundStyle(ink)
            .padding(16)
        }
        .accessibilityElement(children: .combine)
    }

    /// "Nozzle 24° · Bed 23°", or nil.
    private var heaters: String? {
        guard model.deck.process != .resin, model.live, let st = model.status else { return nil }
        let nozzle = st.lcd?.tool ?? st.tool?.actual, bed = st.lcd?.bed ?? st.bed?.actual
        guard let nozzle, let bed else { return nil }
        return "Nozzle \(Int(nozzle.rounded()))° · Bed \(Int(bed.rounded()))°"
    }

    /// The first letters of the words of a name: "TW" for "TAZ Workhorse".
    private func initials(_ name: String) -> String {
        let words = name.split(separator: " ").prefix(3)
        let out = words.compactMap(\.first).map(String.init).joined().uppercased()
        return out.isEmpty ? "?" : out
    }
}
