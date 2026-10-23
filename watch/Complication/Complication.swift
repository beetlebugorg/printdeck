import SwiftUI
import WidgetKit

/// The complications of the watch: the accessory widgets (Shared/DeckWidget.swift), and the corner
/// of the watch face as text with a gauge.
struct ComplicationView: View {
    @Environment(\.widgetFamily) private var family
    let entry: Entry

    var body: some View {
        if family == .accessoryCorner {
            corner
        } else {
            AccessoryView(entry: entry)
        }
    }

    private var corner: some View {
        let a = AccessoryView(entry: entry)
        return Text(a.busy ? a.percentNumber + "%" : "--")
            .font(Theme.digits(16))
            .widgetCurvesContent()
            .widgetLabel {
                if a.busy {
                    Gauge(value: entry.progress ?? 0) { Text(a.leftText) }
                        .tint(Theme.orange)
                } else {
                    Text("PRINTDECK")
                }
            }
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
