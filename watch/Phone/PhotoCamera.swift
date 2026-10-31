// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import AVFoundation
import SwiftUI
import WebKit

/// Take photos: the camera stays open for the next photo, and the app sends each photo to the
/// project, and to the print when it has one, after it is taken.
struct PhotoCamera: View {
    @Environment(\.palette) private var p
    let project: String
    let print: String?
    let close: () -> Void

    @State private var camera = Camera()
    @State private var sending = 0
    @State private var sent = 0
    @State private var message: String?

    var body: some View {
        ZStack(alignment: .bottom) {
            Color.black.ignoresSafeArea()
            CameraPreview(session: camera.session)
                .ignoresSafeArea()
            VStack(spacing: 12) {
                Text(status.uppercased())
                    .font(Theme.plex(13))
                    .foregroundStyle(.white)
                    .padding(8)
                    .background(RoundedRectangle(cornerRadius: 4).fill(.black.opacity(0.6)))
                HStack(spacing: 12) {
                    Button("Done", action: close)
                        .buttonStyle(KeyStyle())
                    Button("Take") { take() }
                        .buttonStyle(KeyStyle(cap: .orange))
                        .disabled(!camera.ready)
                }
            }
            .padding(16)
        }
        .environment(\.palette, p)
        .task { await camera.start() }
        .onDisappear { camera.stop() }
    }

    private var status: String {
        if let message { return message }
        if !camera.ready { return camera.problem ?? "Starting the camera" }
        if sending > 0 { return "\(sent) sent · \(sending) sending" }
        return sent == 0 ? "Take a photo" : "\(sent) sent"
    }

    private func take() {
        Task {
            guard let jpeg = await camera.photo() else {
                message = "No photo from the camera"
                return
            }
            sending += 1
            defer { sending -= 1 }
            do {
                try await ServiceClient(token: Account.shared.session?.token)
                    .addPhoto(project: project, jpeg: jpeg, print: print)
                sent += 1
                message = nil
            } catch {
                message = errorText(error)
            }
        }
    }
}

/// The back camera with a photo output.
@MainActor @Observable
final class Camera: NSObject, AVCapturePhotoCaptureDelegate {
    let session = AVCaptureSession()
    private let output = AVCapturePhotoOutput()
    private(set) var ready = false
    private(set) var problem: String?
    private var waiting: CheckedContinuation<Data?, Never>?

    func start() async {
        guard await AVCaptureDevice.requestAccess(for: .video) else {
            problem = "Allow the camera for Printdeck in Settings"
            return
        }
        guard let dev = AVCaptureDevice.default(.builtInWideAngleCamera, for: .video, position: .back),
              let input = try? AVCaptureDeviceInput(device: dev) else {
            problem = "This iPhone has no camera"
            return
        }
        session.beginConfiguration()
        session.sessionPreset = .photo
        if session.canAddInput(input) { session.addInput(input) }
        if session.canAddOutput(output) { session.addOutput(output) }
        session.commitConfiguration()
        let s = session
        await Task.detached { s.startRunning() }.value
        ready = true
    }

    func stop() {
        let s = session
        Task.detached { s.stopRunning() }
    }

    /// A JPEG of the camera now, or nil.
    func photo() async -> Data? {
        guard waiting == nil else { return nil }
        return await withCheckedContinuation { c in
            waiting = c
            output.capturePhoto(with: AVCapturePhotoSettings(format: [AVVideoCodecKey: AVVideoCodecType.jpeg]), delegate: self)
        }
    }

    nonisolated func photoOutput(_ output: AVCapturePhotoOutput, didFinishProcessingPhoto photo: AVCapturePhoto, error: Error?) {
        let data = photo.fileDataRepresentation()
        Task { @MainActor in
            self.waiting?.resume(returning: data)
            self.waiting = nil
        }
    }
}

/// The picture of the camera.
private struct CameraPreview: UIViewRepresentable {
    let session: AVCaptureSession

    final class PreviewView: UIView {
        override class var layerClass: AnyClass { AVCaptureVideoPreviewLayer.self }
        var preview: AVCaptureVideoPreviewLayer { layer as! AVCaptureVideoPreviewLayer }
    }

    func makeUIView(context: Context) -> PreviewView {
        let v = PreviewView()
        v.preview.session = session
        v.preview.videoGravity = .resizeAspectFill
        return v
    }

    func updateUIView(_ v: PreviewView, context: Context) {}
}

/// A page of the web site with the session of the app: the page reads it from the session
/// storage key "pwf-session" (the JSON of the sign-in reply), as after a sign-in on the site.
struct ServiceWebView: View {
    @Environment(\.dismiss) private var dismiss
    let url: URL

    var body: some View {
        NavigationStack {
            WebPage(url: url)
                .ignoresSafeArea(edges: .bottom)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
                }
                .navigationBarTitleDisplayMode(.inline)
        }
    }

    private struct WebPage: UIViewRepresentable {
        let url: URL

        func makeUIView(context: Context) -> WKWebView {
            let config = WKWebViewConfiguration()
            config.websiteDataStore = .nonPersistent()
            if let script = Self.sessionScript() {
                config.userContentController.addUserScript(
                    WKUserScript(source: script, injectionTime: .atDocumentStart, forMainFrameOnly: true))
            }
            let v = WKWebView(frame: .zero, configuration: config)
            v.load(URLRequest(url: url))
            return v
        }

        func updateUIView(_ v: WKWebView, context: Context) {}

        /// Sets the session in the session storage of the site only.
        static func sessionScript() -> String? {
            guard let s = Account.shared.session, let host = ServiceClient.base.host else { return nil }
            let e = JSONEncoder()
            e.dateEncodingStrategy = .iso8601
            guard let json = try? e.encode(s), let text = String(data: json, encoding: .utf8),
                  let quoted = try? JSONEncoder().encode(text), let lit = String(data: quoted, encoding: .utf8),
                  let hostLit = String(data: (try? JSONEncoder().encode(host)) ?? Data(), encoding: .utf8)
            else { return nil }
            return "if (location.host === \(hostLit)) { sessionStorage.setItem(\"pwf-session\", \(lit)); }"
        }
    }
}
