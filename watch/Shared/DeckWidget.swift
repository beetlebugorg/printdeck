// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI
import WidgetKit

// The timeline and the views of the widgets of the iPhone and the complications of the watch.

struct Entry: TimelineEntry {
    let date: Date
    /// The status of the deck, or nil when the app has none.
    let status: Status?

    var progress: Double? { status?.progress(at: date) }
    var left: Double? { status?.left(at: date) }
}

struct Provider: TimelineProvider {
    static let sample: Status = {
        var s = Status(state: "Printing", busy: true, paused: false, file: "benchy.gcode",
                       progress: 0.42, left: 4980, layer: 12, layers: 27,
                       tool: Temp(actual: 215, target: 215), bed: Temp(actual: 60, target: 60), time: Date())
        s.roll = Roll(name: "Galaxy Black", material: "PLA", left: 642, full: 1000, color: "#2b2d42")
        return s
    }()

    func placeholder(in context: Context) -> Entry { Entry(date: Date(), status: Self.sample) }

    func getSnapshot(in context: Context, completion: @escaping (Entry) -> Void) {
        completion(Entry(date: Date(), status: context.isPreview ? Self.sample : Store.lastStatus))
    }

    /// Reads the deck, and makes an entry every 5 minutes for the next 30 minutes, with the
    /// progress of the print at that time. When the deck does not answer, the last status that
    /// the app or the widget read goes in the entries, for up to 2 hours.
    func getTimeline(in context: Context, completion: @escaping (Timeline<Entry>) -> Void) {
        Task {
            var status = Store.lastStatus
            if let fresh = try? await DeckClient(settings: Store.settings).status() {
                status = fresh
                Store.lastStatus = fresh
            }
            let now = Date()
            if let s = status, now.timeIntervalSince(s.time) > 2 * 3600 {
                status = nil
            }
            var entries = [Entry(date: now, status: status)]
            if status?.busy == true && status?.paused == false {
                for m in stride(from: 5, through: 30, by: 5) {
                    entries.append(Entry(date: now.addingTimeInterval(Double(m) * 60), status: status))
                }
            }
            let next = now.addingTimeInterval(status?.busy == true ? 15 * 60 : 30 * 60)
            completion(Timeline(entries: entries, policy: .after(next)))
        }
    }
}

/// A widget as dots of the printer display: the dots fill the view, dotsAcross of them across.
/// In full color the display has its blue light; with tint the lit dots take the tint and the dots
/// that are off are faint.
struct DotsView: View {
    @Environment(\.widgetRenderingMode) private var mode
    @Environment(\.displayScale) private var scale
    /// The dots across the view: the size of the dots.
    let dotsAcross: Int
    var marginX = 1, marginY = 1
    /// Fits the dots in the height too: dotsDown of them at least.
    var dotsDown = 0
    let draw: (inout LCDCanvas) -> Void

    var body: some View {
        GeometryReader { g in
            let wpx = Int(g.size.width * scale), hpx = Int(g.size.height * scale)
            let dot = max(2, min(wpx / dotsAcross, dotsDown > 0 ? hpx / dotsDown : Int.max))
            var c = LCDCanvas(width: wpx / dot, height: hpx / dot, marginX: marginX, marginY: marginY)
            let _ = draw(&c)
            Image(decorative: ComplicationDots.image(c, dot: dot, fullColor: mode == .fullColor), scale: scale)
                .interpolation(.none)
                .widgetAccentable()
                .frame(width: g.size.width, height: g.size.height)
        }
    }
}

/// The accessory widgets of the Lock Screen and the watch face: the rectangle and the circle as
/// dots, and the inline text.
struct AccessoryView: View {
    @Environment(\.widgetFamily) private var family
    let entry: Entry

    var body: some View {
        let lcd = ComplicationLCD(status: entry.status, date: entry.date)
        switch family {
        case .accessoryCircular:
            // 31 dots across the circle.
            DotsView(dotsAcross: 31, marginX: 0, marginY: 0) { lcd.drawCircle(&$0) }
        case .accessoryInline:
            Text(inlineText)
        default:
            // 84 dots across the rectangle.
            DotsView(dotsAcross: 84) { lcd.drawRect(&$0) }
        }
    }

    var busy: Bool { entry.status?.busy == true }

    var percentNumber: String {
        guard busy, let p = entry.progress else { return "--" }
        return "\(Int((p * 100).rounded(.down)))"
    }

    var leftText: String {
        if entry.status?.paused == true { return "PAUSED" }
        guard busy, let left = entry.left else { return "--" }
        return Format.shortDuration(left)
    }

    private var inlineText: String { busy ? "\(percentNumber)%  \(leftText)" : "Printdeck --" }
}
