import SwiftUI
import WidgetKit

/// The widgets of the iPhone. On the Home Screen: the small widget is the status screen of the
/// watch (the progress first), and the medium widget is the 128 x 64 status screen of the deck. On
/// the Lock Screen: the rectangle, the circle and the inline text of the watch complications.
struct WidgetView: View {
    @Environment(\.widgetFamily) private var family
    let entry: Entry

    var body: some View {
        switch family {
        case .systemSmall:
            // 64 dots across the status screen, in a margin of 4.
            DotsView(dotsAcross: 72, marginX: 4, marginY: 4) { c in
                status.drawPortrait(&c, frame: 0)
            }
        case .systemMedium:
            // The screen of the deck: 128 x 64 dots and its margins.
            DotsView(dotsAcross: 136, marginX: 6, marginY: 4, dotsDown: 72) { c in
                status.draw(&c, frame: 0)
            }
        default:
            AccessoryView(entry: entry)
        }
    }

    private var status: LCDStatus {
        LCDModel.status(entry.status, connected: Store.settings.isComplete, error: nil, now: entry.date, hint: "")
    }
}

@main
struct PrintdeckWidgets: WidgetBundle {
    var body: some Widget {
        PrintdeckWidget()
        PrintActivityWidget()
    }
}

struct PrintdeckWidget: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: "PrintdeckProgress", provider: Provider()) { entry in
            WidgetView(entry: entry)
                .containerBackground(Color(rgb: 0x1f4bd1), for: .widget)
        }
        .configurationDisplayName("Print")
        .description("The progress of the print, on the display of the printer.")
        .supportedFamilies([.systemSmall, .systemMedium, .accessoryRectangular, .accessoryCircular, .accessoryInline])
        .contentMarginsDisabled()
    }
}
