// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import XCTest

/// The controls that the iPhone shows for a printer, and their G-code.
final class ControlsTests: XCTestCase {
    /// Only an FDM printer starts or resumes a print from the app. A resin printer pauses and
    /// cancels.
    func testResinRule() {
        let resin = Controls(process: .resin, busy: true, paused: true)
        XCTAssertFalse(resin.resumes)
        XCTAssertFalse(resin.prints)
        XCTAssertTrue(resin.cancels)
        XCTAssertFalse(resin.heats)
        XCTAssertFalse(resin.moves)
        XCTAssertFalse(resin.fan)
        XCTAssertTrue(Controls(process: .resin, busy: true).pauses)
        XCTAssertFalse(Controls(process: .resin).prints)
        // A printer before its first status has no kind: it has no Print and no Resume.
        XCTAssertFalse(Controls(process: nil).prints)
        XCTAssertFalse(Controls(process: nil, busy: true, paused: true).resumes)

        XCTAssertTrue(Controls(process: .fdm).prints)
        XCTAssertTrue(Controls(process: .fdm, busy: true, paused: true).resumes)
        XCTAssertFalse(Controls(process: .fdm, busy: true).prints)
    }

    /// The head moves between prints, and only on a printer that answers.
    func testMoves() {
        XCTAssertTrue(Controls(process: .fdm).moves)
        XCTAssertFalse(Controls(process: .fdm, busy: true).moves)
        XCTAssertFalse(Controls(process: .fdm, busy: true, paused: true).moves)
        XCTAssertFalse(Controls(process: .fdm, connected: false).moves)
        XCTAssertFalse(Controls(process: .fdm, connected: false).heats)
        XCTAssertTrue(Controls(process: .fdm, busy: true).heats)
    }

    /// The fan and the Z offset are G-code: a Klipper printer, with Signed prints only off. Jog,
    /// home and the temperatures go through other routes and stay on.
    func testGcodeControls() {
        XCTAssertTrue(Controls(process: .fdm).fan)
        XCTAssertTrue(Controls(process: .fdm, busy: true).tunes)
        XCTAssertFalse(Controls(process: .fdm).tunes)
        let signed = Controls(process: .fdm, busy: false, signedOnly: true)
        XCTAssertFalse(signed.fan)
        XCTAssertTrue(signed.moves)
        XCTAssertTrue(signed.heats)
        XCTAssertFalse(Controls(process: .fdm, busy: true, signedOnly: true).tunes)
        let bambu = Controls(process: .fdm, busy: true, bambu: true)
        XCTAssertFalse(bambu.fan)
        XCTAssertFalse(bambu.tunes)
        XCTAssertTrue(bambu.heats)
    }

    /// A Bambu Lab printer tells its nozzles, or its model has the name.
    func testBambuFromStatus() {
        var st = Status(state: "Operational", busy: false, paused: false, time: Date())
        st.process = .fdm
        st.signedOnly = true
        let deck = SavedDeck(settings: DeckSettings(address: "a", key: "k"), process: .fdm)
        XCTAssertFalse(Controls(deck: deck, status: st, live: true).bambu)
        XCTAssertTrue(Controls(deck: deck, status: st, live: true).signedOnly)
        XCTAssertFalse(Controls(deck: deck, status: st, live: false).connected)
        st.nozzles = [DeckNozzle(id: 0, role: "main", diameter: 0.4, type: "HS01")]
        XCTAssertTrue(Controls(deck: deck, status: st, live: true).bambu)
        var x2d = deck
        x2d.model = "Bambu Lab X2D"
        XCTAssertTrue(Controls(deck: x2d, status: nil, live: false).bambu)
    }

    /// The G-code is the G-code of the deck (deck/internal/printer/commands.go).
    func testGcode() {
        XCTAssertEqual(Gcode.fan(0), "M107")
        XCTAssertEqual(Gcode.fan(100), "M106 S255")
        XCTAssertEqual(Gcode.fan(50), "M106 S127")
        XCTAssertEqual(Gcode.fan(150), "M106 S255")
        XCTAssertEqual(Gcode.babystep(0.025), "SET_GCODE_OFFSET Z_ADJUST=0.025 MOVE=1")
        XCTAssertEqual(Gcode.babystep(-0.025), "SET_GCODE_OFFSET Z_ADJUST=-0.025 MOVE=1")
    }

    func testStateText() {
        let t = Date()
        XCTAssertEqual(StateText.of(nil, live: false, error: nil), "Connecting")
        XCTAssertEqual(StateText.of(nil, live: false, error: "x"), "No connection")
        var st = Status(state: "Operational", busy: false, paused: false, time: t)
        XCTAssertEqual(StateText.of(st, live: true, error: nil), "Ready")
        XCTAssertEqual(StateText.of(st, live: false, error: "x"), "No connection")
        st.lcd = DeckLCD(state: "Print done")
        XCTAssertEqual(StateText.of(st, live: true, error: nil), "Print done")
        XCTAssertTrue(StateText.needsYou(st))
        st = Status(state: "Printing", busy: true, paused: false, layer: 3, layers: 9, time: t)
        XCTAssertEqual(StateText.of(st, live: true, error: nil), "Layer 3 of 9")
        XCTAssertFalse(StateText.needsYou(st))
        st.paused = true
        XCTAssertEqual(StateText.of(st, live: true, error: nil), "Paused")
        XCTAssertTrue(StateText.needsYou(st))
    }

    /// The files by folder: the files with no folder first, then the folders by name.
    func testFolders() {
        let f = ["b/x.gcode", "a.gcode", "a/y.gcode", "c.gcode", "b/z.gcode"].map { DeckFile(name: $0, path: $0) }
        let out = DeckFile.folders(f)
        XCTAssertEqual(out.map(\.name), ["", "a", "b"])
        XCTAssertEqual(out[0].files.map(\.path), ["a.gcode", "c.gcode"])
        XCTAssertEqual(out[2].files.map(\.path), ["b/x.gcode", "b/z.gcode"])
    }

    /// The presets are the presets of the keypad of the deck.
    func testPresets() {
        XCTAssertEqual(Preset.tool.map(\.value), [0, 210, 240, 250])
        XCTAssertEqual(Preset.bed.map(\.value), [0, 60, 80, 100])
    }

    private func status(busy: Bool = false, paused: Bool = false, lcd: DeckLCD? = nil) -> Status {
        var st = Status(state: busy ? "Printing" : "Operational", busy: busy, paused: paused, time: Date())
        st.lcd = lcd
        return st
    }

    /// A target shows as done only when the status has it.
    func testTargetEffects() {
        let old = status(lcd: DeckLCD(tool_target: 0, bed_target: 0))
        XCTAssertNil(Effect.bedTarget(60).confirmed(before: old, after: old))
        XCTAssertEqual(Effect.bedTarget(60).confirmed(before: old, after: status(lcd: DeckLCD(bed_target: 60))), "Bed target 60 °C")
        XCTAssertEqual(Effect.bedTarget(0).confirmed(before: old, after: old), "Bed off")
        XCTAssertEqual(Effect.nozzleTarget(210).confirmed(before: old, after: status(lcd: DeckLCD(tool_target: 210))),
                       "Nozzle target 210 °C")
        XCTAssertNil(Effect.nozzleTarget(210).confirmed(before: old, after: status(lcd: DeckLCD(tool_target: 200))))
        // A deck with no status screen has the targets of /api/printer.
        var plain = status()
        plain.bed = Temp(actual: 30, target: 60)
        XCTAssertEqual(Effect.bedTarget(60).confirmed(before: nil, after: plain), "Bed target 60 °C")
        XCTAssertEqual(Effect.bedTarget(60).unconfirmed(before: old), "The deck took the command, but the bed target is not 60 °C yet.")
        XCTAssertEqual(Effect.fan(50).confirmed(before: nil, after: status(lcd: DeckLCD(fan: 127.0 / 255))), "Fan 50%")
        XCTAssertNil(Effect.fan(50).confirmed(before: nil, after: status(lcd: DeckLCD(fan: 1))))
    }

    /// Home all shows as done when the deck says "XYZ homed". A move shows the new position, and
    /// says when the printer reports no position.
    func testMoveEffects() {
        let unhomed = status(lcd: DeckLCD(message: "Not homed"))
        let homed = status(lcd: DeckLCD(message: "XYZ homed", position: "X0 Y0 Z5.00"))
        XCTAssertNil(Effect.home([]).confirmed(before: unhomed, after: unhomed))
        XCTAssertEqual(Effect.home([]).confirmed(before: unhomed, after: homed), "Homed X Y Z · X0 Y0 Z5.00")
        XCTAssertEqual(Effect.home(["x", "y", "z"]).confirmed(before: unhomed, after: homed), "Homed X Y Z · X0 Y0 Z5.00")
        XCTAssertEqual(Effect.home(["x", "y"]).confirmed(before: unhomed, after: homed), "Position X0 Y0 Z5.00")
        XCTAssertNil(Effect.home(["x", "y"]).confirmed(before: homed, after: homed))
        let moved = status(lcd: DeckLCD(message: "XYZ homed", position: "X10 Y0 Z5.00"))
        XCTAssertEqual(Effect.jog.confirmed(before: homed, after: moved), "Position X10 Y0 Z5.00")
        XCTAssertNil(Effect.jog.confirmed(before: homed, after: homed))
        XCTAssertEqual(Effect.jog.unconfirmed(before: unhomed), "The deck took the move. It reports no position before a home.")
        XCTAssertEqual(Effect.jog.unconfirmed(before: homed), "The deck took the move, but the status shows no new position yet.")
        // The Z offset has no field in the status: the text says so, and claims no change.
        XCTAssertEqual(Effect.babystep(-0.025).unobservable(before: homed), "Sent: nozzle down 0.025 mm. The deck does not report the Z offset.")
        XCTAssertNil(Effect.jog.unobservable(before: homed))
    }

    func testPrintEffects() {
        let printing = status(busy: true), paused = status(busy: true, paused: true), idle = status()
        XCTAssertEqual(Effect.pause.confirmed(before: printing, after: paused), "Paused")
        XCTAssertNil(Effect.pause.confirmed(before: printing, after: printing))
        XCTAssertEqual(Effect.resume.confirmed(before: paused, after: printing), "Printing")
        XCTAssertNil(Effect.resume.confirmed(before: paused, after: paused))
        XCTAssertEqual(Effect.cancel.confirmed(before: printing, after: idle), "Cancelled")
        XCTAssertNil(Effect.cancel.confirmed(before: printing, after: printing))
        XCTAssertEqual(Effect.print("Benchy").confirmed(before: idle, after: printing), "Printing Benchy")
        XCTAssertNil(Effect.print("Benchy").confirmed(before: idle, after: idle))
        XCTAssertEqual(Effect.pause.timeout, 20)
        XCTAssertEqual(Effect.jog.timeout, 8)
    }

    /// /api/plugin/printdeck of an X2D on a deck with the control routes (#97).
    private let x2d = #"""
    {"process":"fdm","controls":["jog","home","fan","speed","zoffset","filament","ams_dry","chamber","tool1"],
     "homed":"xyz","position":{"x":128,"y":128,"z":4.2},"speed":100,"speed_levels":["Silent","Standard","Sport","Ludicrous"],
     "speed_level":2,"z_offset":0,"tool1":{"actual":24,"target":0},"chamber":{"actual":38,"target":0},"fan":60,
     "tool_now":1,"action":"","preparing":"",
     "ams":[{"unit":"A","temp":27.5,"humidity":2,"humidity_pct":21,"drying":null}],
     "messages":["0300_1A00_0001_0001: The nozzle temperature is abnormal"],
     "filaments":[{"slot":"A1","index":0,"tray":0,"material":"PLA","remain":0.8,"active":true},
                  {"slot":"Ext","index":1,"tray":254,"nozzle":"main","material":"PLA","remain":null},
                  {"slot":"Ext","index":2,"tray":255,"nozzle":"aux","material":"TPU","remain":null}]}
    """#

    private func state(_ json: String) -> ControlState? { ControlState.of(Data(json.utf8)) }

    func testControlState() throws {
        let cs = try XCTUnwrap(state(x2d))
        XCTAssertTrue(cs.has("tool1"))
        XCTAssertFalse(cs.has("flow"))
        XCTAssertEqual(cs.position, ControlState.Position(x: 128, y: 128, z: 4.2))
        XCTAssertEqual(ControlState.text(cs.position!), "X128 Y128 Z4.20")
        XCTAssertEqual(cs.speed_levels?.count, 4)
        XCTAssertEqual(cs.tool1, Temp(actual: 24, target: 0))
        XCTAssertEqual(cs.fan, 60)
        XCTAssertEqual(cs.ams?.first?.humidity_pct, 21)
        XCTAssertNil(cs.ams?.first?.drying)
        XCTAssertEqual(cs.messages?.count, 1)
        XCTAssertEqual(cs.activeTray, 0)
        // A deck from before the routes has no "controls": no state.
        XCTAssertNil(state(#"{"process":"fdm","job":null}"#))
    }

    /// tool_now is the nozzle that prints: 0, 1, or missing from a deck before the field.
    func testToolNow() throws {
        XCTAssertEqual(state(x2d)?.tool_now, 1)
        XCTAssertEqual(state(#"{"controls":["jog"],"tool_now":0}"#)?.tool_now, 0)
        XCTAssertNil(try XCTUnwrap(state(#"{"controls":["jog"]}"#)).tool_now)
        // The watch panel marks the nozzle in use only on a printer with two, and only with the field.
        var st = Status(state: "Printing", busy: true, paused: false, time: Date())
        st.controls = state(x2d)
        var p = StatusPanel.make(st, connected: true, error: nil, now: Date())
        XCTAssertEqual(p.readouts.map(\.name), ["NOZZLE", "AUX", "BED", "CHAMBER", "FAN"])
        XCTAssertEqual(p.readouts.map(\.inUse), [false, true, false, false, false])
        st.controls?.tool_now = 0
        p = StatusPanel.make(st, connected: true, error: nil, now: Date())
        XCTAssertEqual(p.readouts.map(\.inUse), [true, false, false, false, false])
        st.controls?.tool_now = nil
        p = StatusPanel.make(st, connected: true, error: nil, now: Date())
        XCTAssertTrue(p.readouts.allSatisfy { !$0.inUse })
        // A printer with one nozzle has no mark.
        let one = Status(state: "Printing", busy: true, paused: false, controls: state(#"{"controls":["jog"],"tool_now":0}"#), time: Date())
        XCTAssertTrue(StatusPanel.make(one, connected: true, error: nil, now: Date()).readouts.allSatisfy { !$0.inUse })
    }

    /// A deck with the routes shows only its controls, with the rules of the deck: the jog and the
    /// filament wait for a pause, and the home for the end of a print.
    func testListedControls() {
        let all = ["jog", "home", "fan", "speed", "flow", "zoffset", "extrude", "filament", "ams_dry", "chamber", "tool1"]
        let idle = Controls(process: .fdm, list: all)
        XCTAssertTrue(idle.jogs && idle.homes && idle.loads && idle.extrudes && idle.fan && idle.dries)
        XCTAssertFalse(idle.speed || idle.flow || idle.tunes)
        let printing = Controls(process: .fdm, busy: true, list: all)
        XCTAssertFalse(printing.jogs || printing.homes || printing.loads || printing.extrudes)
        XCTAssertTrue(printing.speed && printing.flow && printing.tunes && printing.fan)
        let paused = Controls(process: .fdm, busy: true, paused: true, list: all)
        XCTAssertTrue(paused.jogs && paused.loads && paused.extrudes)
        XCTAssertFalse(paused.homes)
        XCTAssertTrue(paused.moves)
        // The fan and the Z offset have their routes: Signed prints only and a Bambu printer keep them.
        let signed = Controls(process: .fdm, busy: true, bambu: true, signedOnly: true, list: ["fan", "zoffset"])
        XCTAssertTrue(signed.fan && signed.tunes)
        // A control that the deck does not list is off.
        let few = Controls(process: .fdm, busy: true, list: ["jog"])
        XCTAssertFalse(few.fan || few.tunes || few.speed || few.chamber || few.aux)
        // A resin deck lists no controls.
        let resin = Controls(process: .resin, list: [])
        XCTAssertFalse(resin.moves || resin.fan || resin.loads)
        XCTAssertFalse(Controls(process: .fdm, connected: false, list: all).jogs)
    }

    /// The new effects show from the state of the controls.
    func testControlEffects() throws {
        let before = Status(state: "Printing", busy: true, paused: false, controls: state(x2d), time: Date())
        var after = before
        after.controls?.z_offset = 0.025
        XCTAssertEqual(Effect.babystep(0.025).confirmed(before: before, after: after), "Z offset +0.025 mm")
        XCTAssertNil(Effect.babystep(-0.025).confirmed(before: before, after: after))
        XCTAssertNil(Effect.babystep(0.025).unobservable(before: before))
        after.controls?.speed_level = 3
        XCTAssertEqual(Effect.speedLevel(3).confirmed(before: before, after: after), "Speed Sport")
        after.controls?.speed = 120
        XCTAssertEqual(Effect.speed(120).confirmed(before: before, after: after), "Speed 120%")
        XCTAssertNil(Effect.flow(102).confirmed(before: before, after: after))
        after.controls?.fan = 50
        XCTAssertEqual(Effect.fan(50).confirmed(before: before, after: after), "Fan 50%")
        after.controls?.tool1?.target = 220
        XCTAssertEqual(Effect.auxTarget(220).confirmed(before: before, after: after), "Aux nozzle target 220 °C")
        after.controls?.chamber?.target = 35
        XCTAssertEqual(Effect.chamberTarget(35).confirmed(before: before, after: after), "Chamber target 35 °C")
        // Home by the homed axes, and a jog by the new position.
        var cold = before
        cold.controls?.homed = ""
        XCTAssertNil(Effect.home(["x", "y"]).confirmed(before: cold, after: cold))
        XCTAssertEqual(Effect.home(["x", "y"]).confirmed(before: cold, after: before), "Homed X Y · X128 Y128 Z4.20")
        XCTAssertEqual(Effect.home([]).confirmed(before: cold, after: before), "Homed X Y Z · X128 Y128 Z4.20")
        after.controls?.position = ControlState.Position(x: 138, y: 128, z: 4.2)
        XCTAssertEqual(Effect.jog.confirmed(before: before, after: after), "Position X138 Y128 Z4.20")
        XCTAssertNil(Effect.jog.confirmed(before: before, after: before))
        // The load of a tray: the spool of the tray feeds.
        XCTAssertNil(Effect.load(255).confirmed(before: before, after: before))
        after.controls?.filaments = [ControlState.Spool(slot: "Ext", tray: 255, active: true)]
        XCTAssertEqual(Effect.load(255).confirmed(before: before, after: after), "Loaded the external spool")
        XCTAssertEqual(Effect.load(0).confirmed(before: before, after: before), "Loaded A1")
        after.controls?.filaments = [ControlState.Spool(slot: "A1", tray: 0, active: nil)]
        XCTAssertEqual(Effect.unload(tray: true).confirmed(before: before, after: after), "Unloaded")
        XCTAssertNil(Effect.unload(tray: true).confirmed(before: before, after: before))
        // A Klipper load and an extrusion have no state: the text says that the deck ran them.
        XCTAssertEqual(Effect.load(nil).unobservable(before: before), "The deck ran the load. It does not report the filament in the nozzle.")
        XCTAssertEqual(Effect.extrude(-10).unobservable(before: before), "The deck ran the retract of 10 mm. It does not report the length.")
        // The drying of an AMS.
        after.controls?.ams = [ControlState.AMSUnit(unit: "A", drying: ControlState.Drying(temp: 55, left: 28800, filament: "PLA"))]
        XCTAssertEqual(Effect.dry("A").confirmed(before: before, after: after), "Drying AMS A at 55 °C")
        XCTAssertNil(Effect.stopDrying("A").confirmed(before: before, after: after))
        XCTAssertEqual(Effect.stopDrying("A").confirmed(before: after, after: before), "AMS A stopped drying")
        XCTAssertEqual(Effect.load(0).timeout, 120)
    }

    func testSwapAndDry() {
        XCTAssertEqual(SwapMaterial.of("petg").nozzle, 240)
        XCTAssertEqual(SwapMaterial.of(nil).name, "PLA")
        XCTAssertTrue(SwapMaterial.hot(207, for: 210))
        XCTAssertFalse(SwapMaterial.hot(206.9, for: 210))
        XCTAssertEqual(DryPreset.all.first, DryPreset(material: "PLA", temp: 55, hours: 8))
        XCTAssertEqual(Preset.chamber.map(\.value), [0, 35, 50, 60])
    }
}
