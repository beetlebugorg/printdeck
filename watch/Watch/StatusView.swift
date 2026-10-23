import SwiftUI

/// The glance screen: the print, the temperatures, the roll and the keys.
struct StatusView: View {
    @Environment(DeckModel.self) private var model
    @Environment(\.scenePhase) private var phase
    @State private var confirm: DeckModel.Command?

    var body: some View {
        ScrollView {
            VStack(spacing: 6) {
                if let st = model.status {
                    PrintPanel(status: st)
                    TempPanel(status: st)
                    if let roll = st.roll { RollPanel(roll: roll) }
                    if st.busy { keys(st) }
                }
                footer
            }
        }
        .background(Color.black)
        .task(id: phase == .active) {
            if phase == .active { await model.poll() }
        }
        .confirmationDialog(confirmTitle, isPresented: Binding(get: { confirm != nil }, set: { if !$0 { confirm = nil } }),
                            titleVisibility: .visible) {
            if let c = confirm {
                Button(confirmButton(c), role: c == .cancel ? .destructive : nil) {
                    Task { await model.send(c) }
                }
            }
            Button("Back", role: .cancel) {}
        }
    }

    @ViewBuilder private func keys(_ st: Status) -> some View {
        HStack(spacing: 6) {
            Key(title: "CANCEL", color: Theme.red, ink: Theme.text) { confirm = .cancel }
            if st.paused {
                Key(title: "RESUME", color: Theme.orange) { confirm = .resume }
            } else {
                Key(title: "PAUSE", color: Theme.orange) { confirm = .pause }
            }
        }
        .disabled(model.sending)
    }

    @ViewBuilder private var footer: some View {
        if let error = model.error {
            Text(error)
                .font(Theme.label(11))
                .foregroundStyle(Theme.orange)
                .multilineTextAlignment(.center)
        } else if let st = model.status {
            Text("READ " + st.time.formatted(date: .omitted, time: .standard))
                .font(Theme.label(10))
                .foregroundStyle(Theme.dim)
        } else {
            ProgressView()
        }
    }

    private var confirmTitle: String {
        switch confirm {
        case .pause: "Pause the print?"
        case .resume: "Resume the print?"
        case .cancel: "Cancel the print?"
        case nil: ""
        }
    }

    private func confirmButton(_ c: DeckModel.Command) -> String {
        switch c {
        case .pause: "Pause"
        case .resume: "Resume"
        case .cancel: "Cancel print"
        }
    }
}

/// A dark display window with an uppercase label, as on the deck.
struct Panel<Content: View>: View {
    let label: String
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(label)
                .font(Theme.label(10))
                .foregroundStyle(Theme.dim)
            content
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(8)
        .background(Theme.lcd, in: RoundedRectangle(cornerRadius: 8))
        .overlay(RoundedRectangle(cornerRadius: 8).stroke(Theme.plate, lineWidth: 2))
    }
}

struct PrintPanel: View {
    let status: Status

    var body: some View {
        Panel(label: status.state.uppercased()) {
            if status.busy {
                if let file = status.file {
                    Text(file)
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(Theme.text)
                        .lineLimit(2)
                }
                HStack(alignment: .firstTextBaseline) {
                    Text(Format.percent(status.progress ?? 0))
                        .font(Theme.digits(30))
                        .foregroundStyle(Theme.digits)
                    Spacer()
                    if let layer = status.layer, let layers = status.layers {
                        Text("L \(layer)/\(layers)")
                            .font(Theme.digits(13))
                            .foregroundStyle(Theme.digitsDim)
                    }
                }
                ProgressView(value: status.progress ?? 0)
                    .tint(Theme.orange)
                HStack {
                    Field(label: "LEFT", value: status.left.map(Format.duration) ?? "--")
                    Spacer()
                    Field(label: "END", value: status.end.map(Format.clock) ?? "--")
                }
            } else {
                Text("No print")
                    .font(Theme.digits(15))
                    .foregroundStyle(Theme.digitsDim)
            }
        }
    }
}

struct TempPanel: View {
    let status: Status

    var body: some View {
        Panel(label: "TEMPERATURE") {
            HStack {
                Field(label: "NOZZLE", value: Format.temp(status.tool))
                Spacer()
                Field(label: "BED", value: Format.temp(status.bed))
            }
        }
    }
}

struct RollPanel: View {
    let roll: Roll

    var body: some View {
        Panel(label: "ROLL") {
            HStack(spacing: 6) {
                Circle()
                    .fill(roll.color.flatMap(Color.init(hex:)) ?? Theme.dim)
                    .overlay(Circle().stroke(Theme.dim, lineWidth: 1))
                    .frame(width: 12, height: 12)
                Text(roll.name)
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(Theme.text)
                    .lineLimit(1)
            }
            HStack {
                Text(roll.material)
                    .font(Theme.label(12))
                    .foregroundStyle(Theme.dim)
                Spacer()
                Text("\(Int(roll.left.rounded())) g")
                    .font(Theme.digits(15))
                    .foregroundStyle(Theme.digits)
            }
        }
    }
}

/// A label and a value in amber digits.
struct Field: View {
    let label: String
    let value: String

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(label)
                .font(Theme.label(9))
                .foregroundStyle(Theme.dim)
            Text(value)
                .font(Theme.digits(15))
                .foregroundStyle(Theme.digits)
                .lineLimit(1)
                .minimumScaleFactor(0.7)
        }
    }
}

/// A key of the panel: a colored cap with a monospace label.
struct Key: View {
    let title: String
    let color: Color
    var ink = Color.black
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(title)
                .font(Theme.label(12))
                .frame(maxWidth: .infinity)
        }
        .buttonStyle(.borderedProminent)
        .tint(color)
        .foregroundStyle(ink)
    }
}
