// SPDX-License-Identifier: MIT
// Copyright (c) 2026 the Printdeck authors. See watch/LICENSE.

import SwiftUI

/// A heater of a printer that the app sets: the nozzle (tool0), the aux nozzle (tool1), the bed,
/// or the chamber.
enum Heater: String, Identifiable {
    case nozzle, aux, bed, chamber
    var id: String { rawValue }

    var name: String {
        switch self {
        case .nozzle: "Nozzle"
        case .aux: "Aux nozzle"
        case .bed: "Bed"
        case .chamber: "Chamber"
        }
    }

    var presets: [Preset] {
        switch self {
        case .nozzle, .aux: Preset.tool
        case .bed: Preset.bed
        case .chamber: Preset.chamber
        }
    }

    /// The highest target: the keypad of the deck, and 80 °C for the chamber route.
    var limit: Int {
        switch self {
        case .nozzle, .aux: 300
        case .bed: 120
        case .chamber: 80
        }
    }

    var symbol: String {
        switch self {
        case .nozzle, .aux: "flame"
        case .bed: "square.stack.3d.down.forward"
        case .chamber: "cube.transparent"
        }
    }

    /// The target of the heater in a status, or nil.
    func target(_ st: Status?) -> Double? {
        switch self {
        case .nozzle: st?.lcd?.tool_target ?? st?.tool?.target
        case .aux: st?.controls?.tool1?.target ?? st?.tool1?.target
        case .bed: st?.lcd?.bed_target ?? st?.bed?.target
        case .chamber: st?.controls?.chamber?.target ?? st?.chamber?.target
        }
    }

    /// The temperature of the heater in a status, or nil.
    func actual(_ st: Status?) -> Double? {
        switch self {
        case .nozzle: st?.lcd?.tool ?? st?.tool?.actual
        case .aux: st?.controls?.tool1?.actual ?? st?.tool1?.actual
        case .bed: st?.lcd?.bed ?? st?.bed?.actual
        case .chamber: st?.controls?.chamber?.actual ?? st?.chamber?.actual
        }
    }

    var effect: (Int) -> Effect {
        switch self {
        case .nozzle: Effect.nozzleTarget
        case .aux: Effect.auxTarget
        case .bed: Effect.bedTarget
        case .chamber: Effect.chamberTarget
        }
    }

    /// Sends the target.
    func send(_ t: Int, _ c: DeckClient) async throws {
        switch self {
        case .nozzle: try await c.setTool(t)
        case .aux: try await c.setTool1(t)
        case .bed: try await c.setBed(t)
        case .chamber: try await c.setChamber(t)
        }
    }
}

/// The temperatures of the printer, and the fan. A tap on a heater sets its target. On a printer
/// with two nozzles, the nozzle that prints now has a mark (tool_now of the deck).
struct TemperatureSection: View {
    @Environment(DeckModel.self) private var model
    @Binding var heater: Heater?

    var body: some View {
        let c = model.controls
        if model.deck.process != .resin, let st = model.status {
            let twoNozzles = c.aux || st.nozzles?.contains(where: { $0.role == "aux" }) == true
            let now = twoNozzles ? st.controls?.tool_now : nil
            Section {
                row(.nozzle, st, on: c.heats, inUse: now == 0)
                if c.aux { row(.aux, st, on: c.aux, inUse: now == 1) }
                row(.bed, st, on: c.heats, inUse: false)
                if c.chamber { row(.chamber, st, on: c.chamber, inUse: false) }
                FanRow()
            } header: {
                Text("Temperatures")
            } footer: {
                if let text = footer(c, twoNozzles: twoNozzles) { Text(text) }
            }
        }
    }

    /// Why a control is not there, or nil.
    private func footer(_ c: Controls, twoNozzles: Bool) -> String? {
        guard c.connected else { return nil }
        var out: [String] = []
        if twoNozzles && !c.aux { out.append("The app sets the main nozzle. Set the aux nozzle on the deck.") }
        if !c.fan && c.list == nil && c.bambu { out.append("Set the fan of a Bambu Lab printer on the deck.") }
        if !c.fan && c.list == nil && c.signedOnly { out.append("With Signed prints only on, the deck refuses a fan speed from apps.") }
        return out.isEmpty ? nil : out.joined(separator: " ")
    }

    private func row(_ h: Heater, _ st: Status, on: Bool, inUse: Bool) -> some View {
        let actual = h.actual(st), target = h.target(st)
        return Button { heater = h } label: {
            HStack {
                RowLabel(h.name, symbol: h.symbol)
                if inUse {
                    Text("Printing")
                        .font(.caption2.weight(.semibold))
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(Capsule().fill(Color.accentColor.opacity(0.18)))
                        .foregroundStyle(Color.accentColor)
                        .accessibilityLabel("Prints now")
                }
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

/// The part fan: its speed, and a menu of speeds. A deck that lists the fan has a route for it.
/// From a deck before the list, the app sends G-code, which needs a Klipper printer and Signed
/// prints only off.
private struct FanRow: View {
    @Environment(DeckModel.self) private var model

    var body: some View {
        let c = model.controls
        let fan = model.status?.controls?.fan.map { $0 / 100 } ?? model.status?.lcd?.fan
        Menu {
            ForEach([0, 25, 50, 75, 100], id: \.self) { pct in
                Button(pct == 0 ? "Off" : "\(pct)%") {
                    let route = c.has("fan")
                    Task {
                        await model.run("Fan \(pct == 0 ? "off" : "\(pct)%")", effect: .fan(pct)) {
                            if route { try await $0.setFan(pct) } else { try await $0.gcode([Gcode.fan(pct)]) }
                        }
                    }
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
                            let h = heater
                            await model.run(t == 0 ? "\(h.name) off" : "Heating the \(h.name.lowercased())",
                                            effect: h.effect(t), taken: { dismiss() }) {
                                try await h.send(t, $0)
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
    private var current: Int { Int((heater.target(model.status) ?? 0).rounded()) }
}

/// The spools that the printer has loaded: the trays of each AMS, the external spools, or the
/// roll of a Klipper printer. A deck that lists "filament" loads a tray with a tap, and unloads;
/// a Klipper printer heats the nozzle first, as the swap of the deck. A deck that lists "ams_dry"
/// dries each AMS. The filament waits for a pause during a print.
struct FilamentSection: View {
    @Environment(DeckModel.self) private var model
    @State private var loading: Filament?
    @State private var unloading = false
    @State private var drying: ControlState.AMSUnit?
    @State private var stopping: ControlState.AMSUnit?
    @State private var klipper = false

    var body: some View {
        let st = model.status
        let c = model.controls
        let list = st?.filaments ?? []
        let active = st?.controls?.activeTray
        if model.deck.process != .resin, !list.isEmpty || st?.roll != nil || c.loads || c.extrudes {
            let units = Filament.units(list)
            let trays = !units.isEmpty || list.contains { $0.slot == "Ext" && $0.tray >= 0 }
            ForEach(units, id: \.unit) { u in
                Section {
                    if let a = st?.controls?.ams?.first(where: { $0.unit == u.unit }) { dryRow(a, c) }
                    ForEach(u.trays) { f in trayRow(f, active: active, c) }
                } header: {
                    Text(unitTitle(u.unit))
                }
            }
            Section {
                ForEach(list.filter { !$0.inAMS && $0.slot == "Ext" }) { f in trayRow(f, active: active, c) }
                if units.isEmpty, let roll = st?.roll {
                    RollRow(roll: roll)
                } else if units.isEmpty {
                    ForEach(list.filter { !$0.inAMS && $0.slot != "Ext" }) { FilamentRow(filament: $0) }
                }
                if trays && c.loads {
                    Button { unloading = true } label: { RowLabel("Unload", symbol: "arrow.uturn.backward") }
                } else if c.loads || c.extrudes {
                    Button { klipper = true } label: { RowLabel("Load, unload and extrude", symbol: "arrow.up.and.down.circle", chevron: true) }
                }
            } header: {
                Text(units.isEmpty ? "Filament" : "External")
            } footer: {
                Text(footer(c, trays: trays))
            }
            .confirmationDialog(loading.map { "Load \($0.slot == "Ext" ? "the external spool" : $0.slot) into \(model.deck.title)?" } ?? "",
                                isPresented: Binding(get: { loading != nil }, set: { if !$0 { loading = nil } }),
                                titleVisibility: .visible, presenting: loading) { f in
                Button("Load on \(model.deck.label ?? model.deck.title)") {
                    Task { await model.run("Loading \(f.slot)", effect: .load(f.tray)) { try await $0.loadFilament(tray: f.tray) } }
                }
                Button("Back", role: .cancel) {}
            } message: { _ in
                Text("The printer heats the nozzle for the material of the spool, and feeds it.")
            }
            .confirmationDialog("Unload the filament of \(model.deck.title)?", isPresented: $unloading, titleVisibility: .visible) {
                Button("Unload \(model.deck.label ?? model.deck.title)") {
                    Task { await model.run("Unloading", effect: .unload(tray: true)) { try await $0.unloadFilament() } }
                }
                Button("Back", role: .cancel) {}
            }
            .confirmationDialog(stopping.map { "Stop the drying of AMS \($0.unit) of \(model.deck.title)?" } ?? "",
                                isPresented: Binding(get: { stopping != nil }, set: { if !$0 { stopping = nil } }),
                                titleVisibility: .visible, presenting: stopping) { u in
                Button("Stop drying", role: .destructive) {
                    Task { await model.run("Stopping the drying", effect: .stopDrying(u.unit)) { try await $0.stopDrying(unit: u.unit) } }
                }
                Button("Back", role: .cancel) {}
            }
            .sheet(item: $drying) { u in DrySheet(unit: u, materials: units.first { $0.unit == u.unit }?.trays.map(\.material) ?? []).environment(model) }
            .sheet(isPresented: $klipper) { KlipperFilamentSheet().environment(model) }
        }
    }

    private func unitTitle(_ unit: String) -> String {
        guard let a = model.status?.controls?.ams?.first(where: { $0.unit == unit }) else { return "AMS \(unit)" }
        var parts = ["AMS \(unit)"]
        if let t = a.temp { parts.append(String(format: "%.0f °C", t)) }
        if let h = a.humidity_pct { parts.append(String(format: "%.0f%% humidity", h)) }
        return parts.joined(separator: " · ")
    }

    private func footer(_ c: Controls, trays: Bool) -> String {
        if c.loads || c.extrudes {
            return trays ? "Tap a spool to load it. The filament waits for a pause during a print."
                : "The nozzle heats first. The filament waits for a pause during a print."
        }
        if c.busy && !c.paused && (c.has("filament") || c.has("extrude")) { return "Pause the print to load or unload." }
        return "Load and unload the filament on the touch screen of the deck."
    }

    private func trayRow(_ f: Filament, active: Int?, _ c: Controls) -> some View {
        Button { loading = f } label: {
            FilamentRow(filament: f, feeding: active == f.tray && f.tray >= 0)
        }
        .foregroundStyle(Color.primary)
        .disabled(!c.loads || f.tray < 0)
    }

    @ViewBuilder private func dryRow(_ a: ControlState.AMSUnit, _ c: Controls) -> some View {
        if let d = a.drying {
            Button { stopping = a } label: {
                HStack {
                    RowLabel("Drying \(d.filament ?? "") at \(d.temp) °C".replacingOccurrences(of: "  ", with: " "), symbol: "sun.max")
                    Spacer()
                    Text(Format.duration(Double(d.left)) + " left").font(.subheadline.monospacedDigit()).foregroundStyle(Color.secondary)
                }
            }
            .disabled(!c.dries)
        } else if c.dries {
            Button { drying = a } label: { RowLabel("Dry the filament", symbol: "sun.max", chevron: true) }
        }
    }
}

/// The drying of an AMS: a preset of the deck for each filament, the materials of the unit first.
/// The key names the printer.
private struct DrySheet: View {
    @Environment(DeckModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let unit: ControlState.AMSUnit
    let materials: [String]

    var body: some View {
        let short = model.deck.label ?? model.deck.title
        let first = DryPreset.all.filter { p in materials.contains { $0.caseInsensitiveCompare(p.material) == .orderedSame } }
        let rest = DryPreset.all.filter { !first.contains($0) }
        NavigationStack {
            List {
                Section {
                    ForEach(first + rest, id: \.self) { p in
                        Button {
                            Task {
                                await model.run("Drying AMS \(unit.unit)", effect: .dry(unit.unit), taken: { dismiss() }) {
                                    try await $0.dry(unit: unit.unit, temp: p.temp, hours: p.hours, filament: p.material)
                                }
                            }
                        } label: {
                            HStack {
                                Text(p.material).foregroundStyle(Color.primary)
                                Spacer()
                                Text("\(p.temp) °C for \(p.hours) h").foregroundStyle(Color.secondary)
                            }
                        }
                    }
                } footer: {
                    Text("Dry AMS \(unit.unit) of \(model.deck.title) for the filament. Keep the lid closed.")
                }
            }
            .navigationTitle("Dry AMS \(unit.unit) · \(short)")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { Button("Done") { dismiss() } }
            .disabled(model.sending)
        }
        .presentationDetents([.medium, .large])
    }
}

/// The filament of a Klipper printer: a material for the temperature of the nozzle, then Load,
/// Unload, Extrude or Retract. The nozzle heats first and the control waits until it is hot, as
/// the swap of the deck. Each key names the printer.
private struct KlipperFilamentSheet: View {
    @Environment(DeckModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var material = SwapMaterial.all[0]
    @State private var length = 10.0

    var body: some View {
        let c = model.controls
        let short = model.deck.label ?? model.deck.title
        NavigationStack {
            Form {
                Section {
                    Picker("Material", selection: $material) {
                        ForEach(SwapMaterial.all, id: \.self) { m in Text("\(m.name) · \(m.nozzle) °C").tag(m) }
                    }
                } footer: {
                    Text("The nozzle of \(model.deck.title) heats to \(material.nozzle) °C first.")
                }
                if c.loads {
                    Section {
                        action("Load \(short)", symbol: "arrow.down.to.line", effect: .load(nil)) { try await $0.loadFilament() }
                        action("Unload \(short)", symbol: "arrow.up.to.line", effect: .unload(tray: false)) { try await $0.unloadFilament() }
                    }
                }
                if c.extrudes {
                    Section("Extrude") {
                        Picker("Length", selection: $length) {
                            ForEach([5.0, 10, 50, 100], id: \.self) { Text("\(Int($0)) mm").tag($0) }
                        }
                        .pickerStyle(.segmented)
                        let mm = length
                        action("Extrude \(Int(mm)) mm", symbol: "arrow.down", effect: .extrude(mm)) { try await $0.extrude(mm) }
                        action("Retract \(Int(mm)) mm", symbol: "arrow.up", effect: .extrude(-mm)) { try await $0.extrude(-mm) }
                    }
                }
                if model.heating {
                    Section {
                        Button("Stop waiting for the nozzle", role: .destructive) { model.stopHeating() }
                    } footer: {
                        Text(model.noticeNow ?? "")
                    }
                }
            }
            .navigationTitle("Filament · \(short)")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { Button("Done") { dismiss() } }
        }
        .onAppear { material = SwapMaterial.of(model.status?.roll?.material ?? model.status?.filaments?.first?.material) }
    }

    private func action(_ title: String, symbol: String, effect: Effect,
                        _ f: @escaping (DeckClient) async throws -> Void) -> some View {
        Button {
            let t = material.nozzle
            Task { await model.heatThen(t, doing: title, effect: effect, f) }
        } label: {
            RowLabel(title, symbol: symbol)
        }
        .disabled(model.heating || model.sending || !model.controls.loads && !model.controls.extrudes)
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
    /// The spool feeds the nozzle now.
    var feeding = false

    var body: some View {
        let f = filament
        HStack(spacing: 12) {
            Swatch(hex: f.color)
            VStack(alignment: .leading, spacing: 2) {
                Text([f.slot == "Ext" ? "External spool" : f.slot, f.material].filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(.body.weight(.medium))
                Text([feeding ? "Feeding" : "", f.name ?? "", f.nozzle.map { $0 == "aux" ? "Aux nozzle" : "Main nozzle" } ?? ""]
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

/// The moves of the head, and the tune of a print: the Z offset, the speed and the flow.
struct ControlSection: View {
    @Environment(DeckModel.self) private var model

    var body: some View {
        let c = model.controls
        let cs = model.status?.controls
        if model.deck.process != .resin {
            Section {
                NavigationLink {
                    MoveView().environment(model)
                } label: {
                    RowLabel("Move and home", symbol: "arrow.up.and.down.and.arrow.left.and.right")
                }
                .disabled(!c.moves)
                if c.tunes {
                    stepper("Z offset", symbol: "arrow.up.and.down",
                            value: cs?.z_offset.map { String(format: "%+.3f mm", $0) } ?? "0.025 mm",
                            minus: ("Lower the nozzle 0.025 mm", { babystep(-0.025) }),
                            plus: ("Raise the nozzle 0.025 mm", { babystep(0.025) }))
                }
                if c.speed {
                    if let levels = cs?.speed_levels, !levels.isEmpty {
                        Picker(selection: Binding(get: { cs?.speed_level ?? 0 }, set: { setLevel($0, levels) })) {
                            ForEach(Array(levels.enumerated()), id: \.offset) { i, name in Text(name).tag(i + 1) }
                        } label: {
                            RowLabel("Speed", symbol: "gauge.with.dots.needle.67percent")
                        }
                    } else {
                        let now = cs?.speed ?? 100
                        stepper("Speed", symbol: "gauge.with.dots.needle.67percent", value: "\(now)%",
                                minus: ("Speed \(max(10, now - 10))%", { setSpeed(max(10, now - 10)) }),
                                plus: ("Speed \(min(300, now + 10))%", { setSpeed(min(300, now + 10)) }))
                    }
                }
                if c.flow {
                    let now = cs?.flow ?? 100
                    stepper("Flow", symbol: "drop.halffull", value: "\(now)%",
                            minus: ("Flow \(max(50, now - 2))%", { setFlow(max(50, now - 2)) }),
                            plus: ("Flow \(min(150, now + 2))%", { setFlow(min(150, now + 2)) }))
                }
            } footer: {
                if let reason = footer(c) { Text(reason) }
            }
        }
    }

    /// A row with − and + keys around a value.
    private func stepper(_ title: String, symbol: String, value: String, minus: (String, () -> Void),
                         plus: (String, () -> Void)) -> some View {
        HStack {
            RowLabel(title, symbol: symbol)
            Spacer()
            Button(action: minus.1) { Image(systemName: "minus") }
                .accessibilityLabel(minus.0)
            Text(value).font(.subheadline.monospacedDigit()).foregroundStyle(.secondary).frame(minWidth: 64)
            Button(action: plus.1) { Image(systemName: "plus") }
                .accessibilityLabel(plus.0)
        }
        .buttonStyle(.bordered)
        .disabled(model.sending)
    }

    private func babystep(_ mm: Double) {
        let route = model.controls.has("zoffset")
        Task {
            await model.run(mm < 0 ? "Nozzle down 0.025 mm" : "Nozzle up 0.025 mm", effect: .babystep(mm)) {
                if route { try await $0.adjustZ(mm) } else { try await $0.gcode([Gcode.babystep(mm)]) }
            }
        }
    }

    private func setSpeed(_ pct: Int) {
        Task { await model.run("Speed \(pct)%", effect: .speed(pct)) { try await $0.setSpeed(pct) } }
    }

    private func setLevel(_ level: Int, _ names: [String]) {
        guard names.indices.contains(level - 1) else { return }
        let name = names[level - 1]
        Task { await model.run("Speed \(name)", effect: .speedLevel(level)) { try await $0.setSpeedLevel(name) } }
    }

    private func setFlow(_ pct: Int) {
        Task { await model.run("Flow \(pct)%", effect: .flow(pct)) { try await $0.setFlow(pct) } }
    }

    /// Why a control is off, or nil.
    private func footer(_ c: Controls) -> String? {
        if !c.connected { return nil }
        if c.list != nil {
            if c.busy && !c.paused { return "The head moves during a pause, and homes between prints." }
            if c.busy { return "The head homes between prints." }
            return nil
        }
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
            if let pos = position {
                Section {
                    LabeledContent("Position", value: pos)
                    if let h = model.status?.controls?.homed {
                        LabeledContent("Homed", value: h.isEmpty ? "None" : h.uppercased().map(String.init).joined(separator: " "))
                    }
                }
            }
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
                        .disabled(!c.homes)
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
                    .disabled(!c.homes)
                }
            }
            Section {
                Button { homing = [] } label: {
                    Label("Home all axes", systemImage: "house.fill").frame(maxWidth: .infinity)
                }
                .disabled(!c.homes)
            } footer: {
                Text(c.homes ? "Z moves at most 10 mm a step. Home the axes before a long move."
                     : "Z moves at most 10 mm a step. The head homes between prints.")
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

    /// The position of the head, from the state of the controls or the status screen, or nil.
    private var position: String? {
        if let p = model.status?.controls?.position { return ControlState.text(p) }
        return model.status?.lcd?.position
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
        .disabled(!model.controls.jogs)
        .accessibilityLabel("Move \(name)")
    }
}
