// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import XCTest

final class LCDModelTests: XCTestCase {
    private func printing(at time: Date) -> Status {
        Status(state: "Printing", busy: true, paused: false, file: "benchy.gcode", progress: 0.5, left: 3600,
               layer: 10, layers: 40, time: time)
    }

    /// A live status counts down to the end.
    func testLive() {
        let t = Date()
        let s = LCDModel.status(printing(at: t), connected: true, error: nil, now: t.addingTimeInterval(600), hint: "")
        XCTAssertEqual(s.state, "Printing")
        XCTAssertEqual(s.left, "0:50")
    }

    /// An old status shows its age, and its times stop.
    func testOld() {
        let t = Date()
        let s = LCDModel.status(printing(at: t), connected: true, error: "No connection to the deck.",
                                now: t.addingTimeInterval(180), hint: "", live: false)
        XCTAssertEqual(s.state, "No connection")
        XCTAssertEqual(s.message, "Read 3m ago")
        XCTAssertEqual(s.left, "1:00")
    }

    /// The result of a command is in the message line.
    func testNotice() {
        let s = LCDModel.status(printing(at: Date()), connected: true, error: nil, now: Date(), hint: "",
                                notice: "Pausing")
        XCTAssertEqual(s.message, "Pausing")
    }

    /// A wrong estimate of the deck gives a time, and no trap.
    func testHugeTimes() {
        XCTAssertEqual(LCDModel.hm(1e30), "27777:46")
        XCTAssertEqual(LCDModel.hm(-5), "0:00")
        XCTAssertEqual(Format.shortDuration(.infinity), "27777h46")
    }

    /// A camera before its first picture has no size: the app uses 16:9.
    func testCameraWithNoSize() throws {
        let cam = try JSONDecoder().decode(DeckCamera.self, from: Data(#"{"name":"C270","state":"starting"}"#.utf8))
        XCTAssertEqual(cam.aspect, 16 / 9)
        XCTAssertEqual(cam.problem, "Starting the camera")
        XCTAssertTrue(cam.on)
        let off = try JSONDecoder().decode(DeckCamera.self, from: Data(#"{"name":"","state":"off"}"#.utf8))
        XCTAssertFalse(off.on)
    }
}
