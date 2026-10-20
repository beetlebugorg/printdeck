// Package adc_temperature reads temperatures from analog pins: the link between an analog pin and
// a heater, and the sensors with a table of voltages or resistances (AD595, PT1000, ...).
//
// Ported to Go from klippy/extras/adc_temperature.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2016-2024  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package adc_temperature

import (
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/error_mcu"
	"printdeck/internal/klipper/extras/heaters"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
)

// Interface between MCU adc and heater temperature callbacks

const (
	SampleTime      = 0.001
	SampleCount     = 8
	ReportTime      = 0.300
	RangeCheckCount = 4
)

// Converter converts between adc values (0 to 1) and temperatures.
type Converter interface {
	CalcTemp(adc float64) float64
	CalcADC(temp float64) float64
}

// PrinterADCtoTemperature is the sensor of a heater on an analog pin (heaters.Sensor).
type PrinterADCtoTemperature struct {
	adcConvert          Converter
	mcuADC              *mcu.ADC
	diagHelper          *HelperTemperatureDiagnostics
	temperatureCallback func(readTime, temp float64)
}

// NewPrinterADCtoTemperature makes the sensor of the sensor_pin of a config section.
func NewPrinterADCtoTemperature(config *configfile.Wrapper, adcConvert Converter) *PrinterADCtoTemperature {
	s := &PrinterADCtoTemperature{adcConvert: adcConvert}
	ppins := host.Of(config).LookupObject("pins").(*pins.Pins)
	s.mcuADC = ppins.SetupPin("adc", config.Get("sensor_pin")).(*mcu.ADC)
	s.mcuADC.SetupADCCallback(s.adcCallback)
	s.diagHelper = NewHelperTemperatureDiagnostics(config, s.mcuADC, adcConvert.CalcTemp)
	return s
}

func (s *PrinterADCtoTemperature) SetupCallback(cb func(readTime, temp float64)) {
	s.temperatureCallback = cb
}

func (s *PrinterADCtoTemperature) ReportTimeDelta() float64 { return ReportTime }

func (s *PrinterADCtoTemperature) adcCallback(samples []mcu.Sample) {
	last := samples[len(samples)-1]
	temp := s.adcConvert.CalcTemp(last.Value)
	s.temperatureCallback(last.Time+SampleCount*SampleTime, temp)
}

func (s *PrinterADCtoTemperature) SetupMinMax(minTemp, maxTemp float64) {
	arange := []float64{s.adcConvert.CalcADC(minTemp), s.adcConvert.CalcADC(maxTemp)}
	sort.Float64s(arange)
	minADC, maxADC := arange[0], arange[1]
	s.mcuADC.SetupADCSample(ReportTime, SampleTime, SampleCount, 1, minADC, maxADC, RangeCheckCount)
	s.diagHelper.SetupDiagMinMax(minTemp, maxTemp, minADC, maxADC)
}

// HelperTemperatureDiagnostics registers the pin with query_adc, and adds the temperature to the
// "ADC out of range" shutdown message.
type HelperTemperatureDiagnostics struct {
	printer          *host.Printer
	name             string
	mcuADC           *mcu.ADC
	calcTempCB       func(adc float64) float64
	hasMinMax        bool
	minTemp, maxTemp float64
	minADC, maxADC   float64
}

type adcRegistrar interface {
	RegisterADC(name string, adc *mcu.ADC)
}

func NewHelperTemperatureDiagnostics(config *configfile.Wrapper, mcuADC *mcu.ADC, calcTempCB func(float64) float64) *HelperTemperatureDiagnostics {
	h := &HelperTemperatureDiagnostics{printer: host.Of(config), name: config.Name(), mcuADC: mcuADC,
		calcTempCB: calcTempCB}
	queryADC := h.printer.LoadObject(config, "query_adc").(adcRegistrar)
	queryADC.RegisterADC(h.name, mcuADC)
	errorMCU := h.printer.LoadObject(config, "error_mcu").(*error_mcu.PrinterMCUError)
	errorMCU.AddClarify("ADC out of range", h.clarifyADCRange)
	return h
}

func (h *HelperTemperatureDiagnostics) SetupDiagMinMax(minTemp, maxTemp, minADC, maxADC float64) {
	h.hasMinMax = true
	h.minTemp, h.maxTemp = minTemp, maxTemp
	h.minADC, h.maxADC = minADC, maxADC
}

func (h *HelperTemperatureDiagnostics) clarifyADCRange(msg string, details map[string]any) string {
	if !h.hasMinMax {
		return ""
	}
	last := h.mcuADC.LastValue()
	if last.Time == 0 {
		return ""
	}
	if last.Value >= h.minADC && last.Value <= h.maxADC {
		return ""
	}
	tempstr := fmt.Sprintf("%.3f", h.calcTempCB(last.Value))
	return fmt.Sprintf("Sensor '%s' temperature %s not in range %.3f:%.3f", h.name, tempstr, h.minTemp, h.maxTemp)
}

// Linear interpolation

// LinearInterpolate interpolates between sample points.
type LinearInterpolate struct {
	keys   []float64
	slopes [][2]float64
}

// Sample is a key and a value.
type Sample struct{ Key, Value float64 }

// NewLinearInterpolate makes the interpolation of samples. Two samples with the same key are an
// error, as are less than two samples.
func NewLinearInterpolate(samples []Sample) (*LinearInterpolate, error) {
	s := append([]Sample(nil), samples...)
	sort.Slice(s, func(i, j int) bool {
		if s[i].Key != s[j].Key {
			return s[i].Key < s[j].Key
		}
		return s[i].Value < s[j].Value
	})
	li := &LinearInterpolate{}
	var lastKey, lastValue float64
	haveLast := false
	for _, sm := range s {
		key, value := sm.Key, sm.Value
		if !haveLast {
			lastKey, lastValue, haveLast = key, value, true
			continue
		}
		if key <= lastKey {
			return nil, errors.New("duplicate value")
		}
		gain := (value - lastValue) / (key - lastKey)
		offset := lastValue - lastKey*gain
		if n := len(li.slopes); n > 0 && li.slopes[n-1] == [2]float64{gain, offset} {
			continue
		}
		lastValue, lastKey = value, key
		li.keys = append(li.keys, key)
		li.slopes = append(li.slopes, [2]float64{gain, offset})
	}
	if len(li.keys) == 0 {
		return nil, errors.New("need at least two samples")
	}
	li.keys = append(li.keys, 9999999999999.)
	li.slopes = append(li.slopes, li.slopes[len(li.slopes)-1])
	return li, nil
}

// Interpolate returns the value of a key.
func (li *LinearInterpolate) Interpolate(key float64) float64 {
	// bisect.bisect: the index after the keys that are <= key
	pos := sort.Search(len(li.keys), func(i int) bool { return li.keys[i] > key })
	s := li.slopes[pos]
	return key*s[0] + s[1]
}

// ReverseInterpolate returns the key of a value.
func (li *LinearInterpolate) ReverseInterpolate(value float64) float64 {
	values := make([]float64, len(li.keys))
	for i, k := range li.keys {
		values[i] = k*li.slopes[i][0] + li.slopes[i][1]
	}
	best := len(values) - 1
	for i := range values {
		var ok bool
		if values[0] < values[len(values)-2] {
			ok = values[i] >= value
		} else {
			ok = values[i] <= value
		}
		if ok {
			best = min(best, i)
			break
		}
	}
	s := li.slopes[best]
	return (value - s[1]) / s[0]
}

// Linear voltage to temperature converter

// TempValue is a temperature and a voltage or a resistance.
type TempValue struct{ Temp, Value float64 }

// LinearVoltage converts with a table of voltages, for the linear style conversion chips.
type LinearVoltage struct{ li *LinearInterpolate }

func NewLinearVoltage(config *configfile.Wrapper, params []TempValue) *LinearVoltage {
	adcVoltage := config.GetFloat("adc_voltage", configfile.Default(5.), configfile.Above(0.))
	voltageOffset := config.GetFloat("voltage_offset", configfile.Default(0.0))
	var samples []Sample
	for _, p := range params {
		adc := (p.Value - voltageOffset) / adcVoltage
		if adc < 0. || adc > 1. {
			log.Printf("Ignoring adc sample %.3f/%.3f in heater %s", p.Temp, p.Value, config.Name())
			continue
		}
		samples = append(samples, Sample{adc, p.Temp})
	}
	li, err := NewLinearInterpolate(samples)
	if err != nil {
		configfile.Raise("adc_temperature %s in heater %s", err, config.Name())
	}
	return &LinearVoltage{li}
}

func (lv *LinearVoltage) CalcTemp(adc float64) float64 { return lv.li.Interpolate(adc) }
func (lv *LinearVoltage) CalcADC(temp float64) float64 { return lv.li.ReverseInterpolate(temp) }

func readParams(config *configfile.Wrapper, valueName string) []TempValue {
	var params []TempValue
	for i := 1; i < 1000; i++ {
		t, ok := config.GetFloatOptional(fmt.Sprintf("temperature%d", i), configfile.Default(nil))
		if !ok {
			break
		}
		v := config.GetFloat(fmt.Sprintf("%s%d", valueName, i))
		params = append(params, TempValue{t, v})
	}
	return params
}

func customName(config *configfile.Wrapper) string {
	return strings.Join(strings.Fields(config.Name())[1:], " ")
}

// Linear resistance to temperature converter

// LinearResistance converts with a table of resistances.
type LinearResistance struct {
	pullup float64
	li     *LinearInterpolate
}

func NewLinearResistance(config *configfile.Wrapper, samples []TempValue) *LinearResistance {
	lr := &LinearResistance{pullup: config.GetFloat("pullup_resistor", configfile.Default(4700.), configfile.Above(0.))}
	s := make([]Sample, len(samples))
	for i, p := range samples {
		s[i] = Sample{p.Value, p.Temp}
	}
	li, err := NewLinearInterpolate(s)
	if err != nil {
		configfile.Raise("adc_temperature %s in heater %s", err, config.Name())
	}
	lr.li = li
	return lr
}

func (lr *LinearResistance) CalcTemp(adc float64) float64 {
	// Calculate temperature from adc
	adc = math.Max(.00001, math.Min(.99999, adc))
	r := lr.pullup * adc / (1.0 - adc)
	return lr.li.Interpolate(r)
}

func (lr *LinearResistance) CalcADC(temp float64) float64 {
	// Calculate adc reading from a temperature
	r := lr.li.ReverseInterpolate(temp)
	return r / (lr.pullup + r)
}

// Default sensors

func pairs(v ...float64) []TempValue {
	out := make([]TempValue, len(v)/2)
	for i := range out {
		out[i] = TempValue{v[2*i], v[2*i+1]}
	}
	return out
}

var (
	AD595 = pairs(0., .0027, 10., .101, 20., .200, 25., .250, 30., .300, 40., .401, 50., .503, 60., .605,
		80., .810, 100., 1.015, 120., 1.219, 140., 1.420, 160., 1.620, 180., 1.817, 200., 2.015, 220., 2.213,
		240., 2.413, 260., 2.614, 280., 2.817, 300., 3.022, 320., 3.227, 340., 3.434, 360., 3.641, 380., 3.849,
		400., 4.057, 420., 4.266, 440., 4.476, 460., 4.686, 480., 4.896)
	AD597 = pairs(0., 0., 10., .097, 20., .196, 25., .245, 30., .295, 40., 0.395, 50., 0.496, 60., 0.598,
		80., 0.802, 100., 1.005, 120., 1.207, 140., 1.407, 160., 1.605, 180., 1.801, 200., 1.997, 220., 2.194,
		240., 2.392, 260., 2.592, 280., 2.794, 300., 2.996, 320., 3.201, 340., 3.406, 360., 3.611, 380., 3.817,
		400., 4.024, 420., 4.232, 440., 4.440, 460., 4.649, 480., 4.857, 500., 5.066)
	AD8494 = pairs(-180, -0.714, -160, -0.658, -140, -0.594, -120, -0.523, -100, -0.446, -80, -0.365,
		-60, -0.278, -40, -0.188, -20, -0.095, 0, 0.002, 20, 0.1, 25, 0.125, 40, 0.201, 60, 0.303, 80, 0.406,
		100, 0.511, 120, 0.617, 140, 0.723, 160, 0.829, 180, 0.937, 200, 1.044, 220, 1.151, 240, 1.259,
		260, 1.366, 280, 1.473, 300, 1.58, 320, 1.687, 340, 1.794, 360, 1.901, 380, 2.008, 400, 2.114,
		420, 2.221, 440, 2.328, 460, 2.435, 480, 2.542, 500, 2.65, 520, 2.759, 540, 2.868, 560, 2.979,
		580, 3.09, 600, 3.203, 620, 3.316, 640, 3.431, 660, 3.548, 680, 3.666, 700, 3.786, 720, 3.906,
		740, 4.029, 760, 4.152, 780, 4.276, 800, 4.401, 820, 4.526, 840, 4.65, 860, 4.774, 880, 4.897,
		900, 5.018, 920, 5.138, 940, 5.257, 960, 5.374, 980, 5.49, 1000, 5.606, 1020, 5.72, 1040, 5.833,
		1060, 5.946, 1080, 6.058, 1100, 6.17, 1120, 6.282, 1140, 6.394, 1160, 6.505, 1180, 6.616, 1200, 6.727)
	AD8495 = pairs(-260, -0.786, -240, -0.774, -220, -0.751, -200, -0.719, -180, -0.677, -160, -0.627,
		-140, -0.569, -120, -0.504, -100, -0.432, -80, -0.355, -60, -0.272, -40, -0.184, -20, -0.093,
		0, 0.003, 20, 0.1, 25, 0.125, 40, 0.2, 60, 0.301, 80, 0.402, 100, 0.504, 120, 0.605, 140, 0.705,
		160, 0.803, 180, 0.901, 200, 0.999, 220, 1.097, 240, 1.196, 260, 1.295, 280, 1.396, 300, 1.497,
		320, 1.599, 340, 1.701, 360, 1.803, 380, 1.906, 400, 2.01, 420, 2.113, 440, 2.217, 460, 2.321,
		480, 2.425, 500, 2.529, 520, 2.634, 540, 2.738, 560, 2.843, 580, 2.947, 600, 3.051, 620, 3.155,
		640, 3.259, 660, 3.362, 680, 3.465, 700, 3.568, 720, 3.67, 740, 3.772, 760, 3.874, 780, 3.975,
		800, 4.076, 820, 4.176, 840, 4.275, 860, 4.374, 880, 4.473, 900, 4.571, 920, 4.669, 940, 4.766,
		960, 4.863, 980, 4.959, 1000, 5.055, 1020, 5.15, 1040, 5.245, 1060, 5.339, 1080, 5.432, 1100, 5.525,
		1120, 5.617, 1140, 5.709, 1160, 5.8, 1180, 5.891, 1200, 5.98, 1220, 6.069, 1240, 6.158, 1260, 6.245,
		1280, 6.332, 1300, 6.418, 1320, 6.503, 1340, 6.587, 1360, 6.671, 1380, 6.754)
	AD8496 = pairs(-180, -0.642, -160, -0.59, -140, -0.53, -120, -0.464, -100, -0.392, -80, -0.315,
		-60, -0.235, -40, -0.15, -20, -0.063, 0, 0.027, 20, 0.119, 25, 0.142, 40, 0.213, 60, 0.308,
		80, 0.405, 100, 0.503, 120, 0.601, 140, 0.701, 160, 0.8, 180, 0.9, 200, 1.001, 220, 1.101,
		240, 1.201, 260, 1.302, 280, 1.402, 300, 1.502, 320, 1.602, 340, 1.702, 360, 1.801, 380, 1.901,
		400, 2.001, 420, 2.1, 440, 2.2, 460, 2.3, 480, 2.401, 500, 2.502, 520, 2.603, 540, 2.705,
		560, 2.808, 580, 2.912, 600, 3.017, 620, 3.124, 640, 3.231, 660, 3.34, 680, 3.451, 700, 3.562,
		720, 3.675, 740, 3.789, 760, 3.904, 780, 4.02, 800, 4.137, 820, 4.254, 840, 4.37, 860, 4.486,
		880, 4.6, 900, 4.714, 920, 4.826, 940, 4.937, 960, 5.047, 980, 5.155, 1000, 5.263, 1020, 5.369,
		1040, 5.475, 1060, 5.581, 1080, 5.686, 1100, 5.79, 1120, 5.895, 1140, 5.999, 1160, 6.103,
		1180, 6.207, 1200, 6.311)
	AD8497 = pairs(-260, -0.785, -240, -0.773, -220, -0.751, -200, -0.718, -180, -0.676, -160, -0.626,
		-140, -0.568, -120, -0.503, -100, -0.432, -80, -0.354, -60, -0.271, -40, -0.184, -20, -0.092,
		0, 0.003, 20, 0.101, 25, 0.126, 40, 0.2, 60, 0.301, 80, 0.403, 100, 0.505, 120, 0.605, 140, 0.705,
		160, 0.804, 180, 0.902, 200, 0.999, 220, 1.097, 240, 1.196, 260, 1.296, 280, 1.396, 300, 1.498,
		320, 1.599, 340, 1.701, 360, 1.804, 380, 1.907, 400, 2.01, 420, 2.114, 440, 2.218, 460, 2.322,
		480, 2.426, 500, 2.53, 520, 2.634, 540, 2.739, 560, 2.843, 580, 2.948, 600, 3.052, 620, 3.156,
		640, 3.259, 660, 3.363, 680, 3.466, 700, 3.569, 720, 3.671, 740, 3.773, 760, 3.874, 780, 3.976,
		800, 4.076, 820, 4.176, 840, 4.276, 860, 4.375, 880, 4.474, 900, 4.572, 920, 4.67, 940, 4.767,
		960, 4.863, 980, 4.96, 1000, 5.055, 1020, 5.151, 1040, 5.245, 1060, 5.339, 1080, 5.433, 1100, 5.526,
		1120, 5.618, 1140, 5.71, 1160, 5.801, 1180, 5.891, 1200, 5.981, 1220, 6.07, 1240, 6.158, 1260, 6.246,
		1280, 6.332, 1300, 6.418, 1320, 6.503, 1340, 6.588, 1360, 6.671, 1380, 6.754)
)

// calcPT100 calculates the PT100/PT1000 resistances with the Callendar-Van Dusen formula.
func calcPT100(base float64) []TempValue {
	const a, b = 3.9083e-3, -5.775e-7
	var out []TempValue
	for t := 0; t < 500; t += 10 {
		ft := float64(t)
		out = append(out, TempValue{ft, base * (1. + a*ft + b*ft*ft)})
	}
	return out
}

// calcINA826PT100 is the standard circuit: a 4400 ohm pullup with 10x gain to 5V.
func calcINA826PT100() []TempValue {
	var out []TempValue
	for _, p := range calcPT100(100.) {
		out = append(out, TempValue{p.Temp, 10. * 5. * p.Value / (4400. + p.Value)})
	}
	return out
}

type namedTable struct {
	name   string
	params []TempValue
}

var (
	defaultVoltageSensors = []namedTable{{"AD595", AD595}, {"AD597", AD597}, {"AD8494", AD8494},
		{"AD8495", AD8495}, {"AD8496", AD8496}, {"AD8497", AD8497}, {"PT100 INA826", calcINA826PT100()}}
	defaultResistanceSensors = []namedTable{{"PT1000", calcPT100(1000.)}}
)

func load(config *configfile.Wrapper) any {
	// Register default sensors
	pheaters := heaters.Load(config)
	for _, s := range defaultVoltageSensors {
		params := s.params
		pheaters.AddSensorFactory(s.name, func(config *configfile.Wrapper) heaters.Sensor {
			return NewPrinterADCtoTemperature(config, NewLinearVoltage(config, params))
		})
	}
	for _, s := range defaultResistanceSensors {
		params := s.params
		pheaters.AddSensorFactory(s.name, func(config *configfile.Wrapper) heaters.Sensor {
			return NewPrinterADCtoTemperature(config, NewLinearResistance(config, params))
		})
	}
	return nil
}

func loadPrefix(config *configfile.Wrapper) any {
	name := customName(config)
	pheaters := heaters.Load(config)
	if _, ok := config.GetOptional("resistance1", configfile.Default(nil)); !ok {
		params := readParams(config, "voltage")
		pheaters.AddSensorFactory(name, func(config *configfile.Wrapper) heaters.Sensor {
			return NewPrinterADCtoTemperature(config, NewLinearVoltage(config, params))
		})
	} else {
		samples := readParams(config, "resistance")
		pheaters.AddSensorFactory(name, func(config *configfile.Wrapper) heaters.Sensor {
			return NewPrinterADCtoTemperature(config, NewLinearResistance(config, samples))
		})
	}
	return nil
}

func init() { host.RegisterModule("adc_temperature", load, loadPrefix) }
