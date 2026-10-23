// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

/// The Files page of the deck: the G-code files, newest first. A tap asks to print the file.
struct FilesView: View {
    @Environment(DeckModel.self) private var model
    @Environment(\.palette) private var p
    let close: () -> Void

    @State private var files: [DeckFile] = []
    @State private var message: String? = "Reading the files"
    @State private var confirm: DeckFile?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 10) {
                Heading(title: "Files") {
                    Button("Back", action: close)
                        .buttonStyle(KeyStyle())
                        .frame(width: 100)
                        .environment(\.panelScale, 0.75)
                }
                .padding(.top, 8)
                Text(message?.uppercased() ?? "NEWEST FIRST · \(files.count) FILE\(files.count == 1 ? "" : "S")")
                    .font(Theme.plex(13))
                    .foregroundStyle(p.dim)
                ForEach(files) { f in
                    Button { confirm = f } label: { row(f) }
                        .buttonStyle(.plain)
                }
            }
            .padding(16)
        }
        .background(p.bg.ignoresSafeArea())
        .task { await load() }
        .overlay {
            if let f = confirm {
                ConfirmPanel(question: "Print \(f.name)?", yes: "Print",
                             onYes: {
                                 confirm = nil
                                 Task { await print(f) }
                             },
                             onNo: { confirm = nil })
            }
        }
    }

    private func row(_ f: DeckFile) -> some View {
        HStack(spacing: 14) {
            // The thumbnail of the deck: a part on the bed, in a dark window.
            ZStack(alignment: .bottom) {
                RoundedRectangle(cornerRadius: 4).fill(p.lcd)
                VStack(spacing: 0) {
                    Rectangle().fill(p.lcdDim).frame(width: 14, height: 12)
                    Trapezoid().fill(p.orange).frame(width: 34, height: 12)
                    Rectangle().fill(p.lcdDim).frame(width: 54, height: 3)
                }
                .padding(.bottom, 10)
            }
            .frame(width: 66, height: 66)
            .overlay(RoundedRectangle(cornerRadius: 4).stroke(p.edge, lineWidth: 2))
            VStack(alignment: .leading, spacing: 6) {
                Text(f.name)
                    .font(Theme.plex(18))
                    .foregroundStyle(p.text)
                    .lineLimit(1)
                Text(detail(f))
                    .font(Theme.plex(15, bold: false))
                    .foregroundStyle(p.dim)
            }
            Spacer(minLength: 0)
        }
        .padding(10)
        .background(RoundedRectangle(cornerRadius: 5).fill(p.surface))
        .overlay(RoundedRectangle(cornerRadius: 5).stroke(p.edge, lineWidth: 1))
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
        do {
            try await DeckClient(settings: model.settings).printFile(f.path)
            await model.refresh()
            close()
        } catch {
            message = errorText(error)
        }
    }
}

/// A trapezoid: the part on the bed in the thumbnails of the deck.
struct Trapezoid: Shape {
    func path(in r: CGRect) -> Path {
        var p = Path()
        p.move(to: CGPoint(x: r.minX + r.width * 0.2, y: r.minY))
        p.addLine(to: CGPoint(x: r.maxX - r.width * 0.2, y: r.minY))
        p.addLine(to: CGPoint(x: r.maxX, y: r.maxY))
        p.addLine(to: CGPoint(x: r.minX, y: r.maxY))
        p.closeSubpath()
        return p
    }
}
