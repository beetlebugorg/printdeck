import SwiftUI
import WatchKit

/// The watch as the display of the printer: the status screen of the deck in a portrait layout, and
/// its menu. The Digital Crown turns like the knob of the printer, and a tap on the display is the
/// click of the knob.
struct StatusView: View {
    @Environment(DeckModel.self) private var model
    @Environment(\.scenePhase) private var phase
    @State private var mode = Mode.status
    @State private var crown = 0.0

    @Environment(\.displayScale) private var scale
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
        GeometryReader { g in
            // The display fills the screen: the largest dots of whole pixels that leave at least
            // 64 x 72 of them inside the frame, the size of the status screen of the watch.
            let b = Int((LCDView.bezel * 2 + 2) * scale)
            let wpx = Int(g.size.width * scale) - b, hpx = Int(g.size.height * scale) - b
            let dot = max(2, min(wpx / 64, hpx / 72))
            LCDView(screen: screen, dotsW: wpx / dot, dotsH: hpx / dot, dotPixels: dot)
                .frame(width: g.size.width, height: g.size.height)
                .contentShape(Rectangle())
                .onTapGesture { click() }
        }
        .ignoresSafeArea(edges: .bottom)
        .background(p.bg.ignoresSafeArea())
        .environment(\.palette, p)
        .focusable()
        .digitalCrownRotation($crown, from: 0, through: Double(max(1, actions.count - 1)), by: 1,
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
                                           now: Date(), hint: ""))
        case .info:
            var rows = LCDModel.info(model.status)
            if let e = model.error { rows.insert((e, ""), at: 0) }
            return .rows(rows)
        case .menu:
            return .menu(LCDMenu(title: "Menu", items: actions.map { .init(label: $0.label, icon: $0.icon) },
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
                    a.append(Action(label: "Resume", icon: "play") { open(.confirm(.resume)) })
                } else {
                    a.append(Action(label: "Pause", icon: "pause") { open(.confirm(.pause)) })
                }
                a.append(Action(label: "Cancel", icon: "stop") { open(.confirm(.cancel)) })
            }
            a.append(Action(label: "Info", icon: "status") { open(.info) })
            return a
        case .confirm(let c):
            return [
                Action(label: "No", icon: "no") { open(.menu) },
                Action(label: "Yes", icon: "yes") {
                    open(.status)
                    Task { await model.send(c) }
                },
            ]
        }
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

    private func open(_ m: Mode) {
        mode = m
        crown = 0
    }

    private func question(_ c: DeckModel.Command) -> String {
        switch c {
        case .pause: "Pause?"
        case .resume: "Resume?"
        case .cancel: "Cancel?"
        }
    }

}
