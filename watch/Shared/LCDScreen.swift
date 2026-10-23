import Foundation

// The 128 x 64 display of the printer, as the deck draws it (deck/internal/lcd): the status screen,
// the menus and rows of text. The code follows status.go, menu.go, screen.go and canvas.go of the
// deck, so that the watch shows the same pixels as the touch screen of the deck.

/// The pixels of the display. The screens draw in an area with a margin of 2 pixels at the sides
/// and 1 pixel at the top and the bottom.
struct LCDCanvas {
    static let marginX = 2, marginY = 1
    /// The pixels of the panel: 128 x 64 as the printer, or the portrait display of the watch.
    let panelW: Int, panelH: Int
    /// The area of the screens.
    var w: Int { panelW - 2 * Self.marginX }
    var h: Int { panelH - 2 * Self.marginY }

    private(set) var bits: [Bool]

    init(width: Int = 128, height: Int = 64) {
        panelW = width
        panelH = height
        bits = [Bool](repeating: false, count: width * height)
    }

    func lit(panelX x: Int, y: Int) -> Bool { bits[y * panelW + x] }

    mutating func set(_ x: Int, _ y: Int, _ on: Bool = true) {
        guard x >= 0, x < w, y >= 0, y < h else { return }
        bits[(y + Self.marginY) * panelW + x + Self.marginX] = on
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
    @discardableResult
    mutating func text(_ f: LCDFont, _ x: Int, _ y: Int, _ s: String, bold: Bool = false) -> Int {
        var x = x
        for ch in s {
            for (row, b) in f.glyph(ch).enumerated() {
                for i in 0..<8 where b & (0x80 >> UInt8(i)) != 0 {
                    set(x + i, y + row)
                    if bold { set(x + i + 1, y + row) }
                }
            }
            x += f.w
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

    /// A part moves: the heat waves, the fan, or a line that changes every 3 seconds.
    var moves: Bool { animates || !hint.isEmpty || printing }

    /// The icons move: 4 frames a second.
    var animates: Bool { heating(tool, toolT, toolPow) || heating(bed, bedT, bedPow) || (fan ?? 0) > 0 }

    /// A heater heats while it has power, or while it is more than 2 degrees under its target.
    private func heating(_ temp: Double, _ target: Double, _ power: Double) -> Bool {
        power > 0 || target > 0 && temp < target - 2
    }

    /// The status screen of the watch, on a portrait display about 64 dots wide: the nozzle and the
    /// bed in rows with the temperatures in the large font, then the progress. The parts are the
    /// parts of the status screen of the deck.
    func drawPortrait(_ c: inout LCDCanvas, frame: Int) {
        let W = c.w, small = LCDFont.small, big = LCDFont.big
        // The layout is 70 pixels high, in the middle of a taller display.
        let oy = max(0, (c.h - 70) / 2)
        func heater(_ y: Int, temp: Double, target: Double, icon: (inout LCDCanvas, Int, Int) -> Void) {
            let y = y + oy
            icon(&c, 0, y)
            let t = deg(temp)
            c.text(big, max(19, W - 8 * t.count), y, t)
            if target > 0 {
                let v = deg(target)
                let x = max(19, W - small.w * v.count - 8)
                let m = y + 21
                c.line(x, m, x + 5, m); c.line(x + 3, m - 2, x + 5, m); c.line(x + 3, m + 2, x + 5, m)
                c.text(small, x + 8, y + 17, v)
            }
        }
        heater(0, temp: tool, target: toolT) { c, x, y in
            hotend(&c, x, y, on: toolT > 0, heats: heating(tool, toolT, toolPow), frame: frame)
        }
        heater(26, temp: bed, target: bedT) { c, x, y in
            bedIcon(&c, x, y, on: bedT > 0, heats: heating(bed, bedT, bedPow), frame: frame)
        }
        for x in stride(from: 0, to: W, by: 2) { c.set(x, oy + 52) }
        let line1 = oy + 54, line2 = oy + 62
        let alt = frame / 12 % 2 == 1
        if printing {
            let pct = String(format: "%.0f%%", progress * 100)
            c.roundBar(0, line1 + 1, W - small.w * pct.count - 3, line1 + 7, progress)
            c.text(small, W - small.w * pct.count, line1, pct)
            let l = !left.isEmpty && (!alt || done.isEmpty) ? left + " left" : (!done.isEmpty ? "done " + done : elapsed)
            c.text(small, max(0, (W - small.w * l.count) / 2), line2, lcdFit(l, W / small.w))
            return
        }
        let n = W / small.w
        let m = lcdFit(message.isEmpty ? state : message, n)
        c.text(small, max(0, (W - small.w * m.count) / 2), line1, m)
        let st = lcdFit(alt && !hint.isEmpty ? hint : (message.isEmpty ? "" : state), n - clock.count - 1)
        c.text(small, 0, line2, st)
        c.text(small, W - small.w * clock.count, line2, clock)
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
        let pos = "\(sel + 1)/\(items.count)"
        c.text(f, 0, 0, lcdFit(title, min(19, (W - f.w * pos.count) / f.w - 1)))
        c.text(f, W - f.w * pos.count, 0, pos)
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

/// A screen of the display: the status screen, a menu, or rows of text in the 5 x 8 font (25
/// characters, 7 rows) with text at the left and the right.
enum LCDScreen: Equatable {
    case status(LCDStatus)
    case menu(LCDMenu)
    case rows([(String, String)])

    static func == (a: LCDScreen, b: LCDScreen) -> Bool {
        switch (a, b) {
        case let (.status(x), .status(y)): x == y
        case let (.menu(x), .menu(y)): x == y
        case let (.rows(x), .rows(y)): x.map { $0.0 + "\u{0}" + $0.1 } == y.map { $0.0 + "\u{0}" + $0.1 }
        default: false
        }
    }

    var moves: Bool { if case let .status(s) = self { s.moves } else { false } }
    var animates: Bool { if case let .status(s) = self { s.animates } else { false } }

    /// A frame of the screen on a display of width x height pixels: 128 x 64 as the printer, or a
    /// narrow display, where the status screen has its portrait layout.
    func canvas(frame: Int, width: Int = 128, height: Int = 64) -> LCDCanvas {
        var c = LCDCanvas(width: width, height: height)
        switch self {
        case .status(let s):
            if width < 112 { s.drawPortrait(&c, frame: frame) } else { s.draw(&c, frame: frame) }
        case .menu(let m): m.draw(&c)
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
