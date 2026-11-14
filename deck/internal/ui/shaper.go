package ui

import (
	"fmt"
	"strings"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// The Input shaping page (Calibration > Input shaping): the tests of the input shaper, in order.
//
//	Ringing tower   the shaper off: count the ripples after a notch (Ripples).
//	Shake test      the shaper off: X, Y or both shake through a range of frequencies, and a
//	                phone records the sound or the acceleration. The loudest step of an axis
//	                gives its frequency.
//	Sweep           the shaper on at a frequency in each band, on 1 tower or on 5 over the bed:
//	                the smoothest band of each axis gives its frequency (Sweep result).
//	A/B pair        two towers, the lower and the higher frequency of each axis around the shaper
//	                now: the smoother side moves the frequency, and the step halves (Pair result).
//	                The side of the higher frequency changes each round.
//	Check tower     the shaper on while the acceleration rises: the height where ringing or round
//	                corners start gives max_accel (Max acceleration).
//
// The sweep needs an [input_shaper] section: the result of the ripples makes it. The pair and the
// check need a shaper.

// pairRound is the A/B pair that the deck printed last: the frequencies (X, Y) of each tower, and
// the step. highLeft is true when the next pair has the higher frequencies at the left.
type pairRound struct {
	left, right [2]float64
	step        float64
	highLeft    bool
}

// The first step of the A/B pair, after the sweep: half a band of the sweep.
const pairFirstStep = printer.SweepStep / 2

// shaperLine is the line of the Input shaping row of the Calibration page.
func shaperLine(mach printer.Machine) string {
	if mach.HasShaper && (mach.ShaperFreqX > 0 || mach.ShaperFreqY > 0) {
		return fmt.Sprintf("%s · X %.1f Hz, Y %.1f Hz", strings.ToUpper(mach.ShaperType), mach.ShaperFreqX, mach.ShaperFreqY)
	}
	return "Off · print the ringing tower first"
}

// setShaper sets the MZV shaper at the frequencies, and tells it.
func (a *App) setShaper(fx, fy float64, then string) {
	pr := a.Printer
	a.do(func() error {
		if err := pr.SetInputShaper(fx, fy); err != nil {
			return err
		}
		a.Toast(fmt.Sprintf("Input shaper X %.1f Hz, Y %.1f Hz, in printer.cfg%s", fx, fy, then))
		return nil
	})
}

// drawShaper is the Input shaping page.
func (a *App) drawShaper(c *gfx.Canvas, st printer.State) {
	pr := a.Printer
	mach := pr.Machine()
	f := a.navPage(c, "Input shaping")
	idle := st.Connected && !st.Busy()
	m := a.loaded()
	when := func(run func()) func() {
		return func() {
			if a.probeCal != nil {
				a.Toast("A calibration runs. Stop it first.")
				return
			}
			if !idle {
				a.Toast("Wait until the print ends")
				return
			}
			run()
		}
	}
	askPrint := func(q string, do func() error) func() {
		return when(func() {
			a.confirm = &confirmBox{question: q, yes: "Print", onYes: func() { a.do(do) }}
		})
	}
	set := mach.HasShaper && (mach.ShaperFreqX > 0 || mach.ShaperFreqY > 0)
	items := []menuItem{{"Shaper now", shaperLine(mach), dim, nil}}
	items = append(items,
		menuItem{"Ringing tower", "The shaper off: count ripples · 15 min", dim,
			askPrint(fmt.Sprintf("Print the ringing tower at %d°?", m.Tool), func() error {
				return pr.PrintRingTower(m.Tool, m.Bed, m.Name, false)
			})},
		menuItem{"Ripples", "Enter the ripples of the ringing tower", dim, when(a.askShaper)},
		menuItem{"Shake test", fmt.Sprintf("%.0f to %.0f Hz · record it with a phone", printer.ShakeStart, printer.ShakeEnd), dim,
			when(func() {
				shake := func(axes string) func() {
					return func() { a.do(func() error { return pr.PrintShakeTest(axes) }) }
				}
				a.confirm = &confirmBox{question: "Home, then shake? Start the recording before you press.", choices: []menuItem{
					{"X", "The head · about 1.5 minutes", dim, shake("X")},
					{"Y", "The bed · about 1.5 minutes", dim, shake("Y")},
					{"X and Y", "X, 3 s quiet, then Y · about 3 minutes", dim, shake("XY")},
				}}
			})})
	if mach.HasShaper {
		items = append(items,
			menuItem{"Sweep", fmt.Sprintf("%.0f to %.0f Hz in %.1f mm bands · 1 or 5 towers", printer.SweepStart,
				printer.SweepStart+printer.SweepStep*(printer.SweepBands-1), printer.SweepBand), dim, when(func() {
				a.confirm = &confirmBox{question: fmt.Sprintf("Print the sweep at %d°?", m.Tool), choices: []menuItem{
					{"1 tower", "The middle of the bed · about 10 minutes", dim, func() {
						a.do(func() error { return pr.PrintSweepTower(m.Tool, m.Bed, m.Name, false) })
					}},
					{"5 towers", "The middle and the corners, as the bed level test · about 40 minutes", dim, func() {
						a.do(func() error { return pr.PrintSweepTower(m.Tool, m.Bed, m.Name, true) })
					}},
				}}
			})},
			menuItem{"Sweep result", "The smoothest band of the middle tower", dim, when(a.askSweep)})
	}
	if set {
		step := pairFirstStep
		if a.pair != nil {
			step = a.pair.step
		}
		items = append(items,
			menuItem{"A/B pair", fmt.Sprintf("X %.1f, Y %.1f, each ± %.2f Hz · 6 min", mach.ShaperFreqX, mach.ShaperFreqY, step), dim,
				askPrint(fmt.Sprintf("Print the A/B pair at %d°?", m.Tool), func() error { return a.printPair(m) })},
			menuItem{"Pair result", "The smoother tower of each axis", dim, when(a.askPair)},
			menuItem{"Check tower", "The shaper on, the acceleration up · 15 min", dim,
				askPrint(fmt.Sprintf("Print the check tower at %d°, with the input shaper on?", m.Tool), func() error {
					return pr.PrintRingTower(m.Tool, m.Bed, m.Name, true)
				})})
	}
	items = append(items, menuItem{"Max acceleration", fmt.Sprintf("Now %.0f mm/s² · set it from the check tower", mach.MaxAccel), dim,
		when(a.askMaxAccel)})
	a.menuRowsWait(c, a.listArea(f), f.wide, 86, 12, items, st.Busy())
}

// printPair prints the A/B pair around the shaper now: the step of the last round, or the first
// step. The higher frequency goes to the other side than in the last round.
func (a *App) printPair(m material) error {
	pr := a.Printer
	mach := pr.Machine()
	r := pairRound{step: pairFirstStep}
	if a.pair != nil {
		r.step = a.pair.step
		r.highLeft = a.pair.highLeft
	}
	lo := [2]float64{mach.ShaperFreqX - r.step, mach.ShaperFreqY - r.step}
	hi := [2]float64{mach.ShaperFreqX + r.step, mach.ShaperFreqY + r.step}
	r.left, r.right = lo, hi
	if r.highLeft {
		r.left, r.right = hi, lo
	}
	r.highLeft = !r.highLeft // the next pair: the other side
	if err := pr.PrintPairTowers(m.Tool, m.Bed, m.Name, r.left, r.right); err != nil {
		return err
	}
	a.post(func() { a.pair = &r })
	return nil
}

// askSweep asks for the height of the smoothest band of the middle tower, for X (the left or
// the right wall) and for Y (the front wall), and sets the shaper at their frequencies.
func (a *App) askSweep() {
	var fx float64
	top := printer.SweepBand * printer.SweepBands
	a.editPosition("X: height of the smoothest band, left or right wall", 10, 0, top, func(hx float64) error {
		fx = printer.SweepFreqAt(hx)
		a.post(func() {
			a.editPosition("Y: height of the smoothest band, front wall", 10, 0, top, func(hy float64) error {
				fy := printer.SweepFreqAt(hy)
				a.post(func() {
					q := fmt.Sprintf("Set the input shaper to MZV, X %.0f Hz, Y %.0f Hz?", fx, fy)
					a.confirm = &confirmBox{question: q, yes: "Set", onYes: func() {
						a.pair = &pairRound{step: pairFirstStep}
						a.setShaper(fx, fy, ". Next: the A/B pair.")
					}}
					a.Redraw()
				})
				return nil
			})
			a.Redraw()
		})
		return nil
	})
}

// askPair asks which tower of the last A/B pair is smoother on each axis: left, right, or the
// same. The frequency of an axis moves to the smoother tower, and the next step is half.
func (a *App) askPair() {
	r := a.pair
	if r == nil || r.left == r.right {
		a.Toast("Print the A/B pair first")
		return
	}
	var f [2]float64
	mach := a.Printer.Machine()
	now := [2]float64{mach.ShaperFreqX, mach.ShaperFreqY}
	var ask func(axis int)
	ask = func(axis int) {
		name, wall := "X", "left or right walls"
		if axis == 1 {
			name, wall = "Y", "front walls"
		}
		pick := func(v float64) func() {
			return func() {
				f[axis] = v
				if axis == 0 {
					a.post(func() { ask(1) })
					return
				}
				next := r.step / 2
				if f == now {
					a.pair = &pairRound{step: next, highLeft: r.highLeft}
					a.Toast(fmt.Sprintf("Same on both axes: the shaper stays at X %.1f Hz, Y %.1f Hz.", f[0], f[1]))
					return
				}
				a.pair = &pairRound{step: next, highLeft: r.highLeft}
				a.setShaper(f[0], f[1], fmt.Sprintf(". Next pair: ± %.2f Hz.", next))
			}
		}
		a.confirm = &confirmBox{question: fmt.Sprintf("%s: which tower is smoother on the %s?", name, wall), choices: []menuItem{
			{"Left", fmt.Sprintf("%.2f Hz", r.left[axis]), dim, pick(r.left[axis])},
			{"Right", fmt.Sprintf("%.2f Hz", r.right[axis]), dim, pick(r.right[axis])},
			{"The same", fmt.Sprintf("Keep %.2f Hz", now[axis]), dim, pick(now[axis])},
		}}
		a.Redraw()
	}
	ask(0)
}

// askMaxAccel asks for the height on the check tower where ringing or round corners start, and
// sets the acceleration of the band under it (printer.RingMaxAccel).
func (a *App) askMaxAccel() {
	pr := a.Printer
	a.editPosition("Height where ringing or round corners start", 20, 0, printer.RingTowerHeight, func(h float64) error {
		v := printer.RingMaxAccel(h)
		if v <= 0 {
			return fmt.Errorf("The first band is the lowest acceleration of the tower. Keep max_accel.")
		}
		a.post(func() {
			a.confirm = &confirmBox{question: fmt.Sprintf("Set max_accel to %.0f mm/s²?", v), yes: "Set", onYes: func() {
				a.do(func() error {
					if err := pr.SetMaxAccel(v); err != nil {
						return err
					}
					a.Toast(fmt.Sprintf("max_accel %.0f, in printer.cfg", v))
					return nil
				})
			}}
			a.Redraw()
		})
		return nil
	})
}

// askShaper asks for the ripples of the ringing tower: for each axis, a count of ripples after the
// notch and the distance over them, in mm. X rings on the left and the right walls, Y on the front
// wall. The
// deck then sets the MZV shaper at the frequencies (printer.RingFrequency).
func (a *App) askShaper() {
	pr := a.Printer
	var f [2]float64
	var ask func(axis int)
	ask = func(axis int) {
		name, wall := "X", "left or right wall"
		if axis == 1 {
			name, wall = "Y", "front wall"
		}
		a.editNumber(fmt.Sprintf("%s: ripples after the notch, %s", name, wall), 6, 40, " ripples", func(n int) {
			a.editPosition(fmt.Sprintf("%s: distance over the %d ripples", name, n), 10, 1, 100, func(d float64) error {
				hz := printer.RingFrequency(n, d)
				if hz < 10 || hz > 150 {
					return fmt.Errorf("%.1f Hz is not a ringing frequency. Measure again.", hz)
				}
				f[axis] = hz
				a.post(func() {
					if axis == 0 {
						ask(1)
						return
					}
					q := fmt.Sprintf("Set the input shaper to MZV, X %.1f Hz, Y %.1f Hz?", f[0], f[1])
					a.confirm = &confirmBox{question: q, yes: "Set", onYes: func() {
						a.do(func() error {
							if err := pr.SetInputShaper(f[0], f[1]); err != nil {
								return err
							}
							a.Toast(fmt.Sprintf("Input shaper X %.1f Hz, Y %.1f Hz, in printer.cfg", f[0], f[1]))
							return nil
						})
					}}
					a.Redraw()
				})
				return nil
			})
		})
	}
	ask(0)
}
