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

struct ComplicationView: View {
    @Environment(\.widgetFamily) private var family
    let entry: Entry

    var body: some View {
        switch family {
        case .accessoryCircular: circular
        case .accessoryCorner: corner
        case .accessoryInline: inline
        default: rectangular
        }
    }

    private var busy: Bool { entry.status?.busy == true }

    private var percentNumber: String {
        guard busy, let p = entry.progress else { return "--" }
        return "\(Int((p * 100).rounded(.down)))"
    }

    private var leftText: String {
        if entry.status?.paused == true { return "PAUSED" }
        guard busy, let left = entry.left else { return "--" }
        return Format.shortDuration(left)
    }

    private var circular: some View {
        Gauge(value: busy ? entry.progress ?? 0 : 0) {
            Text("%")
        } currentValueLabel: {
            Text(percentNumber)
                .font(Theme.digits(16))
        }
        .gaugeStyle(.accessoryCircularCapacity)
        .tint(Theme.orange)
        .widgetAccentable()
    }

    private var corner: some View {
        Text(busy ? percentNumber + "%" : "--")
            .font(Theme.digits(16))
            .widgetCurvesContent()
            .widgetLabel {
                if busy {
                    Gauge(value: entry.progress ?? 0) { Text(leftText) }
                        .tint(Theme.orange)
                } else {
                    Text("PRINTDECK")
                }
            }
    }

    private var inline: some View {
        Text(busy ? "\(percentNumber)%  \(leftText)" : "Printdeck --")
    }

    private var rectangular: some View {
        VStack(alignment: .leading, spacing: 1) {
            HStack {
                Text(entry.status?.state.uppercased() ?? "PRINTDECK")
                    .font(Theme.label(12))
                    .foregroundStyle(Theme.orange)
                    .widgetAccentable()
                Spacer()
                if busy {
                    Text(percentNumber + "%")
                        .font(Theme.digits(15))
                }
            }
            if busy, let st = entry.status {
                Text(st.file ?? "")
                    .font(.system(size: 13))
                    .lineLimit(1)
                Gauge(value: entry.progress ?? 0) { EmptyView() }
                    .gaugeStyle(.accessoryLinearCapacity)
                    .tint(Theme.orange)
                HStack {
                    Text(st.paused ? "PAUSED" : "LEFT " + leftText)
                    Spacer()
                    if !st.paused, let end = st.end {
                        Text("END " + end.formatted(date: .omitted, time: .shortened))
                    }
                }
                .font(Theme.label(11))
            } else {
                Text(entry.status == nil ? "No status" : "No print")
                    .font(.system(size: 13))
            }
        }
    }
}

@main
struct PrintdeckComplication: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "PrintdeckProgress", provider: Provider()) { entry in
            ComplicationView(entry: entry)
                .containerBackground(Color.black, for: .widget)
        }
        .configurationDisplayName("Print")
        .description("The progress and the time left of the print.")
        .supportedFamilies([.accessoryCircular, .accessoryRectangular, .accessoryCorner, .accessoryInline])
    }
}
