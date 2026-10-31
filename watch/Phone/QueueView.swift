// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

/// The queue of the deck on the deck screen: the files in their order, each with its material and
/// its time, and the total time. Each file has Up, Down and Remove. The app has no Start: only a
/// press on the touch screen of the deck starts the next file. A deck with no queue route shows no
/// plate.
struct QueuePlate: View {
    @Environment(DeckModel.self) private var model
    @Environment(\.palette) private var p
    @Environment(\.scenePhase) private var phase

    @State private var queue: DeckQueue?
    @State private var removing: DeckQueue.Item?
    @State private var busy = false
    @State private var message: String?

    var body: some View {
        // The stack is there with no files too, so that its task reads the queue.
        VStack(spacing: 0) {
            if let q = queue, !q.items.isEmpty {
                Plate(label: "Queue · \(q.items.count) file\(q.items.count == 1 ? "" : "s")" +
                      (q.seconds.map { $0 > 0 ? " · " + Format.duration(Double($0)) : "" } ?? "")) {
                    ForEach(Array(q.items.enumerated()), id: \.element.id) { i, item in
                        row(item, place: i, count: q.items.count)
                    }
                    Text(model.deck.process == .resin
                         ? "Check the vat and the build plate, then press Start on the deck for the next file."
                         : "Take the part off the bed, then press Start on the deck for the next file.")
                        .font(Theme.plex(12, bold: false))
                        .foregroundStyle(p.dim)
                        .padding(.horizontal, 4)
                    if let message {
                        Text(message.uppercased())
                            .font(Theme.plex(12))
                            .foregroundStyle(p.red)
                            .padding(.horizontal, 4)
                    }
                }
            }
        }
        .task(id: Poll(active: phase == .active, settings: model.settings)) {
            while phase == .active && !Task.isCancelled {
                await load()
                try? await Task.sleep(for: .seconds(5))
            }
        }
        .overlay {
            if let item = removing {
                ConfirmPanel(question: "Remove \(item.name) from the queue?", yes: "Remove", danger: true,
                             onYes: {
                                 removing = nil
                                 Task { await change { try await $0.removeQueued(item.id) } }
                             },
                             onNo: { removing = nil })
            }
        }
    }

    private struct Poll: Equatable {
        let active: Bool
        let settings: DeckSettings
    }

    private func row(_ item: DeckQueue.Item, place: Int, count: Int) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 10) {
                Text("\(place + 1)")
                    .font(Theme.plex(15))
                    .foregroundStyle(p.dim)
                    .frame(width: 22)
                Circle()
                    .fill(item.color.flatMap { $0.isEmpty ? nil : Color(hex: $0) } ?? p.dim)
                    .overlay(Circle().stroke(p.edge, lineWidth: 1))
                    .frame(width: 16, height: 16)
                VStack(alignment: .leading, spacing: 2) {
                    Text(item.name)
                        .font(Theme.plex(15))
                        .foregroundStyle(p.text)
                        .lineLimit(1)
                    Text(detail(item))
                        .font(Theme.plex(12, bold: false))
                        .foregroundStyle(p.dim)
                        .lineLimit(1)
                }
                Spacer()
            }
            HStack(spacing: 8) {
                Button("Up") { Task { await change { try await $0.moveQueued(item.id, by: -1) } } }
                    .buttonStyle(KeyStyle())
                    .disabled(busy || place == 0)
                Button("Down") { Task { await change { try await $0.moveQueued(item.id, by: 1) } } }
                    .buttonStyle(KeyStyle())
                    .disabled(busy || place == count - 1)
                Button("Remove") { removing = item }
                    .buttonStyle(KeyStyle(cap: .danger))
                    .disabled(busy)
            }
            .environment(\.panelScale, 0.75)
        }
        .padding(.vertical, 2)
    }

    /// The material, the time and the project: "MatterHackers PLA Black · 45m · Bittlex".
    private func detail(_ item: DeckQueue.Item) -> String {
        [item.materialName ?? item.material ?? "",
         item.seconds.map { $0 > 0 ? Format.duration(Double($0)) : "" } ?? "",
         item.project ?? ""].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    private func load() async {
        guard model.settings.isComplete else { return }
        do {
            queue = try await DeckClient(settings: model.settings).queue()
            message = nil
        } catch {
            // A failed read keeps the last queue. The deck screen shows the error of the deck.
        }
    }

    private func change(_ f: (DeckClient) async throws -> DeckQueue) async {
        busy = true
        defer { busy = false }
        do {
            queue = try await f(DeckClient(settings: model.settings))
            message = nil
        } catch {
            message = errorText(error)
            await load()
        }
    }
}
