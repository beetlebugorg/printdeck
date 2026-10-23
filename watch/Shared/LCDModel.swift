import Foundation

/// The screens of the printer display for the state of the app, as the deck makes them
/// (lcdStatusScreen and StatusLines in deck/internal/ui).
enum LCDModel {
    /// The time for the display: "2:24PM" on a 12-hour clock, "14:24" on a 24-hour clock.
    static func clock(_ date: Date) -> String {
        let f = DateFormatter()
        let h12 = (DateFormatter.dateFormat(fromTemplate: "j", options: 0, locale: .current) ?? "").contains("a")
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = h12 ? "h:mma" : "HH:mm"
        return f.string(from: date)
    }

    /// Hours and minutes as the display shows them: "1:23".
    static func hm(_ seconds: Double) -> String {
        let m = Int(seconds / 60)
        return String(format: "%d:%02d", m / 60, m % 60)
    }

    static func status(_ st: Status?, connected: Bool, error: String?, now: Date, hint: String) -> LCDStatus {
        var s = LCDStatus(clock: clock(now), hint: hint)
        guard connected else {
            s.state = "Not connected"
            s.message = "Set up on the iPhone"
            s.hint = ""
            return s
        }
        guard let st else {
            s.state = error == nil ? "Reading the deck" : "No connection"
            s.message = error.map(short) ?? ""
            return s
        }
        if let l = st.lcd {
            // The screen of the deck: the same fields as its touch screen.
            s.state = l.state ?? ""
            s.clock = l.clock ?? s.clock
            s.message = l.message ?? ""
            s.tool = l.tool ?? 0; s.toolT = l.tool_target ?? 0
            s.bed = l.bed ?? 0; s.bedT = l.bed_target ?? 0
            s.toolPow = l.tool_power ?? 0; s.bedPow = l.bed_power ?? 0
            s.fan = l.fan ?? 0
            s.progress = l.progress ?? 0
            s.printing = l.printing ?? false
            s.elapsed = l.elapsed ?? ""
            s.left = l.left ?? ""
            s.done = l.done ?? ""
            if let e = error { s.message = short(e) }
            return s
        }
        s.tool = st.tool?.actual ?? 0
        s.toolT = st.tool?.target ?? 0
        s.bed = st.bed?.actual ?? 0
        s.bedT = st.bed?.target ?? 0
        if st.busy {
            s.printing = true
            s.progress = st.progress ?? 0
            s.elapsed = st.elapsed.map(hm) ?? ""
            if let left = st.left(at: now) {
                s.left = hm(left)
                s.done = clock(now.addingTimeInterval(left))
            }
            return s
        }
        s.state = st.state == "Operational" ? "Ready" : st.state
        if let error {
            s.message = short(error)
        } else if let r = st.roll {
            s.message = "\(r.name) \(Int(r.left.rounded()))g"
        }
        return s
    }

    /// The rows of the print info screen.
    static func info(_ st: Status?) -> [(String, String)] {
        guard let st else { return [("No status", "")] }
        var rows: [(String, String)] = []
        if st.busy {
            rows.append((st.file ?? "Print", ""))
            rows.append((st.paused ? "Paused" : "Printing", Format.percent(st.progress ?? 0)))
            if let l = st.layer, let n = st.layers { rows.append(("Layer", "\(l)/\(n)")) }
        } else {
            rows.append((st.state == "Operational" ? "Ready" : st.state, ""))
        }
        rows.append(("Nozzle", Format.temp(st.tool)))
        rows.append(("Bed", Format.temp(st.bed)))
        if let r = st.roll {
            // The screen has 6 rows: the roll is one.
            rows.append((r.material + " " + r.name, "\(Int(r.left.rounded()))g"))
        }
        return rows
    }

    private static func short(_ error: String) -> String {
        if error.hasPrefix("No connection") { return "No connection to deck" }
        return error
    }
}
