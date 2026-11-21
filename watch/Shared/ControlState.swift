// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import Foundation

/// The controls of a printer and the state that shows their effect, at the top of
/// /api/plugin/printdeck (controls and controlState in deck/internal/octoapi/controls.go). A deck
/// with no "controls" in its reply has none of these routes.
struct ControlState: Codable, Equatable {
    /// A position of the head in mm.
    struct Position: Codable, Equatable {
        var x, y, z: Double
    }

    /// The drying of an AMS: its temperature, the seconds left, and the filament.
    struct Drying: Codable, Equatable {
        var temp: Int
        var left: Int
        var filament: String?
    }

    /// A unit of an AMS: its letter, its temperature and humidity, and its drying.
    struct AMSUnit: Codable, Equatable, Identifiable {
        var unit: String
        var temp: Double?
        /// The level of the printer, 1 (dry) to 5 (wet).
        var humidity: Int?
        var humidity_pct: Double?
        var drying: Drying?
        var id: String { unit }
    }

    /// A loaded spool, for the spool that feeds the nozzle.
    struct Spool: Codable, Equatable {
        var slot: String?
        var tray: Int?
        var active: Bool?
    }

    /// The names of the controls of the printer: "jog", "home", "fan", "speed", "flow", "zoffset",
    /// "extrude", "filament", "ams_dry", "chamber" and "tool1".
    var controls: [String]
    /// The axes that the printer homed, for example "xyz", or "".
    var homed: String?
    /// The position of the head, or nil when the deck does not know it.
    var position: Position?
    /// The speed and the flow of the print in percent, or nil when the printer does not report
    /// them.
    var speed: Int?
    var flow: Int?
    /// The speed levels of a Bambu Lab printer, and the level now, from 1.
    var speed_levels: [String]?
    var speed_level: Int?
    /// The Z offset of the baby steps in mm.
    var z_offset: Double?
    /// The aux nozzle and the chamber, or nil.
    var tool1: Temp?
    var chamber: Temp?
    /// The part fan in percent, or nil when the printer does not report it.
    var fan: Double?
    /// The nozzle that prints now: 0 for the main nozzle, 1 for the aux nozzle. Nil from a deck
    /// before the field.
    var tool_now: Int?
    /// The action of the deck that runs now (a load, a calibration), or "".
    var action: String?
    /// The step of the start of a print that is starting, or "".
    var preparing: String?
    var ams: [AMSUnit]?
    /// The messages of the printer, one line each, with the HMS code in the text.
    var messages: [String]?
    var filaments: [Spool]?

    func has(_ name: String) -> Bool { controls.contains(name) }

    /// The tray of the spool that feeds the nozzle, or nil.
    var activeTray: Int? { filaments?.first { $0.active == true }?.tray }

    /// The text of a position: "X110 Y110 Z5.00".
    static func text(_ p: Position) -> String {
        String(format: "X%.0f Y%.0f Z%.2f", p.x, p.y, p.z)
    }

    /// The state of the reply, or nil from a deck with no "controls".
    static func of(_ data: Data) -> ControlState? {
        try? JSONDecoder().decode(ControlState.self, from: data)
    }
}
