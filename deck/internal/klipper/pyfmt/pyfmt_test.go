package pyfmt

import (
	"math"
	"testing"
)

func TestFloat(t *testing.T) {
	for f, want := range map[float64]string{
		0: "0.0", 1: "1.0", -2.5: "-2.5", 0.1: "0.1", 1e-5: "1e-05", 0.0001: "0.0001",
		1e16: "1e+16", 123456789012345.0: "123456789012345.0", 1.5e300: "1.5e+300",
		math.Inf(1): "inf", 0.30000000000000004: "0.30000000000000004", 9.999999999999999e-05: "9.999999999999999e-05",
	} {
		if got := Float(f); got != want {
			t.Errorf("Float(%v) = %q, want %q", f, got, want)
		}
	}
}

func TestParse(t *testing.T) {
	for s, want := range map[string]float64{" 1.5 ": 1.5, "1_000": 1000, "-inf": math.Inf(-1), "1e3": 1000, ".5": .5} {
		if got, err := ParseFloat(s); err != nil || got != want {
			t.Errorf("ParseFloat(%q) = %v, %v", s, got, err)
		}
	}
	for _, s := range []string{"", "0x10", "1__0", "_1", "--1", "nan1", "1,5"} {
		if _, err := ParseFloat(s); err == nil {
			t.Errorf("ParseFloat(%q) gives no error", s)
		}
	}
	if n, err := ParseInt(" +1_0 "); err != nil || n != 10 {
		t.Errorf("ParseInt = %v, %v", n, err)
	}
	if _, err := ParseInt("1.0"); err == nil {
		t.Error("ParseInt(1.0) gives no error")
	}
}
