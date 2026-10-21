// Package pid_calibrate finds the PID settings of a heater (PID_CALIBRATE).
//
// Ported to Go from klippy/extras/pid_calibrate.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2016-2018  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package pid_calibrate

import (
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/heaters"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
)

// PIDCalibrate has the PID_CALIBRATE command.
type PIDCalibrate struct {
	printer *host.Printer
}

func load(config *configfile.Wrapper) any {
	pc := &PIDCalibrate{printer: host.Of(config)}
	gcode.Of(pc.printer).RegisterCommand("PID_CALIBRATE", pc.cmdPIDCalibrate, false, "Run PID calibration test")
	return pc
}

func init() { host.RegisterModule("pid_calibrate", load, nil) }

// toolhead is the part of the toolhead that PID_CALIBRATE uses.
type toolhead interface{ LastMoveTime() float64 }

func lookupHeater(pheaters *heaters.PrinterHeaters, name string) (h *heaters.Heater) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*configfile.Error); ok {
				panic(gcode.Errorf("%s", e.Msg))
			}
			panic(r)
		}
	}()
	return pheaters.LookupHeater(name)
}

func (pc *PIDCalibrate) cmdPIDCalibrate(gcmd *gcode.Command) {
	heaterName := gcmd.GetString("HEATER")
	target := gcmd.GetFloat("TARGET")
	writeFile := gcmd.GetInt("WRITE_FILE", gcode.Default(0))
	pheaters := heaters.Of(pc.printer)
	heater := lookupHeater(pheaters, heaterName)
	pc.printer.LookupObject("toolhead").(toolhead).LastMoveTime()
	calibrate := NewControlAutoTune(heater, target)
	oldControl := heater.SetControl(calibrate)
	func() {
		defer func() {
			if r := recover(); r != nil {
				heater.SetControl(oldControl)
				panic(r)
			}
		}()
		pheaters.SetTemperature(heater, target, true)
	}()
	heater.SetControl(oldControl)
	if writeFile != 0 {
		calibrate.WriteFile("/tmp/heattest.txt")
	}
	if calibrate.CheckBusy(0., 0., 0.) {
		panic(gcmd.Error("pid_calibrate interrupted"))
	}
	// Log and report results
	kp, ki, kd := calibrate.CalcFinalPID()
	log.Printf("Autotune: final: Kp=%f Ki=%f Kd=%f", kp, ki, kd)
	gcmd.RespondInfo(fmt.Sprintf("PID parameters: pid_Kp=%.3f pid_Ki=%.3f pid_Kd=%.3f\n"+
		"The SAVE_CONFIG command will update the printer config file\n"+
		"with these parameters and restart the printer.", kp, ki, kd), true)
	// Store results for SAVE_CONFIG
	cfgname := heater.Name()
	configfile := pc.printer.LookupObject("configfile").(*configfile.PrinterConfig)
	configfile.Set(cfgname, "control", "pid")
	configfile.Set(cfgname, "pid_Kp", fmt.Sprintf("%.3f", kp))
	configfile.Set(cfgname, "pid_Ki", fmt.Sprintf("%.3f", ki))
	configfile.Set(cfgname, "pid_Kd", fmt.Sprintf("%.3f", kd))
}

const tunePIDDelta = 5.0

// heater is the part of a heater that the auto tune uses.
type heater interface {
	MaxPower() float64
	PWMDelay() float64
	SetPWM(readTime, value float64)
	AlterTarget(targetTemp float64)
}

type peak struct{ temp, time float64 }

type sample struct{ time, value float64 }

// ControlAutoTune turns the heater fully on and off around the target, and finds the PID
// settings from the peaks of the temperature.
type ControlAutoTune struct {
	heater         heater
	heaterMaxPower float64
	calibrateTemp  float64
	// Heating control
	heating  bool
	peak     float64
	peakTime float64
	// Peak recording
	peaks []peak
	// Sample recording
	lastPWM     float64
	pwmSamples  []sample
	tempSamples []sample
}

// NewControlAutoTune makes the auto tune of a heater for a target temperature.
func NewControlAutoTune(h heater, target float64) *ControlAutoTune {
	return &ControlAutoTune{heater: h, heaterMaxPower: h.MaxPower(), calibrateTemp: target}
}

// Heater control

func (c *ControlAutoTune) setPWM(readTime, value float64) {
	if value != c.lastPWM {
		c.pwmSamples = append(c.pwmSamples, sample{readTime + c.heater.PWMDelay(), value})
		c.lastPWM = value
	}
	c.heater.SetPWM(readTime, value)
}

// TemperatureUpdate switches the heater at each crossing of the target, and records the peaks.
func (c *ControlAutoTune) TemperatureUpdate(readTime, temp, targetTemp float64) {
	c.tempSamples = append(c.tempSamples, sample{readTime, temp})
	// Check if the temperature has crossed the target and
	// enable/disable the heater if so.
	if c.heating && temp >= targetTemp {
		c.heating = false
		c.checkPeaks()
		c.heater.AlterTarget(c.calibrateTemp - tunePIDDelta)
	} else if !c.heating && temp <= targetTemp {
		c.heating = true
		c.checkPeaks()
		c.heater.AlterTarget(c.calibrateTemp)
	}
	// Check if this temperature is a peak and record it if so
	if c.heating {
		c.setPWM(readTime, c.heaterMaxPower)
		if temp < c.peak {
			c.peak = temp
			c.peakTime = readTime
		}
	} else {
		c.setPWM(readTime, 0.)
		if temp > c.peak {
			c.peak = temp
			c.peakTime = readTime
		}
	}
}

// CheckBusy is true until the test has 12 peaks.
func (c *ControlAutoTune) CheckBusy(eventtime, smoothedTemp, targetTemp float64) bool {
	return c.heating || len(c.peaks) < 12
}

// Analysis

func (c *ControlAutoTune) checkPeaks() {
	c.peaks = append(c.peaks, peak{c.peak, c.peakTime})
	if c.heating {
		c.peak = 9999999.
	} else {
		c.peak = -9999999.
	}
	if len(c.peaks) < 4 {
		return
	}
	c.calcPID(len(c.peaks) - 1)
}

func (c *ControlAutoTune) calcPID(pos int) (kp, ki, kd float64) {
	tempDiff := c.peaks[pos].temp - c.peaks[pos-1].temp
	timeDiff := c.peaks[pos].time - c.peaks[pos-2].time
	// Use Astrom-Hagglund method to estimate Ku and Tu
	amplitude := .5 * math.Abs(tempDiff)
	ku := 4. * c.heaterMaxPower / (math.Pi * amplitude)
	tu := timeDiff
	// Use Ziegler-Nichols method to generate PID parameters
	ti := 0.5 * tu
	td := 0.125 * tu
	kp = 0.6 * ku * heaters.PIDParamBase
	ki = kp / ti
	kd = kp * td
	log.Printf("Autotune: raw=%f/%f Ku=%f Tu=%f  Kp=%f Ki=%f Kd=%f",
		tempDiff, c.heaterMaxPower, ku, tu, kp, ki, kd)
	return kp, ki, kd
}

// CalcFinalPID returns the PID settings of the cycle with the median time.
func (c *ControlAutoTune) CalcFinalPID() (kp, ki, kd float64) {
	type cycle struct {
		time float64
		pos  int
	}
	var cycleTimes []cycle
	for pos := 4; pos < len(c.peaks); pos++ {
		cycleTimes = append(cycleTimes, cycle{c.peaks[pos].time - c.peaks[pos-2].time, pos})
	}
	sort.Slice(cycleTimes, func(i, j int) bool {
		if cycleTimes[i].time != cycleTimes[j].time {
			return cycleTimes[i].time < cycleTimes[j].time
		}
		return cycleTimes[i].pos < cycleTimes[j].pos
	})
	return c.calcPID(cycleTimes[len(cycleTimes)/2].pos)
}

// Offline analysis helper

// WriteFile writes the samples of the test to a file.
func (c *ControlAutoTune) WriteFile(filename string) {
	var lines []string
	for _, s := range c.pwmSamples {
		lines = append(lines, fmt.Sprintf("pwm: %.3f %.3f", s.time, s.value))
	}
	for _, s := range c.tempSamples {
		lines = append(lines, fmt.Sprintf("%.3f %.3f", s.time, s.value))
	}
	os.WriteFile(filename, []byte(strings.Join(lines, "\n")), 0o644)
}
