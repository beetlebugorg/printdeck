// Package heaters controls the pwm heaters (the extruder and the bed) with their temperature
// sensors, and has the commands of the heaters and sensors.
//
// Ported to Go from klippy/extras/heaters.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f. temperature_sensors.cfg is a copy of the file
// of Klipper. Its sections for the sensor modules that are not ported have no effect.
//
//	Copyright (C) 2016-2025  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package heaters

import (
	_ "embed"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
)

//go:embed temperature_sensors.cfg
var temperatureSensorsCfg string

const (
	KelvinToCelsius   = -273.15
	MaxHeatTime       = 3.0
	AmbientTemp       = 25.
	PIDParamBase      = 255.
	maxMainthreadTime = 5.0
	quellStaleTime    = 7.0
	minPWMChangeRatio = 0.05
	pidSettleDelta    = 1.
	pidSettleSlope    = .1
)

// Sensor is a temperature sensor.
type Sensor interface {
	SetupMinMax(minTemp, maxTemp float64)
	SetupCallback(cb func(readTime, temp float64))
	ReportTimeDelta() float64
}

// TempSensor is an object with a temperature, for TEMPERATURE_WAIT and M105.
type TempSensor interface {
	GetTemp(eventtime float64) (float64, float64)
}

// SensorFactory makes a sensor of a type from its config section.
type SensorFactory func(config *configfile.Wrapper) Sensor

// Control is a temperature control algorithm.
type Control interface {
	TemperatureUpdate(readTime, temp, targetTemp float64)
	CheckBusy(eventtime, smoothedTemp, targetTemp float64) bool
}

// toolhead is the part of the toolhead that the heaters use.
type toolhead interface {
	RegisterLookaheadCallback(cb func(printTime float64))
	LastMoveTime() float64
}

// Heater is a pwm heater.
type Heater struct {
	printer              *host.Printer
	name                 string
	shortName            string
	sensor               Sensor
	minTemp, maxTemp     float64
	pwmDelay             float64
	minExtrudeTemp       float64
	canExtrude           bool
	maxPower             float64
	minPWMChange         float64
	smoothTime           float64
	invSmoothTime        float64
	verifyMainthreadTime float64
	mu                   sync.Mutex
	lastTemp             float64
	smoothedTemp         float64
	targetTemp           float64
	lastTempTime         float64
	// pwm caching
	nextPWMTime  float64
	lastPWMValue float64
	control      Control
	mcuPWM       *mcu.PWM
}

func newHeater(config *configfile.Wrapper, sensor Sensor) *Heater {
	printer := host.Of(config)
	name := config.Name()
	f := strings.Fields(name)
	h := &Heater{printer: printer, name: name, shortName: f[len(f)-1], sensor: sensor}
	// Setup sensor
	h.minTemp = config.GetFloat("min_temp", configfile.Min(KelvinToCelsius))
	h.maxTemp = config.GetFloat("max_temp", configfile.Above(h.minTemp))
	h.sensor.SetupMinMax(h.minTemp, h.maxTemp)
	h.sensor.SetupCallback(h.temperatureCallback)
	h.pwmDelay = h.sensor.ReportTimeDelta()
	// Setup temperature checks
	h.minExtrudeTemp = config.GetFloat("min_extrude_temp", configfile.Default(170.),
		configfile.Min(h.minTemp), configfile.Max(h.maxTemp))
	_, isFileoutput := printer.StartArgs().Get("debugoutput")
	h.canExtrude = h.minExtrudeTemp <= 0. || isFileoutput
	h.maxPower = config.GetFloat("max_power", configfile.Default(1.), configfile.Above(0.), configfile.Max(1.))
	h.minPWMChange = h.maxPower * minPWMChangeRatio
	h.smoothTime = config.GetFloat("smooth_time", configfile.Default(1.), configfile.Above(0.))
	h.invSmoothTime = 1. / h.smoothTime
	h.verifyMainthreadTime = -999.
	// Setup control algorithm sub-class
	algos := map[string]any{"watermark": NewControlBangBang, "pid": NewControlPID}
	algo := config.GetChoice("control", algos).(func(*Heater, *configfile.Wrapper) Control)
	h.control = algo(h, config)
	// Setup output heater pin
	heaterPin := config.Get("heater_pin")
	ppins := printer.LookupObject("pins").(*pins.Pins)
	h.mcuPWM = ppins.SetupPin("pwm", heaterPin).(*mcu.PWM)
	pwmCycleTime := config.GetFloat("pwm_cycle_time", configfile.Default(0.100), configfile.Above(0.),
		configfile.Max(h.pwmDelay))
	h.mcuPWM.SetupCycleTime(pwmCycleTime, false)
	h.mcuPWM.SetupMaxDuration(MaxHeatTime)
	// Load additional modules
	printer.TryLoadObject(config, "verify_heater "+h.shortName)
	printer.TryLoadObject(config, "pid_calibrate")
	g := gcode.Of(printer)
	g.RegisterMuxCommand("SET_HEATER_TEMPERATURE", "HEATER", &h.shortName, h.cmdSetHeaterTemperature,
		"Sets a heater temperature")
	printer.RegisterEventHandler("klippy:shutdown", func(...any) { h.verifyMainthreadTime = -999. })
	return h
}

// SetPWM sets the heater power at a read time. A change under 5% of the max power waits.
func (h *Heater) SetPWM(readTime, value float64) {
	if h.targetTemp <= 0. || readTime > h.verifyMainthreadTime {
		value = 0.
	}
	if (readTime < h.nextPWMTime || h.lastPWMValue == 0) && math.Abs(value-h.lastPWMValue) < h.minPWMChange {
		// No significant change in value - can suppress update
		return
	}
	pwmTime := readTime + h.pwmDelay
	h.nextPWMTime = pwmTime + MaxHeatTime - (3.*h.pwmDelay + 0.001)
	h.lastPWMValue = value
	h.mcuPWM.SetPWM(pwmTime, value)
}

func (h *Heater) temperatureCallback(readTime, temp float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	timeDiff := readTime - h.lastTempTime
	h.lastTemp = temp
	h.lastTempTime = readTime
	h.control.TemperatureUpdate(readTime, temp, h.targetTemp)
	tempDiff := temp - h.smoothedTemp
	adjTime := math.Min(timeDiff*h.invSmoothTime, 1.)
	h.smoothedTemp += tempDiff * adjTime
	h.canExtrude = h.smoothedTemp >= h.minExtrudeTemp
}

// External commands

func (h *Heater) Name() string            { return h.name }
func (h *Heater) PWMDelay() float64       { return h.pwmDelay }
func (h *Heater) MaxPower() float64       { return h.maxPower }
func (h *Heater) SmoothTime() float64     { return h.smoothTime }
func (h *Heater) MinTemp() float64        { return h.minTemp }
func (h *Heater) MaxTemp() float64        { return h.maxTemp }
func (h *Heater) MinExtrudeTemp() float64 { return h.minExtrudeTemp }

// CanExtrude reports that the temperature is high enough to extrude.
func (h *Heater) CanExtrude() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.canExtrude
}

// SetCanExtrude sets the extrude check (for the cold extrude setting).
func (h *Heater) SetCanExtrude(v bool) {
	h.mu.Lock()
	h.canExtrude = v
	h.mu.Unlock()
}

// SetMinExtrudeTemp sets the minimum extrude temperature.
func (h *Heater) SetMinExtrudeTemp(t float64) {
	h.mu.Lock()
	h.minExtrudeTemp = t
	h.mu.Unlock()
}

// SetTemp sets the target temperature. A temperature out of range is a command error.
func (h *Heater) SetTemp(degrees float64) {
	if degrees != 0 && (degrees < h.minTemp || degrees > h.maxTemp) {
		panic(gcode.Errorf("Requested temperature (%.1f) out of range (%.1f:%.1f)", degrees, h.minTemp, h.maxTemp))
	}
	h.mu.Lock()
	h.targetTemp = degrees
	h.mu.Unlock()
}

// GetTemp returns the smoothed temperature and the target. An old temperature is 0.
func (h *Heater) GetTemp(eventtime float64) (float64, float64) {
	estPrintTime := h.mcuPWM.MCU().EstimatedPrintTime(eventtime)
	quellTime := estPrintTime - quellStaleTime
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.lastTempTime < quellTime {
		return 0., h.targetTemp
	}
	return h.smoothedTemp, h.targetTemp
}

// CheckBusy reports that the heater is not at its target yet.
func (h *Heater) CheckBusy(eventtime float64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.control.CheckBusy(eventtime, h.smoothedTemp, h.targetTemp)
}

// SetControl sets the control algorithm and turns the heater off. It returns the old algorithm.
func (h *Heater) SetControl(control Control) Control {
	h.mu.Lock()
	defer h.mu.Unlock()
	old := h.control
	h.control = control
	h.targetTemp = 0.
	return old
}

// AlterTarget sets the target, in the range of the heater.
func (h *Heater) AlterTarget(targetTemp float64) {
	if targetTemp != 0 {
		targetTemp = math.Max(h.minTemp, math.Min(h.maxTemp, targetTemp))
	}
	h.targetTemp = targetTemp
}

// Stats returns the statistics line of the heater.
func (h *Heater) Stats(eventtime float64) (bool, string) {
	estPrintTime := h.mcuPWM.MCU().EstimatedPrintTime(eventtime)
	if !h.printer.IsShutdown() {
		h.verifyMainthreadTime = estPrintTime + maxMainthreadTime
	}
	h.mu.Lock()
	targetTemp, lastTemp, lastPWMValue := h.targetTemp, h.lastTemp, h.lastPWMValue
	h.mu.Unlock()
	isActive := targetTemp != 0 || lastTemp > 50.
	return isActive, fmt.Sprintf("%s: target=%.0f temp=%.1f pwm=%.3f", h.shortName, targetTemp, lastTemp, lastPWMValue)
}

// Status returns the status of the heater.
func (h *Heater) Status(eventtime float64) map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	return map[string]any{"temperature": math.Round(h.smoothedTemp*100) / 100, "target": h.targetTemp,
		"power": h.lastPWMValue}
}

func (h *Heater) cmdSetHeaterTemperature(gcmd *gcode.Command) {
	temp := gcmd.GetFloat("TARGET", gcode.Default(0.))
	Of(h.printer).SetTemperature(h, temp, false)
}

// Bang-bang control algo

// ControlBangBang turns the heater on below the target and off above it, with a max delta.
type ControlBangBang struct {
	heater         *Heater
	heaterMaxPower float64
	maxDelta       float64
	heating        bool
}

func NewControlBangBang(heater *Heater, config *configfile.Wrapper) Control {
	return &ControlBangBang{heater: heater, heaterMaxPower: heater.MaxPower(),
		maxDelta: config.GetFloat("max_delta", configfile.Default(2.0), configfile.Above(0.))}
}

func (c *ControlBangBang) TemperatureUpdate(readTime, temp, targetTemp float64) {
	if c.heating && temp >= targetTemp+c.maxDelta {
		c.heating = false
	} else if !c.heating && temp <= targetTemp-c.maxDelta {
		c.heating = true
	}
	if c.heating {
		c.heater.SetPWM(readTime, c.heaterMaxPower)
	} else {
		c.heater.SetPWM(readTime, 0.)
	}
}

func (c *ControlBangBang) CheckBusy(eventtime, smoothedTemp, targetTemp float64) bool {
	return smoothedTemp < targetTemp-c.maxDelta
}

// Proportional Integral Derivative (PID) control algo

// ControlPID is the PID control.
type ControlPID struct {
	heater         *Heater
	heaterMaxPower float64
	Kp, Ki, Kd     float64
	minDerivTime   float64
	tempIntegMax   float64
	prevTemp       float64
	prevTempTime   float64
	prevTempDeriv  float64
	prevTempInteg  float64
}

func NewControlPID(heater *Heater, config *configfile.Wrapper) Control {
	c := &ControlPID{heater: heater, heaterMaxPower: heater.MaxPower(), prevTemp: AmbientTemp}
	c.Kp = config.GetFloat("pid_Kp") / PIDParamBase
	c.Ki = config.GetFloat("pid_Ki") / PIDParamBase
	c.Kd = config.GetFloat("pid_Kd") / PIDParamBase
	c.minDerivTime = heater.SmoothTime()
	if c.Ki != 0 {
		c.tempIntegMax = c.heaterMaxPower / c.Ki
	}
	return c
}

func (c *ControlPID) TemperatureUpdate(readTime, temp, targetTemp float64) {
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
	co := c.Kp*tempErr + c.Ki*tempInteg - c.Kd*tempDeriv
	boundedCo := math.Max(0., math.Min(c.heaterMaxPower, co))
	c.heater.SetPWM(readTime, boundedCo)
	// Store state for next measurement
	c.prevTemp = temp
	c.prevTempTime = readTime
	c.prevTempDeriv = tempDeriv
	if co == boundedCo {
		c.prevTempInteg = tempInteg
	}
}

func (c *ControlPID) CheckBusy(eventtime, smoothedTemp, targetTemp float64) bool {
	tempDiff := targetTemp - smoothedTemp
	return math.Abs(tempDiff) > pidSettleDelta || math.Abs(c.prevTempDeriv) > pidSettleSlope
}

// Sensor and heater lookup

// PrinterHeaters holds the heaters and the sensor types.
type PrinterHeaters struct {
	printer           *host.Printer
	sensorFactories   map[string]SensorFactory
	heaters           map[string]*Heater
	gcodeIDToSensor   map[string]TempSensor
	availableHeaters  []string
	availableSensors  []string
	availableMonitors []string
	hasStarted        bool
	haveLoadSensors   bool
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	ph := &PrinterHeaters{printer: printer, sensorFactories: map[string]SensorFactory{},
		heaters: map[string]*Heater{}, gcodeIDToSensor: map[string]TempSensor{},
		availableHeaters: []string{}, availableSensors: []string{}, availableMonitors: []string{}}
	printer.RegisterEventHandler("klippy:ready", func(...any) { ph.hasStarted = true })
	printer.RegisterEventHandler("gcode:request_restart", func(...any) { ph.TurnOffAllHeaters() })
	// Register commands
	g := gcode.Of(printer)
	g.RegisterCommand("TURN_OFF_HEATERS", func(*gcode.Command) { ph.TurnOffAllHeaters() }, false, "Turn off all heaters")
	g.RegisterCommand("M105", ph.cmdM105, true, "")
	return ph
}

func init() { host.RegisterModule("heaters", load, nil) }

// Of returns the heaters object of a printer. It must be loaded.
func Of(p *host.Printer) *PrinterHeaters { return p.LookupObject("heaters").(*PrinterHeaters) }

// Load returns the heaters object, and loads it the first time.
func Load(config *configfile.Wrapper) *PrinterHeaters {
	return host.Of(config).LoadObject(config, "heaters").(*PrinterHeaters)
}

func (ph *PrinterHeaters) loadConfig(config *configfile.Wrapper) {
	ph.haveLoadSensors = true
	// Load default temperature sensors
	pconfig := ph.printer.LookupObject("configfile").(*configfile.PrinterConfig)
	dconfig, err := pconfig.ReadConfigData(temperatureSensorsCfg, "temperature_sensors.cfg")
	if err != nil {
		log.Printf("Unable to load temperature_sensors.cfg: %v", err)
		configfile.Raise("Cannot load config '%s'", "temperature_sensors.cfg")
	}
	for _, c := range dconfig.PrefixSections("") {
		ph.printer.TryLoadObject(dconfig, c.Name())
	}
}

// AddSensorFactory adds a sensor type.
func (ph *PrinterHeaters) AddSensorFactory(sensorType string, factory SensorFactory) {
	ph.sensorFactories[sensorType] = factory
}

// SetupHeater makes the heater of a config section. gcodeID is its M105 name ("T0", "B"), or "".
func (ph *PrinterHeaters) SetupHeater(config *configfile.Wrapper, gcodeID string) *Heater {
	f := strings.Fields(config.Name())
	heaterName := f[len(f)-1]
	if _, ok := ph.heaters[heaterName]; ok {
		configfile.Raise("Heater %s already registered", heaterName)
	}
	// Setup sensor
	sensor := ph.SetupSensor(config)
	// Create heater
	heater := newHeater(config, sensor)
	ph.heaters[heaterName] = heater
	ph.RegisterSensor(config, heater, gcodeID)
	ph.availableHeaters = append(ph.availableHeaters, config.Name())
	return heater
}

// AllHeaters lists the names of the heaters.
func (ph *PrinterHeaters) AllHeaters() []string { return ph.availableHeaters }

// LookupHeater returns a heater by its short name.
func (ph *PrinterHeaters) LookupHeater(heaterName string) *Heater {
	h, ok := ph.heaters[heaterName]
	if !ok {
		configfile.Raise("Unknown heater '%s'", heaterName)
	}
	return h
}

// SetupSensor makes the sensor of a config section, by its sensor_type.
func (ph *PrinterHeaters) SetupSensor(config *configfile.Wrapper) Sensor {
	if !ph.haveLoadSensors {
		ph.loadConfig(config)
	}
	sensorType := config.Get("sensor_type")
	factory, ok := ph.sensorFactories[sensorType]
	if !ok {
		configfile.Raise("Unknown temperature sensor '%s'", sensorType)
	}
	return factory(config)
}

// RegisterSensor adds a sensor to TEMPERATURE_WAIT, and to M105 when it has a gcode id.
func (ph *PrinterHeaters) RegisterSensor(config *configfile.Wrapper, psensor TempSensor, gcodeID string) {
	sensorName := config.Name()
	ph.availableSensors = append(ph.availableSensors, sensorName)
	g := gcode.Of(ph.printer)
	g.RegisterMuxCommand("TEMPERATURE_WAIT", "SENSOR", &sensorName, ph.cmdTemperatureWait,
		"Wait for a temperature on a sensor")
	if gcodeID == "" {
		id, ok := config.GetOptional("gcode_id", configfile.Default(nil))
		if !ok {
			return
		}
		gcodeID = id
	}
	if _, ok := ph.gcodeIDToSensor[gcodeID]; ok {
		configfile.Raise("G-Code sensor id %s already registered", gcodeID)
	}
	ph.gcodeIDToSensor[gcodeID] = psensor
}

// RegisterMonitor adds a temperature monitor.
func (ph *PrinterHeaters) RegisterMonitor(config *configfile.Wrapper) {
	ph.availableMonitors = append(ph.availableMonitors, config.Name())
}

// Status returns the status of the heaters object.
func (ph *PrinterHeaters) Status(eventtime float64) map[string]any {
	return map[string]any{"available_heaters": ph.availableHeaters, "available_sensors": ph.availableSensors,
		"available_monitors": ph.availableMonitors}
}

// TurnOffAllHeaters sets the target of each heater to 0.
func (ph *PrinterHeaters) TurnOffAllHeaters() {
	for _, h := range ph.heaters {
		h.SetTemp(0.)
	}
}

// G-Code M105 temperature reporting

func (ph *PrinterHeaters) getTemp(eventtime float64) string {
	// Tn:XXX /YYY B:XXX /YYY
	var out []string
	if ph.hasStarted {
		ids := make([]string, 0, len(ph.gcodeIDToSensor))
		for id := range ph.gcodeIDToSensor {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			cur, target := ph.gcodeIDToSensor[id].GetTemp(eventtime)
			out = append(out, fmt.Sprintf("%s:%.1f /%.1f", id, cur, target))
		}
	}
	if len(out) == 0 {
		return "T:0"
	}
	return strings.Join(out, " ")
}

func (ph *PrinterHeaters) cmdM105(gcmd *gcode.Command) {
	// Get Extruder Temperature
	msg := ph.getTemp(ph.printer.Reactor().Monotonic())
	if !gcmd.Ack(msg) {
		gcmd.RespondRaw(msg)
	}
}

func (ph *PrinterHeaters) waitForTemperature(heater *Heater) {
	// Helper to wait on heater.check_busy() and report M105 temperatures
	if _, ok := ph.printer.StartArgs().Get("debugoutput"); ok {
		return
	}
	th := ph.printer.LookupObject("toolhead").(toolhead)
	g := gcode.Of(ph.printer)
	r := ph.printer.Reactor()
	eventtime := r.Monotonic()
	for !ph.printer.IsShutdown() && heater.CheckBusy(eventtime) {
		th.LastMoveTime()
		g.RespondRaw(ph.getTemp(eventtime))
		eventtime = r.Pause(eventtime + 1.)
	}
}

// SetTemperature sets the target of a heater, after the moves in the queue, and waits for it.
func (ph *PrinterHeaters) SetTemperature(heater *Heater, temp float64, wait bool) {
	th := ph.printer.LookupObject("toolhead").(toolhead)
	th.RegisterLookaheadCallback(func(float64) {})
	heater.SetTemp(temp)
	if wait && temp != 0 {
		ph.waitForTemperature(heater)
	}
}

func (ph *PrinterHeaters) cmdTemperatureWait(gcmd *gcode.Command) {
	sensorName := gcmd.GetString("SENSOR")
	minTemp := gcmd.GetFloat("MINIMUM", gcode.Default(math.Inf(-1)))
	maxTemp := gcmd.GetFloat("MAXIMUM", gcode.Default(math.Inf(1)), gcode.Above(minTemp))
	if math.IsInf(minTemp, -1) && math.IsInf(maxTemp, 1) {
		panic(gcode.Errorf("Error on 'TEMPERATURE_WAIT': missing MINIMUM or MAXIMUM."))
	}
	if _, ok := ph.printer.StartArgs().Get("debugoutput"); ok {
		return
	}
	var sensor TempSensor
	if h, ok := ph.heaters[sensorName]; ok {
		sensor = h
	} else {
		sensor = ph.printer.LookupObject(sensorName).(TempSensor)
	}
	th := ph.printer.LookupObject("toolhead").(toolhead)
	r := ph.printer.Reactor()
	eventtime := r.Monotonic()
	for !ph.printer.IsShutdown() {
		temp, _ := sensor.GetTemp(eventtime)
		if temp >= minTemp && temp <= maxTemp {
			return
		}
		th.LastMoveTime()
		gcmd.RespondRaw(ph.getTemp(eventtime))
		eventtime = r.Pause(eventtime + 1.)
	}
}
