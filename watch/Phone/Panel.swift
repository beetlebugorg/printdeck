import SwiftUI

// The parts of the panel of the deck (deck/internal/ui/theme.go), for the iPhone app: plates with
// screw heads and a label, dark display windows with amber type, and membrane keys.

private struct PaletteKey: EnvironmentKey {
    static let defaultValue = Palette.day
}

extension EnvironmentValues {
    var palette: Palette {
        get { self[PaletteKey.self] }
        set { self[PaletteKey.self] = newValue }
    }
}

/// The title of a screen: an orange mark and uppercase type.
struct Heading: View {
    @Environment(\.palette) private var p
    let title: String

    var body: some View {
        HStack(spacing: 10) {
            Rectangle().fill(p.orange).frame(width: 10, height: 22)
            Text(title.uppercased())
                .font(Theme.plex(24))
                .foregroundStyle(p.text)
            Spacer()
        }
    }
}

/// A module plate with screw heads in the corners and a label, like "01 DECK".
struct Plate<Content: View>: View {
    @Environment(\.palette) private var p
    let label: String
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(label.uppercased())
                .font(Theme.plex(13))
                .foregroundStyle(p.dim)
                .padding(.horizontal, 12)
            content
        }
        .padding(.horizontal, 14)
        .padding(.top, 6)
        .padding(.bottom, 16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background {
            RoundedRectangle(cornerRadius: 5)
                .fill(p.surface)
                .overlay(alignment: .top) {
                    Rectangle().fill(p.shine).frame(height: 1).padding(.horizontal, 5).padding(.top, 1)
                }
                .overlay(RoundedRectangle(cornerRadius: 5).stroke(p.edge, lineWidth: 1))
                .overlay { screws }
        }
    }

    private var screws: some View {
        GeometryReader { g in
            ForEach(0..<4, id: \.self) { i in
                Screw()
                    .position(x: i % 2 == 0 ? 9 : g.size.width - 9, y: i < 2 ? 9 : g.size.height - 9)
            }
        }
    }
}

/// A small slotted screw head.
struct Screw: View {
    @Environment(\.palette) private var p

    var body: some View {
        Circle()
            .fill(p.raised)
            .overlay(Circle().stroke(p.edge, lineWidth: 1))
            .overlay(Rectangle().fill(p.edge).frame(width: 6, height: 1))
            .frame(width: 8, height: 8)
    }
}

/// A dark display window sunk into the plate, with amber type.
struct DisplayWindow<Content: View>: View {
    @Environment(\.palette) private var p
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 4) { content }
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: 4).fill(p.lcd))
            .overlay(RoundedRectangle(cornerRadius: 6).stroke(p.edge, lineWidth: 2))
            .foregroundStyle(p.lcdInk)
    }
}

/// A membrane key with a printed frame. An orange key is the main action. A key with an LED shows
/// an on/off state.
struct KeyStyle: ButtonStyle {
    enum Cap { case raised, orange }
    var cap: Cap = .raised
    /// nil for a key with no LED.
    var led: Bool? = nil
    @Environment(\.palette) private var p
    @Environment(\.isEnabled) private var enabled

    func makeBody(configuration: Configuration) -> some View {
        let face = cap == .orange ? p.orange : p.raised
        let ink = cap == .orange ? p.onAcc : p.text
        let pressed = configuration.isPressed
        return ZStack(alignment: .topLeading) {
            RoundedRectangle(cornerRadius: 4).fill(cap == .orange ? Color(rgb: 0xa63a14) : p.edge)
            RoundedRectangle(cornerRadius: 3).fill(face).padding(2)
                .overlay(Color.black.opacity(pressed ? 0.12 : 0).clipShape(RoundedRectangle(cornerRadius: 3)).padding(2))
            RoundedRectangle(cornerRadius: 2).stroke(Color.black.opacity(0.10), lineWidth: 1).padding(6)
            if let led {
                RoundedRectangle(cornerRadius: 2)
                    .fill(led ? p.orange : p.ledOff)
                    .frame(width: 12, height: 5)
                    .padding(.leading, 11).padding(.top, 10)
            }
            configuration.label
                .font(Theme.plex(17))
                .textCase(.uppercase)
                .lineLimit(1)
                .minimumScaleFactor(0.7)
                .foregroundStyle(ink)
                .padding(.horizontal, 12)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .offset(y: pressed ? 1 : 0)
        }
        .frame(height: 58)
        .opacity(enabled ? 1 : 0.45)
    }
}

/// A text field in a display window, with a printed label above it.
struct PanelField: View {
    @Environment(\.palette) private var p
    let label: String
    let prompt: String
    @Binding var text: String
    var keyboard: UIKeyboardType = .default

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(label.uppercased()).font(Theme.plex(12)).foregroundStyle(p.dim)
            DisplayWindow {
                TextField("", text: $text, prompt: Text(prompt).foregroundStyle(p.lcdDim.opacity(0.6)))
                    .font(Theme.plex(17, bold: false))
                    .keyboardType(keyboard)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .tint(p.lcdInk)
            }
        }
    }
}
