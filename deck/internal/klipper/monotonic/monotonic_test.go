package monotonic

import (
	"testing"
	"time"
)

func TestNow(t *testing.T) {
	a := Now()
	time.Sleep(20 * time.Millisecond)
	b := Now()
	if d := b - a; d < 0.015 || d > 0.5 {
		t.Errorf("20 ms of sleep is %.4f s of the clock", d)
	}
}
