// Package thermistor reads temperatures from thermistors, with the Steinhart-Hart equation or a
// beta value.
//
// Ported to Go from klippy/extras/thermistor.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2016-2019  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package thermistor

import (
	"log"
	"math"
	"sort"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/adc_temperature"
	"printdeck/internal/klipper/extras/heaters"
	"printdeck/internal/klipper/host"
)

const kelvinToCelsius = -273.15

// Thermistor converts analog voltages to temperatures.
type Thermistor struct {
	pullup, inlineResistor float64
	c1, c2, c3             float64
}

func NewThermistor(pullup, inlineResistor float64) *Thermistor {
	return &Thermistor{pullup: pullup, inlineResistor: inlineResistor}
}

// SetupCoefficients calculates the Steinhart-Hart coefficients from three temperature
// measurements. It uses a beta value when the third coefficient is not above 0.
func (t *Thermistor) SetupCoefficients(t1, r1, t2, r2, t3, r3 float64, name string) {
	// Arrange samples as 3 linear equations and solve for c1, c2, and c3.
	invT1 := 1. / (t1 - kelvinToCelsius)
	invT2 := 1. / (t2 - kelvinToCelsius)
	invT3 := 1. / (t3 - kelvinToCelsius)
	lnR1, lnR2, lnR3 := math.Log(r1), math.Log(r2), math.Log(r3)
	ln3R1, ln3R2, ln3R3 := math.Pow(lnR1, 3), math.Pow(lnR2, 3), math.Pow(lnR3, 3)

	invT12, invT13 := invT1-invT2, invT1-invT3
	lnR12, lnR13 := lnR1-lnR2, lnR1-lnR3
	ln3R12, ln3R13 := ln3R1-ln3R2, ln3R1-ln3R3

	t.c3 = (invT12 - invT13*lnR12/lnR13) / (ln3R12 - ln3R13*lnR12/lnR13)
	if t.c3 <= 0. {
		beta := lnR13 / invT13
		log.Printf("Using thermistor beta %.3f in heater %s", beta, name)
		t.SetupCoefficientsBeta(t1, r1, beta)
		return
	}
	t.c2 = (invT12 - t.c3*ln3R12) / lnR12
	t.c1 = invT1 - t.c2*lnR1 - t.c3*ln3R1
}

// SetupCoefficientsBeta calculates the equivalent Steinhart-Hart coefficients from a beta value.
func (t *Thermistor) SetupCoefficientsBeta(t1, r1, beta float64) {
	invT1 := 1. / (t1 - kelvinToCelsius)
	lnR1 := math.Log(r1)
	t.c3 = 0.
	t.c2 = 1. / beta
	t.c1 = invT1 - t.c2*lnR1
}

// CalcTemp calculates the temperature of an adc value.
func (t *Thermistor) CalcTemp(adc float64) float64 {
	adc = math.Max(.00001, math.Min(.99999, adc))
	r := t.pullup * adc / (1.0 - adc)
	lnR := math.Log(r - t.inlineResistor)
	invT := t.c1 + t.c2*lnR + t.c3*math.Pow(lnR, 3)
	return 1.0/invT + kelvinToCelsius
}

// CalcADC calculates the adc value of a temperature.
func (t *Thermistor) CalcADC(temp float64) float64 {
	if temp <= kelvinToCelsius {
		return 1.
	}
	invT := 1. / (temp - kelvinToCelsius)
	var lnR float64
	if t.c3 != 0 {
		// Solve for ln_r using Cardano's formula
		y := (t.c1 - invT) / (2. * t.c3)
		x := math.Sqrt(math.Pow(t.c2/(3.*t.c3), 3) + y*y)
		lnR = pyPow(x-y, 1./3.) - pyPow(x+y, 1./3.)
	} else {
		lnR = (invT - t.c1) / t.c2
	}
	r := math.Exp(lnR) + t.inlineResistor
	return r / (t.pullup + r)
}

// pyPow is math.pow of Python: a negative base with a fraction exponent is an error.
func pyPow(x, y float64) float64 {
	if x < 0 && y != math.Trunc(y) {
		panic("math domain error")
	}
	return math.Pow(x, y)
}

// Params are the measurements of a thermistor: three temperatures and resistances, or one and a
// beta value.
type Params struct {
	T1, R1, T2, R2, T3, R3 float64
	Beta                   float64
	HasBeta                bool
}

// NewPrinterThermistor makes the sensor of a thermistor on the sensor_pin of a config section.
func NewPrinterThermistor(config *configfile.Wrapper, p Params) *adc_temperature.PrinterADCtoTemperature {
	pullup := config.GetFloat("pullup_resistor", configfile.Default(4700.), configfile.Above(0.))
	inlineResistor := config.GetFloat("inline_resistor", configfile.Default(0.), configfile.Min(0.))
	t := NewThermistor(pullup, inlineResistor)
	if p.HasBeta {
		t.SetupCoefficientsBeta(p.T1, p.R1, p.Beta)
	} else {
		t.SetupCoefficients(p.T1, p.R1, p.T2, p.R2, p.T3, p.R3, config.Name())
	}
	return adc_temperature.NewPrinterADCtoTemperature(config, t)
}

// customThermistor is a thermistor of a [thermistor name] section.
type customThermistor struct {
	name   string
	params Params
}

func newCustomThermistor(config *configfile.Wrapper) *customThermistor {
	c := &customThermistor{name: strings.Join(strings.Fields(config.Name())[1:], " ")}
	t1 := config.GetFloat("temperature1", configfile.Min(kelvinToCelsius))
	r1 := config.GetFloat("resistance1", configfile.Min(0.))
	if beta, ok := config.GetFloatOptional("beta", configfile.Default(nil), configfile.Above(0.)); ok {
		c.params = Params{T1: t1, R1: r1, Beta: beta, HasBeta: true}
		return c
	}
	t2 := config.GetFloat("temperature2", configfile.Min(kelvinToCelsius))
	r2 := config.GetFloat("resistance2", configfile.Min(0.))
	t3 := config.GetFloat("temperature3", configfile.Min(kelvinToCelsius))
	r3 := config.GetFloat("resistance3", configfile.Min(0.))
	s := [][2]float64{{t1, r1}, {t2, r2}, {t3, r3}}
	sort.Slice(s, func(i, j int) bool {
		if s[i][0] != s[j][0] {
			return s[i][0] < s[j][0]
		}
		return s[i][1] < s[j][1]
	})
	c.params = Params{T1: s[0][0], R1: s[0][1], T2: s[1][0], R2: s[1][1], T3: s[2][0], R3: s[2][1]}
	return c
}

func (c *customThermistor) create(config *configfile.Wrapper) heaters.Sensor {
	return NewPrinterThermistor(config, c.params)
}

func loadPrefix(config *configfile.Wrapper) any {
	t := newCustomThermistor(config)
	heaters.Load(config).AddSensorFactory(t.name, t.create)
	return nil
}

func init() { host.RegisterModule("thermistor", nil, loadPrefix) }
