package gcode_move

import (
	"reflect"
	"testing"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
)

type move struct {
	pos   []float64
	speed float64
}

type fakeTransform struct{ moves []move }

func (f *fakeTransform) Move(newpos []float64, speed float64) {
	f.moves = append(f.moves, move{append([]float64(nil), newpos...), speed})
}

func (f *fakeTransform) Position() []float64 {
	if len(f.moves) == 0 {
		return []float64{0, 0, 0, 0}
	}
	return append([]float64(nil), f.moves[len(f.moves)-1].pos...)
}

func setup(t *testing.T) (*GCodeMove, *fakeTransform, *gcode.Dispatch) {
	p := host.New(reactor.New(), host.StartArgs{})
	pc := configfile.NewPrinterConfig(p)
	config, err := pc.ReadConfigData("[gcode_move]\n", "test.cfg")
	if err != nil {
		t.Fatal(err)
	}
	m := load(config.GetSection("gcode_move")).(*GCodeMove)
	f := &fakeTransform{}
	m.SetMoveTransform(f, false)
	p.SendEvent("klippy:ready")
	return m, f, gcode.Of(p)
}

func TestMoves(t *testing.T) {
	m, f, g := setup(t)
	g.RunScript("G1 X10 Y20 F600\nG91\nG1 X1 E2\nG90\nG92 X0\nG1 X5\nM220 S200\nG1 Y0\nM83\nG1 E1\nM221 S50\nG1 E2")
	want := []move{
		{[]float64{10, 20, 0, 0}, 10},
		{[]float64{11, 20, 0, 2}, 10},
		{[]float64{16, 20, 0, 2}, 10},
		{[]float64{16, 0, 0, 2}, 20},
		{[]float64{16, 0, 0, 3}, 20},
		{[]float64{16, 0, 0, 4}, 20},
	}
	if !reflect.DeepEqual(f.moves, want) {
		t.Errorf("moves\n got %v\nwant %v", f.moves, want)
	}
	// G92 X0 at X11 sets the X base to 11. M221 S50 at E3 sets the E base to 1.5, so E4 is the
	// G-code position (4-1.5)/0.5.
	if p := m.gcodePosition(); !reflect.DeepEqual(p, []float64{5, 0, 0, 5}) {
		t.Errorf("gcode position %v", p)
	}
}

func TestSaveRestore(t *testing.T) {
	m, f, g := setup(t)
	g.RunScript("G1 X10 Y10 Z5 F1200\nSAVE_GCODE_STATE NAME=s\nG91\nG1 X5\nM220 S50\nRESTORE_GCODE_STATE NAME=s MOVE=1")
	last := f.moves[len(f.moves)-1]
	if !reflect.DeepEqual(last.pos, []float64{10, 10, 5, 0}) || last.speed != 20 {
		t.Errorf("restore move %v", last)
	}
	if !m.absoluteCoord || m.speedFactor != 1./60. {
		t.Errorf("state not restored: absolute=%v speed_factor=%v", m.absoluteCoord, m.speedFactor)
	}
	defer func() {
		if r := recover(); r == nil || r.(*gcode.CommandError).Msg != "Unknown g-code state: x" {
			t.Errorf("error %v", r)
		}
	}()
	g.RunScript("RESTORE_GCODE_STATE NAME=x")
}
