// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

/// The home screen: a card for each deck, in the order of the list. A tap opens the deck screen.
/// With no deck, the home screen is the setup of the first deck.
struct HomeView: View {
    @Environment(Fleet.self) private var fleet
    @Environment(\.palette) private var p
    let link: WatchLink
    /// Opens the deck screen of a deck ID.
    let open: (String) -> Void

    @State private var adding = false

    var body: some View {
        Group {
            if fleet.models.isEmpty {
                SetupView(link: link, model: nil, added: open)
            } else {
                cards
            }
        }
        .toolbar(.hidden, for: .navigationBar)
        .fullScreenCover(isPresented: $adding) {
            SetupView(link: link, model: nil) { adding = false } added: { id in
                adding = false
                open(id)
            }
            .environment(fleet)
            .environment(\.palette, p)
        }
    }

    private var cards: some View {
        ScrollView {
            VStack(spacing: 14) {
                Heading(title: "Printdeck") {
                    Button("Add") { adding = true }
                        .buttonStyle(KeyStyle())
                        .frame(width: 100)
                        .environment(\.panelScale, 0.75)
                }
                .padding(.top, 8)
                ForEach(fleet.models) { m in
                    Button { open(m.id) } label: {
                        DeckCard().environment(m)
                    }
                    .buttonStyle(.plain)
                }
                AccountPlate()
            }
            .padding(16)
        }
        .background(p.bg.ignoresSafeArea())
    }
}

/// The card of a deck: the picture, the state, the print, the progress and the time left. When the
/// iPhone cannot reach the deck, the card shows the live print of the deck at the service.
struct DeckCard: View {
    @Environment(DeckModel.self) private var model
    @Environment(\.palette) private var p
    @State private var account = Account.shared

    var body: some View {
        if !model.live, model.error != nil, let l = account.live(of: model.deck) {
            AwayCard(title: label, live: l, time: account.liveTime)
        } else {
            homeCard
        }
    }

    private var label: String { model.deck.title + (model.deck.process == .resin ? " · Resin" : "") }

    private var homeCard: some View {
        let st = model.status
        return Plate(label: label) {
            CardPicture()
            HStack(alignment: .firstTextBaseline) {
                Text(stateText.uppercased())
                    .font(Theme.plex(17))
                    .foregroundStyle(model.error == nil ? p.text : p.red)
                    .lineLimit(1)
                    .minimumScaleFactor(0.7)
                Spacer()
                if let st, st.busy {
                    Text(Format.percent(st.progress ?? 0))
                        .font(Theme.plex(28, bold: false))
                        .foregroundStyle(p.text)
                }
            }
            .padding(.horizontal, 4)
            if let st, st.busy {
                VStack(alignment: .leading, spacing: 8) {
                    Text(st.file ?? "")
                        .font(Theme.plex(14, bold: false))
                        .foregroundStyle(p.dim)
                        .lineLimit(1)
                    HStack {
                        Text(st.left.map { Format.duration($0) + " left" } ?? "")
                        Spacer()
                        if let end = st.end { Text("END " + LCDModel.clock(end)) }
                    }
                    .font(Theme.plex(13))
                    .foregroundStyle(p.dim)
                    Playhead(value: st.progress ?? 0)
                }
                .padding(.horizontal, 4)
            }
        }
        .contentShape(Rectangle())
    }

    /// "Layer 12 of 27", "Paused", the state of the deck, or the error of the last read.
    private var stateText: String {
        guard let st = model.status else { return model.error == nil ? "Reading the deck" : "No connection" }
        if !model.live, model.error != nil { return "No connection" }
        if st.paused { return "Paused" }
        if st.busy {
            if let l = st.layer, let n = st.layers { return "Layer \(l) of \(n)" }
            return "Printing"
        }
        return st.state
    }
}

/// The picture of a card: the camera of the deck when it has one on, else the model of the print
/// in color, else the status screen of the printer display. The camera picture is a snapshot, read
/// again every 10 seconds while the app is active.
private struct CardPicture: View {
    @Environment(DeckModel.self) private var model
    @Environment(\.palette) private var p
    @Environment(\.displayScale) private var scale
    @Environment(\.scenePhase) private var phase

    @State private var photo: CGImage?

    var body: some View {
        let cam = model.camera
        Group {
            if cam?.on == true {
                ZStack {
                    RoundedRectangle(cornerRadius: 4).fill(p.lcd)
                    if let photo {
                        Image(decorative: photo, scale: scale)
                            .resizable()
                            .aspectRatio(contentMode: .fit)
                    } else if let text = cam?.problem {
                        Text(text.uppercased())
                            .font(Theme.plex(12))
                            .foregroundStyle(p.lcdInk)
                    }
                }
                .aspectRatio(cam?.aspect ?? 16 / 9, contentMode: .fit)
                .clipShape(RoundedRectangle(cornerRadius: 4))
                .overlay(RoundedRectangle(cornerRadius: 6).stroke(p.edge, lineWidth: 2))
                .task(id: Snap(active: phase == .active, settings: model.settings, camera: cam?.id,
                               view: model.cameraView)) {
                    await load()
                }
            } else if let img = model.modelImage, model.status?.busy == true {
                Image(decorative: img, scale: 3)
                    .resizable()
                    .aspectRatio(contentMode: .fit)
                    .frame(maxWidth: .infinity, maxHeight: 160)
                    .padding(10)
                    .background(RoundedRectangle(cornerRadius: 4).fill(p.lcd))
                    .overlay(RoundedRectangle(cornerRadius: 6).stroke(p.edge, lineWidth: 2))
            } else {
                LCDView(screen: .status(LCDModel.status(model.status, connected: model.settings.isComplete,
                                                        error: model.error, now: Date(), hint: "",
                                                        live: model.live, notice: model.noticeNow)))
            }
        }
        .onChange(of: model.settings) { photo = nil }
        .onChange(of: cam?.id) { photo = nil }
    }

    /// The snapshots of this camera run while this is active, from the deck of these settings.
    private struct Snap: Equatable {
        let active: Bool
        let settings: DeckSettings
        let camera: String?
        let view: String
    }

    private func load() async {
        guard phase == .active else { return }
        let client = DeckClient(settings: model.settings)
        while !Task.isCancelled {
            if let img = await client.snapshot(width: 640, view: model.cameraView, camera: model.camera?.id) {
                photo = img
            }
            try? await Task.sleep(for: .seconds(10))
        }
    }
}

/// The card of a deck away from home: the live print of the deck at the service, with the picture
/// of the model, the progress and the time left.
private struct AwayCard: View {
    @Environment(\.palette) private var p
    let title: String
    let live: LivePrint
    /// When the app read the service.
    let time: Date?

    var body: some View {
        Plate(label: title) {
            if let url = live.model {
                AsyncImage(url: url) { img in
                    img.resizable().aspectRatio(contentMode: .fit)
                } placeholder: {
                    Color.clear
                }
                .frame(maxWidth: .infinity, maxHeight: 160)
                .padding(10)
                .background(RoundedRectangle(cornerRadius: 4).fill(p.lcd))
                .overlay(RoundedRectangle(cornerRadius: 6).stroke(p.edge, lineWidth: 2))
            }
            HStack(alignment: .firstTextBaseline) {
                Text(stateText.uppercased())
                    .font(Theme.plex(17))
                    .foregroundStyle(p.text)
                    .lineLimit(1)
                    .minimumScaleFactor(0.7)
                Spacer()
                if !live.idle {
                    Text(Format.percent(live.percent / 100))
                        .font(Theme.plex(28, bold: false))
                        .foregroundStyle(p.text)
                }
            }
            .padding(.horizontal, 4)
            VStack(alignment: .leading, spacing: 8) {
                Text(live.project.isEmpty ? live.name : live.name + " · " + live.project)
                    .opacity(live.name.isEmpty ? 0 : 1)
                    .font(Theme.plex(14, bold: false))
                    .foregroundStyle(p.dim)
                    .lineLimit(1)
                HStack {
                    if live.printing, live.left > 0, !live.paused {
                        Text(Format.duration(Double(live.left)) + " left")
                    }
                    Spacer()
                    Text("Away · from the service")
                }
                .font(Theme.plex(13))
                .foregroundStyle(p.dim)
                if !live.idle { Playhead(value: live.percent / 100) }
            }
            .padding(.horizontal, 4)
        }
        .contentShape(Rectangle())
    }

    private var stateText: String {
        switch live.result {
        case "complete": return "Done"
        case "cancelled": return "Cancelled"
        case "error": return "Failed"
        default: break
        }
        if live.idle { return "Ready" }
        if live.paused { return "Paused" }
        if live.layers > 0 { return "Layer \(live.layer) of \(live.layers)" }
        return "Printing"
    }
}

/// The sign-in to printdeck.beetlebug.org. Away from home, the cards show the live prints of the
/// projects of the session.
private struct AccountPlate: View {
    @Environment(\.palette) private var p
    @State private var account = Account.shared

    var body: some View {
        Plate(label: "Printdeck account") {
            DisplayWindow {
                Text(title)
                    .font(Theme.plex(16))
                    .lineLimit(1)
                    .minimumScaleFactor(0.6)
                Text(line)
                    .font(Theme.plex(13, bold: false))
                    .opacity(0.8)
            }
            if account.signedIn {
                Button("Sign out") { account.signOut() }
                    .buttonStyle(KeyStyle(cap: .danger))
            } else {
                Button("Sign in with passkey") { Task { await account.signIn() } }
                    .buttonStyle(KeyStyle(cap: .orange))
                    .disabled(account.busy)
            }
            Text("Away from home, a card shows the print of its deck from the projects at \(ServiceClient.relyingParty).")
                .font(Theme.plex(12, bold: false))
                .foregroundStyle(p.dim)
                .padding(.horizontal, 4)
        }
    }

    private var title: String {
        guard account.signedIn, let s = account.session else { return "NOT SIGNED IN" }
        return ((s.name ?? "Signed in") + " · " + s.kind).uppercased()
    }

    private var line: String {
        if let m = account.message { return m.uppercased() }
        guard account.signedIn, let s = account.session else { return ServiceClient.relyingParty.uppercased() }
        return "UNTIL " + LCDModel.clock(s.expires)
    }
}
