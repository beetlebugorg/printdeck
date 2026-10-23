import SwiftUI

@main
struct PhoneApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @State private var model = DeckModel()
    @State private var link = WatchLink()

    var body: some Scene {
        WindowGroup {
            DeckView(link: link)
                .environment(model)
        }
    }
}

/// The main screen in the look of the touch screen of the deck (its tall layout): the display of
/// the printer at the top, then the keys and the plates of the print, or of a printer with no
/// print.
struct DeckView: View {
    @Environment(DeckModel.self) private var model
    @Environment(\.colorScheme) private var scheme
    @Environment(\.scenePhase) private var phase
    let link: WatchLink

    enum Sheet: Identifiable {
        case setup, files
        var id: Self { self }
    }

    @State private var sheet: Sheet?
    @State private var confirm: DeckModel.Command?
    /// The loading screen shows until the first reading of the deck.
    @State private var loading = true

    var body: some View {
        let p = Palette.of(scheme)
        ZStack {
            if model.settings.isComplete {
                deck
            } else {
                SetupView(link: link)
            }
            if loading {
                LoadingView(lines: model.settings.isComplete ? ["Connecting", model.settings.address] : ["Setting up"])
                    .transition(.opacity)
            }
        }
        .environment(\.palette, p)
        .task {
            // At least a second, so the screen does not flash, and up to 8 seconds for the deck.
            try? await Task.sleep(for: .seconds(1))
            for _ in 0..<35 {
                if !model.settings.isComplete || model.status != nil || model.error != nil { break }
                try? await Task.sleep(for: .milliseconds(200))
            }
            #if DEBUG
            // For screenshots in the simulator: PRINTDECK_SCREEN=loading keeps the loading screen.
            if ProcessInfo.processInfo.environment["PRINTDECK_SCREEN"] == "loading" { return }
            #endif
            withAnimation(.easeOut(duration: 0.35)) { loading = false }
        }
        #if DEBUG
        .onAppear {
            // For screenshots in the simulator: PRINTDECK_SCREEN opens a page.
            switch ProcessInfo.processInfo.environment["PRINTDECK_SCREEN"] {
            case "files": sheet = .files
            case "setup": sheet = .setup
            default: break
            }
        }
        #endif
        .onChange(of: model.status) { _, st in PrintLiveActivity.update(st) }
        .task(id: phase == .active && model.settings.isComplete) {
            if phase == .active && model.settings.isComplete { await model.poll() }
        }
        .fullScreenCover(item: $sheet) { s in
            Group {
                switch s {
                case .setup: SetupView(link: link) { sheet = nil }
                case .files: FilesView { sheet = nil }
                }
            }
            .environment(model)
            .environment(\.palette, p)
        }
    }

    private var deck: some View {
        let p = Palette.of(scheme)
        let st = model.status
        return ScrollView {
            VStack(spacing: 12) {
                LCDView(screen: .status(LCDModel.status(st, connected: true, error: model.error, now: Date(), hint: "")))
                if let st, st.busy {
                    if st.paused { paused(st) } else { printing(st) }
                } else {
                    idle(st)
                }
                if let e = model.error {
                    Text(e.uppercased())
                        .font(Theme.plex(12))
                        .foregroundStyle(p.red)
                }
            }
            .padding(16)
        }
        .background(p.bg.ignoresSafeArea())
        .overlay {
            if let c = confirm {
                ConfirmPanel(question: question(c), yes: yesText(c), danger: c == .cancel,
                             onYes: {
                                 confirm = nil
                                 Task { await model.send(c) }
                             },
                             onNo: { confirm = nil })
            }
        }
    }

    // A print: Pause, Cancel and Menu, the layer and the roll, and the place in the print.
    private func printing(_ st: Status) -> some View {
        VStack(spacing: 12) {
            HStack(spacing: 10) {
                Button("Pause") { confirm = .pause }.buttonStyle(KeyStyle(cap: .orange))
                Button("Cancel") { confirm = .cancel }.buttonStyle(KeyStyle(cap: .danger))
                Button("Menu") { sheet = .setup }.buttonStyle(KeyStyle())
            }
            .disabled(model.sending)
            layerPlate(st, title: layerText(st))
            progressWindow(st)
        }
    }

    // A paused print: what paused, Resume, Cancel and Menu.
    private func paused(_ st: Status) -> some View {
        VStack(spacing: 12) {
            layerPlate(st, title: "Paused" + (st.layer.map { " at layer \($0) of \(st.layers ?? 0)" } ?? ""))
            Button("Resume") { confirm = .resume }
                .buttonStyle(KeyStyle(cap: .orange, height: 88))
                .disabled(model.sending)
            HStack(spacing: 10) {
                Button("Cancel") { confirm = .cancel }.buttonStyle(KeyStyle(cap: .danger))
                Button("Menu") { sheet = .setup }.buttonStyle(KeyStyle())
            }
            .disabled(model.sending)
            progressWindow(st)
        }
    }

    // No print: Print a file, the roll, and Menu.
    private func idle(_ st: Status?) -> some View {
        VStack(spacing: 12) {
            Button("Print a file") { sheet = .files }
                .buttonStyle(KeyStyle(cap: .orange, height: 88))
            if let roll = st?.roll {
                Plate(label: "Roll") { rollRow(roll) }
            }
            Button("Menu") { sheet = .setup }
                .buttonStyle(KeyStyle())
        }
    }

    private func layerText(_ st: Status) -> String {
        if let l = st.layer, let n = st.layers { return "Layer \(l) of \(n)" }
        return "Printing"
    }

    /// The plate under the keys: the layer or the pause at the top, and the roll.
    private func layerPlate(_ st: Status, title: String) -> some View {
        let p = Palette.of(scheme)
        return VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .firstTextBaseline) {
                Text(title.uppercased())
                    .font(Theme.plex(17))
                    .foregroundStyle(p.text)
                Spacer()
                if let end = st.end {
                    Text("END " + LCDModel.clock(end))
                        .font(Theme.plex(13))
                        .foregroundStyle(p.dim)
                }
            }
            if let roll = st.roll { rollRow(roll) }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 5).fill(p.surface))
        .overlay(RoundedRectangle(cornerRadius: 5).stroke(p.edge, lineWidth: 1))
    }

    /// The roll as the deck shows it: its color, a meter of what is left, the name and the grams.
    private func rollRow(_ roll: Roll) -> some View {
        let p = Palette.of(scheme)
        return HStack(spacing: 12) {
            Circle()
                .fill(roll.color.flatMap(Color.init(hex:)) ?? p.dim)
                .overlay(Circle().stroke(p.lcdDim, lineWidth: 3))
                .frame(width: 22, height: 22)
            SegmentMeter(value: roll.full > 0 ? roll.left / roll.full : 0)
            Text("\(roll.name) · \(Int(roll.left.rounded())) g".uppercased())
                .font(Theme.plex(13))
                .foregroundStyle(p.dim)
                .lineLimit(1)
                .minimumScaleFactor(0.7)
        }
    }

    /// The dark window under the plate: the file and the place in the print. The deck shows the
    /// model there; the API has no model, so the window has the file and the playhead.
    private func progressWindow(_ st: Status) -> some View {
        let p = Palette.of(scheme)
        return VStack(alignment: .leading, spacing: 14) {
            Text(st.file ?? "")
                .font(Theme.plex(17))
                .foregroundStyle(p.lcdInk)
                .lineLimit(1)
            HStack(alignment: .firstTextBaseline) {
                Text(Format.percent(st.progress ?? 0))
                    .font(Theme.plex(54, bold: false))
                    .foregroundStyle(p.lcdInk)
                Spacer()
                VStack(alignment: .trailing, spacing: 2) {
                    Text(st.left.map { Format.duration($0) + " left" } ?? "")
                    Text(st.elapsed.map { Format.duration($0) + " in" } ?? "")
                }
                .font(Theme.plex(15, bold: false))
                .foregroundStyle(p.lcdDim)
            }
            Playhead(value: st.progress ?? 0)
        }
        .padding(20)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 4).fill(p.lcd))
        .overlay(RoundedRectangle(cornerRadius: 6).stroke(p.edge, lineWidth: 2))
    }

    private func question(_ c: DeckModel.Command) -> String {
        switch c {
        case .pause: "Pause the print?"
        case .resume: "Resume the print?"
        case .cancel: "Cancel the print?"
        }
    }

    private func yesText(_ c: DeckModel.Command) -> String {
        switch c {
        case .pause: "Pause"
        case .resume: "Resume"
        case .cancel: "Cancel print"
        }
    }
}
