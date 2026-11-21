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
        XCTAssertEqual(Effect.babystep(-0.025).unobservable, "Sent: nozzle down 0.025 mm. The deck does not report the Z offset.")
        XCTAssertNil(Effect.jog.unobservable)
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
}
