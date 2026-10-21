package ui

import (
	"fmt"
	lcdscreen "printdeck/internal/lcd"
	"strings"
	"time"

	"printdeck/internal/printer"
)

// The display of the printer shows the screen of the deck: the module printdeck_display of the
// Klipper host draws it. It shows the heading of the page and the keys of the page as a menu. The
// knob moves through the keys, and a click taps the key. During an action, a filament change or
// a heat-up, it shows a full screen animation.

// lcdCols is the width of the display of the printer, in characters.
const lcdCols = 16

// StatusLines returns the two status lines of the display of the printer. material is the loaded
// material, or "".
func StatusLines(st printer.State, material string, now time.Time, clock24 bool) (string, string) {
	lr := pad
	pct := fmt.Sprintf("%.0f%%", st.Completion)
	left := func() string {
		if !st.HasTimeLeft {
			return st.Activity
		}
		m := int(st.TimeLeft.Minutes())
		return lr("End "+lcdscreen.Clock(now.Add(st.TimeLeft), clock24), fmt.Sprintf("%d:%02d", m/60, m%60))
	}
	switch {
	case !st.Reachable:
		return lr("Klipper error", ""), ""
	case st.ManualProbe:
		return lr("Paper test", ""), lr("Z", fmt.Sprintf("%.3f", st.ManualZ))
	case st.Paused:
		msg := st.Message
		if msg == "" {
			msg = "Resume on deck"
		}
		return lr("Paused", pct), lr(msg, "")
	case st.Busy() && st.Preparing:
		// 16 characters: "Heating bed  52/60°" becomes "HEAT BED 52/60".
		act := strings.Replace(strings.ReplaceAll(st.Activity, "  ", " "), "Heating ", "Heat ", 1)
		return lr("Preparing", ""), lr(act, "")
	case st.Busy() && st.Layer == 1 && st.Layers > 1:
		return lr("First layer", pct), lr("Z offset", fmt.Sprintf("%+.3f", st.ZOffset))
	case st.Busy():
		return lr("Printing", pct), lr(left(), "")
	case st.Finished == "complete":
		return lr("Print done", ""), lr(st.FinishedFile, "")
	case st.Finished == "cancelled":
		return lr("Cancelled", ""), lr(st.FinishedFile, "")
	case st.Finished == "error":
		return lr("Print failed", ""), lr(st.FinishedError, "")
	}
	r := material
	if st.Message != "" {
		return lr("Ready", r), lr(st.Message, "")
	}
	homed := "Not homed"
	if st.Homed == "xyz" {
		homed = "XYZ homed"
	}
	return lr("Ready", r), lr(homed, "")
}

// lcdText makes a text safe for the font of the display: ASCII, with the degree sign.
func lcdText(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '°':
			b.WriteRune(r)
		case r == '·' || r == '−' || r == '–':
			b.WriteByte('-')
		case r == '…':
			b.WriteString("...")
		case r >= 32 && r < 127:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// pad makes a line of the display: 16 characters, l at the left and r at the right, in capitals.
func pad(l, r string) string {
	// The width counts bytes, so the status lines leave out the degree sign.
	l = strings.ReplaceAll(lcdText(strings.ToUpper(l)), "°", "")
	r = strings.ReplaceAll(lcdText(strings.ToUpper(r)), "°", "")
	if r == "" {
		if len(l) > lcdCols {
			l = l[:lcdCols]
		}
		return l + strings.Repeat(" ", lcdCols-len(l))
	}
	n := lcdCols - len(r)
	if len(l) > n-1 {
		l = l[:max(0, n-1)]
	}
	return l + strings.Repeat(" ", max(0, n-len(l))) + r
}

// lcdAction is the screen of the display while an action or a firmware flash runs, or nil.
func (a *App) lcdAction(st printer.State) *printer.LCDScreen {
	if f := a.flash; f != nil && !f.finished {
		return &printer.LCDScreen{Anim: "flash", Title: "Firmware", Caption: f.step,
			Footer: "Keep the printer on", Progress: float64(f.done) / float64(max(f.total, 1))}
	}
	if st.Action == "" || st.ManualProbe {
		return nil
	}
	s := int(a.Now().Sub(st.ActionStart).Seconds())
	return &printer.LCDScreen{Anim: actionAnim(st.Action), Title: st.Action,
		Caption: fmt.Sprintf("%d:%02d", s/60, s%60), Footer: "Stop on the deck"}
}

// actionAnim is the scene of an action on the display, by its name.
func actionAnim(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "pid"):
		return "pid"
	case strings.Contains(n, "tilt"), strings.Contains(n, "mesh"), strings.Contains(n, "probe"),
		strings.Contains(n, "screw"):
		return "probe"
	}
	return "work"
}
