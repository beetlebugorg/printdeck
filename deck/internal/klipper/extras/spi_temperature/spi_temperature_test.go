package spi_temperature

import "testing"

// The conversions of the real Klipper classes of spi_temperature.py.
func TestConversions(t *testing.T) {
	temps := []float64{-10., 0., 25.3, 210.7, 500.}
	adcs := []int64{0, 1234, 8000, 0x7ff8, 0xffff, 1 << 20, 0x3ffff << 5}
	cases := []struct {
		name  string
		c     chip
		adc   []int64
		temps []float64
	}{
		{"MAX6675", max6675{}, []int64{0, 0, 808, 6744, 16000},
			[]float64{0.0, 38.5, 250.0, 1023.75, 2047.75, 32768.0, -2047.25}},
		{"MAX31855", max31855{}, []int64{0, 0, 26476544, 220987392, 524288000},
			[]float64{0.0, 0.0, 0.0, 0.0, 0.0, 1.0, 7.75}},
		{"MAX31856", max31856{}, []int64{0, 0, 103616, 863040, 2048000},
			[]float64{0.0, 0.296875, 1.953125, 7.9921875, 15.9921875, 256.0, 2047.9921875}},
	}
	for _, c := range cases {
		for i, temp := range temps {
			if got := c.c.calcADC(temp); got != c.adc[i] {
				t.Errorf("%s calc_adc(%v) = %d, want %d", c.name, temp, got, c.adc[i])
			}
		}
		for i, adc := range adcs {
			if got := c.c.calcTemp(adc); got != c.temps[i] {
				t.Errorf("%s calc_temp(%d) = %v, want %v", c.name, adc, got, c.temps[i])
			}
		}
	}
	m := &max31865{adcToResistDivNominal: 430. / float64(1<<15) / 100.}
	for i, temp := range []float64{-10., 0., 25.3, 210.7, 2000.} {
		if got, want := m.calcADC(temp), []int64{14644, 15240, 16742, 27400, 65534}[i]; got != want {
			t.Errorf("MAX31865 calc_adc(%v) = %d, want %d", temp, got, want)
		}
	}
	for i, adc := range []int64{16000, 20000, 15000 << 1, 30000} {
		want := []float64{12.767398545119214, 80.86173333091834, 257.57987014868525, 257.57987014868525}[i]
		if got := m.calcTemp(adc); got != want {
			t.Errorf("MAX31865 calc_temp(%d) = %v, want %v", adc, got, want)
		}
	}
}
