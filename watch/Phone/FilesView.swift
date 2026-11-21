// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

/// The files of the printer, newest first, in their folders, with a search. A tap asks to print
/// the file, and a swipe deletes it. Each dialog names the printer. A resin printer, and a printer
/// that prints, list the files with no Print.
struct FilesView: View {
    @Environment(DeckModel.self) private var model
    let close: () -> Void

    @State private var files: [DeckFile] = []
    @State private var message: String? = "Reading the files"
    @State private var search = ""
    @State private var printing: DeckFile?
    @State private var deleting: DeckFile?

    var body: some View {
        let short = model.deck.label ?? model.deck.title
        List {
            if let message {
                Text(message).foregroundStyle(.secondary)
            }
            ForEach(DeckFile.folders(shown), id: \.name) { folder in
                Section(folder.name.isEmpty ? (folder.files.count == shown.count ? "Newest first" : "Files") : folder.name) {
                    ForEach(folder.files) { f in
                        Button { if model.controls.prints { printing = f } } label: { row(f) }
                            .foregroundStyle(.primary)
                            .swipeActions {
                                Button("Delete", role: .destructive) { deleting = f }
                            }
                    }
                }
            }
        }
        .listStyle(.insetGrouped)
        .searchable(text: $search, prompt: "Search the files")
        .navigationTitle("Files · \(short)")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { Button("Done", action: close) }
        .refreshable { await load() }
        .task { await load() }
        .confirmationDialog(printing.map { "Print \($0.name) on \(model.deck.title)?" } ?? "",
                            isPresented: Binding(get: { printing != nil }, set: { if !$0 { printing = nil } }),
                            titleVisibility: .visible, presenting: printing) { f in
            Button("Print on \(short)") { Task { await print(f) } }
            Button("Back", role: .cancel) {}
        } message: { _ in
            Text("The deck asks on its touch screen when the material of the file is not the material in the printer.")
        }
        .confirmationDialog(deleting.map { "Delete \($0.name) from \(model.deck.title)?" } ?? "",
                            isPresented: Binding(get: { deleting != nil }, set: { if !$0 { deleting = nil } }),
                            titleVisibility: .visible, presenting: deleting) { f in
            Button("Delete", role: .destructive) { Task { await delete(f) } }
            Button("Back", role: .cancel) {}
        }
    }

    private var shown: [DeckFile] {
        let q = search.trimmingCharacters(in: .whitespaces)
        return q.isEmpty ? files : files.filter { $0.path.localizedCaseInsensitiveContains(q) }
    }

    private func row(_ f: DeckFile) -> some View {
        HStack(spacing: 12) {
            Image(systemName: model.deck.process == .resin ? "drop" : "doc.text")
                .font(.title3)
                .foregroundStyle(.secondary)
                .frame(width: 28)
            VStack(alignment: .leading, spacing: 2) {
                Text(f.name).lineLimit(2)
                Text(detail(f)).font(.caption).foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
            if model.controls.prints {
                Image(systemName: "play.circle").font(.title3).foregroundStyle(.tint)
                    .accessibilityLabel("Print")
            }
        }
    }

    private func detail(_ f: DeckFile) -> String {
        var parts: [String] = []
        if let d = f.date { parts.append(d.formatted(.relative(presentation: .named))) }
        if let s = f.size { parts.append(ByteCountFormatter.string(fromByteCount: Int64(s), countStyle: .file)) }
        return parts.joined(separator: " · ")
    }

    private func load() async {
        do {
            files = try await DeckClient(settings: model.settings).files()
            message = files.isEmpty ? "No files. Send one from the slicer." : nil
        } catch {
            message = errorText(error)
        }
    }

    private func print(_ f: DeckFile) async {
        // Only an FDM printer with no print starts a print from the app.
        guard model.controls.prints else {
            message = "Start a resin print on the touch screen of the deck."
            return
        }
        await model.run("Starting \(f.name)", effect: .print(f.name), taken: close) { try await $0.printFile(f.path) }
    }

    private func delete(_ f: DeckFile) async {
        do {
            try await DeckClient(settings: model.settings).deleteFile(f.path)
            files.removeAll { $0.path == f.path }
        } catch {
            message = errorText(error)
        }
    }
}
