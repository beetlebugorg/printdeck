// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import Foundation

/// The controls of a printer that the iPhone shows, from its kind and its state. The rules follow
/// the deck: only a press on the touch screen of a resin deck starts or resumes a print, because
/// both turn on the UV light. The head moves only between prints. The G-code controls (the fan and
/// the Z offset) need a Klipper printer, and the deck refuses G-code with Signed prints only on.
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

    init(deck: SavedDeck, status: Status?, live: Bool) {
        process = status?.process ?? deck.process
        connected = live && status != nil
        busy = status?.busy ?? false
        paused = status?.paused ?? false
        bambu = status?.nozzles?.isEmpty == false || deck.model?.hasPrefix("Bambu") == true
        signedOnly = status?.signedOnly ?? false
    }

    init(process: DeckProcess?, connected: Bool = true, busy: Bool = false, paused: Bool = false,
         bambu: Bool = false, signedOnly: Bool = false) {
        self.process = process
        self.connected = connected
        self.busy = busy
        self.paused = paused
        self.bambu = bambu
        self.signedOnly = signedOnly
    }

    private var fdm: Bool { process == .fdm }

    /// Print a file: an FDM printer with no print.
    var prints: Bool { fdm && !busy }
    var pauses: Bool { busy && !paused }
    /// Resume: an FDM printer only.
    var resumes: Bool { fdm && paused }
    var cancels: Bool { busy }
    /// The targets of the nozzle and the bed.
    var heats: Bool { fdm && connected }
    /// The moves and the homing: an FDM printer with no print.
    var moves: Bool { fdm && connected && !busy }
    /// The part fan, with G-code.
    var fan: Bool { fdm && connected && !bambu && !signedOnly }
    /// The Z offset of a running print, with G-code.
    var tunes: Bool { fdm && connected && busy && !bambu && !signedOnly }
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
    /// The part fan in percent.
    case fan(Int)
    /// A home of the axes ("x", "y", "z"), or of all axes with none.
    case home([String])
    case jog
    /// A move of the nozzle during a print, in mm. The deck does not report the Z offset.
    case babystep(Double)
    case pause, resume, cancel
    /// A print of the file with the name.
    case print(String)

    /// The seconds that the app waits for a status that shows the change. A pause waits for the
    /// move of the printer to end.
    var timeout: Double {
        switch self {
        case .pause, .cancel, .print: 20
        case .home: 30
        default: 8
        }
    }

    /// The text when the status cannot show the change, or nil.
    var unobservable: String? {
        guard case .babystep(let mm) = self else { return nil }
        return String(format: "Sent: nozzle %@ %.3f mm. The deck does not report the Z offset.", mm < 0 ? "down" : "up", abs(mm))
    }

    /// The text that the status after the control shows its change, or nil while it does not show
    /// it. before is the status before the control.
    func confirmed(before: Status?, after st: Status) -> String? {
        let lcd = st.lcd
        switch self {
        case .nozzleTarget(let t):
            guard let now = lcd?.tool_target ?? st.tool?.target, Int(now.rounded()) == t else { return nil }
            return t == 0 ? "Nozzle off" : "Nozzle target \(t) °C"
        case .bedTarget(let t):
            guard let now = lcd?.bed_target ?? st.bed?.target, Int(now.rounded()) == t else { return nil }
            return t == 0 ? "Bed off" : "Bed target \(t) °C"
        case .fan(let pct):
            // The deck sends the fan as 0 to 1, from M106 S0 to S255.
            guard let f = lcd?.fan, abs(f * 100 - Double(pct)) < 1.5 else { return nil }
            return pct == 0 ? "Fan off" : "Fan \(pct)%"
        case .home(let axes):
            // The status screen of the deck says "XYZ homed" when all axes are homed, and has the
            // position once an axis is homed.
            if axes.isEmpty || Set(axes) == ["x", "y", "z"] {
                guard lcd?.message == "XYZ homed" else { return nil }
                return "Homed X Y Z" + (lcd?.position.map { " · " + $0 } ?? "")
            }
            guard let pos = lcd?.position, pos != before?.lcd?.position else { return nil }
            return "Position " + pos
        case .jog:
            guard let pos = lcd?.position, pos != before?.lcd?.position else { return nil }
            return "Position " + pos
        case .babystep:
            return nil
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
        case .fan(let pct): "The deck took the command, but the fan is not at \(pct)% yet."
        case .home: "The deck took the command, but the status does not show the axes homed yet."
        case .jog where before?.lcd?.position == nil:
            "The deck took the move. It reports no position before a home."
        case .jog: "The deck took the move, but the status shows no new position yet."
        case .babystep: unobservable ?? ""
        case .pause: "The deck took the command, but the print is not paused yet."
        case .resume: "The deck took the command, but the print has not resumed yet."
        case .cancel: "The deck took the command, but the print has not stopped yet."
        case .print: "The deck took the file, but no print runs yet. Check the touch screen of the deck."
        }
    }
}
