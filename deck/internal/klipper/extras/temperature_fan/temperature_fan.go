// Package temperature_fan is a fan that runs to hold a sensor at a target temperature.
//
// Ported to Go from klippy/extras/temperature_fan.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2016-2020  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package temperature_fan

import (
	"math"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/fan"
	"printdeck/internal/klipper/extras/heaters"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mathutil"
)

const (
	KelvinToCelsius = -273.15
	MaxFanTime      = 5.0
	AmbientTemp     = 25.
	PIDParamBase    = 255.
)

type control interface {
	temperatureCallback(readTime, temp float64)
}

// tempFan is the temperature fan as its control algorithm uses it.
type tempFan interface {
	GetTemp(eventtime float64) (float64, float64)
	getMaxSpeed() float64
	getMinSpeed() float64
	setTFSpeed(readTime, value float64)
}

// TemperatureFan is a [temperature_fan] section.
type TemperatureFan struct {
	name                       string
	printer                    *host.Printer
	fan                        *fan.Fan
	minTemp, maxTemp           float64
	sensor                     heaters.Sensor
	speedDelay                 float64
	maxSpeedConf, maxSpeed     float64
	minSpeedConf, minSpeed     float64
	lastTemp, lastTempTime     float64
	targetTempConf, targetTemp float64
	control                    control
	nextSpeedTime              float64
	lastSpeedValue             float64
}

func loadPrefix(config *configfile.Wrapper) any {
	t := &TemperatureFan{name: strings.Fields(config.Name())[1], printer: host.Of(config)}
	t.fan = fan.NewFan(config, 1.)
	t.minTemp = config.GetFloat("min_temp", configfile.Min(KelvinToCelsius))
	t.maxTemp = config.GetFloat("max_temp", configfile.Above(t.minTemp))
	pheaters := heaters.Load(config)
	t.sensor = pheaters.SetupSensor(config)
	t.sensor.SetupMinMax(t.minTemp, t.maxTemp)
	t.sensor.SetupCallback(t.temperatureCallback)
	pheaters.RegisterSensor(config, t, "")
	t.speedDelay = t.sensor.ReportTimeDelta()
	t.maxSpeedConf = config.GetFloat("max_speed", configfile.Default(1.), configfile.Above(0.), configfile.Max(1.))
	t.maxSpeed = t.maxSpeedConf
	t.minSpeedConf = config.GetFloat("min_speed", configfile.Default(0.3), configfile.Min(0.), configfile.Max(1.))
	t.minSpeed = t.minSpeedConf
	t.lastTemp = 0.
	t.lastTempTime = 0.
	defTarget := t.maxTemp
	if t.maxTemp > 40. {
		defTarget = 40.
	}
	t.targetTempConf = config.GetFloat("target_temp", configfile.Default(defTarget), configfile.Min(t.minTemp),
		configfile.Max(t.maxTemp))
	t.targetTemp = t.targetTempConf
	algos := map[string]any{"watermark": "watermark", "pid": "pid"}
	switch config.GetChoice("control", algos).(string) {
	case "watermark":
		t.control = newControlBangBang(t, config)
	case "pid":
		t.control = newControlPID(t, config)
	}
	t.nextSpeedTime = 0.
	t.lastSpeedValue = 0.
	g := gcode.Of(t.printer)
	g.RegisterMuxCommand("SET_TEMPERATURE_FAN_TARGET", "TEMPERATURE_FAN", &t.name,
		t.cmdSetTemperatureFanTarget, "Sets a temperature fan target and fan speed limits")
	return t
}

func init() { host.RegisterModule("temperature_fan", nil, loadPrefix) }

func (t *TemperatureFan) setTFSpeed(readTime, value float64) {
	if value <= 0. {
		value = 0.
	} else if value < t.minSpeed {
		value = t.minSpeed
	}
	if t.targetTemp <= 0. {
		value = 0.
	}
	if (readTime < t.nextSpeedTime || t.lastSpeedValue == 0) && math.Abs(value-t.lastSpeedValue) < 0.05 {
		// No significant change in value - can suppress update
		return
	}
	speedTime := readTime + t.speedDelay
	t.nextSpeedTime = speedTime + 0.75*MaxFanTime
	t.lastSpeedValue = value
	t.fan.SetSpeed(value, speedTime)
}

func (t *TemperatureFan) temperatureCallback(readTime, temp float64) {
	t.lastTemp = temp
	t.control.temperatureCallback(readTime, temp)
}

func (t *TemperatureFan) GetTemp(eventtime float64) (float64, float64) {
	return t.lastTemp, t.targetTemp
}

func (t *TemperatureFan) getMinSpeed() float64 { return t.minSpeed }
func (t *TemperatureFan) getMaxSpeed() float64 { return t.maxSpeed }

func (t *TemperatureFan) Status(eventtime float64) map[string]any {
	status := t.fan.Status(eventtime)
	status["temperature"] = mathutil.Round(t.lastTemp, 2)
	status["target"] = t.targetTemp
	return status
}

func (t *TemperatureFan) cmdSetTemperatureFanTarget(gcmd *gcode.Command) {
	temp := gcmd.GetFloat("TARGET", gcode.Default(t.targetTempConf))
	t.setTemp(temp)
	minSpeed := gcmd.GetFloat("MIN_SPEED", gcode.Default(t.minSpeed))
	maxSpeed := gcmd.GetFloat("MAX_SPEED", gcode.Default(t.maxSpeed))
	if minSpeed > maxSpeed {
		panic(gcode.Errorf("Requested min speed (%.1f) is greater than max speed (%.1f)", minSpeed, maxSpeed))
	}
	t.setMinSpeed(minSpeed)
	t.setMaxSpeed(maxSpeed)
}

func (t *TemperatureFan) setTemp(degrees float64) {
	if degrees != 0 && (degrees < t.minTemp || degrees > t.maxTemp) {
		panic(gcode.Errorf("Requested temperature (%.1f) out of range (%.1f:%.1f)", degrees, t.minTemp, t.maxTemp))
	}
	t.targetTemp = degrees
}

func (t *TemperatureFan) setMinSpeed(speed float64) {
	if speed != 0 && (speed < 0. || speed > 1.) {
		panic(gcode.Errorf("Requested min speed (%.1f) out of range (0.0 : 1.0)", speed))
	}
	t.minSpeed = speed
}

func (t *TemperatureFan) setMaxSpeed(speed float64) {
	if speed != 0 && (speed < 0. || speed > 1.) {
		panic(gcode.Errorf("Requested max speed (%.1f) out of range (0.0 : 1.0)", speed))
	}
	t.maxSpeed = speed
}

// ######################################################################
// Bang-bang control algo
// ######################################################################

type controlBangBang struct {
	temperatureFan tempFan
	maxDelta       float64
	heating        bool
}

func newControlBangBang(tf tempFan, config *configfile.Wrapper) *controlBangBang {
	return &controlBangBang{temperatureFan: tf,
		maxDelta: config.GetFloat("max_delta", configfile.Default(2.0), configfile.Above(0.))}
}

func (c *controlBangBang) temperatureCallback(readTime, temp float64) {
	_, targetTemp := c.temperatureFan.GetTemp(readTime)
	if c.heating && temp >= targetTemp+c.maxDelta {
		c.heating = false
	} else if !c.heating && temp <= targetTemp-c.maxDelta {
		c.heating = true
	}
	if c.heating {
		c.temperatureFan.setTFSpeed(readTime, 0.)
	} else {
		c.temperatureFan.setTFSpeed(readTime, c.temperatureFan.getMaxSpeed())
	}
}

// ######################################################################
// Proportional Integral Derivative (PID) control algo
// ######################################################################

type controlPID struct {
	temperatureFan tempFan
	kp, ki, kd     float64
	minDerivTime   float64
	tempIntegMax   float64
	prevTemp       float64
	prevTempTime   float64
	prevTempDeriv  float64
	prevTempInteg  float64
}

func newControlPID(tf tempFan, config *configfile.Wrapper) *controlPID {
	c := &controlPID{temperatureFan: tf}
	c.kp = config.GetFloat("pid_Kp") / PIDParamBase
	c.ki = config.GetFloat("pid_Ki") / PIDParamBase
	c.kd = config.GetFloat("pid_Kd") / PIDParamBase
	c.minDerivTime = config.GetFloat("pid_deriv_time", configfile.Default(2.), configfile.Above(0.))
	c.tempIntegMax = 0.
	if c.ki != 0 {
		c.tempIntegMax = tf.getMaxSpeed() / c.ki
	}
	c.prevTemp = AmbientTemp
	return c
}

func (c *controlPID) temperatureCallback(readTime, temp float64) {
	_, targetTemp := c.temperatureFan.GetTemp(readTime)
	timeDiff := readTime - c.prevTempTime
	// Calculate change of temperature
	tempDiff := temp - c.prevTemp
	var tempDeriv float64
	if timeDiff >= c.minDerivTime {
		tempDeriv = tempDiff / timeDiff
	} else {
		tempDeriv = (c.prevTempDeriv*(c.minDerivTime-timeDiff) + tempDiff) / c.minDerivTime
	}
	// Calculate accumulated temperature "error"
	tempErr := targetTemp - temp
	tempInteg := c.prevTempInteg + tempErr*timeDiff
	tempInteg = math.Max(0., math.Min(c.tempIntegMax, tempInteg))
	// Calculate output
	co := c.kp*tempErr + c.ki*tempInteg - c.kd*tempDeriv
	boundedCO := math.Max(0., math.Min(c.temperatureFan.getMaxSpeed(), co))
	c.temperatureFan.setTFSpeed(readTime, math.Max(c.temperatureFan.getMinSpeed(), c.temperatureFan.getMaxSpeed()-boundedCO))
	// Store state for next measurement
	c.prevTemp = temp
	c.prevTempTime = readTime
	c.prevTempDeriv = tempDeriv
	if co == boundedCO {
		c.prevTempInteg = tempInteg
	}
}
