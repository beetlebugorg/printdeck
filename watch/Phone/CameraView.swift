// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

/// The Camera page: the picture of the camera of the deck, 5 times a second while the page is
/// open. It reads /webcam/snapshot at the width of the screen, and the deck sends its newest frame.
struct CameraView: View {
    @Environment(DeckModel.self) private var model
    @Environment(\.palette) private var p
    @Environment(\.displayScale) private var scale
    let close: () -> Void

    @State private var photo: CGImage?
    @State private var message: String? = "Starting the camera"

    var body: some View {
        GeometryReader { r in
            VStack(alignment: .leading, spacing: 10) {
                Heading(title: "Camera") {
                    Button("Back", action: close)
                        .buttonStyle(KeyStyle())
                        .frame(width: 100)
                        .environment(\.panelScale, 0.75)
                }
                .padding(.top, 8)
                Text((message ?? model.status?.camera ?? "").uppercased())
                    .font(Theme.plex(13))
                    .foregroundStyle(p.dim)
                ZStack {
                    RoundedRectangle(cornerRadius: 4).fill(p.lcd)
                    if let photo {
                        Image(decorative: photo, scale: scale)
                            .resizable()
                            .aspectRatio(contentMode: .fit)
                    }
                }
                .aspectRatio(16 / 9, contentMode: .fit)
                .overlay(RoundedRectangle(cornerRadius: 6).stroke(p.edge, lineWidth: 2))
                Spacer()
            }
            .padding(16)
            .task { await load(width: Int((r.size.width - 32) * scale)) }
        }
        .background(p.bg.ignoresSafeArea())
    }

    /// Reads a picture 5 times a second until the page closes. After a failure it waits 2 seconds.
    private func load(width: Int) async {
        let client = DeckClient(settings: model.settings)
        while !Task.isCancelled {
            if let img = await client.snapshot(width: max(320, width)) {
                photo = img
                message = nil
                try? await Task.sleep(for: .milliseconds(200))
            } else {
                message = "No picture from the camera"
                try? await Task.sleep(for: .seconds(2))
            }
        }
    }
}
