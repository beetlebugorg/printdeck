// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import XCTest

final class ServiceTests: XCTestCase {
    /// The live prints of a project view (wire.ProjectLive), as the service sends them.
    private let json = """
    {"live": [
      {"name": "Shoulder", "model": "https://files.example/m.png", "percent": 79.9, "layer": 820,
       "layers": 1036, "left": 5400, "updated": "2026-10-31T21:40:12.123456789Z",
       "printer": "Phenom", "printer_id": "p7k2"},
      {"name": "Bracket", "percent": 100, "result": "complete", "updated": "2026-10-31T20:00:00Z",
       "printer": "TAZ", "printer_id": "t9x1"},
      {"name": "Lid", "percent": 12, "updated": "2026-10-31T21:41:00Z", "printer": "TAZ", "printer_id": "t9x1"}
    ]}
    """

    private struct View: Decodable { var live: [LivePrint] }

    private func live() throws -> [LivePrint] {
        try JSONDecoder().decode(View.self, from: Data(json.utf8)).live
    }

    func testDecode() throws {
        let l = try live()
        XCTAssertEqual(l.count, 3)
        XCTAssertEqual(l[0].percent, 79.9, accuracy: 0.001)
        XCTAssertEqual(l[0].model?.absoluteString, "https://files.example/m.png")
        XCTAssertTrue(l[0].printing)
        XCTAssertFalse(l[1].printing)
        XCTAssertNotNil(l[0].updated)
        XCTAssertNotNil(l[1].updated)
    }

    /// A deck matches by its printer ID, or by its name with no ID. The print that runs comes
    /// before a print that ended.
    func testMatch() throws {
        let l = try live()
        var taz = SavedDeck(name: "TAZ")
        XCTAssertEqual(LivePrint.of(taz, in: l)?.name, "Lid")
        taz.printerID = "t9x1"
        taz.name = "Workhorse"
        XCTAssertEqual(LivePrint.of(taz, in: l)?.name, "Lid")
        let phenom = SavedDeck(name: "phenom")
        XCTAssertEqual(LivePrint.of(phenom, in: l)?.name, "Shoulder")
        XCTAssertNil(LivePrint.of(SavedDeck(name: ""), in: l))
        var other = SavedDeck(name: "TAZ")
        other.printerID = "zzz"
        XCTAssertNil(LivePrint.of(other, in: l))
    }

    func testBase64URL() {
        let d = Data([0xfb, 0xff, 0xfe, 0x01])
        XCTAssertEqual(d.base64URL, "-__-AQ")
        XCTAssertEqual(Data(base64URL: "-__-AQ"), d)
        XCTAssertEqual(Data(base64URL: "E8ooBGkudwMR-qwVrctVAAY_0KIv3LeFOrKYoxNMye4")?.count, 32)
    }

    func testDates() {
        XCTAssertNotNil(ServiceClient.date("2026-10-31T21:40:12Z"))
        XCTAssertNotNil(ServiceClient.date("2026-10-31T21:40:12.5Z"))
        XCTAssertNil(ServiceClient.date("yesterday"))
    }
}
