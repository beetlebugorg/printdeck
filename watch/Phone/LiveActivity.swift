import ActivityKit
import Foundation

/// Starts, updates and ends the Live Activity of a print from the readings of the deck.
@MainActor
enum PrintLiveActivity {
    private static var current: Activity<PrintActivityAttributes>? {
        Activity<PrintActivityAttributes>.activities.first
    }

    /// The state of the last update, so that a reading that changes nothing sends nothing.
    private static var last: PrintActivityAttributes.ContentState?

    static func update(_ st: Status?) {
        guard ActivityAuthorizationInfo().areActivitiesEnabled, let st else { return }
        guard st.busy else {
            end()
            return
        }
        let state = PrintActivityAttributes.ContentState(st)
        // The stale time: a while after the end, when the app has not read the deck since.
        let stale = (state.end ?? Date().addingTimeInterval(3600)).addingTimeInterval(15 * 60)
        let content = ActivityContent(state: state, staleDate: stale)
        if let a = current, a.attributes.file == (st.file ?? "") {
            guard changed(state) else { return }
            last = state
            Task { await a.update(content) }
        } else {
            if let a = current { Task { await a.end(nil, dismissalPolicy: .immediate) } }
            last = state
            _ = try? Activity.request(attributes: PrintActivityAttributes(file: st.file ?? "Print"),
                                      content: content, pushType: nil)
        }
    }

    /// Ends the activity when the deck has no print: done, or stopped before its end. The deck
    /// says "Operational" after both, so the last progress tells them apart. It stays on the Lock
    /// Screen for a while.
    private static func end() {
        guard let a = current else { return }
        var state = last ?? a.content.state
        let projected = state.end.map { $0 <= Date().addingTimeInterval(120) } ?? false
        state.finished = state.progress >= 0.97 || projected ? "Done" : "Stopped"
        if state.finished == "Done" { state.progress = 1 }
        state.start = nil
        state.end = nil
        last = nil
        Task { await a.end(ActivityContent(state: state, staleDate: nil), dismissalPolicy: .default) }
    }

    /// A new state is worth an update: the pause, the layer, or the end time moved a minute.
    private static func changed(_ s: PrintActivityAttributes.ContentState) -> Bool {
        guard let l = last else { return true }
        if s.paused != l.paused || s.layer != l.layer { return true }
        switch (s.end, l.end) {
        case let (a?, b?): return abs(a.timeIntervalSince(b)) > 60
        case (nil, nil): return abs(s.progress - l.progress) > 0.01
        default: return true
        }
    }
}
