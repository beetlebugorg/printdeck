import Foundation

// The 128 x 64 display of the printer, as the deck draws it (deck/internal/lcd): the status screen,
// the menus and rows of text. The code follows status.go, menu.go, screen.go and canvas.go of the
// deck, so that the watch shows the same pixels as the touch screen of the deck.

/// The pixels of the display. The screens draw in an area with a margin of 2 pixels at the sides
/// and 1 pixel at the top and the bottom.
struct LCDCanvas {
    /// The pixels of the panel: 128 x 64 as the printer, or the portrait display of the watch.
    let panelW: Int, panelH: Int
    /// The margin of the area of the screens: 2 and 1 on the printer, more on the round corners
    /// of the watch.
    let marginX: Int, marginY: Int
    /// The margin at the top: marginY, or more on the watch.
    let marginTop: Int
    /// The width of the clock of watchOS at the top right of the area, in pixels, or 0. The first
    /// row of a screen has text at the left only.
    var clockW = 0
    /// The area of the screens.
    var w: Int { panelW - 2 * marginX }
    var h: Int { panelH - marginTop - marginY }

    private(set) var bits: [Bool]

    init(width: Int = 128, height: Int = 64, marginX: Int = 2, marginY: Int = 1, marginTop: Int? = nil) {
        panelW = width
        panelH = height
        self.marginX = marginX
        self.marginY = marginY
        self.marginTop = marginTop ?? marginY
        bits = [Bool](repeating: false, count: width * height)
    }

    func lit(panelX x: Int, y: Int) -> Bool { bits[y * panelW + x] }

    mutating func set(_ x: Int, _ y: Int, _ on: Bool = true) {
        guard x >= 0, x < w, y >= 0, y < h else { return }
        bits[(y + marginTop) * panelW + x + marginX] = on
    }

    /// Sets a rectangle; x1 and y1 are not part of it.
    mutating func fill(_ x0: Int, _ y0: Int, _ x1: Int, _ y1: Int, _ on: Bool = true) {
        guard x1 > x0, y1 > y0 else { return }
        for y in y0..<y1 { for x in x0..<x1 { set(x, y, on) } }
    }

    /// The edge of a rectangle.
    mutating func frame(_ x0: Int, _ y0: Int, _ x1: Int, _ y1: Int) {
        for x in x0..<x1 { set(x, y0); set(x, y1 - 1) }
        for y in y0..<y1 { set(x0, y); set(x1 - 1, y) }
    }

    mutating func line(_ x0: Int, _ y0: Int, _ x1: Int, _ y1: Int) {
        let n = max(abs(x1 - x0), abs(y1 - y0), 1)
        for i in 0...n { set(x0 + (x1 - x0) * i / n, y0 + (y1 - y0) * i / n) }
    }

    /// Text with its top left corner at x, y. bold draws each pixel two times, one pixel apart.
    /// scale draws each pixel of the font as scale x scale pixels.
    @discardableResult
    mutating func text(_ f: LCDFont, _ x: Int, _ y: Int, _ s: String, bold: Bool = false, scale k: Int = 1) -> Int {
        var x = x
        for ch in s {
            for (row, b) in f.glyph(ch).enumerated() {
                for i in 0..<8 where b & (0x80 >> UInt8(i)) != 0 {
                    fill(x + i * k, y + row * k, x + (i + 1) * k, y + (row + 1) * k)
                    if bold { fill(x + (i + 1) * k, y + row * k, x + (i + 2) * k, y + (row + 1) * k) }
                }
            }
            x += f.w * k
        }
        return x
    }

    /// An 8 x 8 icon of the menus. An unknown name is a dot.
    mutating func icon(_ name: String, _ x: Int, _ y: Int) {
        let art = LCDIcons.art[name] ?? LCDIcons.art["item"]!
        for (dy, row) in art.enumerated() {
            for (dx, ch) in row.enumerated() where ch == "#" { set(x + dx, y + dy) }
        }
    }

    /// The edge of a rectangle with round corners; x1 and y1 are not part of it.
    mutating func roundFrame(_ x0: Int, _ y0: Int, _ x1: Int, _ y1: Int) {
        line(x0 + 2, y0, x1 - 3, y0)
        line(x0 + 2, y1 - 1, x1 - 3, y1 - 1)
        line(x0, y0 + 2, x0, y1 - 3)
        line(x1 - 1, y0 + 2, x1 - 1, y1 - 3)
        set(x0 + 1, y0 + 1); set(x1 - 2, y0 + 1); set(x0 + 1, y1 - 2); set(x1 - 2, y1 - 2)
    }

    /// A progress bar with round ends, filled to v (0 to 1).
    mutating func roundBar(_ x0: Int, _ y0: Int, _ x1: Int, _ y1: Int, _ v: Double) {
        roundFrame(x0, y0, x1, y1)
        let w = Int(Double(x1 - x0 - 4) * min(max(v, 0), 1))
        fill(x0 + 2, y0 + 2, x0 + 2 + w, y1 - 2)
    }
}

/// Text that the fonts can draw: printable ASCII and the degree sign, at most n characters.
func lcdFit(_ s: String, _ n: Int) -> String {
    String(s.filter { $0 == "°" || ($0.asciiValue.map { $0 >= 32 && $0 < 127 } ?? false) }.prefix(n))
}

private func wave(_ amp: Double, _ phase: Double) -> Int { Int((amp * sin(phase)).rounded()) }

private func deg(_ v: Double) -> String { String(format: "%.0f°", v) }

/// The status screen: the heaters and the fan with their icons, and the progress of the print. The
/// icons move: heat waves while a heater heats, and the blades of the fan at its speed.
struct LCDStatus: Equatable {
    var state = ""
    var clock = ""
    var message = ""
    var tool = 0.0, toolT = 0.0, bed = 0.0, bedT = 0.0
    /// The power of the heaters now, 0 to 1.
    var toolPow = 0.0, bedPow = 0.0
    /// 0 to 1, or nil when the deck does not say it.
    var fan: Double?
    var progress = 0.0
    var printing = false
    var elapsed = ""
    var left = ""
    var done = ""
    var hint = ""
    /// The layer, as "L12/27", or "".
    var layer = ""

    /// A part moves: the heat waves, the fan, or a line that changes every 3 seconds.
    var moves: Bool { animates || !hint.isEmpty || printing }

    /// The icons move: 4 frames a second.
    var animates: Bool { heating(tool, toolT, toolPow) || heating(bed, bedT, bedPow) || (fan ?? 0) > 0 }

    /// A heater heats while it has power, or while it is more than 2 degrees under its target.
    private func heating(_ temp: Double, _ target: Double, _ power: Double) -> Bool {
        power > 0 || target > 0 && temp < target - 2
    }

    /// The status screen of the watch, on a portrait display about 64 dots wide. The progress
    /// comes first: the state and the layer, the percent in the large font at 2 times its size, the
    /// bar, the time left and the end. The temperatures are the last row, which a small watch
    /// leaves out.
    func drawPortrait(_ c: inout LCDCanvas, frame: Int) {
        let W = c.w, small = LCDFont.small, big = LCDFont.big
        let n = W / small.w
        // A row of small text, when it fits on the screen.
        func row(_ y: Int, _ l: String, _ r: String) {
            guard y + 8 <= c.h else { return }
            let right = lcdFit(r, n)
            c.text(small, 0, y, lcdFit(l, max(0, n - right.count - 1)))
            c.text(small, W - small.w * right.count, y, right)
        }
        let temps = "N" + deg(tool) + (toolT > 0 ? "/" + deg(toolT) : "") + " B" + deg(bed)
        // The first row is beside the clock: text at the left only.
        let first = lcdFit(state, max(1, (W - c.clockW) / small.w))
        if printing {
            c.text(small, 0, 0, first.isEmpty ? "Printing" : first)
            let pct = String(format: "%.0f%%", progress * 100)
            let k = 16 * pct.count <= W ? 2 : 1
            c.text(big, max(0, (W - 8 * k * pct.count) / 2), 11, pct, scale: k)
            c.roundBar(0, 45, W, 52, progress)
            row(56, "Layer", layer.hasPrefix("L") ? String(layer.dropFirst()) : layer)
            row(65, "Left", left.isEmpty ? "--" : left)
            row(74, "End", done.isEmpty ? "--" : done)
            row(83, temps, "")
            return
        }
        c.text(small, 0, 0, first)
        let st = lcdFit(clock, W / big.w)
        c.text(big, max(0, (W - big.w * st.count) / 2), 14, st)
        let m = lcdFit(message, n)
        c.text(small, max(0, (W - small.w * m.count) / 2), 36, m)
        for x in stride(from: 0, to: W, by: 2) { c.set(x, 48) }
        let t = lcdFit(temps, n)
        c.text(small, max(0, (W - small.w * t.count) / 2), 52, t)
    }

    func draw(_ c: inout LCDCanvas, frame: Int) {
        let W = c.w
        let small = LCDFont.small, big = LCDFont.big
        let colTool = W / 2 - 42, colBed = W / 2, colFan = W / 2 + 42
        let top = toolT <= 0 && bedT <= 0 ? 4 : 0
        func bigText(_ cx: Int, _ t: String) { c.text(big, max(0, min(cx - 4 * t.count, W - 8 * t.count)), top + 16, t) }

        hotend(&c, colTool - 8, top, on: toolT > 0, heats: heating(tool, toolT, toolPow), frame: frame)
        bedIcon(&c, colBed - 8, top, on: bedT > 0, heats: heating(bed, bedT, bedPow), frame: frame)
        fanIcon(&c, colFan - 8, top, speed: fan ?? 0, frame: frame)
        bigText(colTool, deg(tool))
        bigText(colBed, deg(bed))
        bigText(colFan, fan.map { String(format: "%.0f%%", $0 * 100) } ?? "--")
        func target(_ cx: Int, _ t: Double) {
            guard t > 0 else { return }
            let v = deg(t)
            let x = max(0, cx - (small.w * v.count + 8) / 2)
            let m = 32 + 4
            c.line(x, m, x + 5, m); c.line(x + 3, m - 2, x + 5, m); c.line(x + 3, m + 2, x + 5, m)
            c.text(small, x + 8, 32, v)
        }
        target(colTool, toolT)
        target(colBed, bedT)
        for x in stride(from: 0, to: W, by: 2) { c.set(x, 42) }

        let line1 = 44, line2 = 54
        if printing {
            let pct = String(format: "%.0f%%", progress * 100)
            c.roundBar(0, line1 + 1, W - small.w * 4 - 3, line1 + 7, progress)
            c.text(small, W - small.w * pct.count, line1, pct)
            c.text(small, 0, line2, done.isEmpty ? elapsed : "done " + done)
            if !left.isEmpty {
                let l = left + " left"
                c.text(small, W - small.w * l.count, line2, l)
            }
            return
        }
        let m = lcdFit(message, 25)
        c.text(small, max(0, (W - small.w * m.count) / 2), line1, m)
        var st = state
        if !hint.isEmpty && frame / 12 % 2 == 1 { st = hint }
        c.text(small, 0, line2, lcdFit(st.trimmingCharacters(in: .whitespaces), 19))
        if !clock.isEmpty { c.text(small, W - small.w * clock.count, line2, clock) }
    }

    private static let hotendArt = [
        "......####......", "......####......", "..############..", "..#..........#..",
        "..############..", "..#..........#..", "..############..", "................",
        "...##########...", "...#........#...", "...#........#...", "...##########...",
        ".....######.....", "......####......", ".......##.......", "................",
    ]
    private static let bedArt = [
        "................", "................", "................", "................",
        "................", "................", "................", "................",
        "################", "#..............#", "#..............#", "################",
        ".##..........##.", ".##..........##.", ".##..........##.", "................",
    ]

    private func art(_ c: inout LCDCanvas, _ x: Int, _ y: Int, _ a: [String]) {
        for (dy, row) in a.enumerated() {
            for (dx, ch) in row.enumerated() where ch == "#" { c.set(x + dx, y + dy) }
        }
    }

    private func hotend(_ c: inout LCDCanvas, _ x: Int, _ y: Int, on: Bool, heats: Bool, frame: Int) {
        art(&c, x, y, Self.hotendArt)
        if on {
            c.fill(x + 4, y + 9, x + 12, y + 11)
            c.fill(x + 6, y + 12, x + 10, y + 13)
        }
        if heats {
            for wx in [x, x + 15] {
                for dy in 0..<8 where (dy + frame) % 4 != 3 {
                    c.set(wx + wave(1, Double(dy + frame) * 1.2), y + 12 - dy)
                }
            }
        }
    }

    private func bedIcon(_ c: inout LCDCanvas, _ x: Int, _ y: Int, on: Bool, heats: Bool, frame: Int) {
        art(&c, x, y, Self.bedArt)
        if on { c.fill(x + 1, y + 9, x + 15, y + 11) }
        for i in 0..<3 {
            let wx = x + 3 + i * 5
            if !heats {
                c.line(wx, y + 4, wx, y + 6)
                continue
            }
            for dy in 0..<7 { c.set(wx + wave(1, Double(dy + frame * 2 + i * 2) / 1.5), y + 6 - dy) }
        }
    }

    private func fanIcon(_ c: inout LCDCanvas, _ x: Int, _ y: Int, speed: Double, frame: Int) {
        c.line(x + 2, y, x + 13, y); c.line(x + 2, y + 15, x + 13, y + 15)
        c.line(x, y + 2, x, y + 13); c.line(x + 15, y + 2, x + 15, y + 13)
        c.set(x + 1, y + 1); c.set(x + 14, y + 1); c.set(x + 1, y + 14); c.set(x + 14, y + 14)
        let turn = -Double(frame) * speed * 0.7
        for py in 1..<15 {
            for px in 1..<15 {
                let dx = Double(px) - 7.5, dy = Double(py) - 7.5
                let r = hypot(dx, dy)
                if r > 6.6 { continue }
                if r < 1.6 { c.set(x + px, y + py); continue }
                let a = atan2(dy, dx) - turn - r * 0.28
                var sector = fmod(a, 2 * .pi / 3)
                if sector < 0 { sector += 2 * .pi / 3 }
                if sector < 0.95 { c.set(x + px, y + py) }
            }
        }
    }
}

/// A list menu: the title and the place in the menu, rows with icons, a frame around the selected
/// row, and a scrollbar.
struct LCDMenu: Equatable {
    struct Item: Equatable {
        var label: String
        var icon: String
    }
    var title: String
    var items: [Item]
    var selected = 0
    var notes: [String] = []

    func draw(_ c: inout LCDCanvas) {
        guard !items.isEmpty else { return }
        let W = c.w, H = c.h, f = LCDFont.small
        let sel = max(0, min(selected, items.count - 1))
        if c.clockW > 0 {
            c.text(f, 0, 0, lcdFit(title, (W - c.clockW) / f.w))
        } else {
            let pos = "\(sel + 1)/\(items.count)"
            c.text(f, 0, 0, lcdFit(title, min(19, (W - f.w * pos.count) / f.w - 1)))
            c.text(f, W - f.w * pos.count, 0, pos)
        }
        c.line(0, 9, W - 1, 9)
        var y = 11
        for n in notes {
            c.text(f, 0, y, lcdFit(n, 25))
            y += 9
        }
        let rowH = 10
        let n = (H - y) / rowH
        var top = max(0, min(sel - n + 2, items.count - n))
        if sel < top { top = sel }
        let long = items.count > n
        let right = long ? W - 4 : W
        for i in top..<min(top + n, items.count) {
            let ry = y + (i - top) * rowH
            c.icon(items[i].icon, 3, ry + 1)
            c.text(f, 14, ry + 1, lcdFit(items[i].label, (right - 16) / f.w))
            if i == sel { c.roundFrame(0, ry, right, ry + rowH) }
        }
        if long {
            let h = H - y
            for ty in stride(from: y, to: H, by: 2) { c.set(W - 2, ty) }
            let th = max(4, h * n / items.count)
            let t0 = y + (h - th) * top / max(1, items.count - n)
            c.fill(W - 3, t0, W, t0 + th)
        }
    }
}

/// A row of a page: text at the left and the right, or a progress bar from 0 to 1.
enum LCDRow: Equatable {
    case text(String, String)
    case bar(Double)
}

/// A page of the watch: a header with the title and the place in the pages, as the menus have
/// it, and rows in the 5 x 8 font.
struct LCDPage: Equatable {
    var title: String
    var rows: [LCDRow]
    /// A picture under the header, in place of the rows: the model of the print.
    var picture: LCDPicture?
    var index = 0
    var count = 1

    func draw(_ c: inout LCDCanvas) {
        let W = c.w, f = LCDFont.small
        if c.clockW > 0 {
            // The clock is at the top right: the title at the left, and the place in the pages as
            // dots at the bottom.
            c.text(f, 0, 0, lcdFit(title, (W - c.clockW) / f.w))
            for i in 0..<count {
                let x = W / 2 - count * 4 + i * 8 + 2
                if i == index { c.fill(x, c.h - 4, x + 4, c.h) } else { c.frame(x, c.h - 4, x + 4, c.h) }
            }
        } else {
            let pos = "\(index + 1)/\(count)"
            c.text(f, 0, 0, lcdFit(title, (W - f.w * pos.count) / f.w - 1))
            c.text(f, W - f.w * pos.count, 0, pos)
        }
        c.line(0, 9, W - 1, 9)
        if let pic = picture {
            let ox = (W - pic.width) / 2, oy = 11 + max(0, (c.h - 17 - pic.height) / 2)
            for y in 0..<pic.height {
                for x in 0..<pic.width where pic.bits[y * pic.width + x] { c.set(ox + x, oy + y) }
            }
            return
        }
        var y = 12
        let n = W / f.w
        let bottom = c.clockW > 0 ? c.h - 6 : c.h
        for r in rows where y + 8 <= bottom {
            switch r {
            case let .text(l, rt):
                let right = lcdFit(rt, n)
                let left = lcdFit(l, max(0, n - right.count - (right.isEmpty ? 0 : 1)))
                c.text(f, 0, y, left)
                c.text(f, W - f.w * right.count, y, right)
            case let .bar(v):
                c.roundBar(0, y + 1, W, y + 7, v)
            }
            y += 10
        }
    }
}

/// A screen of the display: the status screen, a menu, a page, or rows of text in the 5 x 8 font
/// (25 characters, 7 rows) with text at the left and the right.
enum LCDScreen: Equatable {
    case status(LCDStatus)
    case menu(LCDMenu)
    case page(LCDPage)
    case rows([(String, String)])

    static func == (a: LCDScreen, b: LCDScreen) -> Bool {
        switch (a, b) {
        case let (.status(x), .status(y)): x == y
        case let (.menu(x), .menu(y)): x == y
        case let (.page(x), .page(y)): x == y
        case let (.rows(x), .rows(y)): x.map { $0.0 + "\u{0}" + $0.1 } == y.map { $0.0 + "\u{0}" + $0.1 }
        default: false
        }
    }

    var moves: Bool { if case let .status(s) = self { s.moves } else { false } }
    var animates: Bool { if case let .status(s) = self { s.animates } else { false } }

    /// A frame of the screen on a display of width x height pixels: 128 x 64 as the printer, or a
    /// narrow display, where the status screen has its portrait layout.
    func canvas(frame: Int, width: Int = 128, height: Int = 64, marginX: Int = 2, marginY: Int = 1,
                marginTop: Int? = nil, clockW: Int = 0) -> LCDCanvas {
        var c = LCDCanvas(width: width, height: height, marginX: marginX, marginY: marginY, marginTop: marginTop)
        c.clockW = clockW
        switch self {
        case .status(let s):
            if c.w < 112 { s.drawPortrait(&c, frame: frame) } else { s.draw(&c, frame: frame) }
        case .menu(let m): m.draw(&c)
        case .page(let p): p.draw(&c)
        case .rows(let rows):
            let f = LCDFont.small, n = c.w / f.w
            for (i, r) in rows.prefix(c.h / 9).enumerated() {
                let right = lcdFit(r.1, n)
                let left = lcdFit(r.0, max(0, n - right.count - (right.isEmpty ? 0 : 1)))
                c.text(f, 0, i * 9, left)
                c.text(f, c.w - f.w * right.count, i * 9, right)
            }
        }
        return c
    }
}
