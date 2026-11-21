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
