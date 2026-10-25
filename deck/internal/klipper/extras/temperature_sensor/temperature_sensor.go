// Package temperature_sensor is a [temperature_sensor] section: a sensor with no heater.
//
// Ported to Go from klippy/extras/temperature_sensor.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2019  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package temperature_sensor

import (
	"fmt"
	"math"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/heaters"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mathutil"
)

const KelvinToCelsius = -273.15

// PrinterSensorGeneric is a [temperature_sensor] section.
type PrinterSensorGeneric struct {
	printer                  *host.Printer
	name                     string
	sensor                   heaters.Sensor
	minTemp, maxTemp         float64
	lastTemp                 float64
	measuredMin, measuredMax float64
}

func loadPrefix(config *configfile.Wrapper) any {
	parts := strings.Fields(config.Name())
	s := &PrinterSensorGeneric{printer: host.Of(config), name: parts[len(parts)-1]}
	pheaters := heaters.Load(config)
	s.sensor = pheaters.SetupSensor(config)
	s.minTemp = config.GetFloat("min_temp", configfile.Default(KelvinToCelsius), configfile.Min(KelvinToCelsius))
	s.maxTemp = config.GetFloat("max_temp", configfile.Default(99999999.9), configfile.Above(s.minTemp))
	s.sensor.SetupMinMax(s.minTemp, s.maxTemp)
	s.sensor.SetupCallback(s.temperatureCallback)
	pheaters.RegisterSensor(config, s, "")
	s.lastTemp = 0.
	s.measuredMin = 99999999.
	s.measuredMax = 0.
	return s
}

func init() { host.RegisterModule("temperature_sensor", nil, loadPrefix) }

func (s *PrinterSensorGeneric) temperatureCallback(readTime, temp float64) {
	s.lastTemp = temp
	if temp != 0 {
		s.measuredMin = math.Min(s.measuredMin, temp)
		s.measuredMax = math.Max(s.measuredMax, temp)
	}
}

func (s *PrinterSensorGeneric) GetTemp(eventtime float64) (float64, float64) { return s.lastTemp, 0. }

func (s *PrinterSensorGeneric) Stats(eventtime float64) (bool, string) {
	return false, fmt.Sprintf("%s: temp=%.1f", s.name, s.lastTemp)
}

func (s *PrinterSensorGeneric) Status(eventtime float64) map[string]any {
	return map[string]any{
		"temperature":       mathutil.Round(s.lastTemp, 2),
		"measured_min_temp": mathutil.Round(s.measuredMin, 2),
		"measured_max_temp": mathutil.Round(s.measuredMax, 2),
	}
}
