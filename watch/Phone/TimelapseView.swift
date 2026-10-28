// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import AVKit
import Photos
import SwiftUI

/// The Time lapse page: the videos of the deck, newest first, each with the last frame, the print,
/// the date and the length. A tap opens the player.
struct TimelapseView: View {
    @Environment(DeckModel.self) private var model
    @Environment(\.palette) private var p
    let close: () -> Void

    @State private var videos: [DeckVideo] = []
    @State private var stills: [String: CGImage] = [:]
    @State private var message: String? = "Reading the videos"
    @State private var playing: DeckVideo?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 10) {
                Heading(title: "Time lapse") {
                    Button("Back", action: close)
                        .buttonStyle(KeyStyle())
                        .frame(width: 100)
                        .environment(\.panelScale, 0.75)
                }
                .padding(.top, 8)
                Text(message?.uppercased() ?? "NEWEST FIRST · \(videos.count) VIDEO\(videos.count == 1 ? "" : "S")")
                    .font(Theme.plex(13))
                    .foregroundStyle(p.dim)
                ForEach(videos) { v in
                    Button { playing = v } label: { row(v) }
                        .buttonStyle(.plain)
                }
                Text("The deck keeps the last 3 videos.")
                    .font(Theme.plex(12, bold: false))
                    .foregroundStyle(p.dim)
                    .padding(.horizontal, 4)
            }
            .padding(16)
        }
        .background(p.bg.ignoresSafeArea())
        .task { await load() }
        .fullScreenCover(item: $playing) { v in
            TimelapsePlayer(video: v) { playing = nil }
                .environment(model)
                .environment(\.palette, p)
        }
    }

    private func row(_ v: DeckVideo) -> some View {
        HStack(spacing: 14) {
            ZStack {
                RoundedRectangle(cornerRadius: 4).fill(p.lcd)
                if let img = stills[v.name] {
                    Image(decorative: img, scale: 1)
                        .resizable()
                        .aspectRatio(contentMode: .fit)
                        .padding(3)
                }
            }
            .frame(width: 112, height: 66)
            .overlay(RoundedRectangle(cornerRadius: 4).stroke(p.edge, lineWidth: 2))
            VStack(alignment: .leading, spacing: 6) {
                Text(title(v))
                    .font(Theme.plex(18))
                    .foregroundStyle(p.text)
                    .lineLimit(1)
                Text(detail(v))
                    .font(Theme.plex(15, bold: false))
                    .foregroundStyle(p.dim)
                    .lineLimit(1)
                    .minimumScaleFactor(0.7)
            }
            Spacer(minLength: 0)
        }
        .padding(10)
        .background(RoundedRectangle(cornerRadius: 5).fill(p.surface))
        .overlay(RoundedRectangle(cornerRadius: 5).stroke(p.edge, lineWidth: 1))
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
    @Environment(\.palette) private var p
    let video: DeckVideo
    let close: () -> Void

    @State private var file: URL?
    @State private var player: AVPlayer?
    @State private var message: String? = "Downloading and converting the video"
    @State private var saved: String?

    var body: some View {
        VStack(spacing: 12) {
            Heading(title: video.file ?? "Time lapse") {
                Button("Back", action: close)
                    .buttonStyle(KeyStyle())
                    .frame(width: 100)
                    .environment(\.panelScale, 0.75)
            }
            .padding(.top, 8)
            ZStack {
                RoundedRectangle(cornerRadius: 4).fill(Color.black)
                if let player {
                    VideoPlayer(player: player)
                } else {
                    Text((message ?? "").uppercased())
                        .font(Theme.plex(14))
                        .foregroundStyle(p.lcdInk)
                        .multilineTextAlignment(.center)
                        .padding()
                }
            }
            .frame(maxHeight: .infinity)
            .overlay(RoundedRectangle(cornerRadius: 6).stroke(p.edge, lineWidth: 2))
            if let saved {
                Text(saved.uppercased())
                    .font(Theme.plex(13))
                    .foregroundStyle(p.dim)
            }
            HStack(spacing: 10) {
                if let file {
                    ShareLink(item: file) { Text("Share") }
                        .buttonStyle(KeyStyle())
                } else {
                    Button("Share") {}.buttonStyle(KeyStyle()).disabled(true)
                }
                Button("Save to Photos") { Task { await save() } }
                    .buttonStyle(KeyStyle(cap: .orange))
                    .disabled(file == nil)
            }
        }
        .padding(16)
        .background(p.bg.ignoresSafeArea())
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
