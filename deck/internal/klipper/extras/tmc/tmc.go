// Package tmc is the common code of the TMC stepper drivers: the register fields, the error
// checks, the G-code commands, and the virtual endstop for sensorless homing.
//
// Ported to Go from klippy/extras/tmc.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018-2020  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package tmc

import (
	"fmt"
	"log"
	"math"
	"math/bits"
	"sort"
	"strconv"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/bulk_sensor"
	"printdeck/internal/klipper/extras/force_move"
	"printdeck/internal/klipper/extras/heaters"
	"printdeck/internal/klipper/extras/homing"
	"printdeck/internal/klipper/extras/stepper_enable"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/stepper"
	"printdeck/internal/klipper/toolhead"
)

// NoTime is the print time of a register write that has no time (None in Klipper).
var NoTime = math.NaN()

// MCUTMC is the link to a driver: SPI or UART.
type MCUTMC interface {
	Fields() *FieldHelper
	// RegisterRaw reads a register: "data", "spi_status" (SPI only) and "#receive_time".
	RegisterRaw(regName string) map[string]any
	Register(regName string) int64
	// SetRegister writes a register at a print time, or at once with NoTime.
	SetRegister(regName string, val int64, printTime float64)
	// TMCFrequency is the clock of the driver, or 0 for none.
	TMCFrequency() float64
	MCU() *mcu.MCU
	NameToReg() map[string]int
}

// CurrentHelper sets the currents of a driver.
type CurrentHelper interface {
	// Current returns the run current, the hold current (nil for none), the requested hold
	// current and the maximum current.
	Current() (run float64, hold *float64, reqHold, maxCur float64)
	SetCurrent(runCurrent, holdCurrent, printTime float64)
}

func commandError(format string, args ...any) *gcode.CommandError {
	return gcode.Errorf(format, args...)
}

// ######################################################################
// Field helpers
// ######################################################################

// Return the position of the first bit set in a mask
func ffs(mask int64) int { return bits.TrailingZeros64(uint64(mask)) }

func bitLength(v int64) int {
	if v < 0 {
		v = -v
	}
	return bits.Len64(uint64(v))
}

// Registers are register values in the order of their first write (an OrderedDict).
type Registers struct {
	names  []string
	values map[string]int64
}

func NewRegisters() *Registers { return &Registers{values: map[string]int64{}} }

func (r *Registers) Get(name string) (int64, bool) {
	v, ok := r.values[name]
	return v, ok
}

func (r *Registers) Set(name string, v int64) {
	if _, ok := r.values[name]; !ok {
		r.names = append(r.names, name)
	}
	r.values[name] = v
}

// Names returns the register names in order.
func (r *Registers) Names() []string { return append([]string(nil), r.names...) }

// FieldHelper reads and writes the fields of the registers.
type FieldHelper struct {
	AllFields       map[string]map[string]int64
	signedFields    map[string]bool
	fieldFormatters map[string]func(int64) string
	Registers       *Registers
	fieldToRegister map[string]string
}

// NewFieldHelper makes the field helper of the fields of each register. regOrder is the order of
// the registers in the Python dict: a field name in two registers belongs to the last one.
func NewFieldHelper(allFields map[string]map[string]int64, regOrder []string, signedFields []string,
	fieldFormatters map[string]func(int64) string, registers *Registers) *FieldHelper {
	f := &FieldHelper{AllFields: allFields, signedFields: map[string]bool{},
		fieldFormatters: fieldFormatters, Registers: registers, fieldToRegister: map[string]string{}}
	for _, sf := range signedFields {
		f.signedFields[sf] = true
	}
	if f.Registers == nil {
		f.Registers = NewRegisters()
	}
	for _, r := range regOrder {
		for name := range allFields[r] {
			f.fieldToRegister[name] = r
		}
	}
	return f
}

// LookupRegister returns the register of a field, or "".
func (f *FieldHelper) LookupRegister(fieldName string) string { return f.fieldToRegister[fieldName] }

// Field returns the value of a field in the stored register.
func (f *FieldHelper) Field(fieldName string) int64 {
	regName := f.fieldToRegister[fieldName]
	regValue, _ := f.Registers.Get(regName)
	return f.FieldOf(fieldName, regValue, regName)
}

// FieldOf returns the value of a field in a register value. regName "" is the register of the
// field.
func (f *FieldHelper) FieldOf(fieldName string, regValue int64, regName string) int64 {
	if regName == "" {
		regName = f.fieldToRegister[fieldName]
	}
	mask := f.AllFields[regName][fieldName]
	fieldValue := (regValue & mask) >> ffs(mask)
	if f.signedFields[fieldName] && ((regValue&mask)<<1) > mask {
		fieldValue -= 1 << bitLength(fieldValue)
	}
	return fieldValue
}

// SetField returns the register value with the field set, and stores it.
func (f *FieldHelper) SetField(fieldName string, fieldValue int64) int64 {
	regName := f.fieldToRegister[fieldName]
	regValue, _ := f.Registers.Get(regName)
	return f.SetFieldOf(fieldName, fieldValue, regValue, regName)
}

// SetFieldOf sets a field in a register value, and stores the result for the register.
func (f *FieldHelper) SetFieldOf(fieldName string, fieldValue, regValue int64, regName string) int64 {
	mask := f.AllFields[regName][fieldName]
	newValue := (regValue &^ mask) | ((fieldValue << ffs(mask)) & mask)
	f.Registers.Set(regName, newValue)
	return newValue
}

// SetBoolField sets a field of one bit.
func (f *FieldHelper) SetBoolField(fieldName string, v bool) int64 {
	return f.SetField(fieldName, b2i(v))
}

func b2i(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

// SetConfigField sets a field from the config option driver_FIELD.
func (f *FieldHelper) SetConfigField(config *configfile.Wrapper, fieldName string, def int64) int64 {
	// Allow a field to be set from the config file
	configName := "driver_" + strings.ToUpper(fieldName)
	regName := f.fieldToRegister[fieldName]
	mask := f.AllFields[regName][fieldName]
	maxval := mask >> ffs(mask)
	var val int64
	if maxval == 1 {
		val = b2i(config.GetBool(configName, configfile.Default(def != 0)))
	} else if f.signedFields[fieldName] {
		val = int64(config.GetInt(configName, configfile.Default(int(def)),
			configfile.Min(float64(-(maxval/2 + 1))), configfile.Max(float64(maxval/2))))
	} else {
		val = int64(config.GetInt(configName, configfile.Default(int(def)),
			configfile.Min(0), configfile.Max(float64(maxval))))
	}
	return f.SetField(fieldName, val)
}

// PrettyFormat returns a text description of a register.
func (f *FieldHelper) PrettyFormat(regName string, regValue int64) string {
	type field struct {
		mask int64
		name string
	}
	var regFields []field
	for name, mask := range f.AllFields[regName] {
		regFields = append(regFields, field{mask, name})
	}
	sort.Slice(regFields, func(i, j int) bool {
		if regFields[i].mask != regFields[j].mask {
			return regFields[i].mask < regFields[j].mask
		}
		return regFields[i].name < regFields[j].name
	})
	var out strings.Builder
	for _, rf := range regFields {
		fieldValue := f.FieldOf(rf.name, regValue, regName)
		sval := strconv.FormatInt(fieldValue, 10)
		if fm, ok := f.fieldFormatters[rf.name]; ok {
			sval = fm(fieldValue)
		}
		if sval != "" && sval != "0" {
			fmt.Fprintf(&out, " %s=%s", rf.name, sval)
		}
	}
	return fmt.Sprintf("%-11s %08x%s", regName+":", regValue, out.String())
}

// RegFields returns the fields of a register value.
func (f *FieldHelper) RegFields(regName string, regValue int64) map[string]int64 {
	out := map[string]int64{}
	for name := range f.AllFields[regName] {
		out[name] = f.FieldOf(name, regValue, regName)
	}
	return out
}

// ######################################################################
// Periodic error checking
// ######################################################################

type regInfo struct {
	lastValue    int64
	regName      string
	mask         int64
	errMask      int64
	csActualMask int64
}

// TMCErrorCheck reads the status of a driver each second.
type TMCErrorCheck struct {
	printer          *host.Printer
	stepperName      string
	mcuTMC           MCUTMC
	fields           *FieldHelper
	checkTimer       *reactor.Timer
	lastDrvStatus    *int64
	lastDrvFields    map[string]any
	gstatRegInfo     *regInfo
	clearGstat       bool
	irunField        string
	drvStatusRegInfo *regInfo
	adcTemp          *int64
	adcTempReg       string
}

func newTMCErrorCheck(config *configfile.Wrapper, mcuTMC MCUTMC) *TMCErrorCheck {
	e := &TMCErrorCheck{printer: host.Of(config), mcuTMC: mcuTMC, fields: mcuTMC.Fields()}
	nameParts := strings.Fields(config.Name())
	e.stepperName = strings.Join(nameParts[1:], " ")
	// Setup for GSTAT query
	if regName := e.fields.LookupRegister("drv_err"); regName != "" {
		e.gstatRegInfo = &regInfo{0, regName, 0xffffffff, 0xffffffff, 0}
	}
	e.clearGstat = true
	// Setup for DRV_STATUS query
	e.irunField = "irun"
	regName := "DRV_STATUS"
	var mask, errMask, csActualMask int64
	if nameParts[0] == "tmc2130" {
		// TMC2130 driver quirks
		e.clearGstat = false
		csActualMask = e.fields.AllFields[regName]["cs_actual"]
	} else if nameParts[0] == "tmc2660" {
		// TMC2660 driver quirks
		e.irunField = "cs"
		regName = "READRSP@RDSEL2"
		csActualMask = e.fields.AllFields[regName]["se"]
	}
	errFields := []string{"ot", "s2ga", "s2gb", "s2vsa", "s2vsb"}
	warnFields := []string{"otpw", "t120", "t143", "t150", "t157"}
	for i, f := range append(append([]string(nil), errFields...), warnFields...) {
		if m, ok := e.fields.AllFields[regName][f]; ok {
			mask |= m
			if i < len(errFields) {
				errMask |= m
			}
		}
	}
	e.drvStatusRegInfo = &regInfo{0, regName, mask, errMask, csActualMask}
	// Setup for temperature query
	e.adcTempReg = e.fields.LookupRegister("adc_temp")
	if e.adcTempReg != "" {
		pheaters := heaters.Load(config)
		pheaters.RegisterMonitor(config)
	}
	return e
}

func (e *TMCErrorCheck) queryRegister(ri *regInfo, tryClear bool) int64 {
	var clearedFlags int64
	count := 0
	for {
		var val int64
		var retry bool
		func() {
			defer func() {
				if r := recover(); r != nil {
					ce, ok := r.(*gcode.CommandError)
					if !ok {
						panic(r)
					}
					count++
					if count < 3 && strings.HasPrefix(ce.Msg, "Unable to read tmc uart") {
						// Allow more retries on a TMC UART read error
						rc := e.printer.Reactor()
						rc.Pause(rc.Monotonic() + 0.050)
						retry = true
						return
					}
					panic(r)
				}
			}()
			val = e.mcuTMC.Register(ri.regName)
		}()
		if retry {
			continue
		}
		if val&ri.mask != ri.lastValue&ri.mask {
			fm := e.fields.PrettyFormat(ri.regName, val)
			log.Printf("TMC '%s' reports %s", e.stepperName, fm)
		}
		ri.lastValue = val
		if val&ri.errMask == 0 {
			if ri.csActualMask == 0 || val&ri.csActualMask != 0 {
				break
			}
			irun := e.fields.Field(e.irunField)
			if e.checkTimer == nil || irun < 4 {
				break
			}
			if e.irunField == "irun" && e.fields.Field("ihold") == 0 {
				break
			}
			// CS_ACTUAL field of zero - indicates a driver reset
		}
		count++
		if count >= 3 {
			fm := e.fields.PrettyFormat(ri.regName, val)
			panic(commandError("TMC '%s' reports error: %s", e.stepperName, fm))
		}
		if tryClear && val&ri.errMask != 0 {
			tryClear = false
			clearedFlags |= val & ri.errMask
			e.mcuTMC.SetRegister(ri.regName, val&ri.errMask, NoTime)
		}
	}
	return clearedFlags
}

func (e *TMCErrorCheck) queryTemperature() {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(*gcode.CommandError); !ok {
				panic(r)
			}
			// Ignore comms error for temperature
			e.adcTemp = nil
		}
	}()
	v := e.mcuTMC.Register(e.adcTempReg)
	e.adcTemp = &v
}

func (e *TMCErrorCheck) doPeriodicCheck(eventtime float64) (next float64) {
	defer func() {
		if r := recover(); r != nil {
			ce, ok := r.(*gcode.CommandError)
			if !ok {
				panic(r)
			}
			e.printer.InvokeShutdown(ce.Msg, nil)
			next = reactor.Never
		}
	}()
	e.queryRegister(e.drvStatusRegInfo, false)
	if e.gstatRegInfo != nil {
		e.queryRegister(e.gstatRegInfo, false)
	}
	if e.adcTempReg != "" {
		e.queryTemperature()
	}
	return eventtime + 1.
}

func (e *TMCErrorCheck) StopChecks() {
	if e.checkTimer == nil {
		return
	}
	e.printer.Reactor().UnregisterTimer(e.checkTimer)
	e.checkTimer = nil
}

// StartChecks starts the checks, and reports that the driver had a reset.
func (e *TMCErrorCheck) StartChecks() bool {
	if e.checkTimer != nil {
		e.StopChecks()
	}
	var clearedFlags int64
	e.queryRegister(e.drvStatusRegInfo, false)
	if e.gstatRegInfo != nil {
		clearedFlags = e.queryRegister(e.gstatRegInfo, e.clearGstat)
	}
	rc := e.printer.Reactor()
	curtime := rc.Monotonic()
	e.checkTimer = rc.RegisterTimer(e.doPeriodicCheck, curtime+1.)
	if clearedFlags != 0 {
		resetMask := e.fields.AllFields["GSTAT"]["reset"]
		if clearedFlags&resetMask != 0 {
			return true
		}
	}
	return false
}

func (e *TMCErrorCheck) Status(eventtime float64) map[string]any {
	if e.checkTimer == nil {
		return map[string]any{"drv_status": nil, "temperature": nil}
	}
	var temp any
	if e.adcTemp != nil {
		temp = math.Round((float64(*e.adcTemp)-2038)/7.7*100) / 100
	}
	lastValue, regName := e.drvStatusRegInfo.lastValue, e.drvStatusRegInfo.regName
	if e.lastDrvStatus == nil || lastValue != *e.lastDrvStatus {
		e.lastDrvStatus = &lastValue
		fields := e.fields.RegFields(regName, lastValue)
		e.lastDrvFields = map[string]any{}
		for n, v := range fields {
			if v != 0 {
				e.lastDrvFields[n] = v
			}
		}
	}
	return map[string]any{"drv_status": e.lastDrvFields, "temperature": temp}
}

// ######################################################################
// Record driver status
// ######################################################################

type sample struct {
	printTime float64
	sgResult  int64
	csActual  int64
}

// TMCStallguardDump sends the StallGuard results to API clients (tmc/stallguard_dump).
type TMCStallguardDump struct {
	printer      *host.Printer
	stepperName  string
	mcuTMC       MCUTMC
	mcu          *mcu.MCU
	fields       *FieldHelper
	sg2Supp      bool
	sg4RegName   string
	optimizedSPI bool
	samples      []sample
	queryTimer   *reactor.Timer
	err          *gcode.CommandError
	batchBulk    *bulk_sensor.BatchBulkHelper
}

func newTMCStallguardDump(config *configfile.Wrapper, mcuTMC MCUTMC) *TMCStallguardDump {
	d := &TMCStallguardDump{printer: host.Of(config), mcuTMC: mcuTMC, mcu: mcuTMC.MCU(),
		fields: mcuTMC.Fields()}
	d.stepperName = strings.Join(strings.Fields(config.Name())[1:], " ")
	// It is possible to support TMC2660, just disable it for now
	if len(d.fields.AllFields["DRV_STATUS"]) == 0 {
		return d
	}
	// Collect driver capabilities
	if d.fields.AllFields["DRV_STATUS"]["sg_result"] != 0 {
		d.sg2Supp = true
	}
	// New drivers have separate register for SG4 result
	if mcuTMC.NameToReg()["SG_RESULT"] != 0 {
		d.sg4RegName = "SG_RESULT"
	}
	// 2240 supports both SG2 & SG4
	if d.sg4RegName == "" {
		if mcuTMC.NameToReg()["SG4_RESULT"] != 0 {
			d.sg4RegName = "SG4_RESULT"
		}
	}
	// TMC2208: Klipper checks "sg2_supp is None", which is never true, so there is no return here.
	d.optimizedSPI = false
	// Bulk API
	d.batchBulk = bulk_sensor.NewBatchBulkHelper(d.printer, d.dump, d.start, d.stop,
		bulk_sensor.BatchInterval)
	apiResp := map[string]any{"header": []string{"time", "sg_result", "cs_actual"}}
	d.batchBulk.AddMuxEndpoint("tmc/stallguard_dump", "name", d.stepperName, apiResp)
	return d
}

func (d *TMCStallguardDump) start() {
	d.err = nil
	status := d.mcuTMC.RegisterRaw("DRV_STATUS")
	if s, _ := status["spi_status"].(int64); s != 0 {
		d.optimizedSPI = true
	}
	rc := d.printer.Reactor()
	d.queryTimer = rc.RegisterTimer(d.queryTMC, reactor.Now)
}

func (d *TMCStallguardDump) stop() {
	d.printer.Reactor().UnregisterTimer(d.queryTimer)
	d.queryTimer = nil
	d.samples = nil
}

func (d *TMCStallguardDump) queryTMC(eventtime float64) (next float64) {
	sgResult := int64(-1)
	csActual := int64(-1)
	recvTime := eventtime
	failed := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				ce, ok := r.(*gcode.CommandError)
				if !ok {
					panic(r)
				}
				d.err = ce
				failed = true
			}
		}()
		if d.optimizedSPI || d.sg4RegName == "SG4_RESULT" {
			// TMC2130/TMC5160/TMC2240
			status := d.mcuTMC.RegisterRaw("DRV_STATUS")
			regVal := status["data"].(int64)
			csActual = d.fields.FieldOf("cs_actual", regVal, "")
			sgResult = d.fields.FieldOf("sg_result", regVal, "")
			isStealth := d.fields.FieldOf("stealth", regVal, "")
			recvTime = status["#receive_time"].(float64)
			if isStealth != 0 && d.sg4RegName == "SG4_RESULT" {
				sg4Ret := d.mcuTMC.RegisterRaw("SG4_RESULT")
				sgResult = sg4Ret["data"].(int64)
				recvTime = sg4Ret["#receive_time"].(float64)
			}
		} else {
			// TMC2209
			if d.sg4RegName == "SG_RESULT" {
				sg4Ret := d.mcuTMC.RegisterRaw("SG_RESULT")
				sgResult = sg4Ret["data"].(int64)
				recvTime = sg4Ret["#receive_time"].(float64)
			}
		}
	}()
	if failed {
		return reactor.Never
	}
	printTime := d.mcu.EstimatedPrintTime(recvTime)
	d.samples = append(d.samples, sample{printTime, sgResult, csActual})
	if d.optimizedSPI {
		return eventtime + 0.001
	}
	// UART queried as fast as possible
	return eventtime + 0.005
}

func (d *TMCStallguardDump) dump(eventtime float64) map[string]any {
	if d.err != nil {
		panic(d.err)
	}
	samples := d.samples
	d.samples = nil
	data := make([][]any, len(samples))
	for i, s := range samples {
		data[i] = []any{s.printTime, s.sgResult, s.csActual}
	}
	return map[string]any{"data": data}
}

// ######################################################################
// G-Code command helpers
// ######################################################################

// TMCCommandHelper has the G-code commands of a driver, and follows the enable line and the
// phase of its stepper.
type TMCCommandHelper struct {
	printer        *host.Printer
	stepperName    string
	name           string
	mcuTMC         MCUTMC
	currentHelper  CurrentHelper
	fields         *FieldHelper
	stepper        *stepper.MCUStepper
	mcuPhaseOffset *int64
	toff           *int64
	stepperEnable  *stepper_enable.PrinterStepperEnable
	enableMutex    *reactor.Mutex
	readRegisters  []string
	readTranslate  func(regName string, val int64) (string, int64)
	echeckHelper   *TMCErrorCheck
	recordHelper   *TMCStallguardDump
}

func NewTMCCommandHelper(config *configfile.Wrapper, mcuTMC MCUTMC, currentHelper CurrentHelper) *TMCCommandHelper {
	h := &TMCCommandHelper{printer: host.Of(config), mcuTMC: mcuTMC, currentHelper: currentHelper,
		fields: mcuTMC.Fields()}
	parts := strings.Fields(config.Name())
	h.stepperName = strings.Join(parts[1:], " ")
	h.name = parts[len(parts)-1]
	h.stepperEnable = h.printer.LoadObject(config, "stepper_enable").(*stepper_enable.PrinterStepperEnable)
	h.enableMutex = h.printer.Reactor().NewMutex(false)
	// Common tmc helpers
	h.echeckHelper = newTMCErrorCheck(config, mcuTMC)
	h.recordHelper = newTMCStallguardDump(config, mcuTMC)
	TMCMicrostepHelper(config, mcuTMC)
	// Register callbacks
	h.printer.RegisterEventHandler("stepper:sync_mcu_position", func(args ...any) {
		h.handleSyncMCUPos(args[0].(*stepper.MCUStepper))
	})
	h.printer.RegisterEventHandler("stepper:set_dir_inverted", func(args ...any) {
		h.handleSyncMCUPos(args[0].(*stepper.MCUStepper))
	})
	h.printer.RegisterEventHandler("klippy:mcu_identify", func(...any) { h.handleMCUIdentify() })
	h.printer.RegisterEventHandler("klippy:connect", func(...any) { h.handleConnect() })
	// Register commands
	g := gcode.Of(h.printer)
	g.RegisterMuxCommand("SET_TMC_FIELD", "STEPPER", &h.name, h.cmdSetTMCField,
		"Set a register field of a TMC driver")
	g.RegisterMuxCommand("INIT_TMC", "STEPPER", &h.name, h.cmdInitTMC,
		"Initialize TMC stepper driver registers")
	g.RegisterMuxCommand("SET_TMC_CURRENT", "STEPPER", &h.name, h.cmdSetTMCCurrent,
		"Set the current of a TMC driver")
	return h
}

func (h *TMCCommandHelper) initRegisters(printTime float64) {
	// Send registers
	for _, regName := range h.fields.Registers.Names() {
		val, _ := h.fields.Registers.Get(regName) // Val may change during loop
		h.mcuTMC.SetRegister(regName, val, printTime)
	}
}

func (h *TMCCommandHelper) cmdInitTMC(gcmd *gcode.Command) {
	log.Printf("INIT_TMC %s", h.name)
	printTime := toolhead.Of(h.printer).LastMoveTime()
	h.initRegisters(printTime)
}

func (h *TMCCommandHelper) cmdSetTMCField(gcmd *gcode.Command) {
	fieldName := strings.ToLower(gcmd.GetString("FIELD"))
	regName := h.fields.LookupRegister(fieldName)
	if regName == "" {
		panic(gcmd.Error("Unknown field name '%s'", fieldName))
	}
	_, hasValue := gcmd.Get("VALUE", gcode.Default(nil))
	var value int64
	if hasValue {
		value = int64(gcmd.GetInt("VALUE"))
	}
	velocity, hasVelocity := gcmd.GetFloatOptional("VELOCITY", gcode.Min(0.))
	if hasValue == hasVelocity {
		panic(gcmd.Error("Specify either VALUE or VELOCITY"))
	}
	if hasVelocity {
		if h.mcuTMC.TMCFrequency() == 0 {
			panic(gcmd.Error("VELOCITY parameter not supported by this driver"))
		}
		value = TMCtstepHelper(h.mcuTMC, velocity, h.stepper, nil)
	}
	regVal := h.fields.SetField(fieldName, value)
	printTime := toolhead.Of(h.printer).LastMoveTime()
	h.mcuTMC.SetRegister(regName, regVal, printTime)
}

func (h *TMCCommandHelper) cmdSetTMCCurrent(gcmd *gcode.Command) {
	ch := h.currentHelper
	prevCur, prevHoldCur, reqHoldCur, maxCur := ch.Current()
	runCurrent, hasRun := gcmd.GetFloatOptional("CURRENT", gcode.Min(0.), gcode.Max(maxCur))
	holdCurrent, hasHold := gcmd.GetFloatOptional("HOLDCURRENT", gcode.Above(0.), gcode.Max(maxCur))
	if hasRun || hasHold {
		if !hasRun {
			runCurrent = prevCur
		}
		if !hasHold {
			holdCurrent = reqHoldCur
		}
		printTime := toolhead.Of(h.printer).LastMoveTime()
		ch.SetCurrent(runCurrent, holdCurrent, printTime)
		prevCur, prevHoldCur, reqHoldCur, maxCur = ch.Current()
	}
	// Report values
	if prevHoldCur == nil {
		gcmd.RespondInfo(fmt.Sprintf("Run Current: %0.2fA", prevCur), true)
	} else {
		gcmd.RespondInfo(fmt.Sprintf("Run Current: %0.2fA Hold Current: %0.2fA",
			prevCur, *prevHoldCur), true)
	}
}

// Stepper phase tracking

func (h *TMCCommandHelper) phases() int64 { return (256 >> h.fields.Field("mres")) * 4 }

// PhaseOffset returns the phase offset (nil for unknown) and the number of phases.
func (h *TMCCommandHelper) PhaseOffset() (*int64, int64) { return h.mcuPhaseOffset, h.phases() }

func (h *TMCCommandHelper) queryPhase() int64 {
	fieldName := "mscnt"
	if h.fields.LookupRegister(fieldName) == "" {
		// TMC2660 uses MSTEP
		fieldName = "mstep"
	}
	reg := h.mcuTMC.Register(h.fields.LookupRegister(fieldName))
	return h.fields.FieldOf(fieldName, reg, "")
}

func pyMod(a, b int64) int64 {
	m := a % b
	if m != 0 && (m < 0) != (b < 0) {
		m += b
	}
	return m
}

func (h *TMCCommandHelper) handleSyncMCUPos(s *stepper.MCUStepper) {
	if s.Name() != h.stepperName {
		return
	}
	var driverPhase int64
	failed := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(*gcode.CommandError); !ok {
					panic(r)
				}
				log.Printf("Unable to obtain tmc %s phase", h.stepperName)
				h.mcuPhaseOffset = nil
				enableLine := h.stepperEnable.LookupEnable(h.stepperName)
				if enableLine.IsMotorEnabled() {
					panic(r)
				}
				failed = true
			}
		}()
		driverPhase = h.queryPhase()
	}()
	if failed {
		return
	}
	if inv, _ := s.DirInverted(); !inv {
		driverPhase = 1023 - driverPhase
	}
	phases := h.phases()
	phase := int64(float64(driverPhase)/1024*float64(phases)+.5) % phases
	moff := pyMod(phase-s.MCUPosition(), phases)
	if h.mcuPhaseOffset != nil && *h.mcuPhaseOffset != moff {
		log.Printf("Stepper %s phase change (was %d now %d)", h.stepperName, *h.mcuPhaseOffset, moff)
	}
	h.mcuPhaseOffset = &moff
}

// Stepper enable/disable tracking

func (h *TMCCommandHelper) doEnable(printTime float64) {
	if h.toff != nil {
		// Shared enable via comms handling
		h.fields.SetField("toff", *h.toff)
	}
	h.initRegisters(NoTime)
	didReset := h.echeckHelper.StartChecks()
	if didReset {
		h.mcuPhaseOffset = nil
	}
	// Calculate phase offset
	if h.mcuPhaseOffset != nil {
		return
	}
	mutex := gcode.Of(h.printer).Mutex()
	mutex.Lock()
	defer mutex.Unlock()
	if h.mcuPhaseOffset != nil {
		return
	}
	log.Printf("Pausing toolhead to calculate %s phase offset", h.stepperName)
	toolhead.Of(h.printer).WaitMoves()
	h.handleSyncMCUPos(h.stepper)
}

func (h *TMCCommandHelper) doDisable(printTime float64) {
	if h.toff != nil {
		val := h.fields.SetField("toff", 0)
		regName := h.fields.LookupRegister("toff")
		h.mcuTMC.SetRegister(regName, val, printTime)
	}
	h.echeckHelper.StopChecks()
}

func (h *TMCCommandHelper) handleStepperEnable(printTime float64, isEnable bool) {
	enableDisableCB := func(eventtime float64) any {
		defer func() {
			if r := recover(); r != nil {
				ce, ok := r.(*gcode.CommandError)
				if !ok {
					panic(r)
				}
				h.printer.InvokeShutdown(ce.Msg, nil)
			}
		}()
		h.enableMutex.Lock()
		defer h.enableMutex.Unlock()
		if isEnable {
			h.doEnable(printTime)
		} else {
			h.doDisable(printTime)
		}
		return nil
	}
	h.printer.Reactor().RegisterCallback(enableDisableCB, reactor.Now)
}

// Initial startup handling

func (h *TMCCommandHelper) handleMCUIdentify() {
	// Lookup stepper object
	fm := h.printer.LookupObject("force_move").(*force_move.ForceMove)
	h.stepper = fm.LookupStepper(h.stepperName)
	// Note pulse duration and step_both_edge optimizations available
	h.stepper.SetupDefaultPulseDuration(.000000100, true)
}

func (h *TMCCommandHelper) handleConnect() {
	// Check if using step on both edges optimization
	_, stepBothEdge := h.stepper.PulseDuration()
	if stepBothEdge {
		h.fields.SetField("dedge", 1)
	}
	// Check for soft stepper enable/disable
	enableLine := h.stepperEnable.LookupEnable(h.stepperName)
	enableLine.RegisterStateCallback(h.handleStepperEnable)
	if !enableLine.HasDedicatedEnable() {
		toff := h.fields.Field("toff")
		h.toff = &toff
		h.fields.SetField("toff", 0)
		log.Printf("Enabling TMC virtual enable for '%s'", h.stepperName)
	}
	// Send init
	func() {
		defer func() {
			if r := recover(); r != nil {
				ce, ok := r.(*gcode.CommandError)
				if !ok {
					panic(r)
				}
				log.Printf("TMC %s failed to init: %s", h.name, ce.Msg)
			}
		}()
		h.initRegisters(NoTime)
	}()
}

// Status is the get_status information of the driver.
func (h *TMCCommandHelper) Status(eventtime float64) map[string]any {
	var cpos any
	if h.stepper != nil && h.mcuPhaseOffset != nil {
		cpos = h.stepper.MCUToCommandedPosition(*h.mcuPhaseOffset)
	}
	run, hold, _, _ := h.currentHelper.Current()
	var holdV any
	if hold != nil {
		holdV = *hold
	}
	var moff any
	if h.mcuPhaseOffset != nil {
		moff = *h.mcuPhaseOffset
	}
	res := map[string]any{"mcu_phase_offset": moff,
		"phase_offset_position": cpos,
		"run_current":           run,
		"hold_current":          holdV}
	for k, v := range h.echeckHelper.Status(eventtime) {
		res[k] = v
	}
	return res
}

// DUMP_TMC support

// SetupRegisterDump adds DUMP_TMC with the registers that the driver can read. readTranslate
// can be nil.
func (h *TMCCommandHelper) SetupRegisterDump(readRegisters []string,
	readTranslate func(regName string, val int64) (string, int64)) {
	h.readRegisters = readRegisters
	h.readTranslate = readTranslate
	g := gcode.Of(h.printer)
	g.RegisterMuxCommand("DUMP_TMC", "STEPPER", &h.name, h.cmdDumpTMC,
		"Read and display TMC stepper driver registers")
}

func (h *TMCCommandHelper) isReadRegister(regName string) bool {
	for _, r := range h.readRegisters {
		if r == regName {
			return true
		}
	}
	return false
}

func (h *TMCCommandHelper) cmdDumpTMC(gcmd *gcode.Command) {
	log.Printf("DUMP_TMC %s", h.name)
	regName, ok := gcmd.Get("REGISTER", gcode.Default(nil))
	if ok {
		regName = strings.ToUpper(regName)
		val, hasVal := h.fields.Registers.Get(regName)
		if hasVal && !h.isReadRegister(regName) {
			// write-only register
			gcmd.RespondInfo(h.fields.PrettyFormat(regName, val), true)
		} else if h.isReadRegister(regName) {
			// readable register
			val := h.mcuTMC.Register(regName)
			if h.readTranslate != nil {
				regName, val = h.readTranslate(regName, val)
			}
			gcmd.RespondInfo(h.fields.PrettyFormat(regName, val), true)
		} else {
			panic(gcmd.Error("Unknown register name '%s'", regName))
		}
	} else {
		gcmd.RespondInfo("========== Write-only registers ==========", true)
		for _, regName := range h.fields.Registers.Names() {
			if !h.isReadRegister(regName) {
				val, _ := h.fields.Registers.Get(regName)
				gcmd.RespondInfo(h.fields.PrettyFormat(regName, val), true)
			}
		}
		gcmd.RespondInfo("========== Queried registers ==========", true)
		for _, regName := range h.readRegisters {
			val := h.mcuTMC.Register(regName)
			if h.readTranslate != nil {
				regName, val = h.readTranslate(regName, val)
			}
			gcmd.RespondInfo(h.fields.PrettyFormat(regName, val), true)
		}
	}
}

// ######################################################################
// TMC virtual pins
// ######################################################################

type mcuEndstopper interface{ MCUEndstops() []homing.Endstop }

// TMCVirtualPinHelper is the virtual endstop of a driver, for "sensorless homing".
type TMCVirtualPinHelper struct {
	printer      *host.Printer
	mcuTMC       MCUTMC
	fields       *FieldHelper
	diagPin      string
	hasDiagPin   bool
	diagPinField string
	mcuEndstop   any
	dirtyRegs    *Registers
	prevState    *Registers
}

func NewTMCVirtualPinHelper(config *configfile.Wrapper, mcuTMC MCUTMC) *TMCVirtualPinHelper {
	v := &TMCVirtualPinHelper{printer: host.Of(config), mcuTMC: mcuTMC, fields: mcuTMC.Fields()}
	if v.fields.LookupRegister("diag0_stall") != "" {
		if _, ok := config.GetOptional("diag0_pin", configfile.Default(nil)); ok {
			v.diagPin, v.hasDiagPin = config.Get("diag0_pin"), true
			v.diagPinField = "diag0_stall"
		} else {
			v.diagPin, v.hasDiagPin = config.GetOptional("diag1_pin", configfile.Default(nil))
			v.diagPinField = "diag1_stall"
		}
	} else {
		v.diagPin, v.hasDiagPin = config.GetOptional("diag_pin", configfile.Default(nil))
		v.diagPinField = ""
	}
	v.dirtyRegs = NewRegisters()
	v.prevState = NewRegisters()
	// Register virtual_endstop pin
	nameParts := strings.Fields(config.Name())
	ppins := v.printer.LookupObject("pins").(*pins.Pins)
	ppins.RegisterChip(fmt.Sprintf("%s_%s", nameParts[0], nameParts[len(nameParts)-1]), v)
	return v
}

func pinError(format string, args ...any) { panic(&pins.Error{Msg: fmt.Sprintf(format, args...)}) }

// SetupPin makes the virtual endstop.
func (v *TMCVirtualPinHelper) SetupPin(pinType string, pinParams *pins.Params) any {
	// Validate pin
	ppins := v.printer.LookupObject("pins").(*pins.Pins)
	if pinType != "endstop" || pinParams.Pin != "virtual_endstop" {
		pinError("tmc virtual endstop only useful as endstop")
	}
	if pinParams.Invert != 0 || pinParams.Pullup != 0 {
		pinError("Can not pullup/invert tmc virtual pin")
	}
	if !v.hasDiagPin {
		pinError("tmc virtual endstop requires diag pin config")
	}
	// Setup for sensorless homing
	v.printer.RegisterEventHandler("homing:homing_move_begin", func(args ...any) {
		v.handleHomingMoveBegin(args[0].(mcuEndstopper))
	})
	v.printer.RegisterEventHandler("homing:homing_move_end", func(args ...any) {
		v.handleHomingMoveEnd(args[0].(mcuEndstopper))
	})
	v.mcuEndstop = ppins.SetupPin("endstop", v.diagPin)
	return v.mcuEndstop
}

func (v *TMCVirtualPinHelper) setField(fieldName string, value int64) {
	v.prevState.Set(fieldName, v.fields.Field(fieldName))
	regName := v.fields.LookupRegister(fieldName)
	v.dirtyRegs.Set(regName, v.fields.SetField(fieldName, value))
}

func (v *TMCVirtualPinHelper) sendFields() {
	for _, reg := range v.dirtyRegs.Names() {
		val, _ := v.dirtyRegs.Get(reg)
		v.mcuTMC.SetRegister(reg, val, NoTime)
	}
	v.dirtyRegs = NewRegisters()
}

func (v *TMCVirtualPinHelper) hasEndstop(hmove mcuEndstopper) bool {
	for _, es := range hmove.MCUEndstops() {
		if any(es) == v.mcuEndstop {
			return true
		}
	}
	return false
}

func (v *TMCVirtualPinHelper) handleHomingMoveBegin(hmove mcuEndstopper) {
	if !v.hasEndstop(hmove) {
		return
	}
	var sg4Thrs int64
	if v.fields.LookupRegister("sg4_thrs") != "" {
		sg4Thrs = v.fields.Field("sg4_thrs")
	}
	// Enable/disable stealthchop
	if v.fields.LookupRegister("en_pwm_mode") == "" {
		// On "stallguard4" drivers, "stealthchop" must be enabled
		v.setField("tpwmthrs", 0)
		v.setField("en_spreadcycle", 0)
	} else if sg4Thrs != 0 {
		// TMC2240 using SG4, "stealthchop" must be enabled
		v.setField("en_pwm_mode", 1)
		v.setField("tpwmthrs", 0)
		v.setField(v.diagPinField, 1)
	} else {
		// On earlier drivers, "stealthchop" must be disabled
		v.setField("en_pwm_mode", 0)
		v.setField(v.diagPinField, 1)
	}
	// Enable tcoolthrs (if not already)
	if v.fields.Field("tcoolthrs") == 0 {
		v.setField("tcoolthrs", 0xfffff)
	}
	// Disable thigh
	if v.fields.LookupRegister("thigh") != "" {
		v.setField("thigh", 0)
	}
	v.sendFields()
}

func (v *TMCVirtualPinHelper) handleHomingMoveEnd(hmove mcuEndstopper) {
	if !v.hasEndstop(hmove) {
		return
	}
	// Restore previous state
	for _, field := range v.prevState.Names() {
		val, _ := v.prevState.Get(field)
		v.setField(field, val)
	}
	v.sendFields()
	v.prevState = NewRegisters()
}

// ######################################################################
// Config reading helpers
// ######################################################################

// TMCWaveTableHelper initializes the wave table from config or defaults.
func TMCWaveTableHelper(config *configfile.Wrapper, mcuTMC MCUTMC) {
	f := mcuTMC.Fields()
	f.SetConfigField(config, "mslut0", 0xAAAAB554)
	f.SetConfigField(config, "mslut1", 0x4A9554AA)
	f.SetConfigField(config, "mslut2", 0x24492929)
	f.SetConfigField(config, "mslut3", 0x10104222)
	f.SetConfigField(config, "mslut4", 0xFBFFFFFF)
	f.SetConfigField(config, "mslut5", 0xB5BB777D)
	f.SetConfigField(config, "mslut6", 0x49295556)
	f.SetConfigField(config, "mslut7", 0x00404222)
	f.SetConfigField(config, "w0", 2)
	f.SetConfigField(config, "w1", 1)
	f.SetConfigField(config, "w2", 1)
	f.SetConfigField(config, "w3", 1)
	f.SetConfigField(config, "x1", 128)
	f.SetConfigField(config, "x2", 255)
	f.SetConfigField(config, "x3", 255)
	f.SetConfigField(config, "start_sin", 0)
	f.SetConfigField(config, "start_sin90", 247)
}

// TMCMicrostepHelper configures the microstep settings.
func TMCMicrostepHelper(config *configfile.Wrapper, mcuTMC MCUTMC) {
	fields := mcuTMC.Fields()
	stepperName := strings.Join(strings.Fields(config.Name())[1:], " ")
	if !config.HasSection(stepperName) {
		configfile.Raise("Could not find config section '[%s]' required by tmc driver", stepperName)
	}
	sconfig := config.GetSection(stepperName)
	steps := map[int]any{256: 0, 128: 1, 64: 2, 32: 3, 16: 4, 8: 5, 4: 6, 2: 7, 1: 8}
	mres := sconfig.GetIntChoice("microsteps", steps).(int)
	fields.SetField("mres", int64(mres))
	fields.SetBoolField("intpol", config.GetBool("interpolate", configfile.Default(true)))
}

// TMCtstepHelper calculates a TSTEP based value from a velocity, with the step distance of
// pstepper, or of the stepper of the config section when pstepper is nil.
func TMCtstepHelper(mcuTMC MCUTMC, velocity float64, pstepper *stepper.MCUStepper,
	config *configfile.Wrapper) int64 {
	if velocity <= 0. {
		return 0xfffff
	}
	var stepDist float64
	if pstepper != nil {
		stepDist = pstepper.StepDist()
	} else {
		stepperName := strings.Join(strings.Fields(config.Name())[1:], " ")
		sconfig := config.GetSection(stepperName)
		rotationDist, stepsPerRotation := stepper.ParseStepDistance(sconfig, nil, false)
		stepDist = rotationDist / stepsPerRotation
	}
	mres := mcuTMC.Fields().Field("mres")
	stepDist256 := stepDist / float64(int64(1)<<mres)
	tmcFreq := mcuTMC.TMCFrequency()
	threshold := int64(tmcFreq*stepDist256/velocity + .5)
	return max(0, min(0xfffff, threshold))
}

// TMCStealthchopHelper configures the stealthChop-spreadCycle transition velocity.
func TMCStealthchopHelper(config *configfile.Wrapper, mcuTMC MCUTMC) {
	fields := mcuTMC.Fields()
	enPWMMode := false
	velocity, ok := config.GetFloatOptional("stealthchop_threshold", configfile.Default(nil),
		configfile.Min(0.))
	tpwmthrs := int64(0xfffff)

	if ok {
		enPWMMode = true
		tpwmthrs = TMCtstepHelper(mcuTMC, velocity, nil, config)
	}
	fields.SetField("tpwmthrs", tpwmthrs)

	if fields.LookupRegister("en_pwm_mode") != "" {
		fields.SetBoolField("en_pwm_mode", enPWMMode)
	} else {
		// TMC2208 uses en_spreadCycle
		fields.SetBoolField("en_spreadcycle", !enPWMMode)
	}
}

// TMCVcoolthrsHelper configures the StallGuard and CoolStep minimum velocity.
func TMCVcoolthrsHelper(config *configfile.Wrapper, mcuTMC MCUTMC) {
	fields := mcuTMC.Fields()
	velocity, ok := config.GetFloatOptional("coolstep_threshold", configfile.Default(nil),
		configfile.Min(0.))
	var tcoolthrs int64
	if ok {
		tcoolthrs = TMCtstepHelper(mcuTMC, velocity, nil, config)
	}
	fields.SetField("tcoolthrs", tcoolthrs)
}

// TMCVhighHelper configures the StallGuard and CoolStep maximum velocity and the
// SpreadCycle-FullStepping (High velocity) mode threshold.
func TMCVhighHelper(config *configfile.Wrapper, mcuTMC MCUTMC) {
	fields := mcuTMC.Fields()
	velocity, ok := config.GetFloatOptional("high_velocity_threshold", configfile.Default(nil),
		configfile.Min(0.))
	var thigh int64
	if ok {
		thigh = TMCtstepHelper(mcuTMC, velocity, nil, config)
	}
	fields.SetField("thigh", thigh)
}
