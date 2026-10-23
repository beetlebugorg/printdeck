import AppKit
import CoreGraphics

// Renders the Printdeck app icon at 1024 px: the plate of the deck logo with its blue dot-matrix
// window, and the nozzle printing an orange line on the bed. Night palette of deck/internal/ui/theme.go.

func c(_ v: UInt32) -> CGColor {
    CGColor(red: CGFloat(v >> 16 & 0xff) / 255, green: CGFloat(v >> 8 & 0xff) / 255, blue: CGFloat(v & 0xff) / 255, alpha: 1)
}

let bg = c(0x121211), surface = c(0x1c1c1a), raised = c(0x292825), edge = c(0x060606), shine = c(0x34332f)
let glcd = c(0x0f2766), glcdCell = c(0x143079), glcdInk = c(0xd6e2ff)
let cream = c(0xeee9df), orange = c(0xff5a1f)

func rr(_ ctx: CGContext, _ r: CGRect, _ rad: CGFloat, _ fill: CGColor) {
    ctx.addPath(CGPath(roundedRect: r, cornerWidth: rad, cornerHeight: rad, transform: nil))
    ctx.setFillColor(fill)
    ctx.fillPath()
}

func render(_ path: String, watch: Bool) {
    let S = 1024
    let ctx = CGContext(data: nil, width: S, height: S, bitsPerComponent: 8, bytesPerRow: 0,
                        space: CGColorSpace(name: CGColorSpace.sRGB)!, bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue)!
    // Top-left origin, as in the deck code.
    ctx.translateBy(x: 0, y: CGFloat(S))
    ctx.scaleBy(x: 1, y: -1)
    ctx.setFillColor(watch ? surface : bg)
    ctx.fill(CGRect(x: 0, y: 0, width: S, height: S))

    // The watch icon is a circle: no plate, and the drawing a little larger, inside the circle.
    let k: CGFloat = watch ? 1.08 : 1
    ctx.translateBy(x: 512 * (1 - k), y: 512 * (1 - k))
    ctx.scaleBy(x: k, y: k)

    if !watch {
        // The plate: edge, face, light line along the top, and four screws.
        let plate = CGRect(x: 96, y: 96, width: 832, height: 832)
        rr(ctx, plate, 40, edge)
        rr(ctx, plate.insetBy(dx: 6, dy: 6), 34, surface)
        ctx.setFillColor(shine)
        ctx.fill(CGRect(x: 140, y: 103, width: 744, height: 4))
        for (x, y) in [(150, 150), (874, 150), (150, 874), (874, 874)] {
            let r = CGRect(x: x - 20, y: y - 20, width: 40, height: 40)
            rr(ctx, r, 20, edge)
            rr(ctx, r.insetBy(dx: 5, dy: 5), 15, raised)
            ctx.setFillColor(edge)
            ctx.fill(CGRect(x: x - 15, y: y - 3, width: 30, height: 6))
        }
    }

    // The blue dot-matrix window, with unlit cells and one lit row as a bar.
    let win = CGRect(x: 176, y: 214, width: 672, height: 236)
    rr(ctx, win, 26, edge)
    let inner = win.insetBy(dx: 8, dy: 8)
    rr(ctx, inner, 20, glcd)
    let cell: CGFloat = 26, dot: CGFloat = 20
    let cols = Int((inner.width - 40) / cell), rows = Int((inner.height - 40) / cell)
    let ox = inner.midX - CGFloat(cols) * cell / 2, oy = inner.midY - CGFloat(rows) * cell / 2
    for row in 0..<rows {
        for col in 0..<cols {
            // A dot-matrix "progress" row: the middle rows lit for the first 60 percent.
            let lit = (row == rows / 2 || row == rows / 2 - 1) && col < cols * 6 / 10
            rr(ctx, CGRect(x: ox + CGFloat(col) * cell + 3, y: oy + CGFloat(row) * cell + 3, width: dot, height: dot), 3,
               lit ? glcdInk : glcdCell)
        }
    }

    // The nozzle: a block and a tapered tip.
    rr(ctx, CGRect(x: 432, y: 520, width: 160, height: 92), 18, cream)
    ctx.beginPath()
    ctx.move(to: CGPoint(x: 462, y: 600))
    ctx.addLine(to: CGPoint(x: 562, y: 600))
    ctx.addLine(to: CGPoint(x: 530, y: 716))
    ctx.addLine(to: CGPoint(x: 494, y: 716))
    ctx.closePath()
    ctx.setFillColor(cream)
    ctx.fillPath()

    // The bed, and the orange line that the nozzle prints on it.
    rr(ctx, CGRect(x: 216, y: 780, width: 592, height: 34), 8, cream)
    rr(ctx, CGRect(x: 300, y: 748, width: 230, height: 30), 6, orange)

    let img = ctx.makeImage()!
    let rep = NSBitmapImageRep(cgImage: img)
    try! rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: path))
}

let dir = CommandLine.arguments[1]
render(dir + "/AppIcon-phone.png", watch: false)
render(dir + "/AppIcon-watch.png", watch: true)
