package gcode_arcs

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"

	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/pyfmt"
)

type fakeMove struct {
	status map[string]any
	moves  []map[string]float64
}

func (f *fakeMove) Status(float64) map[string]any { return f.status }

func (f *fakeMove) CmdG1(gcmd *gcode.Command) {
	m := map[string]float64{}
	for k, v := range gcmd.Params() {
		m[k], _ = pyfmt.ParseFloat(v)
	}
	f.moves = append(f.moves, m)
}

type arcCase struct {
	Plane           string
	Pos             []float64
	AbsoluteExtrude bool `json:"absolute_extrude"`
	Line            string
	Error           string
	Moves           []map[string]float64
}

// TestArcsMatchKlipper compares the G1 moves with the moves of Klipper's gcode_arcs.py
// (testdata/arcs.json from tools/klipper-reference/dumparcs.py). Go's sin, cos and atan2 can
// differ from the C library in the last bit, so the coordinates have a small tolerance.
func TestArcsMatchKlipper(t *testing.T) {
	data, err := os.ReadFile("testdata/arcs.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []arcCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	var g *gcode.Dispatch
	exact, total := 0, 0
	for _, c := range cases {
		fm := &fakeMove{status: map[string]any{"absolute_coordinates": true,
			"gcode_position":   gcode.Coord{c.Pos[0], c.Pos[1], c.Pos[2], c.Pos[3]},
			"absolute_extrude": c.AbsoluteExtrude}}
		a := &ArcSupport{mmPerArcSegment: 0.5, gcodeMove: fm, gcode: g}
		run := func(line string) {
			parts := strings.Fields(line)
			params := map[string]string{}
			for _, p := range parts[1:] {
				params[p[:1]] = p[1:]
			}
			gcmd := g.CreateCommand(parts[0], line, params)
			map[string]func(*gcode.Command){"G2": a.cmdG2, "G3": a.cmdG3, "G17": a.cmdG17,
				"G18": a.cmdG18, "G19": a.cmdG19}[parts[0]](gcmd)
		}
		errMsg := ""
		func() {
			defer func() {
				if r := recover(); r != nil {
					errMsg = r.(*gcode.CommandError).Error()
				}
			}()
			run(c.Plane)
			run(c.Line)
		}()
		if errMsg != c.Error {
			t.Errorf("%s: error %q, want %q", c.Line, errMsg, c.Error)
			continue
		}
		if len(fm.moves) != len(c.Moves) {
			t.Errorf("%s: %d moves, want %d", c.Line, len(fm.moves), len(c.Moves))
			continue
		}
		for i, want := range c.Moves {
			got := fm.moves[i]
			if len(got) != len(want) {
				t.Errorf("%s move %d: %v, want %v", c.Line, i, got, want)
				continue
			}
			for k, w := range want {
				total++
				if got[k] == w {
					exact++
				} else if math.Abs(got[k]-w) > 1e-12*math.Max(1, math.Abs(w)) {
					t.Errorf("%s move %d %s: %v, want %v", c.Line, i, k, got[k], w)
				}
			}
		}
	}
	t.Logf("%d of %d values are exact", exact, total)
}
