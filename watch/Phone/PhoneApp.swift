// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

@main
struct PhoneApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @State private var fleet = Fleet()
    @State private var link = WatchLink()

    var body: some Scene {
        WindowGroup {
            RootView(link: link)
                .environment(fleet)
        }
    }
}

/// The two tabs of the app: Prints, with a card for each printer and the screen of a printer, and
/// Projects, with the projects of the service. The app reads all decks every 5 seconds while it
/// is active.
struct RootView: View {
    @Environment(Fleet.self) private var fleet
    @Environment(\.colorScheme) private var scheme
    @Environment(\.scenePhase) private var phase
    let link: WatchLink

    @State private var path: [String] = []
    @State private var router = Router.shared
    @State private var tab = Tab.prints
    @State private var projectPath: [ProjectRoute] = []

    enum Tab { case prints, projects }
    /// The loading screen shows until the first reading of the decks.
    @State private var loading = true

    var body: some View {
        let p = Palette.of(scheme)
        ZStack {
            TabView(selection: $tab) {
                NavigationStack(path: $path) {
                    HomeView(link: link) { path.append($0) }
                        .navigationDestination(for: String.self) { id in
                            if let m = fleet.model(id) {
                                PrinterScreen(link: link) { path.removeAll() }
                                    .environment(m)
                            }
                        }
                }
                .tabItem { Label("Prints", systemImage: "printer") }
                .tag(Tab.prints)
                NavigationStack(path: $projectPath) {
                    ProjectsHome()
                }
                .tabItem { Label("Projects", systemImage: "square.stack") }
                .tag(Tab.projects)
            }
            .tint(p.orange)
            if loading {
                LoadingView(lines: loadingLines)
                    .transition(.opacity)
            }
        }
        .environment(\.palette, p)
        .task {
            // At least a second, so the screen does not flash, and up to 8 seconds for the decks.
            try? await Task.sleep(for: .seconds(1))
            for _ in 0..<35 {
                if fleet.models.allSatisfy({ !$0.settings.isComplete || $0.status != nil || $0.error != nil }) { break }
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
            // For screenshots in the simulator: a PRINTDECK_SCREEN of the deck screen opens the
            // first deck.
            switch ProcessInfo.processInfo.environment["PRINTDECK_SCREEN"] {
            case "projects": tab = .projects
            case "project":
                tab = .projects
                projectPath = [ProjectRoute(id: "pj1")]
            default: break
            }
            openDebugScreen()
        }
        // The printers of PRINTDECK_DECK_KEY come after the first appearance.
        .onChange(of: fleet.models.count) { openDebugScreen() }
        #endif
        .onAppear {
            fleet.onRound = { [fleet] in PrintLiveActivity.update(fleet) }
        }
        .onChange(of: fleet.models.map(\.id), initial: true) {
            // The model in dots for the Live Activity, and in color for the cards and the window
            // of the print.
            for m in fleet.models {
                m.modelSize = (48, 48)
                m.modelImageSize = (720, 480)
            }
        }
        // A tap on a push message opens the deck screen of its deck.
        .onChange(of: router.deck) { _, id in
            guard let id, fleet.model(id) != nil else { return }
            tab = .prints
            path = [id]
            router.deck = nil
        }
        // The watch gets each change of the list: a new deck, a name, a camera.
        .onChange(of: fleet.decks) { _, decks in link.send(decks) }
        .task(id: phase == .active) {
            if phase == .active { await fleet.poll() }
        }
        // Away from home, the live prints of the service fill the cards of the decks.
        .task(id: phase == .active) {
            if phase == .active { await Account.shared.poll(fleet) }
        }
    }

    #if DEBUG
    /// For screenshots in the simulator: a PRINTDECK_SCREEN of the screen of a printer opens the
    /// printer of PRINTDECK_PRINTER (its place in the list, from 0), or the first printer.
    private func openDebugScreen() {
        let env = ProcessInfo.processInfo.environment
        let screens = ["deck", "files", "setup", "camera", "timelapse", "nozzle", "move"]
        guard path.isEmpty, let s = env["PRINTDECK_SCREEN"], screens.contains(s) else { return }
        let i = Int(env["PRINTDECK_PRINTER"] ?? "") ?? 0
        guard fleet.models.indices.contains(i) else { return }
        path = [fleet.models[i].id]
    }
    #endif

    private var loadingLines: [String] {
        switch fleet.models.count {
        case 0: ["Setting up"]
        case 1: ["Connecting", fleet.models[0].settings.address]
        case let n: ["Connecting", "\(n) decks"]
        }
    }
}
