// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import AVKit
import Photos
import SwiftUI

/// The time lapse videos of the printer, newest first, each with the last frame, the print, the
/// date and the length. A tap opens the player.
struct TimelapseView: View {
    @Environment(DeckModel.self) private var model
    let close: () -> Void

    @State private var videos: [DeckVideo] = []
    @State private var stills: [String: CGImage] = [:]
    @State private var message: String? = "Reading the videos"

    var body: some View {
        List {
            Section {
                if let message { Text(message).foregroundStyle(.secondary) }
                ForEach(videos) { v in
                    NavigationLink {
                        TimelapsePlayer(video: v).environment(model)
                    } label: { row(v) }
                }
            } footer: {
                Text("The deck keeps the last 3 videos.")
            }
        }
        .listStyle(.insetGrouped)
        .navigationTitle("Time lapses · \(model.deck.label ?? model.deck.title)")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { Button("Done", action: close) }
        .task { await load() }
    }

    private func row(_ v: DeckVideo) -> some View {
        HStack(spacing: 12) {
            ZStack {
                Rectangle().fill(Color.black)
                if let img = stills[v.name] {
                    Image(decorative: img, scale: 1)
                        .resizable()
                        .aspectRatio(contentMode: .fill)
                }
            }
            .frame(width: 96, height: 60)
            .clipShape(RoundedRectangle(cornerRadius: 8))
            VStack(alignment: .leading, spacing: 3) {
                Text(title(v)).lineLimit(1)
                Text(detail(v))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                if let r = v.result, r != "complete" {
                    Text(r == "cancelled" ? "Cancelled" : r == "error" ? "Failed" : r.capitalized)
                        .font(.caption)
                        .foregroundStyle(.orange)
                }
            }
        }
    }

    /// The print of the video, or the name of the file of the video.
    private func title(_ v: DeckVideo) -> String {
        v.file ?? (v.name as NSString).deletingPathExtension
    }

    private func detail(_ v: DeckVideo) -> String {
        var parts: [String] = []
        if let d = v.date { parts.append(d.formatted(date: .abbreviated, time: .shortened)) }
        if v.length > 0 { parts.append(Self.length(v.length)) }
        return parts.joined(separator: " · ")
    }

    /// The play time as minutes and seconds, for example "0:21".
    static func length(_ seconds: Double) -> String {
        let s = Int(seconds.rounded())
        return String(format: "%d:%02d", s / 60, s % 60)
    }

    private func load() async {
        let client = DeckClient(settings: model.settings)
        do {
            videos = try await client.timelapses()
            message = videos.isEmpty ? "No videos. Turn on the time lapse on the deck." : nil
            Timelapse.prune(keep: videos)
        } catch {
            message = errorText(error)
            return
        }
        for v in videos where stills[v.name] == nil {
            if let img = await client.still(v) { stills[v.name] = img }
        }
    }
}

/// The player of a time lapse video, with Share and Save to Photos. The page downloads the video
/// and converts it on the iPhone first, or takes it from the cache.
struct TimelapsePlayer: View {
    @Environment(DeckModel.self) private var model
    let video: DeckVideo

    @State private var file: URL?
    @State private var player: AVPlayer?
    @State private var message: String? = "Downloading and converting the video"
    @State private var saved: String?

    var body: some View {
        VStack(spacing: 16) {
            ZStack {
                Rectangle().fill(Color.black)
                if let player {
                    VideoPlayer(player: player)
                } else {
                    VStack(spacing: 10) {
                        if message == "Downloading and converting the video" { ProgressView().tint(.white) }
                        Text(message ?? "")
                            .font(.subheadline)
                            .foregroundStyle(.white.opacity(0.8))
                            .multilineTextAlignment(.center)
                    }
                    .padding()
                }
            }
            .aspectRatio(16 / 9, contentMode: .fit)
            .clipShape(RoundedRectangle(cornerRadius: 14))
            if let saved {
                Text(saved).font(.footnote).foregroundStyle(.secondary)
            }
            HStack(spacing: 12) {
                if let file {
                    ShareLink(item: file) { Label("Share", systemImage: "square.and.arrow.up").frame(maxWidth: .infinity) }
                        .buttonStyle(.bordered)
                }
                Button { Task { await save() } } label: {
                    Label("Save to Photos", systemImage: "photo.badge.arrow.down").frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .disabled(file == nil)
            }
            .controlSize(.large)
            Spacer()
        }
        .padding(16)
        .navigationTitle(video.file ?? "Time lapse")
        .navigationBarTitleDisplayMode(.inline)
        .task { await prepare() }
        .onDisappear { player?.pause() }
    }

    private func prepare() async {
        do {
            let url = try await Timelapse.prepare(video, client: DeckClient(settings: model.settings))
            file = url
            let pl = AVPlayer(url: url)
            player = pl
            pl.play()
        } catch is CancellationError {
        } catch {
            message = errorText(error)
        }
    }

    private func save() async {
        guard let file else { return }
        let status = await PHPhotoLibrary.requestAuthorization(for: .addOnly)
        guard status == .authorized || status == .limited else {
            saved = "Allow Printdeck to add to Photos in Settings."
            return
        }
        do {
            try await PHPhotoLibrary.shared().performChanges {
                PHAssetCreationRequest.creationRequestForAssetFromVideo(atFileURL: file)
            }
            saved = "Saved to Photos."
        } catch {
            saved = error.localizedDescription
        }
    }
}
