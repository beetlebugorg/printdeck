package ui

import (
	"image"
	"printdeck/kit"
	"strings"

	"golang.org/x/image/font"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// consoleQuick are the keys under the console, for the commands that users type most.
var consoleQuick = [][2]string{{"Home", "G28"}, {"Status", "STATUS"}, {"Help", "HELP"}}

// drawConsole is the Console page: the G-code terminal of Klipper. The newest line is at the
// bottom. Drag up to read older lines.
func (a *App) drawConsole(c *gfx.Canvas) {
	if a.consoleKB != nil {
		a.drawKeyboard(c, a.consoleKB)
		return
	}
	pr := a.Printer
	f := a.page(c, "Console")
	// Tall: the log, Type a command, and the quick keys. Wide: the log at the right, the keys in the
	// left column.
	var win, typeKey, quick image.Rectangle
	if f.wide {
		win = f.body()
		k := kit.Rows(f.aside(), 12, kit.Px(66), kit.Px(66))
		typeKey, quick = k[0], k[1]
	} else {
		r := kit.Rows(kit.Inset(f.body(), 0, menuTop-headH, 0, 0), 6, kit.Fr(1), kit.Px(66), kit.Px(70))
		win, typeKey, quick = kit.Inset(r[0], 0, 0, 0, 6), r[1], r[2]
	}
	window(c, win)
	a.region("Replies", win)
	a.region("Quick keys", quick)
	face := gfx.Face(false, 16)
	const lineH = 22
	var rows []printer.ConsoleLine
	for _, l := range pr.Console() {
		t := l.Text
		if l.Kind == "in" {
			t = "> " + t
		}
		for _, w := range breakLine(face, t, win.Dx()-28) {
			rows = append(rows, printer.ConsoleLine{Kind: l.Kind, Text: w})
		}
	}
	inner := win.Inset(10)
	maxScroll := max(0, len(rows)*lineH-inner.Dy())
	a.consoleScroll = max(0, min(a.consoleScroll, maxScroll))
	c.Clipped(inner, func(c *gfx.Canvas) {
		y := inner.Max.Y + a.consoleScroll
		for i := len(rows) - 1; i >= 0 && y > inner.Min.Y; i-- {
			y -= lineH
			if y > inner.Max.Y {
				continue
			}
			col := lcdDim
			switch rows[i].Kind {
			case "in":
				col = lcdInk
			case "error":
				col = red
			}
			c.Text(image.Rect(inner.Min.X+4, y, inner.Max.X, y+lineH), rows[i].Text, face, col, gfx.Left)
		}
	})
	if len(rows) == 0 {
		c.Text(inner, "Klipper's replies show here", face, lcdDim, gfx.Center)
	}
	a.addHit(kit.Hit{R: win, Scroll: func(dy int) { a.consoleScroll = max(0, min(a.consoleScroll-dy, maxScroll)) }})

	a.printerKey(c, typeKey, "Type a command", raised, text, func() {
		a.consoleKB = &keyboard{title: "G-code command", max: 200, ok: "Send", cancel: "Back",
			back: func() { a.consoleKB = nil },
			done: func(v string) {
				a.consoleKB = nil
				if v = strings.TrimSpace(v); v != "" {
					a.consoleScroll, a.consoleLast = 0, v
					pr.SendConsole(v)
				}
			}}
		if a.consoleLast != "" {
			a.consoleKB.value = a.consoleLast
		}
	})
	for i, r := range kit.Cols(quick, 12, kit.Fr(1), kit.Fr(1), kit.Fr(1)) {
		q := consoleQuick[i]
		cmd := q[1]
		a.button(c, r, q[0], raised, text, func() {
			a.consoleScroll = 0
			pr.SendConsole(cmd)
		})
	}
}

// breakLine breaks one line of text into lines that fit width. It breaks at a space where it can,
// and else in a word. It keeps the spaces of the text.
func breakLine(f font.Face, s string, width int) []string {
	var out []string
	for s != "" {
		if gfx.Width(f, s) <= width {
			return append(out, s)
		}
		rs := []rune(s)
		n := len(rs)
		for n > 1 && gfx.Width(f, string(rs[:n])) > width {
			n--
		}
		if i := strings.LastIndex(string(rs[:n]), " "); i > 0 {
			n = len([]rune(string(rs[:n])[:i]))
			out = append(out, string(rs[:n]))
			s = strings.TrimLeft(string(rs[n:]), " ")
			continue
		}
		out = append(out, string(rs[:n]))
		s = string(rs[n:])
	}
	return out
}
