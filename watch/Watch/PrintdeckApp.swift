// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

@main
struct PrintdeckApp: App {
    @State private var fleet = Fleet()

    var body: some Scene {
        WindowGroup {
            WatchRoot()
                .environment(fleet)
        }
    }
}

/// The deck that prints, or the first deck. The watch reads all decks every 5 seconds while it is
/// active, and shows the other deck when its print ends first.
struct WatchRoot: View {
    @Environment(Fleet.self) private var fleet
    @Environment(\.scenePhase) private var phase

    var body: some View {
        let m = fleet.shown
        StatusView()
            .environment(m)
            // A new deck starts the screen again: the scroll, the menu and the camera picture.
            .id(m.id)
            .task(id: phase == .active) {
                if phase == .active { await fleet.poll() }
            }
    }
}
