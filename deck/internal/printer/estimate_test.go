package printer

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"printdeck/internal/gcode"
)

// curaFile is a print of 60 minutes in the slicer: a start macro, then 60 layers of one minute.
func curaFile() (string, []int64) {
	var b strings.Builder
	b.WriteString(";TIME:3600\nPRINT_START BED=60 EXTRUDER=210\n")
	var ends []int64
	for i := range 60 {
		fmt.Fprintf(&b, ";LAYER:%d\nG1 X10 Y%d E%d F600\n;TIME_ELAPSED:%d\n", i, i, i+1, (i+1)*60)
		ends = append(ends, int64(b.Len()))
	}
	return b.String(), ends
}

func TestTimeLeftFollowsTheSlicerAndThePrinter(t *testing.T) {
	src, ends := curaFile()
	gc, _ := gcode.Parse(strings.NewReader(src))
	st := State{Reachable: true, Printing: true, Text: "Printing", FilePath: "a.gcode", FileSize: int64(len(src))}
	v := &View{gc: gc, file: fileKey(st), view: -1}
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

	// The start macro heats for 10 minutes: the time left is the time of the slicer.
	st.Preparing, st.FilePos, st.PrintDuration = true, gc.StartEnd, 10*time.Minute
	v.TimeLeft(&st, now)
	if !st.HasTimeLeft || st.TimeLeft != time.Hour {
		t.Fatalf("while heating: %v", st.TimeLeft)
	}

	// This printer is 1.5 times slower than the slicer. The heating does not count.
	st.Preparing = false
	var left []time.Duration
	for i, end := range ends {
		slicer := float64(i+1) * 60
		real := 1.5 * slicer
		st.FilePos, st.PrintDuration = end, 10*time.Minute+time.Duration(real*float64(time.Second))
		v.TimeLeft(&st, now.Add(time.Duration(real*float64(time.Second))))
		left = append(left, st.TimeLeft)
	}
	// From a quarter of the print, the estimate is within 10% of the true time left: 1.5 times the
	// time of the slicer. It never jumps: from one layer to the next, it changes by at most 5% of
	// the print time, besides the 1.5 minutes of the layer.
	for i, l := range left {
		truth := time.Duration(float64(59-i) * 1.5 * float64(time.Minute))
		if i >= 15 && (l < truth*9/10-time.Minute || l > truth*11/10+time.Minute) {
			t.Errorf("layer %d: %v, want about %v", i, l, truth)
		}
		if step := 90 * time.Minute / 20; i > 0 && (l-left[i-1] > step || left[i-1]-l > step+90*time.Second) {
			t.Errorf("layer %d: a jump from %v to %v", i, left[i-1], l)
		}
	}
	if left[59] > time.Minute {
		t.Errorf("at the end: %v", left[59])
	}
}
