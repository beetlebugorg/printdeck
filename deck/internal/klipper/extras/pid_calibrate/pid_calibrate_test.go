package pid_calibrate

import (
	"fmt"
	"testing"
)

type fakeHeater struct{ target, pwm float64 }

func (h *fakeHeater) MaxPower() float64          { return 1. }
func (h *fakeHeater) PWMDelay() float64          { return 0.3 }
func (h *fakeHeater) SetPWM(_, v float64)        { h.pwm = v }
func (h *fakeHeater) AlterTarget(target float64) { h.target = target }

// TestAutoTune runs the auto tune on a heater with a lag. The power heats a block, and the block
// heats the sensor. The Python code of Klipper gives the same result for this heater.
func TestAutoTune(t *testing.T) {
	h := &fakeHeater{target: 200.}
	c := NewControlAutoTune(h, 200.)
	block, sensor, tm := 25., 25., 0.
	for c.CheckBusy(tm, sensor, h.target) && tm < 5000 {
		c.TemperatureUpdate(tm, sensor, h.target)
		block += (h.pwm*4. - (block-25.)*0.01) * 0.3
		sensor += (block - sensor) * 0.05
		tm += 0.3
	}
	kp, ki, kd := c.CalcFinalPID()
	got := fmt.Sprintf("%d %.3f %.3f %.3f", len(c.peaks), kp, ki, kd)
	if want := "12 42.643 3.842 118.335"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
