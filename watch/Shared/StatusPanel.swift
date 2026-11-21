// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import Foundation

/// A readout of the status panel: its name, the value, the target and the power of a heater.
struct PanelReadout: Equatable {
    var name: String
    var value: String
    var target = ""
    /// 0 to 1: the power of a heater that is on, or 0.
    var power = 0.0
}

/// A spool that the printer has loaded, as a little roll of the panel: its color, the part left,
/// and its material.
struct PanelRoll: Equatable {
    /// #rrggbb, or nil.
    var color: String?
    /// 0 to 1, or nil when it is not known: a full roll.
    var remain: Double?
    var material: String
    /// The roll feeds the nozzle now.
    var active = false
    /// The external spool, after a line.
    var external = false
}

/// The text of the status panel of the deck (panelLines in deck/internal/ui/statusbox.go): the
/// readouts, the state and the time at the right in the first line, the message in the second,
/// the details in the third, and the progress bar.
struct PanelText: Equatable {
    var readouts: [PanelReadout] = []
    /// The loaded spools, as the deck shows them in the middle of the panel.
    var rolls: [PanelRoll] = []
    var head = ""
    var side = ""
    /// A shorter side for a narrow panel, or "": the end with no "Ends".
    var short = ""
    var msg = ""
    var detail = ""
    var bar = false
    var progress = 0.0
}

/// The status panel for the state of the app, from the status of the deck.
enum StatusPanel {
    /// The panel. live is false for a status from an earlier read: the panel shows its age, and
    /// the heaters show no power. notice is the result of a command, for the message line.
    static func make(_ st: Status?, connected: Bool, error: String?, now: Date, live: Bool = true,
                     notice: String? = nil, resin: Bool = false) -> PanelText {
        var p = text(st, connected: connected, error: error, now: now, live: live, resin: resin)
        p.readouts = readouts(st, live: live, resin: resin, now: now)
        if !resin, live { p.rolls = rolls(st?.filaments ?? []) }
        if let notice, connected, st != nil { p.msg = notice }
        return p
    }

    private static func text(_ st: Status?, connected: Bool, error: String?, now: Date, live: Bool,
                             resin: Bool) -> PanelText {
        let clock = time(now)
        guard connected else {
            return PanelText(head: "Not connected", side: clock, msg: "Set up on the iPhone")
        }
        guard let st else {
            return PanelText(head: error == nil ? "Reading the deck" : "No connection", side: clock,
                             msg: error.map(short) ?? "")
        }
        if !live {
            return PanelText(head: error == nil ? "Reading the deck" : "No connection", side: clock,
                             msg: "Read " + Format.duration(max(0, now.timeIntervalSince(st.time))) + " ago",
                             detail: st.busy ? (st.file ?? "") : "")
        }
        let lcd = st.lcd
        guard st.busy else {
            // The state and the message of the status screen of the deck: "Print done" and the
            // name of the print, or "Ready" and "XYZ homed".
            var head = lcd?.state ?? ""
            if head.isEmpty { head = st.state == "Operational" ? "Ready" : st.state }
            return PanelText(head: head, side: clock, msg: lcd?.message ?? "", detail: lcd?.position ?? "")
        }
        let progress = st.progress(at: now) ?? 0
        let pct = Format.percent(progress)
        var p = PanelText(side: clock, bar: true, progress: progress)
        var left = ""
        if let l = st.left(at: now) {
            p.short = when(now.addingTimeInterval(l), now: now)
            p.side = "Ends " + p.short
            left = hm(l) + " left · "
        }
        p.msg = left + (st.elapsed.map(hm) ?? lcd?.elapsed ?? "0:00") + " so far"
        p.detail = layerDetail(st)
        if st.paused {
            p.head = "Paused · " + pct
            p.msg = resin ? "Resume on the deck" : "Tap to resume"
        } else if lcd?.cooling == true, let to = lcd?.cool_to {
            p.head = "Cooling the bed"
            p.side = lcd?.left ?? clock
            p.short = ""
            p.msg = String(format: "The part comes off at %.0f°", to)
            p.detail = st.file ?? ""
            p.progress = lcd?.progress ?? 0
        } else if st.layer == 1, (st.layers ?? 0) > 1 {
            p.head = "First layer · " + pct
        } else {
            p.head = "Printing · " + pct
        }
        return p
    }

    /// "Layer 12 of 27 · next in 1:32 · Z 3.20", as far as the deck tells it.
    private static func layerDetail(_ st: Status) -> String {
        var parts: [String] = []
        if let l = st.layer, let n = st.layers { parts.append("Layer \(l) of \(n)") }
        if let next = st.lcd?.next, !next.isEmpty, !parts.isEmpty { parts.append("next in " + next) }
        if let z = st.lcd?.z, !z.isEmpty { parts.append("Z " + z) }
        return parts.joined(separator: " · ")
    }

    /// The readouts: the nozzle, the bed and the fan, or the plate, the time and the time left of
    /// a resin printer.
    private static func readouts(_ st: Status?, live: Bool, resin: Bool, now: Date) -> [PanelReadout] {
        guard let st, live else {
            return (resin ? ["PLATE MM", "TIME", "LEFT"] : ["NOZZLE", "BED", "FAN"]).map { PanelReadout(name: $0, value: "--") }
        }
        let lcd = st.lcd
        if resin {
            return [PanelReadout(name: "PLATE MM", value: lcd?.z ?? "--"),
                    PanelReadout(name: "TIME", value: st.busy ? st.elapsed.map(hm) ?? lcd?.elapsed ?? "--" : "--"),
                    PanelReadout(name: "LEFT", value: st.busy ? st.left(at: now).map(hm) ?? "--" : "--")]
        }
        func heater(_ name: String, _ t: Double?, _ target: Double?, _ power: Double?) -> PanelReadout {
            let target = target ?? 0
            return PanelReadout(name: name, value: t.map { String(format: "%.0f°", $0) } ?? "--",
                                target: target > 0 ? String(format: "→%.0f", target) : "off",
                                power: target > 0 ? min(1, max(0, power ?? 0)) : 0)
        }
        let fan = lcd?.fan ?? 0
        // A printer with two nozzles has the aux nozzle after the nozzle, and a printer with a
        // chamber has it between the bed and the fan, as the panel of the deck.
        var out = [heater("NOZZLE", lcd?.tool ?? st.tool?.actual, lcd?.tool_target ?? st.tool?.target, lcd?.tool_power)]
        if let t = st.tool1 {
            out.append(heater("AUX", t.actual, t.target, nil))
        } else if st.nozzles?.contains(where: { $0.role == "aux" }) == true {
            out.append(PanelReadout(name: "AUX", value: "--"))
        }
        out.append(heater("BED", lcd?.bed ?? st.bed?.actual, lcd?.bed_target ?? st.bed?.target, lcd?.bed_power))
        if let c = st.chamber {
            out.append(PanelReadout(name: "CHAMBER", value: String(format: "%.0f°", c.actual),
                                    target: c.target > 0 ? String(format: "→%.0f", c.target) : ""))
        }
        out.append(PanelReadout(name: "FAN", value: fan > 0 ? String(format: "%.0f%%", fan * 100) : "off"))
        return out
    }

    /// The loaded spools as rolls: the trays in their order, then the external spools.
    static func rolls(_ list: [Filament]) -> [PanelRoll] {
        list.map { PanelRoll(color: $0.color, remain: $0.remain, material: $0.material, active: $0.active == true,
                             external: $0.slot == "Ext") }
    }

    /// The clock time, as the deck writes it: "3:47 PM", or "15:47" on a 24-hour clock.
    static func time(_ t: Date) -> String {
        t.formatted(date: .omitted, time: .shortened)
    }

    /// The clock time of t, with its day when t is not today: "3:47 PM", "Tomorrow 3:47 PM", or
    /// "Sat 3:47 PM" (whenText in deck/internal/ui/app.go).
    static func when(_ t: Date, now: Date) -> String {
        let cal = Calendar.current
        if cal.isDate(t, inSameDayAs: now) { return time(t) }
        if let tomorrow = cal.date(byAdding: .day, value: 1, to: now), cal.isDate(t, inSameDayAs: tomorrow) {
            return "Tomorrow " + time(t)
        }
        return t.formatted(.dateTime.weekday(.abbreviated)) + " " + time(t)
    }

    /// Hours and minutes: "1:23".
    static func hm(_ seconds: Double) -> String {
        // Int has 32 bits on the watch: a wrong estimate of the deck must not trap.
        let m = Int(min(max(seconds, 0), 1e8) / 60)
        return String(format: "%d:%02d", m / 60, m % 60)
    }

    private static func short(_ error: String) -> String {
        error.hasPrefix("No connection") ? "No connection to the deck" : error
    }
}
