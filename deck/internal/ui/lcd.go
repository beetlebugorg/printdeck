package ui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"printdeck/internal/printer"
)

// The display of the printer shows two lines of the deck (the macro _PRINTDECK of printdeck.cfg,
// and the layout that printdeck-settings writes): the state, and the finish or the activity.

// mirrorCols is the width of the 16x4 display of the printer.
const mirrorCols = 16

// MirrorLines returns the two lines of the deck for the display of the printer. material is the
// loaded material, or "".
func MirrorLines(st printer.State, material string, now time.Time) (string, string) {
	lr := pad
	pct := fmt.Sprintf("%.0f%%", st.Completion)
	left := func() string {
		if !st.HasTimeLeft {
			return st.Activity
		}
		m := int(st.TimeLeft.Minutes())
		return lr("End "+now.Add(st.TimeLeft).Format("15:04"), fmt.Sprintf("%d:%02d", m/60, m%60))
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

// mirrorText makes a text safe for the display and for the G-code of SET_GCODE_VARIABLE: upper
// case ASCII, with no quotes and no comment characters.
func mirrorText(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		switch {
		case r == '"' || r == '\'' || r == '\\' || r == ';' || r == '#' || r == '*':
		case r == '°':
		case r == '·':
			b.WriteByte('-')
		case r >= 32 && r < 127:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Mirror is what the display of the printer shows of the deck: two lines, and an animation of the
// bottom row ("in", "out", "heat", "work" or "").
type Mirror struct{ Line1, Line2, Anim string }

// Command is the G-code that sets the lines and the animation.
func (m Mirror) Command() []string {
	return []string{
		fmt.Sprintf(`SET_GCODE_VARIABLE MACRO=_PRINTDECK VARIABLE=line1 VALUE='"%s"'`, m.Line1),
		fmt.Sprintf(`SET_GCODE_VARIABLE MACRO=_PRINTDECK VARIABLE=line2 VALUE='"%s"'`, m.Line2),
		fmt.Sprintf(`SET_GCODE_VARIABLE MACRO=_PRINTDECK VARIABLE=anim VALUE='"%s"'`, m.Anim),
	}
}

// mirrorMu protects App.mirror, which MirrorState reads from another goroutine.
var mirrorMu sync.Mutex

// MirrorState returns what the display of the printer shows: the screen of the deck at its last
// frame. It is safe to call from any goroutine.
func (a *App) MirrorState() Mirror {
	mirrorMu.Lock()
	defer mirrorMu.Unlock()
	return a.mirror
}

// pad makes a line of the display: 16 characters, l at the left and r at the right.
func pad(l, r string) string {
	l, r = mirrorText(l), mirrorText(r)
	if r == "" {
		if len(l) > mirrorCols {
			l = l[:mirrorCols]
		}
		return l + strings.Repeat(" ", mirrorCols-len(l))
	}
	n := mirrorCols - len(r)
	if len(l) > n-1 {
		l = l[:max(0, n-1)]
	}
	return l + strings.Repeat(" ", max(0, n-len(l))) + r
}

// screenMirror is what the display of the printer shows of the screen of the deck: the step of a
// filament change, the paper test, the page that is open, or the state of the printer.
func (a *App) screenMirror(st printer.State) Mirror {
	if st.Action != "" && !st.ManualProbe {
		// The same lines as runAction sends before the action.
		return Mirror{pad(st.Action, ""), pad("Please wait", ""), "work"}
	}
	if sw := a.swap; sw != nil {
		title := []string{"Filament swap", "Load filament", "Unload"}[sw.mode]
		switch sw.step {
		case swapChoose:
			return Mirror{pad(title, ""), pad("Choose on deck", ""), ""}
		case swapHeat:
			return Mirror{pad(title, ""), pad("Heat", fmt.Sprintf("%.0f/%d", st.Tool, sw.temp)), "heat"}
		case swapUnload:
			return Mirror{pad(title, ""), pad("Unload", ""), "out"}
		case swapInsert:
			return Mirror{pad(title, ""), pad("Insert, load", ""), "in"}
		default:
			if sw.mode == swapUnloadOnly {
				return Mirror{pad(title, ""), pad("Take it out", ""), ""}
			}
			return Mirror{pad(title, ""), pad("Check nozzle", ""), ""}
		}
	}
	if st.Reachable && !st.Busy() && !st.ManualProbe {
		switch a.topPage() {
		case pageMove:
			return Mirror{pad("Move", st.Homed), pad(fmt.Sprintf("X%.0f Y%.0f Z%.1f", st.X, st.Y, st.Z), ""), ""}
		case pageFilament:
			m := a.Material
			if m == "" {
				m = "none"
			}
			return Mirror{pad("Filament", ""), pad("Loaded", m), ""}
		case pageFiles:
			return Mirror{pad("Choose a file", ""), pad("on the deck", ""), ""}
		case pageFile:
			return Mirror{pad("Print this?", ""), pad(a.file.Name, ""), ""}
		case pageMaintenance:
			return Mirror{pad("Maintenance", ""), pad("", ""), ""}
		case pageMachine, pageStats, pageHistory, pageMacros, pageConsole:
			return Mirror{pad("Machine", ""), pad("", ""), ""}
		}
	}
	l1, l2 := MirrorLines(st, a.Material, a.Now())
	anim := ""
	if st.Busy() && st.Preparing && strings.HasPrefix(st.Activity, "Heating") {
		anim = "heat"
	}
	return Mirror{l1, l2, anim}
}
