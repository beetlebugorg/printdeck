// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

/// The queue of the printer on its screen: the files in their order, each with its material and
/// its time, and the total time. A swipe moves a file up or down, or removes it. The app has no
/// Start: only a press on the touch screen of the deck starts the next file. A deck with no queue
/// route, and an empty queue, show no section.
struct QueueSection: View {
    @Environment(DeckModel.self) private var model

    @State private var removing: DeckQueue.Item?
    @State private var busy = false
    @State private var message: String?

    var body: some View {
        Group {
            if let q = model.queue, !q.items.isEmpty {
                Section {
                    ForEach(Array(q.items.enumerated()), id: \.element.id) { i, item in
                        row(item, place: i)
                            .swipeActions(edge: .trailing) {
                                Button("Remove", role: .destructive) { removing = item }
                            }
                            .swipeActions(edge: .leading) {
                                if i > 0 {
                                    Button { Task { await change { try await $0.moveQueued(item.id, by: -1) } } } label: {
                                        Label("Up", systemImage: "arrow.up")
                                    }
                                    .tint(.blue)
                                }
                                if i < q.items.count - 1 {
                                    Button { Task { await change { try await $0.moveQueued(item.id, by: 1) } } } label: {
                                        Label("Down", systemImage: "arrow.down")
                                    }
                                    .tint(.indigo)
                                }
                            }
                            .disabled(busy)
                    }
                } header: {
                    HStack {
                        Text("Queue")
                        Spacer()
                        Text("\(q.items.count) file\(q.items.count == 1 ? "" : "s")" +
                             (q.seconds.map { $0 > 0 ? " · " + Format.duration(Double($0)) : "" } ?? ""))
                    }
                } footer: {
                    VStack(alignment: .leading, spacing: 4) {
                        Text(model.deck.process == .resin
                             ? "Check the vat and the build plate, then press Start on the deck for the next file."
                             : "Take the part off the bed, then press Start on the deck for the next file. Swipe a file to move or remove it.")
                        if let message { Text(message).foregroundStyle(.red) }
                    }
                }
            }
        }
        .confirmationDialog(removing.map { "Remove \($0.name) from the queue of \(model.deck.title)?" } ?? "",
                            isPresented: Binding(get: { removing != nil }, set: { if !$0 { removing = nil } }),
                            titleVisibility: .visible, presenting: removing) { item in
            Button("Remove", role: .destructive) { Task { await change { try await $0.removeQueued(item.id) } } }
            Button("Back", role: .cancel) {}
        }
    }

    private func row(_ item: DeckQueue.Item, place: Int) -> some View {
        HStack(spacing: 12) {
            Text("\(place + 1)")
                .font(.subheadline.monospacedDigit().weight(.semibold))
                .foregroundStyle(.secondary)
                .frame(width: 20)
            Circle()
                .fill(item.color.flatMap { $0.isEmpty ? nil : Color(hex: $0) } ?? Color(.tertiarySystemFill))
                .overlay(Circle().stroke(.primary.opacity(0.2), lineWidth: 1))
                .frame(width: 18, height: 18)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(item.name).lineLimit(1)
                Text(detail(item))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
        }
    }

    /// The material, the time and the project: "MatterHackers PLA Black · 45m · Bittlex".
    private func detail(_ item: DeckQueue.Item) -> String {
        [item.materialName ?? item.material ?? "",
         item.seconds.map { $0 > 0 ? Format.duration(Double($0)) : "" } ?? "",
         item.project ?? ""].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    private func change(_ f: (DeckClient) async throws -> DeckQueue) async {
        busy = true
        defer { busy = false }
        do {
            model.queue = try await f(DeckClient(settings: model.settings))
            message = nil
        } catch {
            message = errorText(error)
            await model.refreshQueue()
        }
    }
}
