// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import XCTest

final class FleetTests: XCTestCase {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func printing(left: Double?, paused: Bool = false) -> Status {
        Status(state: paused ? "Paused" : "Printing", busy: true, paused: paused, progress: 0.5, left: left, time: now)
    }

    private var idle: Status { Status(state: "Operational", busy: false, paused: false, time: now) }

    /// The print that ends first, then a print with no estimate, then a paused print.
    func testEndsFirst() {
        XCTAssertNil(Status.endsFirst([]))
        XCTAssertNil(Status.endsFirst([idle, nil]))
        XCTAssertEqual(Status.endsFirst([idle, printing(left: 600)]), 1)
        XCTAssertEqual(Status.endsFirst([printing(left: 3600), printing(left: 600)]), 1)
        XCTAssertEqual(Status.endsFirst([printing(left: nil), printing(left: 9000)]), 1)
        XCTAssertEqual(Status.endsFirst([printing(left: 60, paused: true), printing(left: nil)]), 1)
        XCTAssertEqual(Status.endsFirst([printing(left: 60, paused: true), idle]), 0)
    }

    /// A deck of the list from an older app has only some fields. An unknown process reads as nil,
    /// so the deck has no Print a file and no Resume.
    func testSavedDeckDecode() throws {
        let d = try JSONDecoder().decode(SavedDeck.self, from: Data(#"{"id":"1","address":"printdeck.local","key":"k"}"#.utf8))
        XCTAssertEqual(d.title, "printdeck.local")
        XCTAssertNil(d.process)
        XCTAssertFalse(d.printsFromApp)
        let r = try JSONDecoder().decode(SavedDeck.self, from: Data(#"{"id":"2","name":"Phenom","process":"sla"}"#.utf8))
        XCTAssertEqual(r.title, "Phenom")
        XCTAssertNil(r.process)
    }

    /// Only an FDM deck prints a file and resumes from the app.
    func testResinStartsNothing() {
        var d = SavedDeck(settings: DeckSettings(address: "192.168.76.45", key: "k"), process: .resin)
        XCTAssertFalse(d.printsFromApp)
        XCTAssertFalse(d.resumesFromApp)
        d.process = .fdm
        XCTAssertTrue(d.printsFromApp)
        XCTAssertTrue(d.resumesFromApp)
    }

    /// The list goes to the watch and back as JSON with the same values.
    func testSavedDeckRoundTrip() throws {
        let d = SavedDeck(name: "TAZ", settings: DeckSettings(address: "192.168.1.50:8080", key: "k"),
                          process: .fdm, camera: "/dev/video0")
        let back = try JSONDecoder().decode(SavedDeck.self, from: JSONEncoder().encode(d))
        XCTAssertEqual(back, d)
    }

    /// The queue of a deck (GET /api/plugin/printdeck/queue).
    func testQueueDecode() throws {
        let json = ##"{"items":[{"id":"6972a8d74cd7","path":"cube.gcode","name":"Cube","seconds":1500,"from":"slicer","added":"2026-10-31T22:30:26.442516Z"},{"id":"b","path":"door.gcode","name":"Pod door","project":"Bittlex","material":"PLA","material_name":"Black PLA","color":"#111111","from":"build","added":"2026-10-31T22:31:00Z"}],"seconds":1500}"##
        let q = try JSONDecoder().decode(DeckQueue.self, from: Data(json.utf8))
        XCTAssertEqual(q.items.map(\.name), ["Cube", "Pod door"])
        XCTAssertEqual(q.items[1].materialName, "Black PLA")
        XCTAssertEqual(q.seconds, 1500)
    }
}
