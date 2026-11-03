// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import XCTest

final class ProjectTests: XCTestCase {
    /// A project view (wire.ProjectView) with the fields of the overview.
    private let json = """
    {"id": "pj1", "name": "Bittlex", "slug": "bittlex", "is_owner": true,
     "notes": "Paint after", "notes_by": "Sam", "notes_at": "2026-10-30T10:00:00Z",
     "prints": [
       {"id": "a", "n": 1, "name": "Shoulder", "result": "complete", "ended": "2026-10-31T12:00:00Z",
        "seconds": 3600, "printer": "Phenom", "ready": "2026-10-31T13:00:00Z", "ready_note": "On the shelf"},
       {"id": "b", "n": 2, "name": "Hip", "result": "complete", "ended": "2026-10-31T14:00:00.5Z",
        "ready": "2026-10-31T15:00:00Z", "delivered": "2026-10-31T16:00:00Z"},
       {"id": "c", "n": 3, "name": "Fit test", "result": "cancelled", "ended": "2026-10-29T09:00:00Z",
        "ready": "2026-10-31T15:00:00Z", "test": "2026-10-31T15:00:00Z"},
       {"id": "d", "n": 4, "name": "Elbow", "result": "complete", "ended": "2026-10-29T08:00:00Z",
        "ready": "2026-10-31T15:00:00Z", "discarded": "2026-10-31T19:00:00Z", "discard_note": "Warped corner"}
     ],
     "photos": [
       {"id": "p1", "by": "Sam", "added": "2026-10-31T13:10:00Z", "url": "/f/1.jpg", "small": "/f/1s.jpg", "print": "a"},
       {"id": "p2", "by": "Sam", "added": "2026-10-31T13:20:00Z", "url": "/f/2.jpg", "small": "/f/2s.jpg"}
     ],
     "plans": [{"id": "pl1", "commit": "abcdef123456", "kind": "test", "pr": 7, "created": "2026-10-30T08:00:00Z",
       "builds": [{"id": "bd1", "created": "2026-10-30T09:00:00Z", "state": "ready", "plates": [
         {"printer": "Phenom", "process": "resin", "file": "plate-1.ctb", "state": "waiting", "seconds": 7200},
         {"printer": "TAZ", "process": "fdm", "file": "plate-2.gcode", "state": "approved", "by": "Jeremy",
          "at": "2026-10-30T11:00:00Z"},
         {"printer": "TAZ", "process": "fdm", "file": "plate-3.gcode", "state": "approved", "sent_to": "TAZ",
          "at": "0001-01-01T00:00:00Z"},
         {"printer": "TAZ", "process": "fdm", "state": "rejected", "by": "Jeremy", "reason": "Too thin",
          "at": "2026-10-30T11:30:00Z"}]}]}],
     "live": [{"name": "Pod door", "percent": 41, "printer": "TAZ", "printer_id": "t9x1"}],
     "queue": [{"printer": "TAZ", "name": "Lid", "place": 1, "seconds": 2700, "start": "2026-10-31T18:00:00Z",
                "updated": "2026-10-31T17:00:00Z"}]
    }
    """

    private func view() throws -> ProjectDetail {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .custom { dec in
            let s = try dec.singleValueContainer().decode(String.self)
            guard let date = ServiceClient.date(s) else { throw DecodingError.dataCorrupted(.init(codingPath: [], debugDescription: s)) }
            return date
        }
        return try d.decode(ProjectDetail.self, from: Data(json.utf8))
    }

    func testDecode() throws {
        let v = try view()
        XCTAssertTrue(v.owner)
        XCTAssertEqual(v.prints?.count, 4)
        XCTAssertEqual(v.prints?[3].discardNote, "Warped corner")
        XCTAssertNotNil(v.prints?[3].discarded)
        XCTAssertEqual(v.live?.first?.name, "Pod door")
        XCTAssertEqual(v.queue?.first?.place, 1)
        XCTAssertEqual(v.plans?.first?.title, "PR #7")
        XCTAssertEqual(v.photos(of: "a").first?.smallURL?.absoluteString, ServiceClient.base.absoluteString + "/f/1s.jpg")
    }

    /// Ready for pickup: Ready, with no Delivered, no Test and no Rejected.
    func testPickup() throws {
        XCTAssertEqual(try view().readyForPickup.map(\.id), ["a"])
    }

    func testNewest() throws {
        XCTAssertEqual(try view().newest.map(\.id), ["b", "a", "c", "d"])
    }

    /// The plates that wait, and the approved plates with a file that no printer has.
    func testPlatesToDecide() throws {
        XCTAssertEqual(try view().platesToDecide.map(\.index), [0, 1])
    }

    func testFeed() throws {
        let feed = try view().feed
        XCTAssertEqual(feed.first?.text, "Rejected Elbow")
        XCTAssertEqual(feed.first?.sub, "Warped corner")
        XCTAssertTrue(feed.contains { $0.text == "Sam added 2 photos" })
        XCTAssertTrue(feed.contains { $0.text == "Jeremy rejected plate 4 · PR #7" && $0.sub == "Too thin" })
        XCTAssertTrue(feed.contains { $0.text == "Sam changed the notes" })
        XCTAssertLessThanOrEqual(feed.count, 12)
    }
}
