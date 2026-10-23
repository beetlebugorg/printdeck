import AppKit
import CoreGraphics

// Draws the Printdeck app icons at 1024 px: a blue dot-matrix display with "PD", in the colors of
// the character display of the deck (glcd in deck/internal/ui/theme.go). The iPhone and the watch
// get the same picture; the letters stay inside the circle of the watch.
//
//   swift icon/render.swift <folder>

func c(_ v: UInt32) -> CGColor {
    CGColor(red: CGFloat(v >> 16 & 0xff) / 255, green: CGFloat(v >> 8 & 0xff) / 255, blue: CGFloat(v & 0xff) / 255, alpha: 1)
}

let glcd = c(0x1f4bd1), glcdCell = c(0x2a58e0), glcdInk = c(0xf2f6ff)

// The letters in 5 x 7 dots, as on a character display.
let glyphs: [[String]] = [
    ["11110", "10001", "10001", "11110", "10000", "10000", "10000"], // P
    ["11100", "10010", "10001", "10001", "10001", "10010", "11100"], // D
]

func render(_ path: String) {
    let S = 1024
    let ctx = CGContext(data: nil, width: S, height: S, bitsPerComponent: 8, bytesPerRow: 0,
                        space: CGColorSpace(name: CGColorSpace.sRGB)!, bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue)!
    ctx.translateBy(x: 0, y: CGFloat(S))
    ctx.scaleBy(x: 1, y: -1)
    ctx.setFillColor(glcd)
    ctx.fill(CGRect(x: 0, y: 0, width: S, height: S))

    // A grid of 15 x 15 cells. The letters are 11 cells wide (5, a gap, 5) and 7 high, in the middle.
    let n = 15, pitch: CGFloat = 64, dot: CGFloat = 54
    let origin = (CGFloat(S) - CGFloat(n) * pitch) / 2 + (pitch - dot) / 2
    var lit = Set<Int>()
    for (g, rows) in glyphs.enumerated() {
        for (r, row) in rows.enumerated() {
            for (col, ch) in row.enumerated() where ch == "1" {
                lit.insert((4 + r) * n + 2 + g * 6 + col)
            }
        }
    }
    for i in 0..<(n * n) {
        let r = CGRect(x: origin + CGFloat(i % n) * pitch, y: origin + CGFloat(i / n) * pitch, width: dot, height: dot)
        ctx.addPath(CGPath(roundedRect: r, cornerWidth: 8, cornerHeight: 8, transform: nil))
        ctx.setFillColor(lit.contains(i) ? glcdInk : glcdCell)
        ctx.fillPath()
    }

    let rep = NSBitmapImageRep(cgImage: ctx.makeImage()!)
    try! rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: path))
}

let dir = CommandLine.arguments[1]
render(dir + "/AppIcon-phone.png")
render(dir + "/AppIcon-watch.png")
