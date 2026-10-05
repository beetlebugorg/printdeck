package amp

import "testing"

func TestPercentIsAmplitude(t *testing.T) {
	// percent, step: 100% = 0 dB, 50% = -6 dB, 10% = -20 dB, 1% = -40 dB.
	for _, c := range []struct{ p, step int }{{100, 207}, {50, 195}, {10, 167}, {1, 127}, {0, 0}} {
		if got := stepFor(c.p); got != c.step {
			t.Errorf("stepFor(%d) = %d, want %d", c.p, got, c.step)
		}
		if got := percentFor(c.step); got != c.p {
			t.Errorf("percentFor(%d) = %d, want %d", c.step, got, c.p)
		}
	}
}
