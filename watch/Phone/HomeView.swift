// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

/// The home screen: a card for each printer, in the order of the list. The printers of a deck with
/// many printers go under the name of the deck. A tap opens the screen of the printer. With no
/// printer, the home screen is the setup of the first deck.
struct HomeView: View {
    @Environment(Fleet.self) private var fleet
    let link: WatchLink
    /// Opens the screen of a printer ID.
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
        .sheet(isPresented: $adding) {
            NavigationStack {
                SetupView(link: link, model: nil) { adding = false } added: { id in
                    adding = false
                    open(id)
                }
            }
            .environment(fleet)
        }
    }

    private var cards: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 14) {
                ForEach(fleet.refusedDecks, id: \.self) { root in
                    RefusedKeyCard(root: root)
                }
                ForEach(fleet.groups, id: \.first!.id) { group in
                    if group.count > 1 {
                        Text(group[0].deck.deckName ?? group[0].deck.deckRoot)
                            .font(.footnote.weight(.semibold))
                            .textCase(.uppercase)
                            .foregroundStyle(.secondary)
                            .padding(.horizontal, 4)
                            .padding(.top, 6)
                    }
                    ForEach(group) { m in
                        Button { open(m.id) } label: {
                            PrinterCard().environment(m)
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
            .padding(.horizontal, 16)
            .padding(.bottom, 24)
        }
        .background(Color(.systemGroupedBackground))
        .navigationTitle("Printers")
        .navigationBarTitleDisplayMode(.large)
        .toolbar {
            Button { adding = true } label: { Label("Add a deck", systemImage: "plus") }
        }
        .refreshable { await fleet.refresh() }
    }
}

/// A deck that refused its deck key: the owner removed the key on the deck. Each printer of the
/// deck fails, and one Connect again asks the deck for a new key for all of them.
private struct RefusedKeyCard: View {
    @Environment(Fleet.self) private var fleet
    let root: String

    @State private var busy = false
    @State private var message: String?

    var body: some View {
        let name = fleet.decks.first { $0.deckRoot == root }?.deckName ?? root
        VStack(alignment: .leading, spacing: 10) {
            Label("\(name) does not accept the key", systemImage: "key.slash")
                .font(.headline)
            Text(message ?? "Connect again, then tap Allow on the touch screen of the deck. The new key works for each printer of the deck.")
                .font(.subheadline)
                .foregroundStyle(.secondary)
            Button {
                Task { await connect(name) }
            } label: {
                Text(busy ? "Waiting for Allow" : "Connect again").frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .disabled(busy)
        }
        .padding(16)
        .background(RoundedRectangle(cornerRadius: 20).fill(Color(.secondarySystemGroupedBackground)))
    }

    private func connect(_ name: String) async {
        guard let base = DeckSettings(address: root).baseURL else { return }
        busy = true
        defer { busy = false }
        message = nil
        do {
            let g = try await DeckClient.requestKey(base: base, app: "Printdeck on iPhone")
            await fleet.connect(address: root, grant: g, name: name)
        } catch {
            message = errorText(error)
        }
    }
}

/// The card of a printer: the picture, the name, the state, and the progress of a print. When the
/// iPhone cannot reach the deck, the card shows the live print of the printer at the service.
struct PrinterCard: View {
    @Environment(DeckModel.self) private var model
    @State private var account = Account.shared

    var body: some View {
        let away = !model.live && model.error != nil && model.problemNow == nil ? account.live(of: model.deck) : nil
        VStack(alignment: .leading, spacing: 12) {
            CardPicture(away: away)
                .overlay(alignment: .topTrailing) {
                    if StateText.needsYou(model.status) && away == nil {
                        Label("Needs you", systemImage: "exclamationmark.circle.fill")
                            .font(.caption.weight(.semibold))
                            .padding(.horizontal, 8)
                            .padding(.vertical, 4)
                            .background(Capsule().fill(.ultraThinMaterial))
                            .padding(10)
                    }
                }
            if let away { awayText(away) } else { homeText }
        }
        .padding(12)
        .background(RoundedRectangle(cornerRadius: 22).fill(Color(.secondarySystemGroupedBackground)))
        .contentShape(RoundedRectangle(cornerRadius: 22))
    }

    private var homeText: some View {
        let st = model.status
        let busy = st?.busy == true && model.live
        return VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(model.deck.title)
                    .font(.headline)
                    .lineLimit(1)
                KindIcon(process: model.deck.process)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
                if busy, let st {
                    Text(Format.percent(st.progress ?? 0))
                        .font(.title3.weight(.semibold).monospacedDigit())
                }
            }
            Text(stateLine)
                .font(.subheadline)
                .foregroundStyle(stateColor)
                .lineLimit(1)
            if busy, let st {
                ProgressLine(value: st.progress ?? 0, tint: st.paused ? .secondary : .accentColor)
                    .padding(.top, 2)
                HStack {
                    if let left = st.left { Text(Format.duration(left) + " left") }
                    Spacer()
                    if let end = st.end { Text("Ends " + Format.clock(end)) }
                }
                .font(.footnote.monospacedDigit())
                .foregroundStyle(.secondary)
            }
        }
        .padding(.horizontal, 4)
    }

    /// The state, with the name of the print: "Layer 12 of 27 · Benchy".
    private var stateLine: String {
        switch model.problemNow {
        case .gone: return "Not on this deck now"
        case .key where model.deck.deckKey == true: return "Waiting for a new key"
        default: break
        }
        let text = StateText.of(model.status, live: model.live, error: model.error)
        if let st = model.status, st.busy, model.live, let f = st.file { return text + " · " + f }
        return text
    }

    private var stateColor: Color {
        if model.problemNow == nil, model.error != nil { return .red }
        if model.status?.paused == true { return .orange }
        return .secondary
    }

    private func awayText(_ live: LivePrint) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(model.deck.title).font(.headline).lineLimit(1)
                Spacer()
                if !live.idle {
                    Text(Format.percent(live.percent / 100)).font(.title3.weight(.semibold).monospacedDigit())
                }
            }
            Text(awayState(live) + (live.name.isEmpty ? "" : " · " + live.name))
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            if !live.idle { ProgressLine(value: live.percent / 100) }
            HStack {
                if live.printing, live.left > 0, !live.paused { Text(Format.duration(Double(live.left)) + " left") }
                Spacer()
                Label("Away", systemImage: "icloud")
            }
            .font(.footnote)
            .foregroundStyle(.secondary)
            if live.queue > 0 {
                Text("\(live.queue) in the queue" + (live.queueSeconds > 0 ? " · " + Format.duration(Double(live.queueSeconds)) : ""))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.horizontal, 4)
    }

    private func awayState(_ live: LivePrint) -> String {
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

/// The picture of a card: a still of the camera of the printer, else the model of the print,
/// else the color and the name of the printer (PrinterArt). The first still of a network camera
/// can be slow or fail: the card reads it again with a growing wait, and keeps the picture under
/// it until a still comes. Away, the picture is the model from the service.
private struct CardPicture: View {
    @Environment(DeckModel.self) private var model
    @Environment(\.displayScale) private var scale
    @Environment(\.scenePhase) private var phase
    let away: LivePrint?

    @State private var photo: CGImage?
    /// The card is on the screen: the stills run only then.
    @State private var visible = false

    var body: some View {
        let cam = model.camera
        Color.black.opacity(0.9)
            .aspectRatio(16 / 10, contentMode: .fit)
            .overlay { content }
            .overlay(alignment: .topLeading) {
                // PrinterArt has the label large.
                if !showsArt { LabelChip(deck: model.deck, size: 13).padding(10) }
            }
            .clipShape(RoundedRectangle(cornerRadius: 14))
            .onAppear { visible = true }
            .onDisappear { visible = false }
            .task(id: Snap(active: phase == .active && visible && model.hasCameraPicture && away == nil,
                           settings: model.settings, camera: cam?.id, view: model.cameraView)) {
                await load()
            }
            .onChange(of: model.settings) { photo = nil }
            .onChange(of: cam?.id) { photo = nil }
    }

    @ViewBuilder private var content: some View {
        if let away {
            if let url = away.model { ServiceImage(url: url).padding(16) } else { PrinterArt() }
        } else if model.hasCameraPicture, let photo {
            Image(decorative: photo, scale: scale)
                .resizable()
                .aspectRatio(contentMode: .fill)
        } else if let img = model.modelImage, model.status?.busy == true {
            Image(decorative: img, scale: 3)
                .resizable()
                .aspectRatio(contentMode: .fit)
                .padding(16)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            PrinterArt()
        }
    }

    /// The card shows PrinterArt: no still, no model, and no picture from the service.
    private var showsArt: Bool {
        if let away { return away.model == nil }
        if model.hasCameraPicture, photo != nil { return false }
        return !(model.modelImage != nil && model.status?.busy == true)
    }

    /// The stills of this camera run while this is active, from the deck of these settings.
    private struct Snap: Equatable {
        let active: Bool
        let settings: DeckSettings
        let camera: String?
        let view: String
    }

    /// Reads a still every 15 seconds while the card is on the screen, at the width of the card.
    /// The last still stays on the card while the next one loads. After a failure it waits 2, 4, 8
    /// up to 60 seconds. A printer with no such camera (404) stops the reads.
    private func load() async {
        guard phase == .active, visible, model.hasCameraPicture, away == nil else { return }
        let client = DeckClient(settings: model.settings)
        var failures = 0
        while !Task.isCancelled {
            do {
                photo = try await client.still(path: model.deck.stillPath ?? "/webcam/snapshot", width: 960,
                                               view: model.cameraView, camera: model.camera?.id)
                failures = 0
                try? await Task.sleep(for: .seconds(15))
            } catch DeckError.http(404) {
                return
            } catch {
                if Task.isCancelled { return }
                failures += 1
                try? await Task.sleep(for: .seconds(DeckClient.retryDelay(failures)))
            }
        }
    }
}

/// The sign-in to printdeck.beetlebug.org. Away from home, the cards show the live prints of the
/// projects of the session.
struct AccountPlate: View {
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

