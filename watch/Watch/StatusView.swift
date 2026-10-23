import SwiftUI
import WatchKit

/// The watch as the display of the printer: the 128 x 64 status screen of the deck, and its menu.
/// The Digital Crown turns like the knob of the printer, and a tap on the display or OK is the click
/// of the knob. The keys under the display are the actions of the screen.
struct StatusView: View {
    @Environment(DeckModel.self) private var model
    @Environment(\.scenePhase) private var phase
    @State private var mode = Mode.status
    @State private var crown = 0.0

    private let p = Palette.night

    enum Mode: Equatable {
        case status
        case menu
        case info
        case confirm(DeckModel.Command)
    }

    private struct Action {
        let label: String
        let icon: String
        let run: () -> Void
    }

    var body: some View {
        VStack(spacing: 8) {
            LCDView(screen: screen)
                .padding(.horizontal, -1)
                .onTapGesture { click() }
            keys
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 2)
        .background(p.bg.ignoresSafeArea())
        .environment(\.palette, p)
        .environment(\.panelScale, 0.7)
        .focusable(mode != .status && mode != .info)
        .digitalCrownRotation($crown, from: 0, through: Double(max(0, actions.count - 1)), by: 1,
                              sensitivity: .low, isContinuous: false, isHapticFeedbackEnabled: true)
        .task(id: phase == .active) {
            if phase == .active { await model.poll() }
        }
        #if DEBUG
        .onAppear {
            // For screenshots in the simulator: PRINTDECK_SCREEN opens a screen.
            switch ProcessInfo.processInfo.environment["PRINTDECK_SCREEN"] {
            case "menu": open(.menu)
            case "info": open(.info)
            case "cancel": open(.confirm(.cancel))
            default: break
            }
        }
        #endif
        .onChange(of: model.status?.busy) { _, busy in
            // A print that ends closes its menu.
            if busy != true, case .confirm = mode { mode = .status }
        }
    }

    private var selected: Int { max(0, min(actions.count - 1, Int(crown.rounded()))) }

    private var screen: LCDScreen {
        switch mode {
        case .status:
            return .status(LCDModel.status(model.status, connected: model.settings.isComplete, error: model.error,
                                           now: Date(), hint: "Tap for menu"))
        case .info:
            var rows = LCDModel.info(model.status)
            if let e = model.error { rows.insert((e, ""), at: 0) }
            return .rows(rows)
        case .menu:
            return .menu(LCDMenu(title: "Printdeck", items: actions.map { .init(label: $0.label, icon: $0.icon) },
                                 selected: selected))
        case .confirm(let c):
            return .menu(LCDMenu(title: question(c), items: actions.map { .init(label: $0.label, icon: $0.icon) },
                                 selected: selected))
        }
    }

    /// The rows of the menu on the screen, with what they do.
    private var actions: [Action] {
        switch mode {
        case .status, .info:
            return []
        case .menu:
            var a = [Action(label: "Back", icon: "back") { open(.status) }]
            if let st = model.status, st.busy {
                if st.paused {
                    a.append(Action(label: "Resume print", icon: "play") { open(.confirm(.resume)) })
                } else {
                    a.append(Action(label: "Pause print", icon: "pause") { open(.confirm(.pause)) })
                }
                a.append(Action(label: "Cancel print", icon: "stop") { open(.confirm(.cancel)) })
            }
            a.append(Action(label: "Print info", icon: "status") { open(.info) })
            return a
        case .confirm(let c):
            return [
                Action(label: "No", icon: "no") { open(.menu) },
                Action(label: "Yes, " + yesText(c).lowercased(), icon: "yes") {
                    open(.status)
                    Task { await model.send(c) }
                },
            ]
        }
    }

    @ViewBuilder private var keys: some View {
        HStack(spacing: 6) {
            switch mode {
            case .status:
                if let st = model.status, st.busy {
                    Button("Cancel") { open(.confirm(.cancel)) }
                        .buttonStyle(KeyStyle(cap: .red))
                    Button(st.paused ? "Resume" : "Pause") { open(.confirm(st.paused ? .resume : .pause)) }
                        .buttonStyle(KeyStyle(cap: .orange))
                } else {
                    Button("Menu") { open(.menu) }
                        .buttonStyle(KeyStyle())
                }
            case .info:
                Button("Back") { open(.status) }
                    .buttonStyle(KeyStyle())
            case .menu, .confirm:
                Button("Back") { back() }
                    .buttonStyle(KeyStyle())
                Button("OK") { click() }
                    .buttonStyle(KeyStyle(cap: .orange))
            }
        }
        .disabled(model.sending)
    }

    /// The click of the knob.
    private func click() {
        switch mode {
        case .status: open(.menu)
        case .info: open(.status)
        case .menu, .confirm:
            guard actions.indices.contains(selected) else { return }
            WKInterfaceDevice.current().play(.click)
            actions[selected].run()
        }
    }

    private func back() {
        if case .confirm = mode { open(.menu) } else { open(.status) }
    }

    private func open(_ m: Mode) {
        mode = m
        crown = 0
    }

    private func question(_ c: DeckModel.Command) -> String {
        switch c {
        case .pause: "Pause print?"
        case .resume: "Resume print?"
        case .cancel: "Cancel print?"
        }
    }

    private func yesText(_ c: DeckModel.Command) -> String {
        switch c {
        case .pause: "Pause"
        case .resume: "Resume"
        case .cancel: "Cancel"
        }
    }
}
