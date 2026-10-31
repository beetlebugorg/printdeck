// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

/// The projects of the session at the service. A tap opens the overview of a project.
struct ProjectsView: View {
    @Environment(\.palette) private var p
    let close: () -> Void

    @State private var projects: [ServiceProject] = []
    @State private var message: String? = "Reading the projects"

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    Heading(title: "Projects") { BackKey(title: "Back", action: close) }
                        .padding(.top, 8)
                    if let message { Note(text: message) }
                    ForEach(projects) { pr in
                        NavigationLink(value: pr.id) {
                            Plate(label: "Project") {
                                Text(pr.name.uppercased())
                                    .font(Theme.plex(18))
                                    .foregroundStyle(p.text)
                                    .padding(.horizontal, 4)
                            }
                        }
                        .buttonStyle(.plain)
                    }
                }
                .padding(16)
            }
            .background(p.bg.ignoresSafeArea())
            .toolbar(.hidden, for: .navigationBar)
            .navigationDestination(for: String.self) { id in ProjectScreen(id: id) }
            .task { await load() }
        }
    }

    private func load() async {
        do {
            projects = try await ServiceClient(token: Account.shared.session?.token).projects()
            message = projects.isEmpty ? "No projects" : nil
        } catch {
            message = errorText(error)
        }
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
                Heading(title: view?.name ?? "Project") { BackKey(title: "Back") { dismiss() } }
                    .padding(.top, 8)
                if let message { Note(text: message) }
                if let v = view {
                    Button("Take photos") { camera = true }
                        .buttonStyle(KeyStyle())
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
                    VStack(alignment: .leading, spacing: 8) {
                        NavigationLink(value: ProjectPrintRoute(print: pr.id)) {
                            HStack(spacing: 10) {
                                Thumb(url: v.photos(of: pr.id).first?.smallURL ?? pr.modelURL)
                                Row(title: pr.name, detail: pr.readyNote ?? "", value: "")
                            }
                        }
                        .buttonStyle(.plain)
                        if v.owner {
                            Button("Delivered") {
                                Task {
                                    await act { try await client.editPrint(project: id, print: pr.id, ["delivered": true]) }
                                }
                            }
                            .buttonStyle(KeyStyle(cap: .orange))
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
                            Thumb(url: v.photos(of: pr.id).first?.smallURL ?? pr.modelURL)
                            Row(title: pr.name,
                                detail: [pr.resultText, pr.ended.map { $0.formatted(.relative(presentation: .named)) } ?? "",
                                         pr.printer ?? ""].filter { !$0.isEmpty }.joined(separator: " · "),
                                value: "")
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
                Thumb(url: pl.picture)
                Row(title: "Plate \(ref.index + 1) · " + ref.plan.title,
                    detail: [pl.printer ?? "", pl.materialName ?? "",
                             pl.seconds.map { Format.duration(Double($0)) } ?? ""].filter { !$0.isEmpty }.joined(separator: " · "),
                    value: pl.state == "waiting" ? "Waits" : "Approved")
            }
            Button("3D preview") { open() }
                .buttonStyle(KeyStyle())
            if pl.state == "waiting" {
                if rejecting {
                    PanelField(label: "Reason", prompt: "Why the plate is wrong", text: $reason)
                    HStack(spacing: 10) {
                        Button("Reject") { Task { await decide(approve: false) } }
                            .buttonStyle(KeyStyle(cap: .danger))
                            .disabled(busy || reason.trimmingCharacters(in: .whitespaces).isEmpty)
                        Button("Back") { rejecting = false }
                            .buttonStyle(KeyStyle())
                    }
                } else {
                    HStack(spacing: 10) {
                        Button("Approve") { Task { await decide(approve: true) } }
                            .buttonStyle(KeyStyle(cap: .orange))
                        Button("Reject") { rejecting = true }
                            .buttonStyle(KeyStyle(cap: .danger))
                    }
                    .disabled(busy)
                }
            } else if let printers {
                let same = printers.filter { $0.process == nil || $0.process == pl.process }
                if same.isEmpty { Note(text: "No printer for \(pl.process ?? "this process")") }
                ForEach(same) { pr in
                    Button("Send to \(pr.name)") { Task { await send(pr) } }
                        .buttonStyle(KeyStyle(cap: .orange))
                        .disabled(busy)
                }
            } else {
                Button("Send to a printer") { Task { await loadPrinters() } }
                    .buttonStyle(KeyStyle(cap: .orange))
                    .disabled(busy)
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

    private var client: ServiceClient { ServiceClient(token: Account.shared.session?.token) }
    private var print: ProjectPrintItem? { view?.prints?.first { $0.id == printID } }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                Heading(title: print?.name ?? "Print") { BackKey(title: "Back") { dismiss() } }
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
        }
    }

    private func marks(_ pr: ProjectPrintItem) -> some View {
        Plate(label: "Marks") {
            HStack(spacing: 10) {
                Button("Ready") { Task { await save(["ready": pr.ready == nil]) } }
                    .buttonStyle(KeyStyle(led: pr.ready != nil))
                Button("Delivered") { Task { await save(["delivered": pr.delivered == nil]) } }
                    .buttonStyle(KeyStyle(led: pr.delivered != nil))
                Button("Test") { Task { await save(["test": pr.test == nil]) } }
                    .buttonStyle(KeyStyle(led: pr.test != nil))
            }
        }
    }

    private func photos(_ v: ProjectDetail) -> some View {
        Plate(label: "Photos") {
            let list = v.photos(of: printID)
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 96), spacing: 8)], spacing: 8) {
                ForEach(list) { ph in Thumb(url: ph.smallURL, side: 96) }
            }
            Button("Take photos") { camera = true }
                .buttonStyle(KeyStyle(cap: .orange))
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

/// A key for the heading of a page.
struct BackKey: View {
    let title: String
    let action: () -> Void

    var body: some View {
        Button(title, action: action)
            .buttonStyle(KeyStyle())
            .frame(width: 100)
            .environment(\.panelScale, 0.75)
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
                Text(value)
                    .font(Theme.plex(13))
                    .foregroundStyle(p.dim)
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
