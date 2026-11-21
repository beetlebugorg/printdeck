// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import XCTest

/// The status panel of the watch, as the deck writes its panel (panelLines in
/// deck/internal/ui/statusbox.go).
final class StatusPanelTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func printing(paused: Bool = false, layer: Int = 12) -> Status {
        var st = Status(state: "Printing", busy: true, paused: paused, file: "Benchy", progress: 0.42, left: 4980,
                        elapsed: 2700, layer: layer, layers: 27, time: now)
        st.lcd = DeckLCD(state: "Printing", tool: 209.6, tool_target: 210, bed: 60, bed_target: 60, tool_power: 0.4,
                         bed_power: 0.2, fan: 1, progress: 0.42, printing: true, next: "1:32", z: "3.20")
        return st
    }

    func testPrinting() {
        let p = StatusPanel.make(printing(), connected: true, error: nil, now: now)
        XCTAssertEqual(p.head, "Printing · 42%")
        XCTAssertEqual(p.side, "Ends " + Format.clock(now.addingTimeInterval(4980)))
        XCTAssertEqual(p.short, Format.clock(now.addingTimeInterval(4980)))
        XCTAssertEqual(p.msg, "1:23 left · 0:45 so far")
        XCTAssertEqual(p.detail, "Layer 12 of 27 · next in 1:32 · Z 3.20")
        XCTAssertTrue(p.bar)
        XCTAssertEqual(p.progress, 0.42, accuracy: 0.001)
        XCTAssertEqual(p.readouts.map(\.name), ["NOZZLE", "BED", "FAN"])
        XCTAssertEqual(p.readouts.map(\.value), ["210°", "60°", "100%"])
        XCTAssertEqual(p.readouts.map(\.target), ["→210", "→60", ""])
        XCTAssertEqual(p.readouts[0].power, 0.4)
        // Between two reads, the times go on.
        let later = StatusPanel.make(printing(), connected: true, error: nil, now: now.addingTimeInterval(60))
        XCTAssertEqual(later.msg, "1:22 left · 0:45 so far")
    }

    func testFirstLayerAndPause() {
        XCTAssertEqual(StatusPanel.make(printing(layer: 1), connected: true, error: nil, now: now).head, "First layer · 42%")
        let p = StatusPanel.make(printing(paused: true), connected: true, error: nil, now: now)
        XCTAssertEqual(p.head, "Paused · 42%")
        XCTAssertEqual(p.msg, "Tap to resume")
        XCTAssertEqual(StatusPanel.make(printing(paused: true), connected: true, error: nil, now: now, resin: true).msg,
                       "Resume on the deck")
    }

    /// A command shows its text in the message line.
    func testNotice() {
        XCTAssertEqual(StatusPanel.make(printing(), connected: true, error: nil, now: now, notice: "Pausing").msg, "Pausing")
    }

    /// No print: the state and the message of the status screen of the deck, and the position.
    func testIdle() {
        var st = Status(state: "Operational", busy: false, paused: false, time: now)
        st.lcd = DeckLCD(state: "Ready", message: "XYZ homed", tool: 24, tool_target: 0, bed: 23, bed_target: 0,
                         fan: 0, position: "X110 Y110 Z5.00")
        let p = StatusPanel.make(st, connected: true, error: nil, now: now)
        XCTAssertEqual(p.head, "Ready")
        XCTAssertEqual(p.side, Format.clock(now))
        XCTAssertEqual(p.msg, "XYZ homed")
        XCTAssertEqual(p.detail, "X110 Y110 Z5.00")
        XCTAssertFalse(p.bar)
        XCTAssertEqual(p.readouts.map(\.value), ["24°", "23°", "off"])
        XCTAssertEqual(p.readouts.map(\.target), ["off", "off", ""])
        st.lcd = DeckLCD(state: "Print done", message: "Benchy")
        XCTAssertEqual(StatusPanel.make(st, connected: true, error: nil, now: now).head, "Print done")
        // A deck with no status screen.
        st.lcd = nil
        XCTAssertEqual(StatusPanel.make(st, connected: true, error: nil, now: now).head, "Ready")
    }

    /// An old status shows its age, and the readouts show no values.
    func testOld() {
        let p = StatusPanel.make(printing(), connected: true, error: "No connection to the deck.", now: now.addingTimeInterval(180),
                                 live: false)
        XCTAssertEqual(p.head, "No connection")
        XCTAssertEqual(p.msg, "Read 3m ago")
        XCTAssertEqual(p.detail, "Benchy")
        XCTAssertEqual(p.readouts.map(\.value), ["--", "--", "--"])
        XCTAssertEqual(StatusPanel.make(nil, connected: false, error: nil, now: now).msg, "Set up on the iPhone")
        XCTAssertEqual(StatusPanel.make(nil, connected: true, error: nil, now: now).head, "Reading the deck")
    }

    func testResin() {
        var st = printing()
        st.lcd?.z = "12.5"
        let p = StatusPanel.make(st, connected: true, error: nil, now: now, resin: true)
        XCTAssertEqual(p.readouts.map(\.name), ["PLATE MM", "TIME", "LEFT"])
        XCTAssertEqual(p.readouts.map(\.value), ["12.5", "0:45", "1:23"])
    }

    func testCooling() {
        var st = printing()
        st.lcd?.cooling = true
        st.lcd?.cool_to = 40
        st.lcd?.left = "4 min"
        st.lcd?.progress = 0.5
        let p = StatusPanel.make(st, connected: true, error: nil, now: now)
        XCTAssertEqual(p.head, "Cooling the bed")
        XCTAssertEqual(p.side, "4 min")
        XCTAssertEqual(p.msg, "The part comes off at 40°")
        XCTAssertEqual(p.progress, 0.5)
    }
}
