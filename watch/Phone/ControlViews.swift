// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

/// A heater of a printer that the app sets: the nozzle (tool0) or the bed.
enum Heater: String, Identifiable {
    case nozzle, bed
    var id: String { rawValue }

    var name: String { self == .nozzle ? "Nozzle" : "Bed" }
    var presets: [Preset] { self == .nozzle ? Preset.tool : Preset.bed }
    var limit: Int { self == .nozzle ? 300 : 120 }
}

/// The temperatures of the printer, from the status screen of the deck, and the fan. A tap on
/// the nozzle or the bed sets its target.
struct TemperatureSection: View {
    @Environment(DeckModel.self) private var model
    @Binding var heater: Heater?

    var body: some View {
        let c = model.controls
        if model.deck.process != .resin, let st = model.status {
            Section {
                row(.nozzle, actual: st.lcd?.tool ?? st.tool?.actual, target: st.lcd?.tool_target ?? st.tool?.target, on: c.heats)
                row(.bed, actual: st.lcd?.bed ?? st.bed?.actual, target: st.lcd?.bed_target ?? st.bed?.target, on: c.heats)
                FanRow()
            } header: {
                Text("Temperatures")
            } footer: {
                let aux = c.heats && st.nozzles?.contains(where: { $0.role == "aux" }) == true
                if aux || c.bambu {
                    Text([aux ? "The app sets the main nozzle. Set the aux nozzle on the deck." : "",
                          c.bambu ? "Set the fan of a Bambu Lab printer on the deck." : ""]
                        .filter { !$0.isEmpty }.joined(separator: " "))
                } else if c.signedOnly, c.connected {
                    Text("With Signed prints only on, the deck refuses a fan speed from apps.")
                }
            }
        }
    }

    private func row(_ h: Heater, actual: Double?, target: Double?, on: Bool) -> some View {
        Button { heater = h } label: {
            HStack {
                RowLabel(h.name, symbol: h == .nozzle ? "flame" : "square.stack.3d.down.forward")
                Spacer()
                Text(actual.map { "\(Int($0.rounded()))°" } ?? "--")
                    .font(.body.monospacedDigit().weight(.semibold))
                    .foregroundStyle(Color.primary)
                Text(target.map { $0 > 0 ? "→ \(Int($0.rounded()))°" : "Off" } ?? "")
                    .font(.subheadline.monospacedDigit())
                    .foregroundStyle(Color.secondary)
                    .frame(minWidth: 52, alignment: .trailing)
            }
        }
        .disabled(!on)
    }
}

/// The part fan: its speed from the status screen of the deck, and a menu of speeds. The deck
/// sets the fan with G-code, so the menu needs a Klipper printer and Signed prints only off.
private struct FanRow: View {
    @Environment(DeckModel.self) private var model

    var body: some View {
        let c = model.controls
        let fan = model.status?.lcd?.fan
        Menu {
            ForEach([0, 25, 50, 75, 100], id: \.self) { pct in
                Button(pct == 0 ? "Off" : "\(pct)%") {
                    Task { await model.run("Fan \(pct == 0 ? "off" : "\(pct)%")", effect: .fan(pct)) { try await $0.gcode([Gcode.fan(pct)]) } }
                }
            }
        } label: {
            HStack {
                RowLabel("Fan", symbol: "fan")
                Spacer()
                Text(fan.map { $0 > 0 ? Format.percent($0) : "Off" } ?? "--")
                    .font(.body.monospacedDigit().weight(.semibold))
                    .foregroundStyle(Color.primary)
            }
        }
        .disabled(!c.fan)
    }
}

/// The sheet of a heater: the presets of the deck, and a target to the degree. The key names the
/// printer.
struct HeaterSheet: View {
    @Environment(DeckModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let heater: Heater

    @State private var target = 0
    /// The user changed the target in the sheet.
    @State private var edited = false

    var body: some View {
        let short = model.deck.label ?? model.deck.title
        NavigationStack {
            Form {
                Section {
                    HStack(spacing: 8) {
                        ForEach(heater.presets, id: \.self) { p in
                            Button {
                                target = p.value
                            } label: {
                                VStack(spacing: 2) {
                                    Text(p.name).font(.subheadline.weight(.semibold))
                                    Text(p.value == 0 ? " " : "\(p.value)°").font(.caption.monospacedDigit())
                                }
                                .frame(maxWidth: .infinity)
                            }
                            .buttonStyle(.bordered)
                            .tint(target == p.value ? .accentColor : .secondary)
                        }
                    }
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets())
                }
                Section {
                    Stepper(value: $target, in: 0...heater.limit, step: 5) {
                        Text(target == 0 ? "Off" : "\(target)°")
                            .font(.title2.monospacedDigit().weight(.semibold))
                    }
                } footer: {
                    Text("The target goes to \(model.deck.title).")
                }
                Section {
                    Button {
                        let t = target
                        Task {
                            await model.run(t == 0 ? "\(heater.name) off" : "Heating the \(heater.name.lowercased())",
                                            effect: heater == .nozzle ? .nozzleTarget(t) : .bedTarget(t), taken: { dismiss() }) {
                                if heater == .nozzle { try await $0.setTool(t) } else { try await $0.setBed(t) }
                            }
                        }
                    } label: {
                        Text(target == 0 ? "Turn off the \(heater.name.lowercased()) of \(short)" : "Heat \(short) to \(target)°")
                            .frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.borderedProminent)
                    .controlSize(.large)
                    .disabled(model.sending)
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets())
                }
            }
            .navigationTitle("\(heater.name) · \(short)")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { Button("Done") { dismiss() } }
        }
        .presentationDetents([.medium, .large])
        .onAppear { target = current }
        .onChange(of: current) { if !edited { target = current } }
        .onChange(of: target) { if target != current { edited = true } }
    }

    /// The target of the heater on the printer now.
    private var current: Int {
        let lcd = model.status?.lcd
        let now = heater == .nozzle ? lcd?.tool_target ?? model.status?.tool?.target : lcd?.bed_target ?? model.status?.bed?.target
        return Int((now ?? 0).rounded())
    }
}

/// The spools that the printer has loaded: the trays of each AMS, the external spool, or the roll
/// of a Klipper printer.
struct FilamentSection: View {
    @Environment(DeckModel.self) private var model

    var body: some View {
        let st = model.status
        let list = st?.filaments ?? []
        if model.deck.process != .resin, !list.isEmpty || st?.roll != nil {
            let units = Filament.units(list)
            ForEach(units, id: \.unit) { u in
                Section("AMS \(u.unit)") {
                    ForEach(u.trays) { FilamentRow(filament: $0) }
                }
            }
            Section {
                ForEach(list.filter { !$0.inAMS && $0.slot == "Ext" }) { FilamentRow(filament: $0) }
                if units.isEmpty, let roll = st?.roll {
                    RollRow(roll: roll)
                } else if units.isEmpty {
                    ForEach(list.filter { !$0.inAMS && $0.slot != "Ext" }) { FilamentRow(filament: $0) }
                }
            } header: {
                Text(units.isEmpty ? "Filament" : "External")
            } footer: {
                Text("Load and unload the filament on the touch screen of the deck.")
            }
        }
    }
}

/// A swatch of the color of a spool, with its edge.
private struct Swatch: View {
    let hex: String?

    var body: some View {
        Circle()
            .fill(hex.flatMap(Color.init(hex:)) ?? Color(.tertiarySystemFill))
            .overlay(Circle().stroke(.primary.opacity(0.2), lineWidth: 1))
            .frame(width: 26, height: 26)
            .accessibilityHidden(true)
    }
}

/// A loaded spool: the slot, the material and the name, and the part that is left.
private struct FilamentRow: View {
    let filament: Filament

    var body: some View {
        let f = filament
        HStack(spacing: 12) {
            Swatch(hex: f.color)
            VStack(alignment: .leading, spacing: 2) {
                Text([f.slot == "Ext" ? "External spool" : f.slot, f.material].filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(.body.weight(.medium))
                Text([f.name ?? "", f.nozzle.map { $0 == "aux" ? "Aux nozzle" : "Main nozzle" } ?? ""]
                    .filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            if let r = f.remain {
                Gauge(value: r) { EmptyView() } currentValueLabel: { Text(Format.percent(r)).font(.caption2) }
                    .gaugeStyle(.accessoryCircularCapacity)
                    .scaleEffect(0.7)
                    .frame(width: 36, height: 36)
                    .tint(r < 0.15 ? .red : .accentColor)
                    .accessibilityLabel("\(Format.percent(r)) left")
            }
        }
    }
}

/// The roll of a Klipper printer, from the rolls of the deck: its name and the grams left.
private struct RollRow: View {
    let roll: Roll

    var body: some View {
        HStack(spacing: 12) {
            Swatch(hex: roll.color)
            VStack(alignment: .leading, spacing: 2) {
                Text(roll.name).font(.body.weight(.medium))
                Text(roll.material).font(.caption).foregroundStyle(.secondary)
            }
            Spacer()
            Text("\(Int(roll.left.rounded())) g")
                .font(.subheadline.monospacedDigit())
                .foregroundStyle(.secondary)
        }
    }
}

/// The moves of the head, and the Z offset of a print.
struct ControlSection: View {
    @Environment(DeckModel.self) private var model

    var body: some View {
        let c = model.controls
        if model.deck.process != .resin {
            Section {
                NavigationLink {
                    MoveView().environment(model)
                } label: {
                    RowLabel("Move and home", symbol: "arrow.up.and.down.and.arrow.left.and.right")
                }
                .disabled(!c.moves)
                if c.tunes {
                    HStack {
                        RowLabel("Z offset", symbol: "arrow.up.and.down")
                        Spacer()
                        Button { babystep(-0.025) } label: { Image(systemName: "minus") }
                            .accessibilityLabel("Lower the nozzle 0.025 mm")
                        Text("0.025 mm").font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                        Button { babystep(0.025) } label: { Image(systemName: "plus") }
                            .accessibilityLabel("Raise the nozzle 0.025 mm")
                    }
                    .buttonStyle(.bordered)
                    .disabled(model.sending)
                }
            } footer: {
                if let reason = footer(c) { Text(reason) }
            }
        }
    }

    private func babystep(_ mm: Double) {
        Task { await model.run(mm < 0 ? "Nozzle down 0.025 mm" : "Nozzle up 0.025 mm", effect: .babystep(mm)) { try await $0.gcode([Gcode.babystep(mm)]) } }
    }

    /// Why a control is off, or nil.
    private func footer(_ c: Controls) -> String? {
        if !c.connected { return nil }
        if c.busy && c.bambu { return "The head moves between prints." }
        if c.busy && c.signedOnly { return "The head moves between prints. With Signed prints only on, the deck refuses a Z offset from apps." }
        if c.busy { return "The head moves between prints. − and + move the nozzle 0.025 mm during the print." }
        return nil
    }
}

/// Move and home: the head in X and Y, the bed or the head in Z, with a step of 0.1 to 50 mm.
/// Home asks first, and names the printer.
struct MoveView: View {
    @Environment(DeckModel.self) private var model

    @State private var step = 10.0
    @State private var homing: [String]?

    private let steps = [0.1, 1, 10, 50]

    var body: some View {
        let c = model.controls
        let short = model.deck.label ?? model.deck.title
        Form {
            Section {
                Picker("Step", selection: $step) {
                    ForEach(steps, id: \.self) { s in Text(s < 1 ? "0.1 mm" : "\(Int(s)) mm").tag(s) }
                }
                .pickerStyle(.segmented)
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets())
            }
            Section("X and Y") {
                VStack(spacing: 10) {
                    key("arrow.up", "Y +") { try await $0.jog(y: step) }
                    HStack(spacing: 10) {
                        key("arrow.left", "X −") { try await $0.jog(x: -step) }
                        Button { homing = ["x", "y"] } label: {
                            Image(systemName: "house").font(.title2).frame(width: 64, height: 64)
                        }
                        .buttonStyle(.bordered)
                        .accessibilityLabel("Home X and Y")
                        key("arrow.right", "X +") { try await $0.jog(x: step) }
                    }
                    key("arrow.down", "Y −") { try await $0.jog(y: -step) }
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, 8)
            }
            Section("Z") {
                HStack(spacing: 10) {
                    key("arrow.up.to.line", "Z +") { try await $0.jog(z: min(step, 10)) }
                    key("arrow.down.to.line", "Z −") { try await $0.jog(z: -min(step, 10)) }
                    Button { homing = ["z"] } label: {
                        Label("Home Z", systemImage: "house").frame(maxWidth: .infinity, minHeight: 52)
                    }
                    .buttonStyle(.bordered)
                }
            }
            Section {
                Button { homing = [] } label: {
                    Label("Home all axes", systemImage: "house.fill").frame(maxWidth: .infinity)
                }
            } footer: {
                Text("Z moves at most 10 mm a step. Home the axes before a long move.")
            }
        }
        .disabled(!c.moves || model.sending)
        .navigationTitle("Move · \(short)")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .principal) {
                HStack(spacing: 6) {
                    Text("Move").font(.headline)
                    LabelChip(deck: model.deck, size: 11)
                    if !model.deck.onSharedDeck { Text(model.deck.title).font(.headline).foregroundStyle(.secondary) }
                }
            }
        }
        .confirmationDialog(homeQuestion, isPresented: Binding(get: { homing != nil }, set: { if !$0 { homing = nil } }),
                            titleVisibility: .visible, presenting: homing) { axes in
            Button("Home \(short)") { Task { await model.run("Homing", effect: .home(axes)) { try await $0.home(axes) } } }
            Button("Back", role: .cancel) {}
        }
        .overlay(alignment: .bottom) {
            if let n = model.noticeNow {
                Text(n)
                    .font(.subheadline.weight(.medium))
                    .padding(.horizontal, 16)
                    .padding(.vertical, 10)
                    .background(Capsule().fill(.regularMaterial))
                    .padding(.bottom, 12)
            }
        }
    }

    private var homeQuestion: String {
        let what = switch homing ?? [] {
        case []: "all axes"
        case ["z"]: "Z"
        default: "X and Y"
        }
        return "Home \(what) of \(model.deck.title)?"
    }

    private func key(_ symbol: String, _ name: String, _ f: @escaping (DeckClient) async throws -> Void) -> some View {
        Button {
            Task { await model.run("Moving \(name)", effect: .jog, f) }
        } label: {
            Image(systemName: symbol).font(.title2).frame(width: 64, height: 52)
        }
        .buttonStyle(.bordered)
        .accessibilityLabel("Move \(name)")
    }
}
