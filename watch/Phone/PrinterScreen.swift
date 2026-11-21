// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

/// The screen of one printer: the camera or the model, the print with Pause, Resume and Cancel,
/// the temperatures, the filament, the moves, the queue, the files, the time lapses and the
/// settings. A dialog that starts, stops, moves or heats names the printer. Only the touch screen
/// of a resin deck starts or resumes a print, because both turn on the UV light.
struct PrinterScreen: View {
    @Environment(Fleet.self) private var fleet
    @Environment(DeckModel.self) private var model
    @Environment(\.scenePhase) private var phase
    let link: WatchLink
    /// Goes back to the home screen after Remove.
    let close: () -> Void

    enum Sheet: String, Identifiable {
        case setup, files, timelapse
        var id: String { rawValue }
    }

    @State private var sheet: Sheet?
    @State private var camera = false
    @State private var heater: Heater?
    @State private var confirm: DeckModel.Command?
    /// The Move page, for the screenshots of a debug build.
    @State private var moving = false

    var body: some View {
        let c = model.controls
        ScrollViewReader { proxy in
        List {
            Section {
                hero
                    .listRowInsets(EdgeInsets())
            }
            printSection(c)
            MessagesSection()
            TemperatureSection(heater: $heater).id("temps")
            FilamentSection().id("filament")
            ControlSection().id("control")
            QueueSection().id("queue")
            Section {
                Button { sheet = .files } label: {
                    RowLabel(c.prints ? "Print a file" : "Files", symbol: c.prints ? "doc.badge.plus" : "doc.on.doc", chevron: true)
                }
                Button { sheet = .timelapse } label: { RowLabel("Time lapses", symbol: "film.stack", chevron: true) }
                if model.cameras.count > 1 { CameraPicker() }
            }
            PrinterInfoSection().id("info")
            Section {
                Button { sheet = .setup } label: { RowLabel("Settings", symbol: "gearshape", chevron: true) }
            }
        }
        .listStyle(.insetGrouped)
        #if DEBUG
        // For screenshots in the simulator: PRINTDECK_SCROLL scrolls to a section.
        .task {
            guard let id = ProcessInfo.processInfo.environment["PRINTDECK_SCROLL"] else { return }
            try? await Task.sleep(for: .seconds(2))
            proxy.scrollTo(id, anchor: .top)
        }
        #endif
        }
        .navigationTitle(model.deck.title)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if model.deck.onSharedDeck {
                ToolbarItem(placement: .principal) { PrinterTitle(deck: model.deck) }
            }
        }
        .refreshable {
            await model.refresh()
            await model.refreshQueue()
        }
        // The queue, every 5 seconds while the screen is open.
        .task(id: phase == .active) {
            while phase == .active && !Task.isCancelled {
                await model.refreshQueue()
                try? await Task.sleep(for: .seconds(5))
            }
        }
        .overlay(alignment: .bottom) { notice }
        .confirmationDialog(confirm.map(question) ?? "", isPresented: Binding(get: { confirm != nil }, set: { if !$0 { confirm = nil } }),
                            titleVisibility: .visible, presenting: confirm) { cmd in
            Button(yes(cmd), role: cmd == .cancel ? .destructive : nil) { Task { await model.send(cmd) } }
            Button("Back", role: .cancel) {}
        }
        .sheet(item: $heater) { h in
            HeaterSheet(heater: h).environment(model)
        }
        .sheet(item: $sheet) { s in
            NavigationStack {
                switch s {
                case .setup:
                    SetupView(link: link, model: model) { sheet = nil } removed: {
                        sheet = nil
                        close()
                    }
                case .files: FilesView { sheet = nil }
                case .timelapse: TimelapseView { sheet = nil }
                }
            }
            .environment(model)
            .environment(fleet)
        }
        .navigationDestination(isPresented: $moving) { MoveView().environment(model) }
        .fullScreenCover(isPresented: $camera) {
            CameraView { camera = false }.environment(model)
        }
        #if DEBUG
        .onAppear {
            // For screenshots in the simulator: PRINTDECK_SCREEN opens a page.
            switch ProcessInfo.processInfo.environment["PRINTDECK_SCREEN"] {
            case "files": sheet = .files
            case "setup": sheet = .setup
            case "camera": camera = true
            case "timelapse": sheet = .timelapse
            case "nozzle": heater = .nozzle
            case "move": moving = true
            default: break
            }
        }
        #endif
    }

    // MARK: The picture

    /// The camera when the printer has one on, else the model of the print, else the kind of the
    /// printer. A tap on the camera opens it over the whole screen.
    @ViewBuilder private var hero: some View {
        if model.camera?.on == true {
            Color.black
                .aspectRatio(4 / 3, contentMode: .fit)
                .overlay { CameraFeed(fps: 5, framed: false) }
                .clipped()
                .overlay(alignment: .bottomTrailing) {
                    Image(systemName: "arrow.up.left.and.arrow.down.right")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(.white)
                        .padding(8)
                        .background(Circle().fill(.black.opacity(0.45)))
                        .padding(10)
                        .accessibilityHidden(true)
                }
                .contentShape(Rectangle())
                .onTapGesture { camera = true }
                .accessibilityAddTraits(.isButton)
                .accessibilityLabel("Camera")
        } else if let img = model.modelImage, model.status?.busy == true {
            Image(decorative: img, scale: 3)
                .resizable()
                .aspectRatio(contentMode: .fit)
                .frame(maxWidth: .infinity, maxHeight: 220)
                .padding(16)
                .background(Color.black.opacity(0.9))
        } else {
            KindIcon(process: model.deck.process)
                .font(.system(size: 44))
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, minHeight: 120)
        }
    }

    // MARK: The print

    @ViewBuilder private func printSection(_ c: Controls) -> some View {
        let st = model.status
        Section {
            if let a = st?.controls?.action, !a.isEmpty, model.live {
                HStack(spacing: 10) {
                    ProgressView()
                    Text(a).font(.subheadline)
                }
            }
            if let st, st.busy, model.live {
                PrintSummary(status: st)
                HStack(spacing: 12) {
                    if c.pauses {
                        Button { confirm = .pause } label: {
                            Label("Pause", systemImage: "pause.fill").frame(maxWidth: .infinity)
                        }
                        .buttonStyle(.bordered)
                    }
                    if c.resumes {
                        Button { confirm = .resume } label: {
                            Label("Resume", systemImage: "play.fill").frame(maxWidth: .infinity)
                        }
                        .buttonStyle(.borderedProminent)
                    }
                    if c.cancels {
                        Button(role: .destructive) { confirm = .cancel } label: {
                            Label("Cancel", systemImage: "xmark").frame(maxWidth: .infinity)
                        }
                        .buttonStyle(.bordered)
                    }
                }
                .controlSize(.large)
                .disabled(model.sending)
                .listRowSeparator(.hidden)
                if st.paused, !c.resumes {
                    Text("Resume on the touch screen of the deck, with the build plate and the vat in place.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            } else {
                HStack {
                    Text(StateText.of(st, live: model.live, error: model.error))
                        .font(.headline)
                    Spacer()
                    if let m = st?.lcd?.message, !m.isEmpty {
                        Text(m).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                    }
                }
                if let e = model.error, !model.live {
                    Text(e).font(.footnote).foregroundStyle(.red)
                }
                if c.prints, model.live {
                    Button { sheet = .files } label: {
                        Label("Print a file", systemImage: "play.fill").frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.large)
                    .listRowSeparator(.hidden)
                }
                if model.deck.process == .resin {
                    Text("Start a resin print on the touch screen of the deck.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
        }
    }

    // MARK: The notice

    @ViewBuilder private var notice: some View {
        if let n = model.noticeNow {
            Text(n)
                .font(.subheadline.weight(.medium))
                .padding(.horizontal, 16)
                .padding(.vertical, 10)
                .background(Capsule().fill(.regularMaterial))
                .padding(.bottom, 12)
                .transition(.move(edge: .bottom).combined(with: .opacity))
        }
    }

    // MARK: The dialogs

    private func question(_ c: DeckModel.Command) -> String {
        let name = model.deck.title
        return switch c {
        case .pause: "Pause the print on \(name)?"
        case .resume: "Resume the print on \(name)?"
        case .cancel: "Cancel the print on \(name)?"
        }
    }

    private func yes(_ c: DeckModel.Command) -> String {
        let short = model.deck.label ?? model.deck.title
        return switch c {
        case .pause: "Pause \(short)"
        case .resume: "Resume \(short)"
        case .cancel: "Cancel the print on \(short)"
        }
    }
}

/// The print: its name, the progress, the layer and the times.
private struct PrintSummary: View {
    let status: Status

    var body: some View {
        let st = status
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(st.file ?? "Print")
                        .font(.headline)
                        .lineLimit(2)
                    Text(st.paused ? "Paused" : st.controls?.preparing.flatMap { $0.isEmpty ? nil : $0 }
                         ?? StateText.of(st, live: true, error: nil))
                        .font(.subheadline)
                        .foregroundStyle(st.paused ? .orange : .secondary)
                }
                Spacer()
                Text(Format.percent(st.progress ?? 0))
                    .font(.system(size: 34, weight: .semibold, design: .rounded).monospacedDigit())
            }
            ProgressLine(value: st.progress ?? 0, tint: st.paused ? .orange : .accentColor)
            Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 6) {
                GridRow {
                    fact("Left", st.left.map(Format.duration))
                    fact("Ends", st.end.map(Format.clock))
                }
                GridRow {
                    fact("So far", st.elapsed.map(Format.duration))
                    fact("Part off the bed", st.release.map { "in " + Format.duration($0) })
                }
            }
        }
        .padding(.vertical, 4)
    }

    @ViewBuilder private func fact(_ name: String, _ value: String?) -> some View {
        if let value {
            VStack(alignment: .leading, spacing: 1) {
                Text(name).font(.caption).foregroundStyle(.secondary)
                Text(value).font(.subheadline.monospacedDigit())
            }
        } else {
            Color.clear.frame(height: 1)
        }
    }
}

/// The information of the printer: its deck, its model, its nozzles and plate, and its address.
private struct PrinterInfoSection: View {
    @Environment(DeckModel.self) private var model

    var body: some View {
        let d = model.deck
        let st = model.status
        Section("Printer") {
            if let deck = d.deckName, d.onSharedDeck { LabeledContent("Deck", value: deck) }
            LabeledContent("Kind", value: d.process == .resin ? "Resin" : "FDM")
            if let m = d.model { LabeledContent("Model", value: m) }
            ForEach(st?.nozzles ?? []) { n in
                LabeledContent(n.name, value: String(format: "%.1f mm", n.diameter) + (n.type.isEmpty ? "" : " · " + n.type))
            }
            if let plate = st?.plate { LabeledContent("Build plate", value: plate) }
            if st?.signedOnly == true { LabeledContent("Signed prints only", value: "On") }
            if let cs = st?.controls, let h = cs.homed {
                LabeledContent("Homed", value: h.isEmpty ? "None" : h.uppercased().map(String.init).joined(separator: " "))
            }
            LabeledContent("Address", value: d.address)
        }
    }
}

/// The cameras of the printer that are on. The choice is the camera of the screen, the card and
/// the watch.
private struct CameraPicker: View {
    @Environment(DeckModel.self) private var model

    var body: some View {
        Picker(selection: Binding(get: { model.camera?.id }, set: { model.chooseCamera($0) })) {
            ForEach(model.cameras, id: \.id) { c in
                Text(c.name).tag(c.id)
            }
        } label: {
            RowLabel("Camera", symbol: "video")
        }
    }
}

/// The messages of the printer, one line each: the HMS messages of a Bambu Lab printer have
/// their codes in the text.
private struct MessagesSection: View {
    @Environment(DeckModel.self) private var model

    var body: some View {
        if let list = model.status?.controls?.messages, !list.isEmpty, model.live {
            Section("Printer messages") {
                ForEach(Array(list.enumerated()), id: \.offset) { _, line in
                    Label {
                        Text(line).font(.subheadline)
                    } icon: {
                        Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.orange)
                    }
                }
            }
        }
    }
}
