package motion

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"printdeck/internal/klipper/serialqueue"
)

// The Go solver and step compression give the same messages as Klipper's itersolve.c,
// kin_cartesian.c, kin_extruder.c and stepcompress.c for the scenarios of testdata (from
// tools/klipper-reference/dumpsteps.sh), byte for byte.
func TestStepsMatchKlipper(t *testing.T) {
	for _, name := range []string{"steps", "steps-extruder"} {
		t.Run(name, func(t *testing.T) { runSteps(t, name) })
	}
}

func runSteps(t *testing.T, name string) {
	in, err := os.Open("testdata/" + name + ".txt")
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	want, err := os.ReadFile("testdata/" + name + ".out")
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	tq := NewTrapQ()
	var q []serialqueue.Message
	sc := NewStepCompress(&q)
	var sk *StepperKinematics
	freq := 0.
	s := bufio.NewScanner(in)
	for s.Scan() {
		f := strings.Fields(s.Text())
		if len(f) == 0 {
			continue
		}
		a := make([]float64, len(f)-1)
		for i, v := range f[1:] {
			a[i], err = strconv.ParseFloat(v, 64)
			if err != nil {
				t.Fatal(err)
			}
		}
		switch f[0] {
		case "setup":
			freq = a[2]
			if a[3] == 3 {
				sk = NewExtruderStepper()
			} else {
				sk = NewCartesianStepper('x' + byte(a[3]))
			}
			sk.SetTrapQ(tq, a[0])
			sc.Fill(3, uint32(a[1]), 7, 9)
			sc.SetTime(0., freq)
		case "move":
			tq.Append(a[0], a[1], a[2], a[3], a[4], a[5], a[6], a[7], a[8], a[9], a[10], a[11], a[12])
		case "gen":
			tq.CheckSentinels()
			ret := int(sk.GenerateSteps(sc, a[0]))
			if ret == 0 {
				ret = sc.Flush(uint64(a[0] * freq))
			}
			for _, m := range q {
				fmt.Fprintf(&out, "msg %d %d %x\n", m.MinClock, m.ReqClock, m.Data)
			}
			q = q[:0]
			fmt.Fprintf(&out, "pos %s %d ret=%d\n", hexFloat(sk.CommandedPos), sc.FindPastPosition(math.MaxUint64), ret)
		case "pa":
			ExtruderSetPressureAdvance(sk, a[0], a[1], a[2])
		case "finalize":
			tq.FinalizeMoves(a[0], 0.)
		}
	}
	got, wantLines := strings.Split(out.String(), "\n"), strings.Split(string(want), "\n")
	for i := range wantLines {
		if i >= len(got) || got[i] != wantLines[i] {
			g := ""
			if i < len(got) {
				g = got[i]
			}
			t.Fatalf("line %d:\n got %q\nwant %q", i+1, g, wantLines[i])
		}
	}
}

// hexFloat writes a float as C's printf("%a").
func hexFloat(f float64) string {
	if f == 0 {
		if math.Signbit(f) {
			return "-0x0p+0"
		}
		return "0x0p+0"
	}
	s := strconv.FormatFloat(f, 'x', -1, 64)
	// Go writes "0x1.8p+01"; C writes "0x1.8p+1".
	mant, exp, _ := strings.Cut(s, "p")
	sign := exp[0]
	n, _ := strconv.Atoi(exp[1:])
	return fmt.Sprintf("%sp%c%d", mant, sign, n)
}
