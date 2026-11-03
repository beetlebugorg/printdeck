// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

/// The Projects tab: a card for each project of the session, with what it does now: the prints
/// that run, the prints ready for pickup, the plates that wait, and the files in the queues. A tap
/// opens the project. With no session, the tab has the sign-in.
struct ProjectsHome: View {
    @Environment(\.colorScheme) private var scheme
    @Environment(\.scenePhase) private var phase
    @State private var account = Account.shared

    var body: some View {
        let p = Palette.of(scheme)
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                Heading(title: "Projects")
                    .padding(.top, 8)
                if account.signedIn {
                    if account.projects.isEmpty {
                        Note(text: account.message ?? (account.liveTime == nil ? "Reading the projects" : "No projects"))
                    }
                    ForEach(account.projects) { pr in
                        NavigationLink(value: ProjectRoute(id: pr.id)) {
                            ProjectCard(project: pr, view: account.details[pr.id])
                        }
                        .buttonStyle(.plain)
                    }
                }
                if account.signedIn {
                    SignedInLine()
                } else {
                    AccountPlate()
                }
            }
            .padding(16)
        }
        .background(p.bg.ignoresSafeArea())
        .environment(\.palette, p)
        .toolbar(.hidden, for: .navigationBar)
        .navigationDestination(for: ProjectRoute.self) { r in
            ProjectScreen(id: r.id)
                .environment(\.palette, p)
        }
        .task(id: phase == .active && account.signedIn) {
            while phase == .active && account.signedIn && !Task.isCancelled {
                await account.refresh()
                try? await Task.sleep(for: .seconds(20))
            }
        }
    }
}

/// The session in one line, with Sign out.
private struct SignedInLine: View {
    @Environment(\.palette) private var p
    @State private var account = Account.shared

    var body: some View {
        HStack {
            Text(((account.session?.name ?? "Signed in") + " · " + (account.session?.kind ?? "")).uppercased())
                .font(Theme.plex(12))
                .foregroundStyle(p.dim)
                .lineLimit(1)
            Spacer()
            SmallKey(title: "Sign out", cap: .danger) { account.signOut() }
        }
        .padding(.horizontal, 4)
        .padding(.top, 8)
    }
}

/// The value of a navigation to a project.
struct ProjectRoute: Hashable {
    var id: String
}

/// The card of a project: its picture, its name, and a line of what it does now.
private struct ProjectCard: View {
    @Environment(\.palette) private var p
    let project: ServiceProject
    let view: ProjectDetail?

    var body: some View {
        HStack(spacing: 12) {
            Thumb(url: project.coverURL, side: 64)
            VStack(alignment: .leading, spacing: 4) {
                Text(project.name)
                    .font(Theme.plex(17))
                    .foregroundStyle(p.text)
                    .lineLimit(1)
                Text(summary.isEmpty ? "Nothing waits" : summary)
                    .font(Theme.plex(12, bold: false))
                    .foregroundStyle(summary.isEmpty ? p.dim : p.text)
                    .lineLimit(2)
                if let l = view?.live?.first(where: \.printing) {
                    Playhead(value: l.percent / 100)
                        .padding(.top, 2)
                }
            }
            Spacer(minLength: 0)
            Image(systemName: "chevron.right")
                .foregroundStyle(p.dim)
        }
        .padding(12)
        .background(RoundedRectangle(cornerRadius: 5).fill(p.surface))
        .overlay(RoundedRectangle(cornerRadius: 5).stroke(p.edge, lineWidth: 1))
        .contentShape(Rectangle())
    }

    /// "1 printing · 2 ready · 1 plate waits · 3 queued".
    private var summary: String {
        guard let v = view else { return "" }
        let printing = (v.live ?? []).filter(\.printing).count
        let ready = v.readyForPickup.count
        let plates = v.owner ? v.platesToDecide.filter { $0.plate.state == "waiting" }.count : 0
        let queued = v.queue?.count ?? 0
        var parts: [String] = []
        if printing > 0 { parts.append("\(printing) printing") }
        if ready > 0 { parts.append("\(ready) ready") }
        if plates > 0 { parts.append("\(plates) plate\(plates == 1 ? "" : "s") wait\(plates == 1 ? "s" : "")") }
        if queued > 0 { parts.append("\(queued) queued") }
        return parts.joined(separator: " · ")
    }
}

/// The overview of a project, as the web page has it: what prints now and the plates that wait,
/// the prints that are ready to pick up, the files in the queues, the newest prints, and what
/// happened.
struct ProjectScreen: View {
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    @Environment(\.scenePhase) private var phase
    let id: String

    @State private var view: ProjectDetail?
    @State private var message: String?
    @State private var camera = false
    @State private var web: URL?

    private var client: ServiceClient { ServiceClient(token: Account.shared.session?.token) }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                Heading(title: view?.name ?? "Project") {
                    HStack(spacing: 8) {
                        SmallKey(title: "Photos") { camera = true }
                        SmallKey(title: "Back") { dismiss() }
                    }
                }
                .padding(.top, 8)
                if let message { Note(text: message) }
                if let v = view {
                    now(v)
                    pickup(v)
                    queue(v)
                    newest(v)
                    feed(v)
                }
            }
            .padding(16)
        }
        .background(p.bg.ignoresSafeArea())
        .toolbar(.hidden, for: .navigationBar)
        .navigationDestination(for: ProjectPrintRoute.self) { r in
            PrintScreen(project: id, printID: r.print) { await load() }
        }
        .task(id: phase == .active) {
            while phase == .active && !Task.isCancelled {
                await load()
                try? await Task.sleep(for: .seconds(15))
            }
        }
        .fullScreenCover(isPresented: $camera) {
            PhotoCamera(project: id, print: nil) { camera = false }
                .environment(\.palette, p)
        }
        .sheet(item: $web) { url in ServiceWebView(url: url) }
    }

    private func load() async {
        if view == nil { view = Account.shared.details[id] }
        do {
            view = try await client.project(id)
            message = nil
        } catch {
            message = errorText(error)
        }
    }

    // MARK: Sections

    @ViewBuilder
    private func now(_ v: ProjectDetail) -> some View {
        let live = v.live ?? []
        let plates = v.owner ? v.platesToDecide : []
        if !live.isEmpty || !plates.isEmpty {
            Plate(label: "Now") {
                ForEach(Array(live.enumerated()), id: \.offset) { _, l in
                    VStack(alignment: .leading, spacing: 6) {
                        Row(title: l.name, detail: [l.printer, l.paused ? "Paused" : l.printing ? "Printing" : l.result]
                            .filter { !$0.isEmpty }.joined(separator: " · "),
                            value: Format.percent(l.percent / 100))
                        if l.printing { Playhead(value: l.percent / 100) }
                    }
                }
                ForEach(plates) { ref in
                    PlateDecision(project: id, ref: ref) { await load() } open: {
                        web = Self.buildPage(v, ref.build.id)
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func pickup(_ v: ProjectDetail) -> some View {
        let ready = v.readyForPickup
        if !ready.isEmpty {
            Plate(label: "Ready for pickup") {
                ForEach(ready) { pr in
                    HStack(spacing: 10) {
                        NavigationLink(value: ProjectPrintRoute(print: pr.id)) {
                            HStack(spacing: 10) {
                                Thumb(url: v.photos(of: pr.id).first?.smallURL ?? pr.modelURL, side: 44)
                                Row(title: pr.name, detail: pr.readyNote ?? "", value: "")
                            }
                        }
                        .buttonStyle(.plain)
                        if v.owner {
                            SmallKey(title: "Delivered") {
                                Task {
                                    await act { try await client.editPrint(project: id, print: pr.id, ["delivered": true]) }
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func queue(_ v: ProjectDetail) -> some View {
        let q = v.queue ?? []
        if !q.isEmpty {
            Plate(label: "In the queues") {
                ForEach(q) { f in
                    Row(title: f.name,
                        detail: ["\(f.printer) · place \(f.place)",
                                 f.seconds.map { Format.duration(Double($0)) } ?? "",
                                 f.start.map { "starts " + LCDModel.clock($0) } ?? ""]
                            .filter { !$0.isEmpty }.joined(separator: " · "),
                        value: "")
                }
            }
        }
    }

    @ViewBuilder
    private func newest(_ v: ProjectDetail) -> some View {
        let prints = Array(v.newest.prefix(12))
        if !prints.isEmpty {
            Plate(label: "Newest prints") {
                ForEach(prints) { pr in
                    NavigationLink(value: ProjectPrintRoute(print: pr.id)) {
                        HStack(spacing: 10) {
                            Thumb(url: v.photos(of: pr.id).first?.smallURL ?? pr.modelURL, side: 44)
                            Row(title: pr.name,
                                detail: [pr.resultText, pr.ended.map { $0.formatted(.relative(presentation: .named)) } ?? "",
                                         pr.printer ?? ""].filter { !$0.isEmpty }.joined(separator: " · "),
                                value: pr.discarded != nil ? "Rejected" : "", alert: pr.discarded != nil)
                        }
                    }
                    .buttonStyle(.plain)
                }
            }
        }
    }

    @ViewBuilder
    private func feed(_ v: ProjectDetail) -> some View {
        let evs = v.feed
        if !evs.isEmpty {
            Plate(label: "What happened") {
                ForEach(evs) { e in
                    Row(title: e.text, detail: e.sub,
                        value: e.at.map { $0.formatted(.relative(presentation: .named)) } ?? "")
                }
            }
        }
    }

    private func act(_ f: () async throws -> Void) async {
        do {
            try await f()
            await load()
        } catch {
            message = errorText(error)
        }
    }

    /// The build page of the web site, with the 3D preview of the plates.
    static func buildPage(_ v: ProjectDetail, _ build: String) -> URL? {
        guard let slug = v.slug ?? v.id else { return nil }
        return ServiceClient.base.appending(path: "/projects/\(slug)/builds/\(build)")
    }
}

/// The value of a navigation to the page of a print.
struct ProjectPrintRoute: Hashable {
    var print: String
}

/// A plate that waits for the approval of the owner, or an approved plate to send: its picture,
/// its printer and material, Approve and Reject with a reason, or Send to a printer of the same
/// process.
private struct PlateDecision: View {
    @Environment(\.palette) private var p
    let project: String
    let ref: PlateRef
    let changed: () async -> Void
    let open: () -> Void

    @State private var rejecting = false
    @State private var reason = ""
    @State private var printers: [ServicePrinterInfo]?
    @State private var busy = false
    @State private var message: String?

    private var client: ServiceClient { ServiceClient(token: Account.shared.session?.token) }

    var body: some View {
        let pl = ref.plate
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 10) {
                Button { open() } label: { Thumb(url: pl.picture, side: 44) }
                    .buttonStyle(.plain)
                Row(title: "Plate \(ref.index + 1) · " + ref.plan.title,
                    detail: [pl.printer ?? "", pl.materialName ?? "",
                             pl.seconds.map { Format.duration(Double($0)) } ?? ""].filter { !$0.isEmpty }.joined(separator: " · "),
                    value: "")
            }
            if pl.state == "waiting" {
                if rejecting {
                    PanelField(label: "Reason", prompt: "Why the plate is wrong", text: $reason)
                    HStack(spacing: 8) {
                        SmallKey(title: "Reject", cap: .danger) { Task { await decide(approve: false) } }
                            .disabled(busy || reason.trimmingCharacters(in: .whitespaces).isEmpty)
                        SmallKey(title: "Back") { rejecting = false }
                    }
                } else {
                    HStack(spacing: 8) {
                        SmallKey(title: "Approve", cap: .orange) { Task { await decide(approve: true) } }
                        SmallKey(title: "Reject", cap: .danger) { rejecting = true }
                        SmallKey(title: "3D") { open() }
                    }
                    .disabled(busy)
                }
            } else if let printers {
                let same = printers.filter { $0.process == nil || $0.process == pl.process }
                if same.isEmpty { Note(text: "No printer for \(pl.process ?? "this process")") }
                HStack(spacing: 8) {
                    ForEach(same) { pr in
                        SmallKey(title: pr.name, cap: .orange) { Task { await send(pr) } }
                            .disabled(busy)
                    }
                }
            } else {
                HStack(spacing: 8) {
                    SmallKey(title: "Send", cap: .orange) { Task { await loadPrinters() } }
                        .disabled(busy)
                    SmallKey(title: "3D") { open() }
                }
            }
            if let message { Note(text: message) }
        }
        .padding(.vertical, 4)
    }

    private func decide(approve: Bool) async {
        busy = true
        defer { busy = false }
        do {
            try await client.decide(project: project, build: ref.build.id, plate: ref.index, approve: approve,
                                    reason: reason.trimmingCharacters(in: .whitespaces))
            await changed()
        } catch {
            message = errorText(error)
        }
    }

    private func loadPrinters() async {
        busy = true
        defer { busy = false }
        do {
            printers = try await client.printerList()
        } catch {
            message = errorText(error)
        }
    }

    private func send(_ printer: ServicePrinterInfo) async {
        busy = true
        defer { busy = false }
        do {
            try await client.sendPlate(project: project, build: ref.build.id, plate: ref.index, printer: printer.id)
            message = "Sent to \(printer.name). The deck adds it to its queue."
            await changed()
        } catch {
            message = errorText(error)
        }
    }
}

/// The page of a print: the result and the facts, the marks of the owner, the notes, the source,
/// and the photos, with Take photos for the print.
struct PrintScreen: View {
    @Environment(\.palette) private var p
    @Environment(\.dismiss) private var dismiss
    let project: String
    let printID: String
    let changed: () async -> Void

    @State private var view: ProjectDetail?
    @State private var notes = ""
    @State private var title = ""
    @State private var source = ""
    @State private var message: String?
    @State private var camera = false
    @State private var web: URL?
    /// The note box of Reject is open.
    @State private var discarding = false
    @State private var discardNote = ""

    private var client: ServiceClient { ServiceClient(token: Account.shared.session?.token) }
    private var print: ProjectPrintItem? { view?.prints?.first { $0.id == printID } }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                Heading(title: print?.name ?? "Print") {
                    HStack(spacing: 8) {
                        SmallKey(title: "Photos") { camera = true }
                        SmallKey(title: "Back") { dismiss() }
                    }
                }
                .padding(.top, 8)
                if let message { Note(text: message) }
                if let v = view, let pr = print {
                    facts(pr)
                    if v.owner { marks(pr) }
                    Plate(label: "Notes") {
                        TextEditor(text: $notes)
                            .font(Theme.plex(15, bold: false))
                            .frame(minHeight: 90)
                            .scrollContentBackground(.hidden)
                            .padding(6)
                            .background(RoundedRectangle(cornerRadius: 4).fill(p.raised))
                        Button("Save the notes") { Task { await save(["notes": notes]) } }
                            .buttonStyle(KeyStyle())
                            .disabled(notes == (pr.notes ?? ""))
                    }
                    if v.owner {
                        Plate(label: "Title and source") {
                            PanelField(label: "Title", prompt: pr.name, text: $title)
                            PanelField(label: "Source", prompt: "https://github.com/…", text: $source, keyboard: .URL)
                            Button("Save") { Task { await save(["title": title, "source": source]) } }
                                .buttonStyle(KeyStyle())
                        }
                    }
                    photos(v)
                    if let slug = v.slug ?? v.id, let n = pr.n {
                        Button("Open on the web") { web = ServiceClient.base.appending(path: "/projects/\(slug)/\(n)") }
                            .buttonStyle(KeyStyle())
                    }
                }
            }
            .padding(16)
        }
        .background(p.bg.ignoresSafeArea())
        .toolbar(.hidden, for: .navigationBar)
        .task { await load() }
        .fullScreenCover(isPresented: $camera) {
            PhotoCamera(project: project, print: printID) {
                camera = false
                Task { await load() }
            }
            .environment(\.palette, p)
        }
        .sheet(item: $web) { url in ServiceWebView(url: url) }
    }

    private func facts(_ pr: ProjectPrintItem) -> some View {
        Plate(label: "Print") {
            DisplayWindow {
                WindowRow(label: "Result", value: pr.resultText)
                if let e = pr.ended { WindowRow(label: "Ended", value: e.formatted(date: .abbreviated, time: .shortened)) }
                if let s = pr.seconds { WindowRow(label: "Time", value: Format.duration(Double(s))) }
                if let pn = pr.printer { WindowRow(label: "Printer", value: pn) }
                if let m = pr.material { WindowRow(label: "Material", value: m) }
                if let g = pr.grams, g > 0 { WindowRow(label: "Grams", value: String(format: "%.0f g", g)) }
            }
            if let d = pr.discarded {
                Text("Rejected " + d.formatted(date: .abbreviated, time: .omitted)
                     + ((pr.discardNote ?? "").isEmpty ? "" : " · " + (pr.discardNote ?? "")))
                    .font(Theme.plex(13))
                    .foregroundStyle(p.red)
                    .padding(.horizontal, 4)
            }
        }
    }

    private func marks(_ pr: ProjectPrintItem) -> some View {
        Plate(label: "Marks") {
            HStack(spacing: 6) {
                Button("Ready") { Task { await save(["ready": pr.ready == nil]) } }
                    .buttonStyle(KeyStyle(led: pr.ready != nil))
                Button("Delivered") { Task { await save(["delivered": pr.delivered == nil]) } }
                    .buttonStyle(KeyStyle(led: pr.delivered != nil))
                Button("Test") { Task { await save(["test": pr.test == nil]) } }
                    .buttonStyle(KeyStyle(led: pr.test != nil))
                // Reject asks for the note of why. A tap on a rejected print clears the mark and the note.
                // The API field names are from the earlier mark Discard.
                Button("Reject") {
                    if pr.discarded != nil {
                        Task { await save(["discard": false]) }
                    } else {
                        discardNote = ""
                        discarding.toggle()
                    }
                }
                .buttonStyle(KeyStyle(cap: .danger, led: pr.discarded != nil))
            }
            .environment(\.panelScale, 0.7)
            if discarding && pr.discarded == nil {
                PanelField(label: "Why", prompt: "Warped corner, or it does not fit", text: $discardNote)
                Button("Reject the print") {
                    discarding = false
                    Task { await save(["discard": true, "discard_note": discardNote.trimmingCharacters(in: .whitespaces)]) }
                }
                .buttonStyle(KeyStyle(cap: .danger))
            }
        }
    }

    private func photos(_ v: ProjectDetail) -> some View {
        Plate(label: "Photos") {
            let list = v.photos(of: printID)
            if list.isEmpty { Note(text: "No photos yet. Tap Photos at the top.") }
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 96), spacing: 8)], spacing: 8) {
                ForEach(list) { ph in Thumb(url: ph.smallURL, side: 96) }
            }
        }
    }

    private func load() async {
        do {
            view = try await client.project(project)
            if let pr = print {
                notes = pr.notes ?? ""
                title = pr.name
                source = pr.source ?? ""
            }
            message = nil
        } catch {
            message = errorText(error)
        }
    }

    private func save(_ fields: [String: Any]) async {
        do {
            try await client.editPrint(project: project, print: printID, fields)
            await load()
            await changed()
        } catch {
            message = errorText(error)
        }
    }
}

// MARK: Parts

/// A small key in a row: an action of a list, or a key of a heading.
struct SmallKey: View {
    let title: String
    var cap: KeyStyle.Cap = .raised
    let action: () -> Void

    var body: some View {
        Button(title, action: action)
            .buttonStyle(KeyStyle(cap: cap))
            .frame(width: 96)
            .environment(\.panelScale, 0.65)
    }
}

/// A short line in the dim type.
private struct Note: View {
    @Environment(\.palette) private var p
    let text: String

    var body: some View {
        Text(text.uppercased())
            .font(Theme.plex(12))
            .foregroundStyle(p.dim)
            .padding(.horizontal, 4)
    }
}

/// A row of a list: a title, a detail under it, and a value at the right.
private struct Row: View {
    @Environment(\.palette) private var p
    let title: String
    let detail: String
    let value: String
    /// The value is a badge in red, as Discarded.
    var alert = false

    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(Theme.plex(15))
                    .foregroundStyle(p.text)
                    .lineLimit(2)
                if !detail.isEmpty {
                    Text(detail)
                        .font(Theme.plex(12, bold: false))
                        .foregroundStyle(p.dim)
                        .lineLimit(2)
                }
            }
            Spacer()
            if !value.isEmpty {
                Text(alert ? value.uppercased() : value)
                    .font(Theme.plex(13))
                    .foregroundStyle(alert ? p.red : p.dim)
            }
        }
        .padding(.horizontal, 4)
        .contentShape(Rectangle())
    }
}

/// A small picture from the service, or an empty square.
private struct Thumb: View {
    @Environment(\.palette) private var p
    let url: URL?
    var side: CGFloat = 56

    var body: some View {
        ServiceImage(url: url, fill: true)
            .frame(width: side, height: side)
            .background(p.lcd)
        .clipShape(RoundedRectangle(cornerRadius: 4))
        .overlay(RoundedRectangle(cornerRadius: 4).stroke(p.edge, lineWidth: 1))
    }
}

extension URL: @retroactive Identifiable {
    public var id: String { absoluteString }
}
