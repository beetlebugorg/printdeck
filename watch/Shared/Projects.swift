// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import Foundation

// The view of a project at the service (wire.ProjectView in deck/internal/cloud/wire), with the
// fields of the overview of the web page. A missing field is nil.

struct ProjectDetail: Decodable {
    var id: String?
    var name: String
    var slug: String?
    var notes: String?
    var notesBy: String?
    var notesAt: Date?
    var isOwner: Bool?
    var prints: [ProjectPrintItem]?
    var photos: [ProjectPhotoItem]?
    var plans: [ProjectPlanItem]?
    var live: [LivePrint]?
    var queue: [ProjectQueuedItem]?

    enum CodingKeys: String, CodingKey {
        case id, name, slug, notes, prints, photos, plans, live, queue
        case notesBy = "notes_by", notesAt = "notes_at", isOwner = "is_owner"
    }

    var owner: Bool { isOwner ?? false }

    /// The prints that are ready to pick up: Ready, and not delivered, and no test print.
    var readyForPickup: [ProjectPrintItem] {
        (prints ?? []).filter { $0.ready != nil && $0.delivered == nil && $0.test == nil }
    }

    /// The prints, the newest end first.
    var newest: [ProjectPrintItem] {
        (prints ?? []).sorted { ($0.ended ?? .distantPast) > ($1.ended ?? .distantPast) }
    }

    /// The photos of a print, the newest first.
    func photos(of print: String) -> [ProjectPhotoItem] {
        (photos ?? []).filter { $0.print == print }.sorted { ($0.added ?? .distantPast) > ($1.added ?? .distantPast) }
    }

    /// The plates of the builds of the plans that wait for the approval of the owner, and the
    /// approved plates with a file that no printer has yet.
    var platesToDecide: [PlateRef] {
        var out: [PlateRef] = []
        for plan in plans ?? [] {
            for b in plan.builds ?? [] {
                for (i, p) in (b.plates ?? []).enumerated() {
                    let waiting = p.state == "waiting"
                    let toSend = p.state == "approved" && p.file != nil && p.sentTo == nil
                    if waiting || toSend { out.append(PlateRef(plan: plan, build: b, index: i, plate: p)) }
                }
            }
        }
        return out
    }

    /// What happened in the project, the newest first, as the feed of the web page: the prints,
    /// the marks, the photos of one person on one day, the plans, the slices, the decisions on the
    /// plates and the notes.
    var feed: [FeedEvent] {
        var evs: [FeedEvent] = []
        for p in prints ?? [] {
            let sub = [Format.duration(Double(p.seconds ?? 0)), p.printer ?? ""].filter { !$0.isEmpty }.joined(separator: " · ")
            switch p.result {
            case "complete": evs.append(FeedEvent(at: p.ended, text: "Finished " + p.name, sub: sub))
            case "cancelled": evs.append(FeedEvent(at: p.ended, text: "Cancelled " + p.name, sub: sub))
            default: evs.append(FeedEvent(at: p.ended, text: p.name + " stopped", sub: sub))
            }
            if let r = p.ready { evs.append(FeedEvent(at: r, text: p.name + " is ready to pick up", sub: p.readyNote ?? "")) }
            if let d = p.delivered { evs.append(FeedEvent(at: d, text: "Delivered " + p.name, sub: "")) }
        }
        let photos = (self.photos ?? []).sorted { ($0.added ?? .distantPast) > ($1.added ?? .distantPast) }
        var i = 0
        let cal = Calendar.current
        while i < photos.count {
            var j = i + 1
            while j < photos.count, photos[j].by == photos[i].by,
                  let a = photos[j].added, let b = photos[i].added, cal.isDate(a, inSameDayAs: b) {
                j += 1
            }
            let n = j - i
            evs.append(FeedEvent(at: photos[i].added, text: "\(photos[i].by ?? "Someone") added \(n) photo\(n == 1 ? "" : "s")", sub: ""))
            i = j
        }
        for plan in plans ?? [] {
            evs.append(FeedEvent(at: plan.created, text: "New plan · " + plan.title, sub: String((plan.commit ?? "").prefix(7))))
            for b in plan.builds ?? [] {
                let n = b.plates?.count ?? 0
                evs.append(FeedEvent(at: b.created, text: "Slice of \(n) plate\(n == 1 ? "" : "s") · " + plan.title,
                                     sub: b.stateText.lowercased()))
                for (k, pl) in (b.plates ?? []).enumerated() {
                    if pl.state == "approved" {
                        evs.append(FeedEvent(at: pl.at, text: "\(pl.by ?? "") approved plate \(k + 1) · " + plan.title, sub: ""))
                    } else if pl.state == "rejected" {
                        evs.append(FeedEvent(at: pl.at, text: "\(pl.by ?? "") rejected plate \(k + 1) · " + plan.title, sub: pl.reason ?? ""))
                    }
                }
            }
        }
        if let at = notesAt { evs.append(FeedEvent(at: at, text: (notesBy ?? "") + " changed the notes", sub: "")) }
        return Array(evs.filter { $0.at != nil }.sorted { $0.at! > $1.at! }.prefix(12))
    }
}

/// A line of the feed of a project.
struct FeedEvent: Identifiable {
    var at: Date?
    var text: String
    var sub: String
    let id = UUID()
}

/// A print of a project (wire.ProjectPrint).
struct ProjectPrintItem: Decodable, Identifiable {
    var id: String
    var n: Int?
    var name: String
    var notes: String?
    var notesBy: String?
    var delivered: Date?
    var source: String?
    var ready: Date?
    var readyNote: String?
    var test: Date?
    var file: String?
    var result: String?
    var ended: Date?
    var seconds: Int?
    var material: String?
    var grams: Double?
    var printer: String?
    var model: String?

    enum CodingKeys: String, CodingKey {
        case id, n, name, notes, delivered, source, ready, test, file, result, ended, seconds, material, grams,
             printer, model
        case notesBy = "notes_by", readyNote = "ready_note"
    }

    var modelURL: URL? { model.flatMap { URL(string: $0, relativeTo: ServiceClient.base)?.absoluteURL } }

    var resultText: String {
        switch result {
        case "complete": "Done"
        case "cancelled": "Cancelled"
        default: "Failed"
        }
    }
}

/// A photo of a project (wire.ProjectPhoto).
struct ProjectPhotoItem: Decodable, Identifiable {
    var id: String
    var by: String?
    var added: Date?
    var url: String?
    var small: String?
    var print: String?

    var smallURL: URL? { (small ?? url).flatMap { URL(string: $0, relativeTo: ServiceClient.base)?.absoluteURL } }
}

/// A plan of a project with its builds (wire.ProjectPlan).
struct ProjectPlanItem: Decodable, Identifiable {
    var id: String
    var commit: String?
    var kind: String?
    var pr: Int?
    var tag: String?
    var branch: String?
    var created: Date?
    var builds: [ProjectBuildItem]?

    /// The short name of the plan: "PR #7", "v1.2", or the branch.
    var title: String {
        switch kind {
        case "test": "PR #\(pr ?? 0)"
        case "release": tag ?? ""
        default: branch ?? ""
        }
    }
}

/// A build of a plan (wire.ProjectBuild).
struct ProjectBuildItem: Decodable, Identifiable {
    var id: String
    var created: Date?
    var state: String?
    var plates: [ProjectPlateItem]?

    var stateText: String {
        switch state {
        case "slicing": "Slicing"
        case "ready": "Ready"
        case "failed": "Failed"
        default: state ?? ""
        }
    }
}

/// A plate of a build (wire.ProjectBuildPlate).
struct ProjectPlateItem: Decodable {
    var printer: String?
    var process: String?
    var materialName: String?
    var file: String?
    var seconds: Int?
    var grams: Double?
    var ml: Double?
    var state: String?
    var by: String?
    var at: Date?
    var reason: String?
    var pictureURL: String?
    var sentTo: String?

    enum CodingKeys: String, CodingKey {
        case printer, process, file, seconds, grams, ml, state, by, at, reason
        case materialName = "material_name", pictureURL = "picture_url", sentTo = "sent_to"
    }

    var picture: URL? { pictureURL.flatMap { URL(string: $0, relativeTo: ServiceClient.base)?.absoluteURL } }
}

/// A plate and its place: the plan, the build and the index of the plate in the build.
struct PlateRef: Identifiable {
    var plan: ProjectPlanItem
    var build: ProjectBuildItem
    var index: Int
    var plate: ProjectPlateItem
    var id: String { build.id + "/" + String(index) }
}

/// A file of the project in the queue of a printer (wire.ProjectQueued).
struct ProjectQueuedItem: Decodable, Identifiable {
    var printer: String
    var name: String
    var place: Int
    var seconds: Int?
    var start: Date?
    var id: String { printer + "/" + String(place) + "/" + name }
}

/// A printer at the service, with its process, for Send of a plate.
struct ServicePrinterInfo: Decodable, Identifiable {
    var id: String
    var name: String
    var process: String?
}

extension ServiceClient {
    func project(_ id: String) async throws -> ProjectDetail {
        try await get("projects/" + id)
    }

    /// Changes a print: ready, delivered, test, title, notes or source. A member changes only the
    /// notes.
    func editPrint(project: String, print: String, _ fields: [String: Any]) async throws {
        _ = try await send("projects/\(project)/prints/\(print)", method: "PATCH",
                           body: JSONSerialization.data(withJSONObject: fields))
    }

    /// Approves a plate, or rejects it with a reason.
    func decide(project: String, build: String, plate: Int, approve: Bool, reason: String = "") async throws {
        let body: [String: Any] = approve ? [:] : ["reason": reason]
        _ = try await send("projects/\(project)/builds/\(build)/plates/\(plate)/\(approve ? "approve" : "reject")",
                           method: "POST", body: JSONSerialization.data(withJSONObject: body))
    }

    /// Sends an approved plate to the queue of a printer of the same process.
    func sendPlate(project: String, build: String, plate: Int, printer: String) async throws {
        _ = try await send("projects/\(project)/builds/\(build)/plates/\(plate)/send", method: "POST",
                           body: JSONSerialization.data(withJSONObject: ["printer": printer]))
    }

    /// The printers of the owner.
    func printerList() async throws -> [ServicePrinterInfo] {
        try await get("printers")
    }

    /// Adds a photo to a project, and to a print, or to none: a grant, the upload of the JPEG to
    /// the grant, then done.
    func addPhoto(project: String, jpeg: Data, print: String?) async throws {
        struct Reply: Decodable {
            struct Grant: Decodable {
                var url: String
                var fields: [String: String]
                var prefix: String
            }
            var photo: String
            var grant: Grant
        }
        let r: Reply
        do {
            let data = try await send("projects/\(project)/photos", method: "POST", body: Data("{}".utf8))
            r = try JSONDecoder().decode(Reply.self, from: data)
        } catch let e as ServiceError {
            throw e
        } catch {
            throw ServiceError.badReply
        }
        guard let url = URL(string: r.grant.url) else { throw ServiceError.badReply }
        try await Self.upload(url: url, fields: r.grant.fields, key: r.grant.prefix + "photo", jpeg: jpeg)
        let done: [String: Any] = print.map { ["print": $0] } ?? [:]
        _ = try await send("projects/\(project)/photos/\(r.photo)/done", method: "POST",
                           body: JSONSerialization.data(withJSONObject: done))
    }

    /// Posts a JPEG to a grant as a multipart form: the fields of the grant, the key, the type
    /// and the cache control, then the file.
    static func upload(url: URL, fields: [String: String], key: String, jpeg: Data) async throws {
        let boundary = "printdeck-" + UUID().uuidString
        var body = Data()
        func field(_ name: String, _ value: String) {
            body.append(Data("--\(boundary)\r\nContent-Disposition: form-data; name=\"\(name)\"\r\n\r\n\(value)\r\n".utf8))
        }
        for (k, v) in fields.sorted(by: { $0.key < $1.key }) { field(k, v) }
        field("key", key)
        field("Content-Type", "image/jpeg")
        field("Cache-Control", "no-store")
        body.append(Data("--\(boundary)\r\nContent-Disposition: form-data; name=\"file\"; filename=\"photo.jpg\"\r\nContent-Type: image/jpeg\r\n\r\n".utf8))
        body.append(jpeg)
        body.append(Data("\r\n--\(boundary)--\r\n".utf8))
        var req = URLRequest(url: url)
        req.httpMethod = "POST"
        req.setValue("multipart/form-data; boundary=\(boundary)", forHTTPHeaderField: "Content-Type")
        req.timeoutInterval = 60
        let (_, resp) = try await URLSession.shared.upload(for: req, from: body)
        let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(code) else { throw ServiceError.http(code, "The upload of the photo failed (HTTP \(code)).") }
    }
}
