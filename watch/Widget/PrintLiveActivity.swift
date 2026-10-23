import ActivityKit
import SwiftUI
import WidgetKit

/// The Live Activity of a print, in the look of the display of the printer: the blue light, the
/// hotend in dots, and IBM Plex Mono. The time left and the bar are system timers, which move to the
/// end time between the readings of the app.
struct PrintActivityWidget: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: PrintActivityAttributes.self) { context in
            LockScreenView(file: context.attributes.file, state: context.state)
                .activityBackgroundTint(Color(rgb: 0x1f4bd1))
                .activitySystemActionForegroundColor(.white)
        } dynamicIsland: { context in
            let st = context.state
            return DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    Nozzle(size: 34).padding(.leading, 4)
                }
                DynamicIslandExpandedRegion(.trailing) {
                    TimeLeft(state: st, size: 26)
                }
                DynamicIslandExpandedRegion(.center) {
                    Text(context.attributes.file)
                        .font(Theme.plex(14))
                        .lineLimit(1)
                }
                DynamicIslandExpandedRegion(.bottom) {
                    VStack(spacing: 6) {
                        Bar(state: st)
                        Footer(state: st)
                    }
                }
            } compactLeading: {
                Nozzle(size: 18)
            } compactTrailing: {
                TimeLeft(state: st, size: 14)
                    .frame(maxWidth: 52)
            } minimal: {
                if let start = st.start, let end = st.end {
                    ProgressView(timerInterval: start...end, countsDown: false) { EmptyView() } currentValueLabel: { EmptyView() }
                        .progressViewStyle(.circular)
                        .tint(.white)
                } else {
                    Nozzle(size: 16)
                }
            }
            .keylineTint(Color(rgb: 0x1f4bd1))
        }
    }
}

/// The Lock Screen: the hotend and the file, the time left, the bar, and the end and the layer.
private struct LockScreenView: View {
    let file: String
    let state: PrintActivityAttributes.ContentState

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .center, spacing: 12) {
                Nozzle(size: 40)
                VStack(alignment: .leading, spacing: 2) {
                    Text(title)
                        .font(Theme.plex(12))
                        .foregroundStyle(.white.opacity(0.75))
                    Text(file)
                        .font(Theme.plex(16))
                        .lineLimit(1)
                }
                Spacer(minLength: 4)
                TimeLeft(state: state, size: 30)
            }
            Bar(state: state)
            Footer(state: state)
        }
        .foregroundStyle(.white)
        .padding(16)
    }

    private var title: String {
        if !state.finished.isEmpty { return state.finished.uppercased() }
        return state.paused ? "PAUSED" : "PRINTING"
    }
}

/// The hotend of the status screen of the deck, in dots.
private struct Nozzle: View {
    let size: CGFloat
    @Environment(\.displayScale) private var scale

    var body: some View {
        let dot = max(2, Int(size * scale / 18))
        var c = LCDCanvas(width: 18, height: 18, marginX: 1, marginY: 1)
        let _ = ComplicationLCD.hotend(&c, x: 0, y: 0)
        Image(decorative: ComplicationDots.image(c, dot: dot, fullColor: true), scale: scale)
            .interpolation(.none)
            .frame(width: CGFloat(18 * dot) / scale, height: CGFloat(18 * dot) / scale)
            .clipShape(RoundedRectangle(cornerRadius: 4))
    }
}

/// The time left: a timer to the end, or the state when there is no end.
private struct TimeLeft: View {
    let state: PrintActivityAttributes.ContentState
    let size: CGFloat

    var body: some View {
        Group {
            if !state.finished.isEmpty {
                Text(state.finished == "Done" ? "100%" : "--")
            } else if state.paused {
                Text("PAUSED")
            } else if let end = state.end {
                Text(timerInterval: Date()...end, countsDown: true)
            } else {
                Text("\(Int((state.progress * 100).rounded(.down)))%")
            }
        }
        .font(Theme.plex(size))
        .monospacedDigit()
        .multilineTextAlignment(.trailing)
        .foregroundStyle(.white)
    }
}

/// The bar of the progress: a system timer to the end, or the progress of the last reading.
private struct Bar: View {
    let state: PrintActivityAttributes.ContentState

    var body: some View {
        Group {
            if state.finished.isEmpty, !state.paused, let start = state.start, let end = state.end {
                ProgressView(timerInterval: start...end, countsDown: false) { EmptyView() } currentValueLabel: { EmptyView() }
            } else {
                ProgressView(value: state.progress)
            }
        }
        .progressViewStyle(.linear)
        .tint(.white)
    }
}

/// The end of the print on the clock and the layer.
private struct Footer: View {
    let state: PrintActivityAttributes.ContentState

    var body: some View {
        HStack {
            if let end = state.end, state.finished.isEmpty {
                Text("END " + LCDModel.clock(end))
            }
            Spacer()
            Text(state.layer)
        }
        .font(Theme.plex(12))
        .foregroundStyle(.white.opacity(0.8))
    }
}
