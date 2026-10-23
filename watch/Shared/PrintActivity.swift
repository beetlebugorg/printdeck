#if os(iOS)
import ActivityKit
import Foundation

/// The Live Activity of a print: on the Lock Screen and in the Dynamic Island. The app starts it
/// when it sees a print, and updates and ends it from its readings of the deck. Between the
/// readings, the system timers move the time left and the bar to the end time.
struct PrintActivityAttributes: ActivityAttributes {
    struct ContentState: Codable, Hashable {
        /// The progress from 0 to 1 at the reading.
        var progress: Double
        /// The time of the progress 0, on the line from the reading to the end, for the bar of the
        /// system timer. nil when the deck has no end time.
        var start: Date?
        /// The end time of the print, or nil.
        var end: Date?
        var paused: Bool
        /// "L12/27", or "".
        var layer: String
        /// The state at the end of the print: "Done" or "Stopped", or "".
        var finished: String
        /// The model of the print in dots, packed (LCDPicture.packed), modelSide x modelSide, or
        /// nil.
        var model: Data?
        var modelSide = 0

        var modelPicture: LCDPicture? {
            model.flatMap { LCDPicture(packed: $0, width: modelSide, height: modelSide) }
        }
    }

    /// The file of the print.
    var file: String
}

extension PrintActivityAttributes.ContentState {
    /// The state of an activity for a status of the deck.
    init(_ st: Status, model pic: LCDPicture?, now: Date = Date()) {
        let p = min(1, max(0, st.progress ?? 0))
        progress = p
        paused = st.paused
        if let l = st.layer, let n = st.layers { layer = "L\(l)/\(n)" } else { layer = "" }
        finished = ""
        if let pic, pic.width == pic.height {
            model = pic.packed
            modelSide = pic.width
        }
        if !st.paused, let left = st.left(at: now), left > 0, p < 1 {
            end = now.addingTimeInterval(left)
            // The line through the progress now and 1 at the end gives the time of 0.
            start = now.addingTimeInterval(-left * p / (1 - p))
        } else {
            end = nil
            start = nil
        }
    }
}
#endif
