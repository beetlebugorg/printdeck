// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI
import WatchKit

/// The watch as the display of the printer: the whole watch is the blue dot-matrix display. The
/// status of the print fills the first screen. Under it, in one scroll that the Digital Crown
/// moves, are the picture of the camera of the deck (when it has one), the model of the print as
/// the deck draws it, the print (layers and times), and the filament. During a print, a tap opens
/// a short menu over the scroll: Pause or Resume, and Cancel, each with a question first. In the
/// menu the crown moves the selection, as the knob of the printer, and a tap selects.
struct StatusView: View {
    @Environment(DeckModel.self) private var model
    @Environment(\.scenePhase) private var phase
    @Environment(\.displayScale) private var scale
    @Environment(\.isLuminanceReduced) private var dimmed
    @FocusState private var focus: Focus?
    @State private var mode = Mode.scroll
    @State private var crown = 0.0
    /// The last picture of the camera of the deck, or nil.
    @State private var photo: CGImage?
    /// The camera picture is on the screen.
    @State private var cameraShown = false
    /// The camera did not answer the last time.
    @State private var cameraFailed = false
    /// The end of the last scroll: a tap right after a scroll only stops the scroll.
    @State private var scrolled = Date.distantPast
    @State private var scrolling = false

    /// What the crown moves: the scroll or the menu.
    enum Focus { case scroll, menu }

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
            // The menu covers the scroll, which stays in place under it.
            ZStack(alignment: .top) {
                scroll(grid)
                    .allowsHitTesting(mode == .scroll)
                if mode != .scroll {
                    menu(grid)
                }
            }
            .frame(width: WKInterfaceDevice.current().screenBounds.width,
                   height: WKInterfaceDevice.current().screenBounds.height, alignment: .top)
            .offset(y: -r.frame(in: .global).minY)
            .onAppear {
                clockBand = r.frame(in: .global).minY
                setModelSize(grid)
                focus = .scroll
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
        .onChange(of: model.settings) {
            photo = nil
            cameraFailed = false
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
                    lcd(statusScreen, grid: grid, rows: grid.window, top: 0, animated: !dimmed)
                    if let cam = model.status?.camera, cam.on, !dimmed {
                        cameraPicture(grid, cam)
                    }
                    ForEach(sections, id: \.self) { s in
                        if let pg = page(s) {
                            lcd(.page(pg), grid: grid, rows: pg.height + 4, top: 2)
                        }
                    }
                }
                .contentShape(Rectangle())
                .onTapGesture {
                    // A tap that stops a scroll opens no menu.
                    if !scrolling, Date().timeIntervalSince(scrolled) > 0.4 { click() }
                }
            }
            .scrollIndicators(.hidden)
            .focused($focus, equals: .scroll)
            .onScrollPhaseChange { _, new in
                scrolling = new != .idle
                if new == .idle { scrolled = Date() }
            }
            #if DEBUG
            // For screenshots in the simulator: PRINTDECK_SCREEN=bottom opens the end of the scroll.
            .defaultScrollAnchor(ProcessInfo.processInfo.environment["PRINTDECK_SCREEN"] == "bottom" ? .bottom : .top)
            #endif
            .frame(height: CGFloat(grid.window * grid.dot) / scale)
            .clipped()
            .padding(.top, CGFloat(grid.top * grid.dot) / scale)
        }
    }

    /// The picture of the camera of the deck, under the status, as wide as the text of the
    /// display. It reads a small picture every 3 seconds while the app is active and the picture is
    /// on the screen. A camera with no picture shows why.
    private func cameraPicture(_ grid: Grid, _ cam: DeckCamera) -> some View {
        let w = CGFloat((grid.cols - 2 * Grid.mx) * grid.dot) / scale
        let aspect = photo.map { CGFloat($0.width) / CGFloat(max(1, $0.height)) } ?? cam.aspect
        let problem = cam.problem ?? (cameraFailed ? "No picture" : photo == nil ? "Camera" : nil)
        return ZStack {
            Rectangle().fill(.black)
            if let photo {
                Image(decorative: photo, scale: scale)
                    .resizable()
                    .aspectRatio(contentMode: .fit)
                    .opacity(cameraFailed ? 0.4 : 1)
            }
            if let problem {
                Text(problem.uppercased())
                    .font(.system(size: 12, weight: .semibold, design: .monospaced))
                    .foregroundStyle(.white)
                    .multilineTextAlignment(.center)
                    .padding(4)
            }
        }
        .frame(width: w, height: w / aspect)
        .padding(.vertical, CGFloat(2 * grid.dot) / scale)
        .onScrollVisibilityChange(threshold: 0.1) { cameraShown = $0 }
        .task(id: CameraTask(active: phase == .active && cameraShown, settings: model.settings)) {
            guard phase == .active, cameraShown else { return }
            let client = DeckClient(settings: model.settings)
            while !Task.isCancelled {
                if let img = await client.snapshot(width: Int(w * scale)) {
                    photo = img
                    cameraFailed = false
                } else if !Task.isCancelled {
                    cameraFailed = true
                }
                try? await Task.sleep(for: .seconds(3))
            }
        }
    }

    /// The camera reads while this is active, from the deck of these settings.
    private struct CameraTask: Equatable {
        let active: Bool
        let settings: DeckSettings
    }

    /// A part of the display, rows dots high.
    private func lcd(_ screen: LCDScreen, grid: Grid, rows: Int, top: Int, animated: Bool = true) -> some View {
        LCDView(screen: screen, dotsW: grid.cols, dotsH: rows, dotPixels: grid.dot,
                marginX: Grid.mx, marginY: 0, marginTop: top, framed: false,
                framesPerSecond: animated ? 2 : 0)
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
                                now: Date(), hint: "", live: model.live, notice: model.noticeNow))
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
            .focusEffectDisabled()
            .focused($focus, equals: .menu)
            .digitalCrownRotation($crown, from: 0, through: Double(max(1, actions.count - 1)), by: 1,
                                  sensitivity: .low, isContinuous: false, isHapticFeedbackEnabled: true)
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

    /// The click of the knob: the menu during a print. An old status or a command on its way opens
    /// no menu: the print can have changed.
    private func click() {
        switch mode {
        case .scroll:
            if model.status?.busy == true, model.live, !model.sending { open(.menu) }
        default:
            guard actions.indices.contains(selected) else { return }
            WKInterfaceDevice.current().play(.click)
            actions[selected].run()
        }
    }

    private func open(_ m: Mode) {
        mode = m
        crown = 0
        // The crown goes to the menu, and back to the scroll when the menu closes. The focus moves
        // after the view of the new mode is on the screen.
        let f: Focus = m == .scroll ? .scroll : .menu
        Task { @MainActor in focus = f }
    }

    private func question(_ c: DeckModel.Command) -> String {
        switch c {
        case .pause: "Pause?"
        case .resume: "Resume?"
        case .cancel: "Cancel?"
        }
    }
}
