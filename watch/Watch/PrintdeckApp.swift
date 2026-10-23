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
