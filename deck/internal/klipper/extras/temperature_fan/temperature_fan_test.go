package temperature_fan

import "testing"

type stubFan struct {
	target float64
	calls  [][2]float64
}

func (s *stubFan) GetTemp(float64) (float64, float64) { return 0., s.target }
func (s *stubFan) getMaxSpeed() float64               { return 1.0 }
func (s *stubFan) getMinSpeed() float64               { return 0.3 }
func (s *stubFan) setTFSpeed(t, v float64)            { s.calls = append(s.calls, [2]float64{t, v}) }

var temps = [][2]float64{{1.0, 20.}, {1.3, 30.}, {1.6, 38.}, {2.0, 41.}, {2.3, 43.}, {2.9, 44.5}, {4.0, 39.},
	{6.5, 36.}, {7.0, 50.}, {9.5, 42.}}

// The speeds of the real Klipper ControlPID and ControlBangBang (temperature_fan.py) for a series
// of temperatures, with Kp 40, Ki 2, Kd 30, max_delta 2 and a target of 40.
func TestControls(t *testing.T) {
	pid := &stubFan{target: 40.}
	c := &controlPID{temperatureFan: pid, kp: 40. / PIDParamBase, ki: 2. / PIDParamBase, kd: 30. / PIDParamBase,
		minDerivTime: 2., prevTemp: AmbientTemp}
	c.tempIntegMax = pid.getMaxSpeed() / c.ki
	bang := &stubFan{target: 40.}
	b := &controlBangBang{temperatureFan: bang, maxDelta: 2.0}
	for _, tt := range temps {
		c.temperatureCallback(tt[0], tt[1])
		b.temperatureCallback(tt[0], tt[1])
	}
	wantPID := []float64{0.3, 0.3, 1.0, 1.0, 1.0, 1.0, 0.7973775980392157, 0.3, 1.0, 0.8894117647058823}
	wantBang := []float64{0.0, 0.0, 0.0, 0.0, 1.0, 1.0, 1.0, 0.0, 1.0, 1.0}
	for i := range temps {
		if pid.calls[i][1] != wantPID[i] {
			t.Errorf("pid %d: %v, want %v", i, pid.calls[i][1], wantPID[i])
		}
		if bang.calls[i][1] != wantBang[i] {
			t.Errorf("bang %d: %v, want %v", i, bang.calls[i][1], wantBang[i])
		}
	}
}
