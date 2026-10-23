// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

@main
struct PrintdeckApp: App {
    @State private var model = DeckModel()

    var body: some Scene {
        WindowGroup {
            StatusView()
                .environment(model)
        }
    }
}
