import SwiftUI

/// The loading screen: the start screen of the deck in dots over the whole screen, with the deck
/// that the app connects to and the version of the app.
struct LoadingView: View {
    @Environment(\.palette) private var p
    @Environment(\.displayScale) private var scale
    /// The lines under the title: what the app does, and the deck.
    let lines: [String]

    private var version: String {
        "v" + (Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "")
    }

    var body: some View {
        GeometryReader { g in
            // 160 dots across, as the start screen of the deck on its 480 pixels.
            let dot = max(2, Int(g.size.width * scale) / 160)
            LCDView(screen: .splash("PRINTDECK", lines + ["", version]),
                    dotsW: Int(g.size.width * scale) / dot, dotsH: Int(g.size.height * scale) / dot,
                    dotPixels: dot, marginX: 8, marginY: 4, framed: false)
                .frame(width: g.size.width, height: g.size.height)
        }
        .ignoresSafeArea()
        .background(p.glcd.ignoresSafeArea())
    }
}
