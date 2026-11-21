// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import XCTest

/// The printers of a deck with many printers, and the fields of /api/plugin/printdeck for them.
final class PrintersTests: XCTestCase {
    /// /slicer/printer.json of a deck with two printers, as slicerPrinterFile writes it.
    private let twoPrinters = #"""
    {"name":"Workshop","kind":"printdeck","url":"","https":"","files":"gcode",
     "printers":[
      {"id":"","name":"TAZ Workhorse","label":"TAZ","color":"#2f6fd0","kind":"fdm","url":"","files":"gcode","state":"ready"},
      {"id":"x2d","name":"Bambu X2D","label":"X2D","color":"#2a9d8f","kind":"fdm","url":"/printers/x2d","files":"gcode.3mf","model":"Bambu Lab X2D","state":"printing","progress":0.4,"left":600},
      {"id":"phenom","name":"Phenom","label":"PHE","color":"#8e5bd0","kind":"resin","url":"/printers/phenom","files":"ctb","model":""}
     ]}
    """#

    func testKeyGrant() throws {
        XCTAssertEqual(try DeckClient.grant(Data(#"{"api_key":"k1","scope":"deck"}"#.utf8)), KeyGrant(key: "k1", deck: true))
        XCTAssertEqual(try DeckClient.grant(Data(#"{"api_key":"k2","scope":"printer"}"#.utf8)), KeyGrant(key: "k2", deck: false))
        // An older deck sends no scope.
        XCTAssertEqual(try DeckClient.grant(Data(#"{"api_key":"k3"}"#.utf8)), KeyGrant(key: "k3", deck: false))
        XCTAssertThrowsError(try DeckClient.grant(Data(#"{"scope":"deck"}"#.utf8)))
    }

    /// Each printer of the list gets an entry, with the address of its API and the deck key.
    func testEntriesOfManyPrinters() throws {
        let list = try JSONDecoder().decode(DeckPrinterList.self, from: Data(twoPrinters.utf8))
        let e = list.entries(address: "192.168.1.50:8080/", key: "dk", name: "printdeck", existing: [])
        XCTAssertEqual(e.map(\.address), ["192.168.1.50:8080", "192.168.1.50:8080/printers/x2d",
                                          "192.168.1.50:8080/printers/phenom"])
        XCTAssertEqual(e.map(\.name), ["TAZ Workhorse", "Bambu X2D", "Phenom"])
        XCTAssertEqual(e.map(\.label), ["TAZ", "X2D", "PHE"])
        XCTAssertEqual(e.map(\.process), [.fdm, .fdm, .resin])
        XCTAssertEqual(e.map(\.model), [nil, "Bambu Lab X2D", nil])
        XCTAssertTrue(e.allSatisfy { $0.key == "dk" && $0.deckKey == true && $0.deckAddress == "192.168.1.50:8080" })
        XCTAssertTrue(e.allSatisfy { $0.deckName == "Workshop" && $0.onSharedDeck })
        XCTAssertEqual(Set(e.map(\.id)).count, 3)
        // The resin printer prints and resumes only from its touch screen.
        XCTAssertFalse(e[2].printsFromApp)
        XCTAssertFalse(e[2].resumesFromApp)
    }

    /// A printer that the app has keeps its ID and its camera, so its status and pairing stay.
    func testEntriesKeepIDs() throws {
        let list = try JSONDecoder().decode(DeckPrinterList.self, from: Data(twoPrinters.utf8))
        var old = SavedDeck(id: "1", name: "My TAZ", settings: DeckSettings(address: "192.168.1.50:8080", key: "old"),
                            camera: "cam2")
        old.printerID = "p-1"
        let e = list.entries(address: "192.168.1.50:8080", key: "dk", name: "", existing: [old])
        XCTAssertEqual(e[0].id, "1")
        XCTAssertEqual(e[0].camera, "cam2")
        XCTAssertEqual(e[0].printerID, "p-1")
        XCTAssertEqual(e[0].key, "dk")
        // The name of a printer comes from the deck.
        XCTAssertEqual(e[0].name, "TAZ Workhorse")
    }

    /// A deck with one printer has no list: one entry, with the name that the user gave it.
    func testEntriesOfOnePrinter() throws {
        let list = try JSONDecoder().decode(DeckPrinterList.self, from: Data(#"{"name":"printdeck","kind":"printdeck","url":""}"#.utf8))
        let e = list.entries(address: "printdeck.local", key: "dk", name: "TAZ", existing: [])
        XCTAssertEqual(e.count, 1)
        XCTAssertEqual(e[0].address, "printdeck.local")
        XCTAssertEqual(e[0].name, "TAZ")
        XCTAssertNil(e[0].printer)
        XCTAssertNil(e[0].label)
        XCTAssertFalse(e[0].onSharedDeck)
        XCTAssertEqual(e[0].deckKey, true)
    }

    func testRoot() {
        XCTAssertEqual(SavedDeck.root("192.168.1.50/printers/x2d"), "192.168.1.50")
        XCTAssertEqual(SavedDeck.root("http://printdeck.local:8080/printers/x2d/"), "http://printdeck.local:8080")
        XCTAssertEqual(SavedDeck.root("192.168.1.50 "), "192.168.1.50")
        let d = SavedDeck(settings: DeckSettings(address: "192.168.1.50/printers/x2d", key: "k"))
        XCTAssertEqual(d.deckRoot, "192.168.1.50")
    }

    /// The API of a printer is under its path. A path from a reply of the deck has that path in
    /// front already.
    func testURLs() {
        let s = DeckSettings(address: "192.168.1.50:8080/printers/x2d", key: "k")
        XCTAssertEqual(s.url("/api/job")?.absoluteString, "http://192.168.1.50:8080/printers/x2d/api/job")
        XCTAssertEqual(s.url("/webcam/snapshot?w=320&view=full")?.absoluteString,
                       "http://192.168.1.50:8080/printers/x2d/webcam/snapshot?w=320&view=full")
        XCTAssertEqual(s.url("/printers/x2d/downloads/timelapse/cube-1.mov")?.absoluteString,
                       "http://192.168.1.50:8080/printers/x2d/downloads/timelapse/cube-1.mov")
        let root = DeckSettings(address: "printdeck.local", key: "k")
        XCTAssertEqual(root.url("/downloads/timelapse/a.mov/still")?.absoluteString,
                       "http://printdeck.local/downloads/timelapse/a.mov/still")
        XCTAssertEqual(root.url("/printers/x2d/api/job")?.absoluteString, "http://printdeck.local/printers/x2d/api/job")
    }

    /// The spools, the nozzles and the plate of an X2D, as servePrintdeck writes them.
    func testPluginOfX2D() throws {
        let json = #"""
        {"process":"fdm","printer_id":"","material":"PLA","diameter":1.75,"spool":null,"spools":null,
         "job":{"file":"cube.gcode.3mf","title":"Cube","state":"printing","layer":3,"layers":50,"progress":0.06,"left":3000,"release":-1,"pos":0},
         "camera":null,"cameras":[],"projects":null,"limits":null,"signed_only":true,
         "filaments":[{"slot":"A1","index":0,"tray":0,"material":"PLA","color":"#898989","name":"Bambu PLA Basic","remain":0.8},
                      {"slot":"A3","index":1,"tray":2,"nozzle":"main","material":"PETG","color":"#161616","remain":null},
                      {"slot":"B2","index":2,"tray":5,"material":"PLA","remain":0.25},
                      {"slot":"Ext","index":3,"tray":255,"nozzle":"aux","material":"PLA","color":"#dcdcdc","remain":null}],
         "nozzles":[{"id":0,"role":"main","diameter":0.4,"type":"HS01"},{"id":1,"role":"aux","diameter":0.4,"type":"HS01"}],
         "plate":"P0101"}
        """#
        let p = try JSONDecoder().decode(PluginReply.self, from: Data(json.utf8))
        let f = try XCTUnwrap(p.filaments)
        XCTAssertEqual(f.map(\.slot), ["A1", "A3", "B2", "Ext"])
        XCTAssertEqual(f[0].remain, 0.8)
        XCTAssertNil(f[1].remain)
        XCTAssertNil(f[0].nozzle)
        XCTAssertEqual(f[3].nozzle, "aux")
        XCTAssertEqual(f[0].name, "Bambu PLA Basic")
        XCTAssertNil(f[2].color)
        XCTAssertEqual(f.map(\.inAMS), [true, true, true, false])
        let units = Filament.units(f)
        XCTAssertEqual(units.map(\.unit), ["A", "B"])
        XCTAssertEqual(units[0].trays.map(\.slot), ["A1", "A3"])
        XCTAssertEqual(p.nozzles?.map(\.name), ["Nozzle", "Aux"])
        XCTAssertEqual(p.plate, "P0101")
        XCTAssertEqual(p.signed_only, true)
        XCTAssertEqual(p.job?.release, -1)
    }

    /// A Klipper printer tells its roll with no slot. An older deck sends none of the new fields.
    func testPluginOfKlipper() throws {
        let p = try JSONDecoder().decode(PluginReply.self, from: Data(#"""
        {"process":"fdm","filaments":[{"slot":"","index":0,"tray":-1,"material":"PLA","color":"#ff0000","remain":0.5}],
         "job":{"file":"a.gcode","state":"paused","layer":1,"layers":2,"progress":0.5,"left":-1,"release":120}}
        """#.utf8))
        XCTAssertEqual(p.filaments?.first?.inAMS, false)
        XCTAssertEqual(p.job?.release, 120)
        XCTAssertNil(p.nozzles)
        let old = try JSONDecoder().decode(PluginReply.self, from: Data(#"{"spool":null,"job":null}"#.utf8))
        XCTAssertNil(old.filaments)
        XCTAssertNil(old.signed_only)
    }

    /// A status in the store from an older app has none of the new fields.
    func testOldStoredStatus() throws {
        let st = try JSONDecoder().decode(Status.self, from: Data(#"{"state":"Printing","busy":true,"paused":false,"time":0}"#.utf8))
        XCTAssertNil(st.filaments)
        XCTAssertNil(st.release)
    }
}
