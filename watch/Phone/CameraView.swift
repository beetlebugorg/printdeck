// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

/// The Camera page: the camera of the deck while the page is open. It reads /webcam/stream at the
/// width of the screen, at most 10 pictures a second, and connects again after a failure.
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
                Text((model.status?.camera?.problem ?? message ?? model.status?.camera?.name ?? "").uppercased())
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
                .aspectRatio(aspect, contentMode: .fit)
                .overlay(RoundedRectangle(cornerRadius: 6).stroke(p.edge, lineWidth: 2))
                Spacer()
            }
            .padding(16)
            .task { await load(width: Int((r.size.width - 32) * scale)) }
        }
        .background(p.bg.ignoresSafeArea())
    }

    /// The shape of the picture: the last picture, or the camera before the first one.
    private var aspect: CGFloat {
        if let photo, photo.height > 0 { return CGFloat(photo.width) / CGFloat(photo.height) }
        return model.status?.camera?.aspect ?? 16 / 9
    }

    /// Reads the stream until the page closes. After a failure it waits 2 seconds and connects
    /// again.
    private func load(width: Int) async {
        let client = DeckClient(settings: model.settings)
        while !Task.isCancelled {
            do {
                for try await img in client.stream(width: max(320, width), fps: 10) {
                    photo = img
                    message = nil
                }
            } catch {
                if Task.isCancelled { return }
                message = "No picture from the camera"
            }
            try? await Task.sleep(for: .seconds(2))
        }
    }
}
