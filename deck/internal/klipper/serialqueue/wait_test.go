package serialqueue

import (
	"testing"
	"time"
)

func TestTimerWait(t *testing.T) {
	for _, c := range []struct {
		next, now float64
		want      time.Duration
	}{
		{10, 9.5, 500 * time.Millisecond},
		{9, 10, 0},
		{prNever, 10, time.Hour},
		{5.76e11, 10, time.Hour}, // MaxClock as a time: this was a negative wait on amd64
	} {
		if got := timerWait(c.next, c.now); got != c.want {
			t.Errorf("timerWait(%g, %g) = %v, want %v", c.next, c.now, got, c.want)
		}
	}
}
