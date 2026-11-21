// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import Foundation

/// The controls of a printer that the iPhone shows, from its kind and its state. The rules follow
/// the deck: only a press on the touch screen of a resin deck starts or resumes a print, because
/// both turn on the UV light. A deck that lists its controls (ControlState) has a route for each,
/// and the app shows only those: the jog and the filament wait for a pause, and the home for the
/// end of a print. A deck from before the list has the moves between prints, and the fan and the
/// Z offset as G-code, which needs a Klipper printer and Signed prints only off.
struct Controls: Equatable {
    var process: DeckProcess?
    /// The last read of the printer worked.
    var connected: Bool
    /// A print runs or is paused.
    var busy: Bool
    var paused: Bool
    /// The printer is a Bambu Lab printer: it has nozzles in the status, or a Bambu model.
    var bambu: Bool
    var signedOnly: Bool
    /// The names of the controls of the deck, or nil from a deck before the list.
    var list: [String]?

    init(deck: SavedDeck, status: Status?, live: Bool) {
        process = status?.process ?? deck.process
        connected = live && status != nil
        busy = status?.busy ?? false
        paused = status?.paused ?? false
        bambu = status?.nozzles?.isEmpty == false || deck.model?.hasPrefix("Bambu") == true
        signedOnly = status?.signedOnly ?? false
        list = status?.controls?.controls
    }

    init(process: DeckProcess?, connected: Bool = true, busy: Bool = false, paused: Bool = false,
         bambu: Bool = false, signedOnly: Bool = false, list: [String]? = nil) {
        self.process = process
        self.connected = connected
        self.busy = busy
        self.paused = paused
        self.bambu = bambu
        self.signedOnly = signedOnly
        self.list = list
    }

    private var fdm: Bool { process == .fdm }

    /// The deck lists the control.
    func has(_ name: String) -> Bool { list?.contains(name) == true }

    /// A print runs and is not paused.
    private var running: Bool { busy && !paused }

    /// Print a file: an FDM printer with no print.
    var prints: Bool { fdm && !busy }
    var pauses: Bool { busy && !paused }
    /// Resume: an FDM printer only.
    var resumes: Bool { fdm && paused }
    var cancels: Bool { busy }
    /// The targets of the nozzle and the bed.
    var heats: Bool { fdm && connected }
    /// The jog: between prints and during a pause.
    var jogs: Bool { list == nil ? fdm && connected && !busy : has("jog") && connected && !running }
    /// The home: between prints.
    var homes: Bool { list == nil ? fdm && connected && !busy : has("home") && connected && !busy }
    /// The Move page: a jog or a home.
    var moves: Bool { jogs || homes }
    /// The part fan: by its route, or as G-code from a deck before the list.
    var fan: Bool { list == nil ? fdm && connected && !bambu && !signedOnly : has("fan") && connected }
    /// The Z offset of a running print: by its route, or as G-code from a deck before the list.
    var tunes: Bool { list == nil ? fdm && connected && busy && !bambu && !signedOnly : has("zoffset") && connected && busy }
    /// The speed and the flow of a print.
    var speed: Bool { has("speed") && connected && busy }
    var flow: Bool { has("flow") && connected && busy }
    /// Extrude and retract, and the load and the unload: between prints and during a pause.
    var extrudes: Bool { has("extrude") && connected && !running }
    var loads: Bool { has("filament") && connected && !running }
    /// The load and the unload of an AMS tray, and the drying of an AMS: between prints. The deck
    /// refuses them during a print, also a paused one (Bambu.LoadTray, UnloadTray and DryAMS).
    var loadsTray: Bool { has("filament") && connected && !busy }
    var dries: Bool { has("ams_dry") && connected && !busy }
    /// Stop drying: the deck stops a drying at any time.
    var stopsDrying: Bool { has("ams_dry") && connected }
    var chamber: Bool { has("chamber") && connected }
    var aux: Bool { has("tool1") && connected }
}

/// The filaments of the swap of the deck, with the temperature of the nozzle for each
/// (deck/internal/ui/printnav.go). A Klipper printer loads and unloads with a hot nozzle.
struct SwapMaterial: Hashable {
    var name: String
    var nozzle: Int

    static let all = [SwapMaterial(name: "PLA", nozzle: 210), SwapMaterial(name: "PETG", nozzle: 240),
                      SwapMaterial(name: "ABS", nozzle: 250), SwapMaterial(name: "TPU", nozzle: 225)]

    /// The material of the name, or PLA.
    static func of(_ material: String?) -> SwapMaterial {
        all.first { $0.name.caseInsensitiveCompare(material ?? "") == .orderedSame } ?? all[0]
    }

    /// The nozzle is hot enough at 3 °C under the target, as the swap of the deck has it.
    static func hot(_ actual: Double, for target: Int) -> Bool { actual >= Double(target) - 3 }
}

/// The drying of the AMS for each filament, as the deck has it (dryPresets in
/// deck/internal/ui/ams.go).
struct DryPreset: Hashable {
    var material: String
    var temp: Int
    var hours: Int

    static let all = [DryPreset(material: "PLA", temp: 55, hours: 8), DryPreset(material: "PETG", temp: 65, hours: 8),
                      DryPreset(material: "ABS", temp: 65, hours: 8), DryPreset(material: "ASA", temp: 65, hours: 8),
                      DryPreset(material: "TPU", temp: 65, hours: 8), DryPreset(material: "PA", temp: 65, hours: 12),
                      DryPreset(material: "PC", temp: 65, hours: 12)]
}

/// The G-code of the controls, as the deck writes it (deck/internal/printer/commands.go).
enum Gcode {
    /// The part fan in percent.
    static func fan(_ percent: Int) -> String {
        if percent <= 0 { return "M107" }
        return "M106 S\(min(255, percent * 255 / 100))"
    }

    /// Moves the nozzle up (positive) or down during a print, in mm.
    static func babystep(_ mm: Double) -> String {
        String(format: "SET_GCODE_OFFSET Z_ADJUST=%.3f MOVE=1", mm)
    }
}

/// A target of a heater, with its name, as the keypad of the deck has them
/// (deck/internal/ui/keypad.go).
struct Preset: Hashable {
    var name: String
    var value: Int

    static let tool = [Preset(name: "Off", value: 0), Preset(name: "PLA", value: 210),
                       Preset(name: "PETG", value: 240), Preset(name: "ABS", value: 250)]
    static let bed = [Preset(name: "Off", value: 0), Preset(name: "PLA", value: 60),
                      Preset(name: "PETG", value: 80), Preset(name: "ABS", value: 100)]
    /// The chamber keypad of the deck (chamberPresets in deck/internal/ui/statusbox.go).
    static let chamber = [Preset(name: "Off", value: 0), Preset(name: "PETG", value: 35),
                          Preset(name: "ABS", value: 50), Preset(name: "PC", value: 60)]
}

/// The state of a printer in a few words, for a list: "Printing", "Paused", "Ready", "Offline".
/// The status screen of the deck has the end of a print (`lcd.state`): "Print done", "Cancelled"
/// and "Print failed".
enum StateText {
    static func of(_ st: Status?, live: Bool, error: String?) -> String {
        guard let st else { return error == nil ? "Connecting" : "No connection" }
        if !live, error != nil { return "No connection" }
        if st.paused { return "Paused" }
        if st.busy {
            if let l = st.layer, let n = st.layers { return "Layer \(l) of \(n)" }
            return "Printing"
        }
        if let s = st.lcd?.state, ["Print done", "Cancelled", "Print failed"].contains(s) { return s }
        if st.state == "Operational" { return "Ready" }
        return st.state
    }

    /// The printer needs the user: a pause, a failed print, or a print that is done.
    static func needsYou(_ st: Status?) -> Bool {
        guard let st else { return false }
        if st.paused { return true }
        return ["Print done", "Print failed"].contains(st.lcd?.state ?? "")
    }
}

/// What a control changes on the printer, and how a later status shows it. The app shows a
/// control as done only when a status after it shows the change.
enum Effect: Equatable {
    case nozzleTarget(Int)
    case bedTarget(Int)
    case auxTarget(Int)
    case chamberTarget(Int)
    /// The part fan in percent.
    case fan(Int)
    /// A home of the axes ("x", "y", "z"), or of all axes with none.
    case home([String])
    case jog
    /// A move of the nozzle during a print, in mm.
    case babystep(Double)
    /// The speed in percent, or a speed level by its number from 1.
    case speed(Int)
    case speedLevel(Int)
    case flow(Int)
    /// Extrude mm, or retract with a negative mm.
    case extrude(Double)
    /// The load of a tray of an AMS by its number, or of the extruder of a Klipper printer.
    case load(Int?)
    /// The unload, to its tray on a printer with an AMS.
    case unload(tray: Bool)
    /// The drying of an AMS unit starts, or stops.
    case dry(String)
    case stopDrying(String)
    case pause, resume, cancel
    /// A print of the file with the name.
    case print(String)

    /// The seconds that the app waits for a status that shows the change. A pause waits for the
    /// move of the printer to end, and a load of an AMS for the filament to reach the nozzle.
    var timeout: Double {
        switch self {
        case .pause, .cancel, .print: 20
        case .home: 30
        case .load, .unload: 120
        default: 8
        }
    }

    /// The text when the status cannot show the change, or nil. The status of a deck before the
    /// control routes has no Z offset, and no status has the filament in the extruder of a
    /// Klipper printer.
    func unobservable(before: Status?) -> String? {
        switch self {
        case .babystep(let mm) where before?.controls?.z_offset == nil:
            String(format: "Sent: nozzle %@ %.3f mm. The deck does not report the Z offset.", mm < 0 ? "down" : "up", abs(mm))
        case .extrude(let mm):
            String(format: "The deck ran the %@ of %.0f mm. It does not report the length.", mm < 0 ? "retract" : "extrusion", abs(mm))
        case .load(nil):
            "The deck ran the load. It does not report the filament in the nozzle."
        case .unload(tray: false):
            "The deck ran the unload. It does not report the filament in the nozzle."
        default:
            nil
        }
    }

    /// The text that the status after the control shows its change, or nil while it does not show
    /// it. before is the status before the control.
    func confirmed(before: Status?, after st: Status) -> String? {
        let lcd = st.lcd
        let cs = st.controls
        func degrees(_ t: Int) -> String { "\(t) °C" }
        switch self {
        case .nozzleTarget(let t):
            guard let now = lcd?.tool_target ?? st.tool?.target, Int(now.rounded()) == t else { return nil }
            return t == 0 ? "Nozzle off" : "Nozzle target " + degrees(t)
        case .bedTarget(let t):
            guard let now = lcd?.bed_target ?? st.bed?.target, Int(now.rounded()) == t else { return nil }
            return t == 0 ? "Bed off" : "Bed target " + degrees(t)
        case .auxTarget(let t):
            guard let now = cs?.tool1?.target ?? st.tool1?.target, Int(now.rounded()) == t else { return nil }
            return t == 0 ? "Aux nozzle off" : "Aux nozzle target " + degrees(t)
        case .chamberTarget(let t):
            guard let now = cs?.chamber?.target ?? st.chamber?.target, Int(now.rounded()) == t else { return nil }
            return t == 0 ? "Chamber off" : "Chamber target " + degrees(t)
        case .fan(let pct):
            // The state of the controls has the fan in percent; the status screen 0 to 1.
            guard let f = cs?.fan ?? lcd?.fan.map({ $0 * 100 }), abs(f - Double(pct)) < 1.5 else { return nil }
            return pct == 0 ? "Fan off" : "Fan \(pct)%"
        case .home(let axes):
            let want = axes.isEmpty ? ["x", "y", "z"] : axes
            if let homed = cs?.homed {
                guard want.allSatisfy({ homed.contains($0) }) else { return nil }
                let pos = cs?.position.map { " · " + ControlState.text($0) } ?? ""
                return "Homed " + want.map { $0.uppercased() }.joined(separator: " ") + pos
            }
            // The status screen of the deck says "XYZ homed" when all axes are homed, and has the
            // position once an axis is homed.
            if Set(want) == ["x", "y", "z"] {
                guard lcd?.message == "XYZ homed" else { return nil }
                return "Homed X Y Z" + (lcd?.position.map { " · " + $0 } ?? "")
            }
            guard let pos = lcd?.position, pos != before?.lcd?.position else { return nil }
            return "Position " + pos
        case .jog:
            if let p = cs?.position {
                guard p != before?.controls?.position else { return nil }
                return "Position " + ControlState.text(p)
            }
            guard let pos = lcd?.position, pos != before?.lcd?.position else { return nil }
            return "Position " + pos
        case .babystep(let mm):
            guard let old = before?.controls?.z_offset, let now = cs?.z_offset, abs(now - (old + mm)) < 0.0005 else { return nil }
            return String(format: "Z offset %+.3f mm", now)
        case .speed(let pct):
            guard let now = cs?.speed, now == pct else { return nil }
            return "Speed \(pct)%"
        case .speedLevel(let level):
            guard cs?.speed_level == level, let names = cs?.speed_levels, names.indices.contains(level - 1) else { return nil }
            return "Speed " + names[level - 1]
        case .flow(let pct):
            guard let now = cs?.flow, now == pct else { return nil }
            return "Flow \(pct)%"
        case .extrude:
            return nil
        case .load(let tray):
            guard let tray, cs?.activeTray == tray else { return nil }
            let slot = cs?.filaments?.first { $0.tray == tray }?.slot ?? ""
            return "Loaded " + (slot == "Ext" ? "the external spool" : slot)
        case .unload(tray: true):
            guard let cs, cs.filaments != nil, cs.activeTray == nil else { return nil }
            return "Unloaded"
        case .unload:
            return nil
        case .dry(let unit):
            guard let d = cs?.ams?.first(where: { $0.unit == unit })?.drying else { return nil }
            return "Drying AMS \(unit) at \(d.temp) °C"
        case .stopDrying(let unit):
            guard let u = cs?.ams?.first(where: { $0.unit == unit }), u.drying == nil else { return nil }
            return "AMS \(unit) stopped drying"
        case .pause:
            return st.busy && st.paused ? "Paused" : nil
        case .resume:
            return st.busy && !st.paused ? "Printing" : nil
        case .cancel:
            return st.busy ? nil : "Cancelled"
        case .print(let name):
            return st.busy ? "Printing " + name : nil
        }
    }

    /// The text when the status does not show the change within the timeout.
    func unconfirmed(before: Status?) -> String {
        switch self {
        case .nozzleTarget(let t): "The deck took the command, but the nozzle target is not \(t) °C yet."
        case .bedTarget(let t): "The deck took the command, but the bed target is not \(t) °C yet."
        case .auxTarget(let t): "The deck took the command, but the aux nozzle target is not \(t) °C yet."
        case .chamberTarget(let t): "The deck took the command, but the chamber target is not \(t) °C yet."
        case .fan(let pct): "The deck took the command, but the fan is not at \(pct)% yet."
        case .home: "The deck took the command, but the status does not show the axes homed yet."
        case .jog where before?.controls?.position == nil && before?.lcd?.position == nil:
            "The deck took the move. It reports no position before a home."
        case .jog: "The deck took the move, but the status shows no new position yet."
        case .babystep: "The deck took the command, but the Z offset has not changed yet."
        case .speed(let pct): "The deck took the command, but the speed is not \(pct)% yet."
        case .speedLevel: "The deck took the command, but the speed level has not changed yet."
        case .flow(let pct): "The deck took the command, but the flow is not \(pct)% yet."
        case .extrude, .load(nil), .unload(tray: false): unobservable(before: before) ?? ""
        case .load: "The deck took the command, but the spool does not feed the nozzle yet."
        case .unload: "The deck took the command, but a spool still feeds the nozzle."
        case .dry(let unit): "The deck took the command, but AMS \(unit) does not dry yet."
        case .stopDrying(let unit): "The deck took the command, but AMS \(unit) still dries."
        case .pause: "The deck took the command, but the print is not paused yet."
        case .resume: "The deck took the command, but the print has not resumed yet."
        case .cancel: "The deck took the command, but the print has not stopped yet."
        case .print: "The deck took the file, but no print runs yet. Check the touch screen of the deck."
        }
    }
}
