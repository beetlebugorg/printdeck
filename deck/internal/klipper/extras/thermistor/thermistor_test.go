package thermistor

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/adc_temperature"
)

type reference struct {
	ADCs    []float64 `json:"adcs"`
	Temps   []float64 `json:"temps"`
	Sensors map[string]struct {
		Temps []float64  `json:"temps"`
		ADCs  []*float64 `json:"adcs"`
	} `json:"sensors"`
}

func calcADC(c adc_temperature.Converter, t float64) (v *float64) {
	defer func() {
		if recover() != nil {
			v = nil
		}
	}()
	a := c.CalcADC(t)
	return &a
}

// close reports two values equal to 1e-12 of their size. Go's math.Log and math.Pow differ from
// the C library of Python in the last bits.
func close(a, b float64) bool { return math.Abs(a-b) <= 1e-12*math.Max(math.Abs(a), math.Abs(b)) }

// The Go conversions are equal (to the last bits) to the conversions of Klipper's thermistor.py
// and adc_temperature.py (testdata/temps.json, from tools/klipper-reference/dumptemps.py), for
// each thermistor of temperature_sensors.cfg and each default sensor table.
func TestConversionsMatchKlipper(t *testing.T) {
	b, err := os.ReadFile("testdata/temps.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref reference
	if err := json.Unmarshal(b, &ref); err != nil {
		t.Fatal(err)
	}
	cfgPath := "../heaters/temperature_sensors.cfg"
	data, err := configfile.ReadConfigFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	fc, err := configfile.BuildFileconfig(data, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	pc := configfile.NewPrinterConfig(nil)
	test, err := pc.ReadConfigData("[test]\n", "test.cfg")
	if err != nil {
		t.Fatal(err)
	}
	section := test.GetSection("test")
	conv := map[string]adc_temperature.Converter{}
	for _, s := range fc.Sections() {
		if !strings.HasPrefix(s, "thermistor ") {
			continue
		}
		g := func(o string) float64 {
			v, _ := fc.Get(s, o)
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				t.Fatal(err)
			}
			return f
		}
		th := NewThermistor(4700., 0.)
		if fc.HasOption(s, "beta") {
			th.SetupCoefficientsBeta(g("temperature1"), g("resistance1"), g("beta"))
		} else {
			p := [][2]float64{}
			for i := 1; i <= 3; i++ {
				p = append(p, [2]float64{g(fmt.Sprintf("temperature%d", i)), g(fmt.Sprintf("resistance%d", i))})
			}
			sort.Slice(p, func(i, j int) bool { return p[i][0] < p[j][0] })
			th.SetupCoefficients(p[0][0], p[0][1], p[1][0], p[1][1], p[2][0], p[2][1], "")
		}
		conv[s] = th
	}
	for name, params := range map[string][]adc_temperature.TempValue{"AD595": adc_temperature.AD595,
		"AD597": adc_temperature.AD597, "AD8494": adc_temperature.AD8494, "AD8495": adc_temperature.AD8495,
		"AD8496": adc_temperature.AD8496, "AD8497": adc_temperature.AD8497} {
		conv[name] = adc_temperature.NewLinearVoltage(section, params)
	}
	if len(conv) != len(ref.Sensors)-2 {
		t.Errorf("%d sensors, want %d", len(conv), len(ref.Sensors)-2)
	}
	for name, c := range conv {
		want, ok := ref.Sensors[name]
		if !ok {
			t.Errorf("%s: no reference", name)
			continue
		}
		for i, a := range ref.ADCs {
			if got := c.CalcTemp(a); !close(got, want.Temps[i]) {
				t.Errorf("%s: calc_temp(%v) = %v, want %v", name, a, got, want.Temps[i])
			}
		}
		for i, temp := range ref.Temps {
			got := calcADC(c, temp)
			w := want.ADCs[i]
			if (got == nil) != (w == nil) || (got != nil && !close(*got, *w)) {
				t.Errorf("%s: calc_adc(%v) = %v, want %v", name, temp, deref(got), deref(w))
			}
		}
	}
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}
