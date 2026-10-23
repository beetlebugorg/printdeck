import CoreGraphics
import Foundation

/// The dots of the complications: the rectangle and the circle, for a status at a time.
struct ComplicationLCD {
    /// The hotend of the status screen of the deck, 16 x 16.
    static func hotend(_ c: inout LCDCanvas, x: Int, y: Int) {
        let art = ["......####......", "......####......", "..############..", "..#..........#..",
                   "..############..", "..#..........#..", "..############..", "................",
                   "...##########...", "...#........#...", "...#........#...", "...##########...",
                   ".....######.....", "......####......", ".......##.......", "................"]
        for (dy, row) in art.enumerated() {
            for (dx, ch) in row.enumerated() where ch == "#" { c.set(x + dx, y + dy) }
        }
    }

    let status: Status?
    let date: Date

    private var entryStatus: Status? { status }
    private var busy: Bool { status?.busy == true }
    private var progress: Double { busy ? status?.progress(at: date) ?? 0 : 0 }

    private var percentNumber: String {
        guard busy, let p = status?.progress(at: date) else { return "--" }
        return "\(Int((p * 100).rounded(.down)))"
    }

    private var leftText: String {
        if status?.paused == true { return "PAUSED" }
        guard busy, let left = status?.left(at: date) else { return "--" }
        return Format.shortDuration(left)
    }

    /// The rectangle: the percent in the large font, the time left, the bar, and the end and the
    /// layer.
    func drawRect(_ c: inout LCDCanvas) {
        let W = c.w, H = c.h, small = LCDFont.small, big = LCDFont.big
        let oy = max(0, (H - 35) / 2)
        guard let st = entryStatus, busy else {
            let t = entryStatus == nil ? "No deck" : (entryStatus?.state == "Operational" ? "Ready" : entryStatus!.state)
            let tt = lcdFit(t, W / big.w)
            c.text(big, (W - big.w * tt.count) / 2, oy + 2, tt)
            let m = lcdFit(entryStatus?.roll.map { "\($0.name) \(Int($0.left.rounded()))g" } ?? "Printdeck", W / small.w)
            c.text(small, (W - small.w * m.count) / 2, oy + 24, m)
            return
        }
        let pct = percentNumber + "%"
        c.text(big, 0, oy, pct)
        let l = st.paused ? "Paused" : leftText
        c.text(small, W - small.w * l.count, oy, l)
        if !st.paused { c.text(small, W - small.w * 4, oy + 9, "left") }
        c.roundBar(0, oy + 19, W, oy + 26, progress)
        let end = st.paused ? "" : (st.end.map { "End " + LCDModel.clock($0) } ?? "")
        c.text(small, 0, oy + 28, lcdFit(end, W / small.w))
        if let la = st.layer, let n = st.layers {
            let t = "L\(la)/\(n)"
            if small.w * (end.count + t.count + 1) <= W { c.text(small, W - small.w * t.count, oy + 28, t) }
        }
    }

    /// The circle: a ring of dots that light with the progress, around the percent in the large
    /// font. With no print, the hotend of the status screen.
    func drawCircle(_ c: inout LCDCanvas) {
        let W = c.w, H = c.h, big = LCDFont.big
        let cx = Double(W - 1) / 2, cy = Double(H - 1) / 2
        let r = Double(min(W, H)) / 2 - 1.5
        let n = 36
        for i in 0..<n {
            let a = Double(i) / Double(n) * 2 * .pi - .pi / 2
            let x = Int((cx + r * cos(a)).rounded()), y = Int((cy + r * sin(a)).rounded())
            if Double(i) < progress * Double(n) {
                c.fill(x - 1, y - 1, x + 1, y + 1)
            } else {
                c.set(x, y)
            }
        }
        guard busy else {
            Self.hotend(&c, x: W / 2 - 8, y: H / 2 - 7)
            return
        }
        let t = percentNumber
        c.text(big, (W - big.w * t.count) / 2, (H - 16) / 2, t)
    }

}

/// The picture of the dots of a complication.
enum ComplicationDots {
    /// In full color: the blue light, the dots that are off, and the dots that are on. With tint:
    /// no light, faint dots that are off, and white dots that are on, which take the tint.
    static func image(_ c: LCDCanvas, dot px: Int, fullColor: Bool) -> CGImage {
        // RGBA, premultiplied, in the order of the bytes in memory.
        func rgba(_ r: UInt32, _ g: UInt32, _ b: UInt32, _ a: UInt32) -> UInt32 {
            (r * a / 255) | (g * a / 255) << 8 | (b * a / 255) << 16 | a << 24
        }
        let gap = fullColor ? rgba(0x1f, 0x4b, 0xd1, 255) : 0
        let off = fullColor ? rgba(0x2a, 0x58, 0xe0, 255) : rgba(255, 255, 255, 40)
        let on = fullColor ? rgba(0xf2, 0xf6, 0xff, 255) : rgba(255, 255, 255, 255)
        let w = c.panelW * px, h = c.panelH * px
        var buf = [UInt32](repeating: gap, count: w * h)
        for y in 0..<c.panelH {
            for x in 0..<c.panelW {
                let col = c.lit(panelX: x, y: y) ? on : off
                for dy in 0..<(px - 1) {
                    let row = (y * px + dy) * w + x * px
                    for dx in 0..<(px - 1) { buf[row + dx] = col }
                }
            }
        }
        let data = buf.withUnsafeBytes { Data($0) } as CFData
        return CGImage(width: w, height: h, bitsPerComponent: 8, bitsPerPixel: 32, bytesPerRow: w * 4,
                       space: CGColorSpace(name: CGColorSpace.sRGB)!,
                       bitmapInfo: CGBitmapInfo(rawValue: CGImageAlphaInfo.premultipliedLast.rawValue),
                       provider: CGDataProvider(data: data)!, decode: nil, shouldInterpolate: false,
                       intent: .defaultIntent)!
    }
}

