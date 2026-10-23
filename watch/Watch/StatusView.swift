import SwiftUI
import WatchKit

/// The watch as the display of the printer: the whole watch is the blue dot-matrix display. The
/// status of the print is the first page. The Digital Crown, or a swipe up and down, goes through
/// the pages: the status, the model of the print as the deck draws it, the print (layer and
/// times), and the filament. During a print, a tap opens
/// a short menu: Pause or Resume, and Cancel, each with a question first. In the menu the crown
/// moves the selection, as the knob of the printer, and a tap selects.
struct StatusView: View {
    @Environment(DeckModel.self) private var model
    @Environment(\.scenePhase) private var phase
    @Environment(\.displayScale) private var scale
    @FocusState private var focused: Bool
    @State private var mode = Mode.pages
    @State private var crown = 0.0

    private let p = Palette.night

    enum Mode: Equatable {
        case pages
        case menu
        case confirm(DeckModel.Command)
    }

    enum Page { case status, model, print, filament }

    private struct Action {
        let label: String
        let icon: String
        let run: () -> Void
    }

    var body: some View {
        display
            .ignoresSafeArea()
        .background(p.glcd.ignoresSafeArea())
        .environment(\.palette, p)
        .focusable()
        .focused($focused)
        .digitalCrownRotation($crown, from: 0, through: Double(crownMax), by: 1,
                              sensitivity: .low, isContinuous: false, isHapticFeedbackEnabled: true)
        .gesture(DragGesture(minimumDistance: 20).onEnded { g in
            guard mode == .pages, abs(g.translation.height) > abs(g.translation.width) else { return }
            let next = Int(crown.rounded()) + (g.translation.height < 0 ? 1 : -1)
            crown = Double(max(0, min(pages.count - 1, next)))
        })
        .onAppear { focused = true }
        .task(id: phase == .active) {
            if phase == .active {
                focused = true
                await model.poll()
            }
        }
        #if DEBUG
        .onAppear {
            // For screenshots in the simulator: PRINTDECK_SCREEN opens a screen.
            switch ProcessInfo.processInfo.environment["PRINTDECK_SCREEN"] {
            case "menu": open(.menu)
            case "page2": crown = 1
            case "page3": crown = 2
            case "cancel": open(.confirm(.cancel))
            default: break
            }
        }
        #endif
        .onChange(of: model.status?.busy) { _, busy in
            // A print that ends closes the menu.
            if busy != true, mode != .pages { open(.pages) }
        }
    }

    private var display: some View {
        GeometryReader { g in
            // The dots fill the screen: the largest dots of whole pixels that leave 64 x 72 of them
            // inside the margins. The margins keep the screens away from the round corners: 18
            // points at the top, beside the clock of watchOS at the top right.
            let mx = 4, my = 5
            let wpx = Int(g.size.width * scale), hpx = Int(g.size.height * scale)
            let topPx = Int(18 * scale)
            let dot = max(2, min(wpx / (64 + 2 * mx), (hpx - topPx) / (72 + my)))
            let top = (topPx + dot - 1) / dot
            // The picture of the model fills the page under its header: a square.
            let side = max(16, min(wpx / dot - 2 * mx, hpx / dot - top - my - 18))
            LCDView(screen: screen, dotsW: wpx / dot, dotsH: hpx / dot, dotPixels: dot,
                    marginX: mx, marginY: my, marginTop: top, clockW: Int(72 * scale) / dot, framed: false)
                .onAppear { model.modelSize = (side, side) }
                .onChange(of: side) { _, v in model.modelSize = (v, v) }
                .frame(width: g.size.width, height: g.size.height)
                .contentShape(Rectangle())
                .onTapGesture { click() }
        }
    }

    /// The pages: the status always, the print during a print, and the filament with a roll.
    private var pages: [Page] {
        var out: [Page] = [.status]
        if model.status?.busy == true, model.model != nil { out.append(.model) }
        if model.status?.busy == true { out.append(.print) }
        if model.status?.roll != nil { out.append(.filament) }
        return out
    }

    private var page: Page { pages[max(0, min(pages.count - 1, Int(crown.rounded())))] }

    private var crownMax: Int {
        switch mode {
        case .pages: max(1, pages.count - 1)
        default: max(1, actions.count - 1)
        }
    }

    private var selected: Int { max(0, min(actions.count - 1, Int(crown.rounded()))) }

    private var screen: LCDScreen {
        switch mode {
        case .pages:
            let now = Date()
            var pg: LCDPage
            switch page {
            case .status:
                return .status(LCDModel.status(model.status, connected: model.settings.isComplete,
                                               error: model.error, now: now, hint: ""))
            case .model:
                guard let pic = model.model else { return .rows([]) }
                pg = LCDPage(title: "Model", rows: [], picture: pic)
            case .print:
                guard let st = model.status else { return .rows([]) }
                pg = LCDModel.printPage(st, now: now)
            case .filament:
                guard let r = model.status?.roll else { return .rows([]) }
                pg = LCDModel.filamentPage(r)
            }
            pg.index = pages.firstIndex(of: page) ?? 0
            pg.count = pages.count
            return .page(pg)
        case .menu:
            return .menu(LCDMenu(title: "Print", items: actions.map { .init(label: $0.label, icon: $0.icon) },
                                 selected: selected))
        case .confirm(let c):
            return .menu(LCDMenu(title: question(c), items: actions.map { .init(label: $0.label, icon: $0.icon) },
                                 selected: selected))
        }
    }

    /// The rows of the menu on the screen, with what they do.
    private var actions: [Action] {
        switch mode {
        case .pages:
            return []
        case .menu:
            var a = [Action(label: "Back", icon: "back") { open(.pages) }]
            if model.status?.paused == true {
                a.append(Action(label: "Resume", icon: "play") { open(.confirm(.resume)) })
            } else {
                a.append(Action(label: "Pause", icon: "pause") { open(.confirm(.pause)) })
            }
            a.append(Action(label: "Cancel", icon: "stop") { open(.confirm(.cancel)) })
            return a
        case .confirm(let c):
            return [
                Action(label: "No", icon: "no") { open(.menu) },
                Action(label: "Yes", icon: "yes") {
                    open(.pages)
                    Task { await model.send(c) }
                },
            ]
        }
    }

    /// The click of the knob: the menu during a print.
    private func click() {
        switch mode {
        case .pages:
            if model.status?.busy == true { open(.menu) }
        default:
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
