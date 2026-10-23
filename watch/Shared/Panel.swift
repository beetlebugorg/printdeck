import SwiftUI

// The parts of the panel of the deck (deck/internal/ui/theme.go), for the iPhone and the watch:
// plates with screw heads and a label, the blue character display, dark display windows with amber
// type, and membrane keys.

private struct PaletteKey: EnvironmentKey {
    static let defaultValue = Palette.night
}

private struct PanelScaleKey: EnvironmentKey {
    static let defaultValue: CGFloat = 1
}

extension EnvironmentValues {
    var palette: Palette {
        get { self[PaletteKey.self] }
        set { self[PaletteKey.self] = newValue }
    }

    /// The size of the panel parts: 1 on the iPhone, smaller on the watch.
    var panelScale: CGFloat {
        get { self[PanelScaleKey.self] }
        set { self[PanelScaleKey.self] = newValue }
    }
}

/// The title of a screen: an orange mark and uppercase type.
struct Heading<Accessory: View>: View {
    @Environment(\.palette) private var p
    let title: String
    @ViewBuilder var accessory: Accessory

    var body: some View {
        HStack(spacing: 10) {
            Rectangle().fill(p.orange).frame(width: 10, height: 22)
            Text(title.uppercased())
                .font(Theme.plex(24))
                .foregroundStyle(p.text)
            Spacer()
            accessory
        }
    }
}

extension Heading where Accessory == EmptyView {
    init(title: String) {
        self.init(title: title) { EmptyView() }
    }
}

/// A module plate with screw heads in the corners and a label, like "01 PRINT".
struct Plate<Content: View>: View {
    @Environment(\.palette) private var p
    @Environment(\.panelScale) private var s
    let label: String
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 10 * s) {
            Text(label.uppercased())
                .font(Theme.plex(13 * s))
                .foregroundStyle(p.dim)
                .padding(.horizontal, 12 * s)
            content
        }
        .padding(.horizontal, 14 * s)
        .padding(.top, 6 * s)
        .padding(.bottom, 16 * s)
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
            let m = 9 * s
            ForEach(0..<4, id: \.self) { i in
                Screw()
                    .position(x: i % 2 == 0 ? m : g.size.width - m, y: i < 2 ? m : g.size.height - m)
            }
        }
    }
}

/// A small slotted screw head.
struct Screw: View {
    @Environment(\.palette) private var p
    @Environment(\.panelScale) private var s

    var body: some View {
        Circle()
            .fill(p.raised)
            .overlay(Circle().stroke(p.edge, lineWidth: 1))
            .overlay(Rectangle().fill(p.edge).frame(width: 6 * s, height: 1))
            .frame(width: 8 * s, height: 8 * s)
    }
}

/// A dark display window sunk into the plate, with amber type.
struct DisplayWindow<Content: View>: View {
    @Environment(\.palette) private var p
    @Environment(\.panelScale) private var s
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 4 * s) { content }
            .padding(.horizontal, 12 * s)
            .padding(.vertical, 10 * s)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: 4).fill(p.lcd))
            .overlay(RoundedRectangle(cornerRadius: 6).stroke(p.edge, lineWidth: 2))
            .foregroundStyle(p.lcdInk)
    }
}

/// A label and a value in a display window, as the detail rows of the deck.
struct WindowRow: View {
    @Environment(\.palette) private var p
    @Environment(\.panelScale) private var s
    let label: String
    let value: String

    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            Text(label.uppercased())
                .font(Theme.plex(12 * s))
                .foregroundStyle(p.lcdDim)
            Spacer()
            Text(value)
                .font(Theme.plex(20 * s, bold: false))
                .lineLimit(1)
                .minimumScaleFactor(0.6)
        }
    }
}

/// The 128 x 64 display of the printer, as the deck shows it: each pixel a dot with a gap of one
/// pixel, the dots that are off visible, in the blue frame of the display. A screen that moves (heat
/// waves, the fan) draws 4 frames a second, as on the deck.
struct LCDView: View {
    @Environment(\.palette) private var p
    @Environment(\.displayScale) private var displayScale
    let screen: LCDScreen

    private static let dotsW = LCDCanvas.panelW, dotsH = LCDCanvas.panelH

    var body: some View {
        TimelineView(.animation(minimumInterval: 0.25, paused: !screen.moves)) { t in
            let frame = screen.moves ? Int(t.date.timeIntervalSinceReferenceDate * 4) : 0
            let bits = screen.canvas(frame: frame)
            Canvas { ctx, size in
                let px = 1 / displayScale
                var pitch = min(size.width / CGFloat(Self.dotsW), size.height / CGFloat(Self.dotsH))
                pitch = max(2 * px, (pitch * displayScale).rounded(.down) / displayScale)
                let dot = pitch - px
                let ox = ((size.width - CGFloat(Self.dotsW) * pitch) / 2 * displayScale).rounded() / displayScale
                let oy = ((size.height - CGFloat(Self.dotsH) * pitch) / 2 * displayScale).rounded() / displayScale
                var on = Path(), off = Path()
                for y in 0..<Self.dotsH {
                    for x in 0..<Self.dotsW {
                        let r = CGRect(x: ox + CGFloat(x) * pitch, y: oy + CGFloat(y) * pitch, width: dot, height: dot)
                        if bits.lit(panelX: x, y: y) { on.addRect(r) } else { off.addRect(r) }
                    }
                }
                ctx.fill(off, with: .color(p.glcdCell))
                ctx.fill(on, with: .color(p.glcdInk))
            }
        }
        .aspectRatio(CGFloat(Self.dotsW) / CGFloat(Self.dotsH), contentMode: .fit)
        .padding(3)
        .background(RoundedRectangle(cornerRadius: 5).fill(p.glcd))
        .overlay(RoundedRectangle(cornerRadius: 6).stroke(p.edge, lineWidth: 1.5))
    }
}

/// A membrane key with a printed frame. An orange key is the main action, a red key stops. A key
/// with an LED shows an on/off state.
struct KeyStyle: ButtonStyle {
    /// danger is a raised key with red text, as Cancel on the deck. red is a red cap.
    enum Cap { case raised, orange, red, danger }
    var cap: Cap = .raised
    /// nil for a key with no LED.
    var led: Bool? = nil
    /// The height for a large key, as Print a file; nil for the height of a key.
    var height: CGFloat? = nil
    @Environment(\.palette) private var p
    @Environment(\.panelScale) private var s
    @Environment(\.isEnabled) private var enabled

    func makeBody(configuration: Configuration) -> some View {
        let face: Color, ink: Color, border: Color
        switch cap {
        case .raised: (face, ink, border) = (p.raised, p.text, p.edge)
        case .orange: (face, ink, border) = (p.orange, p.onAcc, Color(rgb: 0xa63a14))
        case .red: (face, ink, border) = (p.redCap, p.onRed, Color(rgb: 0x5e140c))
        case .danger: (face, ink, border) = (p.raised, p.red, p.edge)
        }
        let pressed = configuration.isPressed
        return ZStack(alignment: .topLeading) {
            RoundedRectangle(cornerRadius: 4).fill(border)
            RoundedRectangle(cornerRadius: 3).fill(face).padding(2)
                .overlay(Color.black.opacity(pressed ? 0.12 : 0).clipShape(RoundedRectangle(cornerRadius: 3)).padding(2))
            RoundedRectangle(cornerRadius: 2).stroke(Color.black.opacity(0.10), lineWidth: 1).padding(6 * s)
            if let led {
                RoundedRectangle(cornerRadius: 2)
                    .fill(led ? p.orange : p.ledOff)
                    .frame(width: 12 * s, height: 5 * s)
                    .padding(.leading, 11 * s).padding(.top, 10 * s)
            }
            configuration.label
                .font(Theme.plex(17 * s))
                .textCase(.uppercase)
                .lineLimit(1)
                .minimumScaleFactor(0.6)
                .foregroundStyle(ink)
                .padding(.horizontal, 10 * s)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .offset(y: pressed ? 1 : 0)
        }
        .frame(height: height ?? 58 * s)
        .opacity(enabled ? 1 : 0.45)
        .contentShape(Rectangle())
    }
}

/// A meter of lit segments in a dark slot, as the roll meter of the deck.
struct SegmentMeter: View {
    @Environment(\.palette) private var p
    let value: Double
    var count = 8

    var body: some View {
        HStack(spacing: 2) {
            ForEach(0..<count, id: \.self) { i in
                RoundedRectangle(cornerRadius: 1)
                    .fill(Double(i) < (min(1, max(0, value)) * Double(count)).rounded() ? p.lcdInk : Color(rgb: 0x3a2a20))
                    .frame(width: 6, height: 5)
            }
        }
        .padding(.horizontal, 4)
        .padding(.vertical, 3)
        .background(Capsule().fill(p.lcd))
        .overlay(Capsule().stroke(p.edge, lineWidth: 1))
    }
}

/// The position in a print as the deck shows it: a thin slot with the done part in orange, a
/// cursor, and a tick mark each tenth.
struct Playhead: View {
    @Environment(\.palette) private var p
    let value: Double

    var body: some View {
        GeometryReader { g in
            let w = g.size.width, f = min(1, max(0, value))
            ZStack(alignment: .leading) {
                Capsule().fill(Color(rgb: 0x080808)).frame(height: 6)
                Rectangle().fill(p.orange).frame(width: w * f, height: 2).padding(.leading, 1)
                ForEach(0...10, id: \.self) { i in
                    Rectangle().fill(p.edge).frame(width: 1, height: 4)
                        .offset(x: w * CGFloat(i) / 10, y: 8)
                }
                Rectangle().fill(p.orange).frame(width: 4, height: 22).offset(x: w * f - 2)
                Rectangle().fill(p.orange).frame(width: 12, height: 4).offset(x: w * f - 6, y: -11)
            }
            .frame(height: g.size.height)
        }
        .frame(height: 30)
    }
}

/// The question of the deck before an action, as its CONFIRM plate: the question, the key that
/// goes back at the left, and the key that acts at the right.
struct ConfirmPanel: View {
    @Environment(\.palette) private var p
    @Environment(\.panelScale) private var s
    let question: String
    let yes: String
    var danger = false
    let onYes: () -> Void
    let onNo: () -> Void

    var body: some View {
        ZStack {
            Color.black.opacity(0.55).ignoresSafeArea()
                .onTapGesture(perform: onNo)
            Plate(label: "Confirm") {
                Text(question)
                    .font(Theme.plex(20 * s))
                    .foregroundStyle(p.text)
                    .multilineTextAlignment(.center)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 10 * s)
                HStack(spacing: 8 * s) {
                    Button("Back", action: onNo)
                        .buttonStyle(KeyStyle())
                    Button(yes, action: onYes)
                        .buttonStyle(KeyStyle(cap: danger ? .red : .orange))
                }
            }
            .padding(.horizontal, 8 * s)
        }
    }
}

#if os(iOS)
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
#endif
