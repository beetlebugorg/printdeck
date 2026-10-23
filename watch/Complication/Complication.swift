import SwiftUI
import WidgetKit

struct Entry: TimelineEntry {
    let date: Date
    /// The status of the deck, or nil when the app has none.
    let status: Status?

    var progress: Double? { status?.progress(at: date) }
    var left: Double? { status?.left(at: date) }
}

struct Provider: TimelineProvider {
    private static let sample = Status(state: "Printing", busy: true, paused: false, file: "benchy.gcode",
                                       progress: 0.42, left: 4980, layer: 12, layers: 27, time: Date())

    func placeholder(in context: Context) -> Entry { Entry(date: Date(), status: Self.sample) }

    func getSnapshot(in context: Context, completion: @escaping (Entry) -> Void) {
        completion(Entry(date: Date(), status: context.isPreview ? Self.sample : Store.lastStatus))
    }

    /// Reads the deck, and makes an entry every 5 minutes for the next 30 minutes, with the
    /// progress of the print at that time. When the deck does not answer, the last status that
    /// the app or the complication read goes in the entries, for up to 2 hours.
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

/// The complications as the dot-matrix display of the printer. On a face with tint the lit dots
/// take the tint and the dots that are off are faint; on a face in full color the display has its
/// blue light.
struct ComplicationView: View {
    @Environment(\.widgetFamily) private var family
    @Environment(\.widgetRenderingMode) private var mode
    @Environment(\.displayScale) private var scale
    let entry: Entry

    var body: some View {
        switch family {
        case .accessoryCircular: dots(circle: true)
        case .accessoryCorner: corner
        case .accessoryInline: inline
        default: dots(circle: false)
        }
    }

    private var busy: Bool { entry.status?.busy == true }
    private var progress: Double { busy ? entry.progress ?? 0 : 0 }

    private var percentNumber: String {
        guard busy, let p = entry.progress else { return "--" }
        return "\(Int((p * 100).rounded(.down)))"
    }

    private var leftText: String {
        if entry.status?.paused == true { return "PAUSED" }
        guard busy, let left = entry.left else { return "--" }
        return Format.shortDuration(left)
    }

    // MARK: The dots

    /// The display: the rectangle, or the round display of the circular complication.
    private func dots(circle: Bool) -> some View {
        GeometryReader { g in
            let px = Int(g.size.width * scale)
            // Dots of 2 pixels or more: 84 across the rectangle, 31 across the circle.
            let dot = max(2, px / (circle ? 31 : 84))
            let cols = Int(g.size.width * scale) / dot, rows = Int(g.size.height * scale) / dot
            var c = LCDCanvas(width: cols, height: rows, marginX: circle ? 0 : 1, marginY: circle ? 0 : 1)
            let lcd = ComplicationLCD(status: entry.status, date: entry.date)
            let _ = circle ? lcd.drawCircle(&c) : lcd.drawRect(&c)
            Image(decorative: ComplicationDots.image(c, dot: dot, fullColor: mode == .fullColor), scale: scale)
                .interpolation(.none)
                .widgetAccentable()
                .frame(width: g.size.width, height: g.size.height)
        }
    }

    // MARK: Text

    private var corner: some View {
        Text(busy ? percentNumber + "%" : "--")
            .font(Theme.digits(16))
            .widgetCurvesContent()
            .widgetLabel {
                if busy {
                    Gauge(value: progress) { Text(leftText) }
                        .tint(Theme.orange)
                } else {
                    Text("PRINTDECK")
                }
            }
    }

    private var inline: some View {
        Text(busy ? "\(percentNumber)%  \(leftText)" : "Printdeck --")
    }
}

@main
struct PrintdeckComplication: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "PrintdeckProgress", provider: Provider()) { entry in
            ComplicationView(entry: entry)
                .containerBackground(Color(rgb: 0x1f4bd1), for: .widget)
        }
        .configurationDisplayName("Print")
        .description("The progress and the time left of the print.")
        .supportedFamilies([.accessoryCircular, .accessoryRectangular, .accessoryCorner, .accessoryInline])
    }
}
