package octoprint

import (
	"fmt"
	"strings"
)

// activity names what the printer does, like the message on the printer's own display.
//
// OctoPrint does not pass on the message of the printer. So this is inferred: from the job state,
// the temperatures, and the position in the G-code. The G-code gives the temperatures of the start macro
// and the layer. c.mu must be locked.
func (c *Client) activity(st *State) {
	st.Activity = st.Text
	if !st.Busy() {
		return
	}
	low := strings.ToLower(st.Text)
	switch {
	case st.Paused:
		st.Activity = "Paused"
	case strings.Contains(low, "cancel"):
		st.Activity = "Cancelling"
	case strings.Contains(low, "paus"):
		st.Activity = "Pausing"
	case strings.Contains(low, "resum"):
		st.Activity = "Resuming"
	default:
		st.Activity = "Printing"
	}
	if st.Paused || low != "printing" {
		return
	}

	const near = 3.0 // degrees: closer than this to the target counts as reached
	toolCold := st.ToolTarget > 0 && st.Tool < st.ToolTarget-near
	bedCold := st.BedTarget > 0 && st.Bed < st.BedTarget-near
	gc := c.pv.gc
	if c.pv.path != st.FilePath {
		gc = nil
	}

	if gc == nil {
		// The G-code is not loaded yet. The temperatures show the heating.
		switch {
		case bedCold:
			st.Activity = fmt.Sprintf("Heating bed  %.0f/%.0f°", st.Bed, st.BedTarget)
		case toolCold:
			st.Activity = fmt.Sprintf("Heating nozzle  %.0f/%.0f°", st.Tool, st.ToolTarget)
		}
		return
	}
	st.Layers = gc.LayerCount
	st.Speeds = gc.Speeds
	if gc.StartEnd == 0 || st.FilePos > gc.StartEnd {
		st.Fan, st.HasFan = float64(gc.FanAt(st.FilePos))*100, true
		st.Z, st.HasZ = float64(gc.ZAt(st.FilePos)), true
		if sg, ok := gc.At(st.FilePos); ok {
			st.NowKind, st.NowSpeed, st.HasNow = sg.Kind.String(), float64(sg.Feed), true
		}
	}

	switch {
	case gc.StartEnd > 0 && st.FilePos <= gc.StartEnd:
		// The start macro runs. The file position does not move until it ends.
		switch {
		case st.BedTarget == 0 && st.ToolTarget == 0:
			st.Activity = "Starting"
		case bedCold:
			st.Activity = fmt.Sprintf("Heating bed  %.0f/%.0f°", st.Bed, st.BedTarget)
		case gc.WipeTemp > 0 && st.ToolTarget > 0 && st.ToolTarget <= gc.WipeTemp+near:
			if toolCold {
				st.Activity = fmt.Sprintf("Heating nozzle to wipe  %.0f/%.0f°", st.Tool, st.ToolTarget)
			} else {
				st.Activity = "Wiping nozzle"
			}
		case toolCold:
			st.Activity = fmt.Sprintf("Heating nozzle  %.0f/%.0f°", st.Tool, st.ToolTarget)
		default:
			st.Activity = "Probing and priming"
		}
	case gc.EndStart > 0 && st.FilePos >= gc.EndStart:
		st.Activity = "Finishing"
	default:
		st.Layer = gc.LayerAt(st.FilePos)
		switch {
		case toolCold && st.Layer <= 1:
			st.Activity = fmt.Sprintf("Heating nozzle  %.0f/%.0f°", st.Tool, st.ToolTarget)
		case bedCold:
			st.Activity = fmt.Sprintf("Heating bed  %.0f/%.0f°", st.Bed, st.BedTarget)
		}
	}
}
