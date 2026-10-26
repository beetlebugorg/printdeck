// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

/// The Camera page: the camera of the deck over the whole screen, at most 10 pictures a second
/// while the page is open. The page turns with the iPhone. Pinch or double-tap to zoom, and drag
/// to move a zoomed picture.
struct CameraView: View {
    @Environment(DeckModel.self) private var model
    let close: () -> Void

    @State private var zoom: CGFloat = 1
    @State private var lastZoom: CGFloat = 1
    @State private var pan: CGSize = .zero
    @State private var lastPan: CGSize = .zero
    /// The view of the picker, or nil for the view that follows the print.
    @State private var chosen: String?

    var body: some View {
        GeometryReader { g in
            CameraFeed(fps: 10, framed: false, view: chosen)
                .frame(width: g.size.width, height: g.size.height)
                .scaleEffect(zoom)
                .offset(pan)
                .contentShape(Rectangle())
                .gesture(MagnifyGesture()
                    .onChanged { v in zoom = min(6, max(1, lastZoom * v.magnification)) }
                    .onEnded { _ in
                        lastZoom = zoom
                        if zoom == 1 { reset() }
                    })
                .simultaneousGesture(DragGesture()
                    .onChanged { v in
                        guard zoom > 1 else { return }
                        pan = clamp(CGSize(width: lastPan.width + v.translation.width,
                                           height: lastPan.height + v.translation.height), in: g.size)
                    }
                    .onEnded { _ in lastPan = pan })
                .onTapGesture(count: 2) {
                    withAnimation(.easeOut(duration: 0.2)) {
                        if zoom > 1 { reset() } else { zoom = 2.5; lastZoom = 2.5 }
                    }
                }
        }
        .ignoresSafeArea()
        .background(Color.black.ignoresSafeArea())
        .overlay(alignment: .topLeading) { bar }
        .overlay(alignment: .bottom) { picker }
        .onAppear { turn(true) }
        .onDisappear { turn(false) }
    }

    /// The name of the camera and Back, over the picture.
    private var bar: some View {
        HStack(spacing: 12) {
            Button(action: close) {
                Image(systemName: "xmark")
                    .font(.system(size: 17, weight: .bold))
                    .frame(width: 44, height: 44)
                    .background(Circle().fill(.black.opacity(0.5)))
            }
            .accessibilityLabel("Back")
            Text((model.status?.camera?.name ?? "Camera").uppercased())
                .font(Theme.plex(13))
                .padding(.horizontal, 10)
                .padding(.vertical, 6)
                .background(Capsule().fill(.black.opacity(0.5)))
        }
        .foregroundStyle(.white)
        .padding(16)
    }

    /// The views of the camera, at the bottom. A camera with no calibration shows a note in place
    /// of the views.
    @ViewBuilder private var picker: some View {
        let cam = model.status?.camera
        let views = cam?.views ?? []
        let now = chosen ?? model.cameraView
        Group {
            if cam?.calibrated == false {
                Text("CALIBRATE THE CAMERA ON THE DECK TO FOLLOW THE PRINT")
                    .font(Theme.plex(12))
                    .multilineTextAlignment(.center)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
                    .background(Capsule().fill(.black.opacity(0.5)))
            } else if views.count > 1 {
                HStack(spacing: 6) {
                    ForEach(DeckCamera.viewNames.filter { views.contains($0.id) }, id: \.id) { v in
                        let on = v.id == now
                        Button {
                            withAnimation(.easeOut(duration: 0.2)) { reset() }
                            chosen = v.id
                        } label: {
                            Text(v.name.uppercased())
                                .font(Theme.plex(12))
                                .foregroundStyle(on ? .black : .white)
                                .padding(.horizontal, 10)
                                .padding(.vertical, 8)
                                .background(Capsule().fill(on ? .white : .black.opacity(0.5)))
                        }
                        .accessibilityAddTraits(on ? .isSelected : [])
                    }
                }
            }
        }
        .foregroundStyle(.white)
        .padding(16)
    }

    private func reset() {
        zoom = 1
        lastZoom = 1
        pan = .zero
        lastPan = .zero
    }

    /// Keeps a zoomed picture over the screen.
    private func clamp(_ p: CGSize, in size: CGSize) -> CGSize {
        let mx = size.width * (zoom - 1) / 2, my = size.height * (zoom - 1) / 2
        return CGSize(width: min(mx, max(-mx, p.width)), height: min(my, max(-my, p.height)))
    }

    /// Lets the page turn with the iPhone, and turns the app upright again at the end.
    private func turn(_ on: Bool) {
        AppDelegate.landscape = on
        guard let scene = UIApplication.shared.connectedScenes.first as? UIWindowScene else { return }
        scene.keyWindow?.rootViewController?.setNeedsUpdateOfSupportedInterfaceOrientations()
        if !on { scene.requestGeometryUpdate(.iOS(interfaceOrientations: .portrait)) }
    }
}

/// The camera of the deck in a dark display window. It reads /webcam/stream at the width of the
/// window while the app is active, and connects again after a failure. A camera with no picture
/// shows why.
struct CameraFeed: View {
    @Environment(DeckModel.self) private var model
    @Environment(\.palette) private var p
    @Environment(\.displayScale) private var scale
    @Environment(\.scenePhase) private var phase
    /// The most pictures a second.
    var fps: Double = 5
    /// A display window with a frame, or the bare picture on black.
    var framed = true
    /// The view of the camera, or nil for the view that follows the print.
    var view: String?

    @State private var photo: CGImage?
    @State private var failed = false

    var body: some View {
        let cam = model.status?.camera
        let text = cam?.problem ?? (failed ? "No picture from the camera" : photo == nil ? "Starting the camera" : nil)
        GeometryReader { r in
            ZStack {
                RoundedRectangle(cornerRadius: 4).fill(framed ? p.lcd : .black)
                if let photo {
                    Image(decorative: photo, scale: scale)
                        .resizable()
                        .aspectRatio(contentMode: .fit)
                        .opacity(failed ? 0.4 : 1)
                }
                if let text {
                    Text(text.uppercased())
                        .font(Theme.plex(13))
                        .foregroundStyle(p.lcdInk)
                        .padding(8)
                        .background(RoundedRectangle(cornerRadius: 3).fill(p.lcd.opacity(0.8)))
                }
            }
            .clipShape(RoundedRectangle(cornerRadius: framed ? 4 : 0))
            .task(id: Feed(active: phase == .active, settings: model.settings, width: Int(r.size.width * scale),
                           view: shown)) {
                guard phase == .active else { return }
                await load(width: Int(r.size.width * scale), view: shown)
            }
        }
        .aspectRatio(framed ? aspect : nil, contentMode: .fit)
        .overlay(RoundedRectangle(cornerRadius: 6).stroke(framed ? p.edge : .clear, lineWidth: 2))
        .onChange(of: model.settings) { photo = nil }
    }

    /// The stream runs while this is active, from the deck of these settings, at this width.
    private struct Feed: Equatable {
        let active: Bool
        let settings: DeckSettings
        let width: Int
        let view: String
    }

    /// The view to read: the chosen one, or the one that follows the print.
    private var shown: String {
        view ?? model.cameraView
    }

    /// The shape of the picture: the last picture, or the camera before the first one.
    private var aspect: CGFloat {
        if let photo, photo.height > 0 { return CGFloat(photo.width) / CGFloat(photo.height) }
        return model.status?.camera?.aspect ?? 16 / 9
    }

    /// Reads the stream until the task ends. After a failure it waits 2 seconds and connects again.
    private func load(width: Int, view: String) async {
        let client = DeckClient(settings: model.settings)
        while !Task.isCancelled {
            do {
                for try await img in client.stream(width: max(320, width), fps: fps, view: view) {
                    photo = img
                    failed = false
                }
            } catch {
                if Task.isCancelled { return }
                failed = true
            }
            try? await Task.sleep(for: .seconds(2))
        }
    }
}
