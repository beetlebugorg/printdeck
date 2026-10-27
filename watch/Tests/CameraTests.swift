// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import XCTest

final class CameraTests: XCTestCase {
    /// The cameras of /api/plugin/printdeck of a deck with two cameras.
    private let json = """
    [{"id": "/dev/v4l/by-path/usb-0:1-video-index0", "number": 1, "roles": ["main", "lapse", "picture"],
      "name": "B0561 1", "state": "idle", "calibrated": false, "views": ["full"]},
     {"id": "/dev/v4l/by-path/usb-0:2-video-index0", "number": 2,
      "name": "B0561 2", "state": "on", "width": 1280, "height": 720, "calibrated": true, "views": ["full", "part"]}]
    """

    private func cameras() throws -> [DeckCamera] {
        try JSONDecoder().decode([DeckCamera].self, from: Data(json.utf8))
    }

    func testDecode() throws {
        let list = try cameras()
        XCTAssertEqual(list.count, 2)
        XCTAssertEqual(list[1].number, 2)
        XCTAssertEqual(list[0].roles, ["main", "lapse", "picture"])
        XCTAssertNil(list[1].roles)
        XCTAssertEqual(list[1].aspect, 16 / 9, accuracy: 0.001)
    }

    /// The chosen camera when it is on, or else the main camera.
    func testPick() throws {
        var list = try cameras()
        let main = list[0]
        XCTAssertEqual(DeckCamera.pick(list, id: list[1].id, main: main)?.name, "B0561 2")
        XCTAssertEqual(DeckCamera.pick(list, id: nil, main: main)?.name, "B0561 1")
        XCTAssertEqual(DeckCamera.pick(list, id: "gone", main: main)?.name, "B0561 1")
        list[1].state = "off"
        XCTAssertEqual(DeckCamera.pick(list, id: list[1].id, main: main)?.name, "B0561 1")
        // An older deck: no list, the main camera.
        XCTAssertEqual(DeckCamera.pick([], id: list[1].id, main: main)?.name, "B0561 1")
        XCTAssertEqual(DeckCamera.pick(list, id: nil, main: nil)?.name, "B0561 1")
    }

    /// The camera parameter of the webcam has the ID with its slashes encoded.
    func testCameraQuery() {
        XCTAssertEqual(DeckClient.cameraQuery(nil), "")
        XCTAssertEqual(DeckClient.cameraQuery("2"), "&camera=2")
        XCTAssertEqual(DeckClient.cameraQuery("/dev/v4l/by-path/usb-0:2-video-index0"),
                       "&camera=%2Fdev%2Fv4l%2Fby-path%2Fusb-0%3A2-video-index0")
    }
}
