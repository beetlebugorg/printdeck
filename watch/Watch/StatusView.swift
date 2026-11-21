// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI
import WatchKit

/// The watch as the status panel of the deck: the readouts, the state, the message, the details
/// and the progress bar in amber on the dark window, as the touch screen of the deck shows them.
/// Under it, in one scroll that the Digital Crown moves, are the picture of the camera of the deck
/// (when it has one), the model of the print as the deck draws it, the print (layers and times),
/// and the filament. During a print, a tap opens a short menu over the scroll: Pause or Resume,
/// and Cancel, each with a question first. In the menu the crown moves the selection, as the knob
/// of the printer, and a tap selects.
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

    private struct Action {
        let label: String
        let run: () -> Void
    }

    var body: some View {
        ZStack(alignment: .top) {
            scroll
                .allowsHitTesting(mode == .scroll)
            if mode != .scroll {
                menu
            }
        }
        .background(p.lcd.ignoresSafeArea())
        .environment(\.palette, p)
        .onAppear {
            // The picture of the model in dots, as wide as the panel.
            model.modelSize = (96, 96)
            focus = .scroll
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
        .onChange(of: model.cameraID) {
            photo = nil
            cameraFailed = false
        }
    }

    // MARK: The scroll

    /// The panel, then the sections. The crown scrolls, as in any scroll of watchOS.
    private var scroll: some View {
        ScrollView {
            VStack(spacing: 8) {
                PanelView(text: panel)
                // The camera stops while the watch is dimmed.
                if let cam = model.camera, cam.on, !dimmed {
                    cameraPicture(cam)
                }
                if model.status?.busy == true, let pic = model.model {
                    PanelSection(title: "Model") {
                        DotPicture(picture: pic).frame(maxWidth: .infinity)
                    }
                }
                if let st = model.status, st.busy {
                    PanelSection(title: "Print") {
                        ForEach(Array(LCDModel.printPage(st, now: Date()).rows.enumerated()), id: \.offset) { _, row in
                            if case let .text(name, value) = row { PanelRow(name: name, value: value) }
                        }
                    }
                }
                if let r = model.status?.roll, model.deck.process != .resin {
                    PanelSection(title: "Filament") {
                        PanelRow(name: r.name)
                        PanelRow(name: "Material", value: r.material)
                        PanelRow(name: "Left", value: "\(Int(r.left.rounded()))g")
                        if r.full > 0 {
                            PanelBar(value: r.left / r.full)
                            PanelRow(name: "Of", value: "\(Int(r.full.rounded()))g")
                        }
                    }
                }
            }
            .padding(.horizontal, 2)
            .contentShape(Rectangle())
            .onTapGesture {
                // A tap that stops a scroll opens no menu.
                if !scrolling, Date().timeIntervalSince(scrolled) > 0.4 { click() }
            }
        }
        .scrollIndicators(.hidden)
        // The text stays under the clock of watchOS.
        .clipped()
        .focused($focus, equals: .scroll)
        .onScrollPhaseChange { _, new in
            scrolling = new != .idle
            if new == .idle { scrolled = Date() }
        }
        #if DEBUG
        // For screenshots in the simulator: PRINTDECK_SCREEN=bottom opens the end of the scroll.
        .defaultScrollAnchor(ProcessInfo.processInfo.environment["PRINTDECK_SCREEN"] == "bottom" ? .bottom : .top)
        #endif
    }

    /// The panel for the state now.
    private var panel: PanelText {
        StatusPanel.make(model.status, connected: model.settings.isComplete, error: model.error, now: Date(),
                         live: model.live, notice: model.noticeNow, resin: model.deck.process == .resin)
    }

    /// The picture of the camera that the iPhone shows, under the panel, as wide as the panel. It
    /// reads a small picture every 3 seconds while the app is active and the picture is on the
    /// screen: the toolhead during a print, and the part at other times. A camera with no picture
    /// shows why.
    private func cameraPicture(_ cam: DeckCamera) -> some View {
        let aspect = photo.map { CGFloat($0.width) / CGFloat(max(1, $0.height)) } ?? cam.aspect
        let problem = cam.problem ?? (cameraFailed ? "No picture" : photo == nil ? "Camera" : nil)
        return GeometryReader { g in
            ZStack {
                Rectangle().fill(.black)
                if let photo {
                    Image(decorative: photo, scale: scale)
                        .resizable()
                        .aspectRatio(contentMode: .fit)
                        .opacity(cameraFailed ? 0.4 : 1)
                }
                if let problem {
                    Text(problem)
                        .font(Theme.plex(11))
                        .foregroundStyle(p.lcdInk)
                        .multilineTextAlignment(.center)
                        .padding(4)
                }
            }
            .task(id: CameraTask(active: phase == .active && cameraShown, settings: model.settings,
                                 view: model.cameraView, camera: cam.id)) {
                guard phase == .active, cameraShown else { return }
                let view = model.cameraView
                let client = DeckClient(settings: model.settings)
                while !Task.isCancelled {
                    if let img = await client.snapshot(width: Int(g.size.width * scale), view: view, camera: cam.id) {
                        photo = img
                        cameraFailed = false
                    } else if !Task.isCancelled {
                        cameraFailed = true
                    }
                    try? await Task.sleep(for: .seconds(3))
                }
            }
        }
        .aspectRatio(aspect, contentMode: .fit)
        .clipShape(RoundedRectangle(cornerRadius: 8))
        .onScrollVisibilityChange(threshold: 0.1) { cameraShown = $0 }
    }

    /// The camera reads this view of this camera while this is active, from the deck of these
    /// settings.
    private struct CameraTask: Equatable {
        let active: Bool
        let settings: DeckSettings
        let view: String
        let camera: String?
    }

    // MARK: The menu

    private var menu: some View {
        ScrollView {
            PanelMenu(title: menuTitle, items: actions.map(\.label), selected: selected)
                .padding(.horizontal, 2)
        }
        .scrollDisabled(true)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        .background(p.lcd.ignoresSafeArea())
        .contentShape(Rectangle())
        .onTapGesture { click() }
        .focusable()
        .focusEffectDisabled()
        .focused($focus, equals: .menu)
        .digitalCrownRotation($crown, from: 0, through: Double(max(1, actions.count - 1)), by: 1,
                              sensitivity: .low, isContinuous: false, isHapticFeedbackEnabled: true)
    }

    private var selected: Int { max(0, min(actions.count - 1, Int(crown.rounded()))) }

    private var menuTitle: String {
        if case .confirm(let c) = mode { return question(c) }
        return "Print"
    }

    /// The rows of the menu on the screen, with what they do.
    private var actions: [Action] {
        switch mode {
        case .scroll:
            return []
        case .menu:
            var a = [Action(label: "Back") { open(.scroll) }]
            if model.status?.paused == true {
                // A resin deck resumes only from its touch screen.
                if model.resumes {
                    a.append(Action(label: "Resume") { open(.confirm(.resume)) })
                }
            } else {
                a.append(Action(label: "Pause") { open(.confirm(.pause)) })
            }
            a.append(Action(label: "Cancel") { open(.confirm(.cancel)) })
            return a
        case .confirm(let c):
            return [
                Action(label: "No") { open(.menu) },
                Action(label: "Yes") {
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
