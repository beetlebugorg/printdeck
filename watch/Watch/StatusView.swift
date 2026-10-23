import SwiftUI
import WatchKit

/// The watch as the display of the printer: the whole watch is the blue dot-matrix display. The
/// status of the print fills the first screen. Under it, in one scroll that the Digital Crown
/// moves, are the model of the print as the deck draws it, the print (layers and times), and the
/// filament. During a print, a tap opens a short menu: Pause or Resume, and Cancel, each with a
/// question first. In the menu the crown moves the selection, as the knob of the printer, and a tap
/// selects.
struct StatusView: View {
    @Environment(DeckModel.self) private var model
    @Environment(\.scenePhase) private var phase
    @Environment(\.displayScale) private var scale
    @FocusState private var focused: Bool
    @State private var mode = Mode.scroll
    @State private var crown = 0.0

    private let p = Palette.night

    enum Mode: Equatable {
        case scroll
        case menu
        case confirm(DeckModel.Command)
    }

    enum Section { case status, model, print, filament }

    private struct Action {
        let label: String
        let icon: String
        let run: () -> Void
    }

    /// The dots of the screen, and the window of the scroll in it: from under the clock of watchOS
    /// to 24 points over the bottom, so that the text stays away from the round corners. The dots
    /// are the largest of whole pixels that leave 64 x 72 of them in the window.
    private struct Grid {
        let dot, cols, rows, top, window: Int
        static let mx = 4

        init(screen: CGSize, clockBand: CGFloat, scale: CGFloat) {
            let wpx = Int(screen.width * scale), hpx = Int(screen.height * scale)
            // The safe area of watchOS starts well under the clock (56 points on a 49 mm watch);
            // the clock itself ends at about 36 points.
            let topPx = Int(min(max(clockBand, 20), 40) * scale), bottomPx = Int(24 * scale)
            dot = max(2, min(wpx / (64 + 2 * Self.mx), (hpx - topPx - bottomPx) / 72))
            cols = wpx / dot
            rows = hpx / dot
            top = (topPx + dot - 1) / dot
            window = (hpx - bottomPx) / dot - top
        }
    }

    /// The height of the clock of watchOS at the top of the screen, in points.
    @State private var clockBand: CGFloat = 0

    var body: some View {
        // The safe area starts under the clock: its top is the height of the clock band.
        GeometryReader { r in
            let grid = Grid(screen: WKInterfaceDevice.current().screenBounds.size, clockBand: clockBand, scale: scale)
            Group {
                if mode == .scroll {
                    scroll(grid)
                } else {
                    menu(grid)
                }
            }
            .frame(width: WKInterfaceDevice.current().screenBounds.width,
                   height: WKInterfaceDevice.current().screenBounds.height, alignment: .top)
            .offset(y: -r.frame(in: .global).minY)
            .onAppear {
                clockBand = r.frame(in: .global).minY
                setModelSize(grid)
            }
            .onChange(of: r.frame(in: .global).minY) { _, v in clockBand = v }
            .onChange(of: grid.dot) { setModelSize(grid) }
        }
        .background(p.glcd.ignoresSafeArea())
        .environment(\.palette, p)
        .task(id: phase == .active) {
            if phase == .active { await model.poll() }
        }
        #if DEBUG
        .onAppear {
            // For screenshots in the simulator: PRINTDECK_SCREEN opens a screen.
            switch ProcessInfo.processInfo.environment["PRINTDECK_SCREEN"] {
            case "menu": open(.menu)
            case "cancel": open(.confirm(.cancel))
            default: break
            }
        }
        #endif
        .onChange(of: model.status?.busy) { _, busy in
            // A print that ends closes the menu.
            if busy != true, mode != .scroll { open(.scroll) }
        }
    }

    // MARK: The scroll

    /// The status on the first screen, then the sections, each as high as its rows. The crown
    /// scrolls, as in any scroll of watchOS. The scroll is in the window of the grid, over dots that
    /// are off, so the text stays away from the clock and the round corners, and the whole screen
    /// is the display.
    private func scroll(_ grid: Grid) -> some View {
        ZStack(alignment: .top) {
            lcd(.rows([]), grid: grid, rows: grid.rows, top: 0)
            ScrollView {
                VStack(spacing: 0) {
                    lcd(statusScreen, grid: grid, rows: grid.window, top: 0)
                    ForEach(sections, id: \.self) { s in
                        if let pg = page(s) {
                            lcd(.page(pg), grid: grid, rows: pg.height + 4, top: 2)
                        }
                    }
                }
                .contentShape(Rectangle())
                .onTapGesture { click() }
            }
            .scrollIndicators(.hidden)
            #if DEBUG
            // For screenshots in the simulator: PRINTDECK_SCREEN=bottom opens the end of the scroll.
            .defaultScrollAnchor(ProcessInfo.processInfo.environment["PRINTDECK_SCREEN"] == "bottom" ? .bottom : .top)
            #endif
            .frame(height: CGFloat(grid.window * grid.dot) / scale)
            .clipped()
            .padding(.top, CGFloat(grid.top * grid.dot) / scale)
        }
    }

    /// A part of the display, rows dots high.
    private func lcd(_ screen: LCDScreen, grid: Grid, rows: Int, top: Int) -> some View {
        LCDView(screen: screen, dotsW: grid.cols, dotsH: rows, dotPixels: grid.dot,
                marginX: Grid.mx, marginY: 0, marginTop: top, framed: false)
            .frame(height: CGFloat(rows * grid.dot) / scale)
    }

    private var sections: [Section] {
        var out: [Section] = []
        if model.status?.busy == true, model.model != nil { out.append(.model) }
        if model.status?.busy == true { out.append(.print) }
        if model.status?.roll != nil { out.append(.filament) }
        return out
    }

    private var statusScreen: LCDScreen {
        .status(LCDModel.status(model.status, connected: model.settings.isComplete, error: model.error,
                                now: Date(), hint: ""))
    }

    private func page(_ s: Section) -> LCDPage? {
        var pg: LCDPage
        switch s {
        case .status:
            return nil
        case .model:
            guard let pic = model.model else { return nil }
            pg = LCDPage(title: "Model", rows: [], picture: pic)
        case .print:
            guard let st = model.status else { return nil }
            pg = LCDModel.printPage(st, now: Date())
        case .filament:
            guard let r = model.status?.roll else { return nil }
            pg = LCDModel.filamentPage(r)
        }
        pg.position = false
        return pg
    }

    /// The picture of the model is a square as wide as the screen.
    private func setModelSize(_ grid: Grid) {
        let side = max(16, grid.cols - 2 * Grid.mx)
        model.modelSize = (side, side)
    }

    // MARK: The menu

    private func menu(_ grid: Grid) -> some View {
        LCDView(screen: menuScreen, dotsW: grid.cols, dotsH: grid.top + grid.window, dotPixels: grid.dot,
                marginX: Grid.mx, marginY: 0, marginTop: grid.top, framed: false)
            .frame(maxHeight: .infinity, alignment: .top)
            .background(lcd(.rows([]), grid: grid, rows: grid.rows, top: 0), alignment: .top)
            .contentShape(Rectangle())
            .onTapGesture { click() }
            .focusable()
            .focused($focused)
            .digitalCrownRotation($crown, from: 0, through: Double(max(1, actions.count - 1)), by: 1,
                                  sensitivity: .low, isContinuous: false, isHapticFeedbackEnabled: true)
            .onAppear { focused = true }
    }

    private var selected: Int { max(0, min(actions.count - 1, Int(crown.rounded()))) }

    private var menuScreen: LCDScreen {
        let items = actions.map { LCDMenu.Item(label: $0.label, icon: $0.icon) }
        switch mode {
        case .confirm(let c): return .menu(LCDMenu(title: question(c), items: items, selected: selected))
        default: return .menu(LCDMenu(title: "Print", items: items, selected: selected))
        }
    }

    /// The rows of the menu on the screen, with what they do.
    private var actions: [Action] {
        switch mode {
        case .scroll:
            return []
        case .menu:
            var a = [Action(label: "Back", icon: "back") { open(.scroll) }]
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
                    open(.scroll)
                    Task { await model.send(c) }
                },
            ]
        }
    }

    /// The click of the knob: the menu during a print.
    private func click() {
        switch mode {
        case .scroll:
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
