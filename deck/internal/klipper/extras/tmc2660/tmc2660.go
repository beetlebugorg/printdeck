// Package tmc2660 is the TMC2660 stepper driver on SPI.
//
// Ported to Go from klippy/extras/tmc2660.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018-2019  Florian Heilmann <Florian.Heilmann@gmx.net>
//	Copyright (C) 2019-2021  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package tmc2660

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/bus"
	"printdeck/internal/klipper/extras/tmc"
	"printdeck/internal/klipper/extras/tmc2130"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/reactor"
)

var Registers = map[string]int{
	"DRVCONF": 0xE, "SGCSCONF": 0xC, "SMARTEN": 0xA,
	"CHOPCONF": 0x8, "DRVCTRL": 0x0,
}

var ReadRegisters = []string{"READRSP@RDSEL0", "READRSP@RDSEL1", "READRSP@RDSEL2"}

// FieldOrder is the order of the registers of Fields.
var FieldOrder = []string{"DRVCTRL", "CHOPCONF", "SMARTEN", "SGCSCONF", "DRVCONF",
	"READRSP@RDSEL0", "READRSP@RDSEL1", "READRSP@RDSEL2"}

// Fields returns the fields of the registers. Each driver gets its own copy.
func Fields() map[string]map[string]int64 {
	f := map[string]map[string]int64{}
	f["DRVCTRL"] = map[string]int64{
		"mres":   0x0f,
		"dedge":  0x01 << 8,
		"intpol": 0x01 << 9,
	}
	f["CHOPCONF"] = map[string]int64{
		"toff":  0x0f,
		"hstrt": 0x7 << 4,
		"hend":  0x0f << 7,
		"hdec":  0x03 << 11,
		"rndtf": 0x01 << 13,
		"chm":   0x01 << 14,
		"tbl":   0x03 << 15,
	}
	f["SMARTEN"] = map[string]int64{
		"semin":  0x0f,
		"seup":   0x03 << 5,
		"semax":  0x0f << 8,
		"sedn":   0x03 << 13,
		"seimin": 0x01 << 15,
	}
	f["SGCSCONF"] = map[string]int64{
		"cs":    0x1f,
		"sgt":   0x7F << 8,
		"sfilt": 0x01 << 16,
	}
	f["DRVCONF"] = map[string]int64{
		"rdsel":  0x03 << 4,
		"vsense": 0x01 << 6,
		"sdoff":  0x01 << 7,
		"ts2g":   0x03 << 8,
		"diss2g": 0x01 << 10,
		"slpl":   0x03 << 12,
		"slph":   0x03 << 14,
		"tst":    0x01 << 16,
	}
	status := func(extra map[string]int64) map[string]int64 {
		m := map[string]int64{
			"stallguard": 0x01 << 4,
			"ot":         0x01 << 5,
			"otpw":       0x01 << 6,
			"s2ga":       0x01 << 7,
			"s2gb":       0x01 << 8,
			"ola":        0x01 << 9,
			"olb":        0x01 << 10,
			"stst":       0x01 << 11,
		}
		maps.Copy(m, extra)
		return m
	}
	f["READRSP@RDSEL0"] = status(map[string]int64{"mstep": 0x3ff << 14})
	f["READRSP@RDSEL1"] = status(map[string]int64{"sg_result": 0x3ff << 14})
	f["READRSP@RDSEL2"] = status(map[string]int64{"se": 0x1f << 14, "sg_result@rdsel2": 0x1f << 19})
	return f
}

var SignedFields = []string{"sgt"}

func choose(v int64, yes, no string) string {
	if v != 0 {
		return yes
	}
	return no
}

var FieldFormatters = func() map[string]func(int64) string {
	f := maps.Clone(tmc2130.FieldFormatters)
	f["chm"] = func(v int64) string { return choose(v, "1(constant toff)", "0(spreadCycle)") }
	f["vsense"] = func(v int64) string { return choose(v, "1(165mV)", "0(305mV)") }
	f["sdoff"] = func(v int64) string { return choose(v, "1(Step/Dir disabled!)", "") }
	f["diss2g"] = func(v int64) string { return choose(v, "1(Short to GND disabled!)", "") }
	f["se"] = func(v int64) string { return choose(v, fmt.Sprintf("%d", v), "0(Reset?)") }
	return f
}()

// ######################################################################
// TMC stepper current config helper
// ######################################################################

const MaxCurrent = 2.400

// TMC2660CurrentHelper sets the current of a TMC2660, with a lower current when idle.
type TMC2660CurrentHelper struct {
	printer               *host.Printer
	name                  string
	mcuTMC                tmc.MCUTMC
	fields                *tmc.FieldHelper
	current               float64
	senseResistor         float64
	idleCurrentPercentage int
}

func newTMC2660CurrentHelper(config *configfile.Wrapper, mcuTMC tmc.MCUTMC) *TMC2660CurrentHelper {
	parts := strings.Fields(config.Name())
	c := &TMC2660CurrentHelper{printer: host.Of(config), name: parts[len(parts)-1], mcuTMC: mcuTMC,
		fields: mcuTMC.Fields()}
	c.current = config.GetFloat("run_current", configfile.Min(0.1), configfile.Max(MaxCurrent))
	c.senseResistor = config.GetFloat("sense_resistor")
	vsense, cs := c.calcCurrent(c.current)
	c.fields.SetField("cs", cs)
	c.fields.SetBoolField("vsense", vsense)

	// Register ready/printing handlers
	c.idleCurrentPercentage = config.GetInt("idle_current_percent", configfile.Default(100),
		configfile.Min(0), configfile.Max(100))
	if c.idleCurrentPercentage < 100 {
		c.printer.RegisterEventHandler("idle_timeout:printing", func(args ...any) {
			c.handlePrinting(args[0].(float64))
		})
		c.printer.RegisterEventHandler("idle_timeout:ready", func(args ...any) {
			c.handleReady(args[0].(float64))
		})
	}
	return c
}

func (c *TMC2660CurrentHelper) calcCurrentBits(current float64, vsense bool) int64 {
	vref := 0.310
	if vsense {
		vref = 0.165
	}
	sr := c.senseResistor
	cs := int64(32.*sr*current*math.Sqrt(2.)/vref+.5) - 1
	return max(0, min(31, cs))
}

func (c *TMC2660CurrentHelper) calcCurrentFromBits(cs int64, vsense bool) float64 {
	vref := 0.310
	if vsense {
		vref = 0.165
	}
	return float64(cs+1) * vref / (32. * c.senseResistor * math.Sqrt(2.))
}

func (c *TMC2660CurrentHelper) calcCurrent(runCurrent float64) (bool, int64) {
	vsense := true
	irun := c.calcCurrentBits(runCurrent, true)
	if irun == 31 {
		cur := c.calcCurrentFromBits(irun, true)
		if cur < runCurrent {
			irun2 := c.calcCurrentBits(runCurrent, false)
			cur2 := c.calcCurrentFromBits(irun2, false)
			if math.Abs(runCurrent-cur2) < math.Abs(runCurrent-cur) {
				vsense = false
				irun = irun2
			}
		}
	}
	return vsense, irun
}

func (c *TMC2660CurrentHelper) handlePrinting(printTime float64) {
	printTime -= 0.100 // Schedule slightly before deadline
	c.printer.Reactor().RegisterCallback(func(float64) any {
		c.updateCurrent(c.current, printTime)
		return nil
	}, reactor.Now)
}

func (c *TMC2660CurrentHelper) handleReady(printTime float64) {
	current := c.current * float64(c.idleCurrentPercentage) / 100.
	c.printer.Reactor().RegisterCallback(func(float64) any {
		c.updateCurrent(current, printTime)
		return nil
	}, reactor.Now)
}

func (c *TMC2660CurrentHelper) updateCurrent(current, printTime float64) {
	vsense, cs := c.calcCurrent(current)
	val := c.fields.SetField("cs", cs)
	c.mcuTMC.SetRegister("SGCSCONF", val, printTime)
	// Only update VSENSE if we need to
	if vsense != (c.fields.Field("vsense") != 0) {
		val = c.fields.SetBoolField("vsense", vsense)
		c.mcuTMC.SetRegister("DRVCONF", val, printTime)
	}
}

// Current returns the run current. The TMC2660 has no hold current.
func (c *TMC2660CurrentHelper) Current() (float64, *float64, float64, float64) {
	return c.current, nil, math.NaN(), MaxCurrent
}

func (c *TMC2660CurrentHelper) SetCurrent(runCurrent, holdCurrent, printTime float64) {
	c.current = runCurrent
	c.updateCurrent(runCurrent, printTime)
}

// ######################################################################
// TMC2660 SPI
// ######################################################################

// MCUTMC2660SPI is the link to a TMC2660 on SPI.
type MCUTMC2660SPI struct {
	printer   *host.Printer
	mutex     *reactor.Mutex
	spi       *bus.MCUSPI
	nameToReg map[string]int
	fields    *tmc.FieldHelper
}

func newMCUTMC2660SPI(config *configfile.Wrapper, nameToReg map[string]int, fields *tmc.FieldHelper) *MCUTMC2660SPI {
	s := &MCUTMC2660SPI{printer: host.Of(config), nameToReg: nameToReg, fields: fields}
	s.mutex = s.printer.Reactor().NewMutex(false)
	s.spi = bus.MCUSPIFromConfig(config, 0, "cs_pin", 4000000, "", false)
	return s
}

func (s *MCUTMC2660SPI) Fields() *tmc.FieldHelper  { return s.fields }
func (s *MCUTMC2660SPI) NameToReg() map[string]int { return s.nameToReg }

func regMsg(reg int, val int64) []byte {
	return []byte{byte(((val >> 16) | int64(reg)) & 0xff), byte((val >> 8) & 0xff), byte(val & 0xff)}
}

func (s *MCUTMC2660SPI) RegisterRaw(regName string) map[string]any {
	newRdsel := int64(slices.Index(ReadRegisters, regName))
	if newRdsel < 0 {
		panic(fmt.Errorf("ValueError: '%s' is not in list", regName))
	}
	reg := s.nameToReg["DRVCONF"]
	if _, ok := s.printer.StartArgs().Get("debugoutput"); ok {
		return map[string]any{"data": int64(0), "#receive_time": .0}
	}
	s.mutex.Lock()
	oldRdsel := s.fields.Field("rdsel")
	val := s.fields.SetField("rdsel", newRdsel)
	msg := regMsg(reg, val)
	if newRdsel != oldRdsel {
		// Must set RDSEL value first
		s.spi.SPISend(msg, 0, 0)
	}
	params := s.spi.SPITransfer(msg, 0, 0)
	s.mutex.Unlock()
	pr := params.Bytes("response")
	return map[string]any{
		"data":          int64(pr[0])<<16 | int64(pr[1])<<8 | int64(pr[2]),
		"#receive_time": params.Float("#receive_time"),
	}
}

func (s *MCUTMC2660SPI) Register(regName string) int64 { return s.RegisterRaw(regName)["data"].(int64) }

func (s *MCUTMC2660SPI) SetRegister(regName string, val int64, printTime float64) {
	var minclock int64
	if !math.IsNaN(printTime) {
		minclock = s.spi.MCU().PrintTimeToClock(printTime)
	}
	reg := s.nameToReg[regName]
	msg := regMsg(reg, val)
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.spi.SPISend(msg, minclock, 0)
}

// TMCFrequency is 0: the TMC2660 has no TSTEP based thresholds.
func (s *MCUTMC2660SPI) TMCFrequency() float64 { return 0 }
func (s *MCUTMC2660SPI) MCU() *mcu.MCU         { return s.spi.MCU() }

// ######################################################################
// TMC2660 printer object
// ######################################################################

// TMC2660 is a [tmc2660] section.
type TMC2660 struct {
	fields    *tmc.FieldHelper
	mcuTMC    *MCUTMC2660SPI
	cmdhelper *tmc.TMCCommandHelper
}

func loadPrefix(config *configfile.Wrapper) any {
	t := &TMC2660{}
	// Setup mcu communication
	t.fields = tmc.NewFieldHelper(Fields(), FieldOrder, SignedFields, FieldFormatters, nil)
	t.fields.SetField("sdoff", 0) // Access DRVCTRL in step/dir mode
	t.mcuTMC = newMCUTMC2660SPI(config, Registers, t.fields)
	// Register commands
	currentHelper := newTMC2660CurrentHelper(config, t.mcuTMC)
	t.cmdhelper = tmc.NewTMCCommandHelper(config, t.mcuTMC, currentHelper)
	t.cmdhelper.SetupRegisterDump(ReadRegisters, nil)

	// CHOPCONF
	set := func(name string, def int64) { t.fields.SetConfigField(config, name, def) }
	set("tbl", 2)
	set("rndtf", 0)
	set("hdec", 0)
	set("chm", 0)
	set("hend", 3)
	set("hstrt", 3)
	set("toff", 4)
	if t.fields.Field("chm") == 0 {
		if t.fields.Field("hstrt")+t.fields.Field("hend") > 15 {
			configfile.Raise("driver_HEND + driver_HSTRT must be <= 15")
		}
	}
	// SMARTEN
	set("seimin", 0)
	set("sedn", 0)
	set("semax", 0)
	set("seup", 0)
	set("semin", 0)

	// SGSCONF
	set("sfilt", 0)
	set("sgt", 0)

	// DRVCONF
	set("slph", 0)
	set("slpl", 0)
	set("diss2g", 0)
	set("ts2g", 3)
	return t
}

func init() { host.RegisterModule("tmc2660", nil, loadPrefix) }

// PhaseOffset returns the phase offset (nil for unknown) and the number of phases.
func (t *TMC2660) PhaseOffset() (*int64, int64) { return t.cmdhelper.PhaseOffset() }

func (t *TMC2660) Status(eventtime float64) map[string]any { return t.cmdhelper.Status(eventtime) }
