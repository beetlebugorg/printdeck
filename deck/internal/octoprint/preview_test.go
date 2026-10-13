package octoprint

import "testing"

func TestMatchPicksClosest(t *testing.T) {
	stls := []string{"base.stl", "body.stl", "cap.stl", "frame.stl", "inky_frame.stl", "pad.stl"}
	for gc, want := range map[string]string{
		"speakerdeck_inky_frame.gcode": "inky_frame.stl",
		"CE3E3V2_frame.gcode":          "frame.stl",
		"Body.gcode":                   "body.stl",
		"speakerdeck_cap.gcode":        "", // too short to match
	} {
		best, score := "", 0
		for _, s := range stls {
			if n := match(gc, s); n > score {
				best, score = s, n
			}
		}
		if best != want {
			t.Errorf("%s: got %q, want %q", gc, best, want)
		}
	}
}
