// Package tmc2130 is the TMC2130 stepper driver on SPI. Other SPI drivers use its SPI code.
//
// Ported to Go from klippy/extras/tmc2130.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018-2019  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package tmc2130

import (
	"fmt"
	"math"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/bus"
	"printdeck/internal/klipper/extras/tmc"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
	"printdeck/internal/klipper/reactor"
)

const TMCFrequency = 13200000.

var Registers = map[string]int{
	"GCONF": 0x00, "GSTAT": 0x01, "IOIN": 0x04, "IHOLD_IRUN": 0x10,
	"TPOWERDOWN": 0x11, "TSTEP": 0x12, "TPWMTHRS": 0x13, "TCOOLTHRS": 0x14,
	"THIGH": 0x15, "XDIRECT": 0x2d, "MSLUT0": 0x60, "MSLUT1": 0x61,
	"MSLUT2": 0x62, "MSLUT3": 0x63, "MSLUT4": 0x64, "MSLUT5": 0x65,
	"MSLUT6": 0x66, "MSLUT7": 0x67, "MSLUTSEL": 0x68, "MSLUTSTART": 0x69,
	"MSCNT": 0x6a, "MSCURACT": 0x6b, "CHOPCONF": 0x6c, "COOLCONF": 0x6d,
	"DCCTRL": 0x6e, "DRV_STATUS": 0x6f, "PWMCONF": 0x70, "PWM_SCALE": 0x71,
	"ENCM_CTRL": 0x72, "LOST_STEPS": 0x73,
}

var ReadRegisters = []string{
	"GCONF", "GSTAT", "IOIN", "TSTEP", "XDIRECT", "MSCNT", "MSCURACT",
	"CHOPCONF", "DRV_STATUS", "PWM_SCALE", "LOST_STEPS",
}

// Fields returns the fields of the registers. Each driver gets its own copy.
func Fields() map[string]map[string]int64 {
	f := map[string]map[string]int64{}
	f["GCONF"] = map[string]int64{
		"i_scale_analog": 1 << 0, "internal_rsense": 1 << 1, "en_pwm_mode": 1 << 2,
		"enc_commutation": 1 << 3, "shaft": 1 << 4, "diag0_error": 1 << 5,
		"diag0_otpw": 1 << 6, "diag0_stall": 1 << 7, "diag1_stall": 1 << 8,
		"diag1_index": 1 << 9, "diag1_onstate": 1 << 10, "diag1_steps_skipped": 1 << 11,
		"diag0_int_pushpull": 1 << 12, "diag1_pushpull": 1 << 13,
		"small_hysteresis": 1 << 14, "stop_enable": 1 << 15, "direct_mode": 1 << 16,
		"test_mode": 1 << 17,
	}
	f["GSTAT"] = map[string]int64{"reset": 1 << 0, "drv_err": 1 << 1, "uv_cp": 1 << 2}
	f["IOIN"] = map[string]int64{
		"step": 1 << 0, "dir": 1 << 1, "dcen_cfg4": 1 << 2, "dcin_cfg5": 1 << 3,
		"drv_enn_cfg6": 1 << 4, "dco": 1 << 5, "version": 0xff << 24,
	}
	f["IHOLD_IRUN"] = map[string]int64{
		"ihold": 0x1f << 0, "irun": 0x1f << 8, "iholddelay": 0x0f << 16,
	}
	f["TPOWERDOWN"] = map[string]int64{"tpowerdown": 0xff}
	f["TSTEP"] = map[string]int64{"tstep": 0xfffff}
	f["TPWMTHRS"] = map[string]int64{"tpwmthrs": 0xfffff}
	f["TCOOLTHRS"] = map[string]int64{"tcoolthrs": 0xfffff}
	f["THIGH"] = map[string]int64{"thigh": 0xfffff}
	f["MSLUT0"] = map[string]int64{"mslut0": 0xffffffff}
	f["MSLUT1"] = map[string]int64{"mslut1": 0xffffffff}
	f["MSLUT2"] = map[string]int64{"mslut2": 0xffffffff}
	f["MSLUT3"] = map[string]int64{"mslut3": 0xffffffff}
	f["MSLUT4"] = map[string]int64{"mslut4": 0xffffffff}
	f["MSLUT5"] = map[string]int64{"mslut5": 0xffffffff}
	f["MSLUT6"] = map[string]int64{"mslut6": 0xffffffff}
	f["MSLUT7"] = map[string]int64{"mslut7": 0xffffffff}
	f["MSLUTSEL"] = map[string]int64{
		"x3": 0xFF << 24,
		"x2": 0xFF << 16,
		"x1": 0xFF << 8,
		"w3": 0x03 << 6,
		"w2": 0x03 << 4,
		"w1": 0x03 << 2,
		"w0": 0x03 << 0,
	}
	f["MSLUTSTART"] = map[string]int64{
		"start_sin":   0xFF << 0,
		"start_sin90": 0xFF << 16,
	}
	f["MSCNT"] = map[string]int64{"mscnt": 0x3ff}
	f["MSCURACT"] = map[string]int64{"cur_a": 0x1ff, "cur_b": 0x1ff << 16}
	f["CHOPCONF"] = map[string]int64{
		"toff": 0x0f, "hstrt": 0x07 << 4, "hend": 0x0f << 7, "fd3": 1 << 11,
		"disfdcc": 1 << 12, "rndtf": 1 << 13, "chm": 1 << 14, "tbl": 0x03 << 15,
		"vsense": 1 << 17, "vhighfs": 1 << 18, "vhighchm": 1 << 19, "sync": 0x0f << 20,
		"mres": 0x0f << 24, "intpol": 1 << 28, "dedge": 1 << 29, "diss2g": 1 << 30,
	}
	f["COOLCONF"] = map[string]int64{
		"semin": 0x0f, "seup": 0x03 << 5, "semax": 0x0f << 8, "sedn": 0x03 << 13,
		"seimin": 1 << 15, "sgt": 0x7f << 16, "sfilt": 1 << 24,
	}
	f["DRV_STATUS"] = map[string]int64{
		"sg_result": 0x3ff, "fsactive": 1 << 15, "cs_actual": 0x1f << 16,
		"stallguard": 1 << 24, "ot": 1 << 25, "otpw": 1 << 26, "s2ga": 1 << 27,
		"s2gb": 1 << 28, "ola": 1 << 29, "olb": 1 << 30, "stst": 1 << 31,
	}
	f["PWMCONF"] = map[string]int64{
		"pwm_ampl": 0xff, "pwm_grad": 0xff << 8, "pwm_freq": 0x03 << 16,
		"pwm_autoscale": 1 << 18, "pwm_symmetric": 1 << 19, "freewheel": 0x03 << 20,
	}
	f["PWM_SCALE"] = map[string]int64{"pwm_scale": 0xff}
	f["LOST_STEPS"] = map[string]int64{"lost_steps": 0xfffff}
	return f
}

var SignedFields = []string{"cur_a", "cur_b", "sgt"}

func flag(text string) func(int64) string {
	return func(v int64) string {
		if v != 0 {
			return text
		}
		return ""
	}
}

var FieldFormatters = map[string]func(int64) string{
	"i_scale_analog": flag("1(ExtVREF)"),
	"shaft":          flag("1(Reverse)"),
	"reset":          flag("1(Reset)"),
	"drv_err":        flag("1(ErrorShutdown!)"),
	"uv_cp":          flag("1(Undervoltage!)"),
	"version":        func(v int64) string { return fmt.Sprintf("%#x", v) },
	"mres":           func(v int64) string { return fmt.Sprintf("%d(%dusteps)", v, 0x100>>v) },
	"otpw":           flag("1(OvertempWarning!)"),
	"ot":             flag("1(OvertempError!)"),
	"s2ga":           flag("1(ShortToGND_A!)"),
	"s2gb":           flag("1(ShortToGND_B!)"),
	"ola":            flag("1(OpenLoad_A!)"),
	"olb":            flag("1(OpenLoad_B!)"),
	"cs_actual": func(v int64) string {
		if v != 0 {
			return fmt.Sprintf("%d", v)
		}
		return "0(Reset?)"
	},
}

// ######################################################################
// TMC stepper current config helper
// ######################################################################

const MaxCurrent = 2.000

// TMCCurrentHelper sets the currents of a TMC2130.
type TMCCurrentHelper struct {
	printer        *host.Printer
	name           string
	mcuTMC         tmc.MCUTMC
	fields         *tmc.FieldHelper
	reqHoldCurrent float64
	senseResistor  float64
}

func NewTMCCurrentHelper(config *configfile.Wrapper, mcuTMC tmc.MCUTMC) *TMCCurrentHelper {
	parts := strings.Fields(config.Name())
	c := &TMCCurrentHelper{printer: host.Of(config), name: parts[len(parts)-1], mcuTMC: mcuTMC,
		fields: mcuTMC.Fields()}
	runCurrent := config.GetFloat("run_current", configfile.Above(0.), configfile.Max(MaxCurrent))
	holdCurrent := config.GetFloat("hold_current", configfile.Default(MaxCurrent),
		configfile.Above(0.), configfile.Max(MaxCurrent))
	c.reqHoldCurrent = holdCurrent
	c.senseResistor = config.GetFloat("sense_resistor", configfile.Default(0.110), configfile.Above(0.))
	vsense, irun, ihold := c.calcCurrent(runCurrent, holdCurrent)
	c.fields.SetBoolField("vsense", vsense)
	c.fields.SetField("ihold", ihold)
	c.fields.SetField("irun", irun)
	return c
}

func (c *TMCCurrentHelper) calcCurrentBits(current float64, vsense bool) int64 {
	senseResistor := c.senseResistor + 0.020
	vref := 0.32
	if vsense {
		vref = 0.18
	}
	cs := int64(32.*senseResistor*current*math.Sqrt(2.)/vref+.5) - 1
	return max(0, min(31, cs))
}

func (c *TMCCurrentHelper) calcCurrentFromBits(cs int64, vsense bool) float64 {
	senseResistor := c.senseResistor + 0.020
	vref := 0.32
	if vsense {
		vref = 0.18
	}
	return float64(cs+1) * vref / (32. * senseResistor * math.Sqrt(2.))
}

func (c *TMCCurrentHelper) calcCurrent(runCurrent, holdCurrent float64) (bool, int64, int64) {
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
	ihold := c.calcCurrentBits(math.Min(holdCurrent, runCurrent), vsense)
	return vsense, irun, ihold
}

func (c *TMCCurrentHelper) Current() (float64, *float64, float64, float64) {
	irun := c.fields.Field("irun")
	ihold := c.fields.Field("ihold")
	vsense := c.fields.Field("vsense") != 0
	runCurrent := c.calcCurrentFromBits(irun, vsense)
	holdCurrent := c.calcCurrentFromBits(ihold, vsense)
	return runCurrent, &holdCurrent, c.reqHoldCurrent, MaxCurrent
}

func (c *TMCCurrentHelper) SetCurrent(runCurrent, holdCurrent, printTime float64) {
	c.reqHoldCurrent = holdCurrent
	vsense, irun, ihold := c.calcCurrent(runCurrent, holdCurrent)
	if vsense != (c.fields.Field("vsense") != 0) {
		val := c.fields.SetBoolField("vsense", vsense)
		c.mcuTMC.SetRegister("CHOPCONF", val, printTime)
	}
	c.fields.SetField("ihold", ihold)
	val := c.fields.SetField("irun", irun)
	c.mcuTMC.SetRegister("IHOLD_IRUN", val, printTime)
}

// ######################################################################
// TMC2130 SPI
// ######################################################################

// MCUTMCSPIChain is an SPI link, with one driver or a daisy chain of drivers.
type MCUTMCSPIChain struct {
	printer             *host.Printer
	chainLen            int
	mutex               *reactor.Mutex
	spi                 *bus.MCUSPI
	takenChainPositions []int
}

func NewMCUTMCSPIChain(config *configfile.Wrapper, chainLen int) *MCUTMCSPIChain {
	c := &MCUTMCSPIChain{printer: host.Of(config), chainLen: chainLen}
	c.mutex = c.printer.Reactor().NewMutex(false)
	share := ""
	if chainLen > 1 {
		share = "tmc_spi_cs"
	}
	c.spi = bus.MCUSPIFromConfig(config, 3, "cs_pin", 4000000, share, false)
	return c
}

func (c *MCUTMCSPIChain) buildCmd(data []byte, chainPos int) []byte {
	out := make([]byte, (c.chainLen-chainPos)*5, (c.chainLen)*5)
	out = append(out, data...)
	return append(out, make([]byte, (chainPos-1)*5)...)
}

func (c *MCUTMCSPIChain) batchMode() bool {
	_, ok := c.printer.StartArgs().Get("debugoutput")
	return ok
}

func (c *MCUTMCSPIChain) RegRead(reg, chainPos int) map[string]any {
	cmd := c.buildCmd([]byte{byte(reg), 0x00, 0x00, 0x00, 0x00}, chainPos)
	c.spi.SPISend(cmd, 0, 0)
	if c.batchMode() {
		return map[string]any{
			"spi_status":    int64(0),
			"data":          int64(0),
			"#receive_time": .0,
		}
	}
	params := c.spi.SPITransfer(cmd, 0, 0)
	pr := params.Bytes("response")
	pr = pr[(c.chainLen-chainPos)*5 : (c.chainLen-chainPos+1)*5]
	return map[string]any{
		"spi_status":    int64(pr[0]),
		"data":          int64(pr[1])<<24 | int64(pr[2])<<16 | int64(pr[3])<<8 | int64(pr[4]),
		"#receive_time": params.Float("#receive_time"),
	}
}

func (c *MCUTMCSPIChain) RegWrite(reg int, val int64, chainPos int, printTime float64) int64 {
	var minclock int64
	if !math.IsNaN(printTime) {
		minclock = c.spi.MCU().PrintTimeToClock(printTime)
	}
	data := []byte{byte((reg | 0x80) & 0xff), byte((val >> 24) & 0xff), byte((val >> 16) & 0xff),
		byte((val >> 8) & 0xff), byte(val & 0xff)}
	if c.batchMode() {
		c.spi.SPISend(c.buildCmd(data, chainPos), minclock, 0)
		return val
	}
	writeCmd := c.buildCmd(data, chainPos)
	dummyRead := c.buildCmd([]byte{0x00, 0x00, 0x00, 0x00, 0x00}, chainPos)
	params := c.spi.SPITransferWithPreface(writeCmd, dummyRead, minclock, 0)
	pr := params.Bytes("response")
	pr = pr[(c.chainLen-chainPos)*5 : (c.chainLen-chainPos+1)*5]
	return int64(pr[1])<<24 | int64(pr[2])<<16 | int64(pr[3])<<8 | int64(pr[4])
}

func (c *MCUTMCSPIChain) MCU() *mcu.MCU { return c.spi.MCU() }

// LookupTMCSPIChain sets up an SPI daisy chain bus from the settings in a config section.
func LookupTMCSPIChain(config *configfile.Wrapper) (*MCUTMCSPIChain, int) {
	chainLen, ok := config.GetIntOptional("chain_length", configfile.Default(nil), configfile.Min(2))
	if !ok {
		// Simple, non daisy chained SPI connection
		return NewMCUTMCSPIChain(config, 1), 1
	}

	// Shared SPI bus - lookup existing MCU_TMC_SPI_chain
	ppins := host.Of(config).LookupObject("pins").(*pins.Pins)
	csPinParams := ppins.LookupPin(config.Get("cs_pin"), false, false, "tmc_spi_cs")
	tmcSPI, _ := csPinParams.Class.(*MCUTMCSPIChain)
	if tmcSPI == nil {
		tmcSPI = NewMCUTMCSPIChain(config, chainLen)
		csPinParams.Class = tmcSPI
	}
	if chainLen != tmcSPI.chainLen {
		configfile.Raise("TMC SPI chain must have same length")
	}
	chainPos := config.GetInt("chain_position", configfile.Min(1), configfile.Max(float64(chainLen)))
	for _, p := range tmcSPI.takenChainPositions {
		if p == chainPos {
			configfile.Raise("TMC SPI chain can not have duplicate position")
		}
	}
	tmcSPI.takenChainPositions = append(tmcSPI.takenChainPositions, chainPos)
	return tmcSPI, chainPos
}

// MCUTMCSPI is the link to a driver on SPI.
type MCUTMCSPI struct {
	printer      *host.Printer
	name         string
	tmcSPI       *MCUTMCSPIChain
	chainPos     int
	mutex        *reactor.Mutex
	nameToReg    map[string]int
	fields       *tmc.FieldHelper
	tmcFrequency float64
}

func NewMCUTMCSPI(config *configfile.Wrapper, nameToReg map[string]int, fields *tmc.FieldHelper,
	tmcFrequency float64) *MCUTMCSPI {
	parts := strings.Fields(config.Name())
	s := &MCUTMCSPI{printer: host.Of(config), name: parts[len(parts)-1], nameToReg: nameToReg,
		fields: fields, tmcFrequency: tmcFrequency}
	s.tmcSPI, s.chainPos = LookupTMCSPIChain(config)
	s.mutex = s.tmcSPI.mutex
	return s
}

func (s *MCUTMCSPI) Fields() *tmc.FieldHelper  { return s.fields }
func (s *MCUTMCSPI) NameToReg() map[string]int { return s.nameToReg }

func (s *MCUTMCSPI) RegisterRaw(regName string) map[string]any {
	reg := s.nameToReg[regName]
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.tmcSPI.RegRead(reg, s.chainPos)
}

// DecodeSPIStatus returns the flags of the SPI status byte.
func DecodeSPIStatus(spiStatus int64) map[string]int64 {
	return map[string]int64{
		"standstill":   spiStatus >> 3 & 0x1,
		"sg2":          spiStatus >> 2 & 0x1,
		"driver_error": spiStatus >> 1 & 0x1,
		"reset_flag":   spiStatus & 0x1,
	}
}

func (s *MCUTMCSPI) Register(regName string) int64 {
	return s.RegisterRaw(regName)["data"].(int64)
}

func (s *MCUTMCSPI) SetRegister(regName string, val int64, printTime float64) {
	reg := s.nameToReg[regName]
	ok := func() bool {
		s.mutex.Lock()
		defer s.mutex.Unlock()
		for range 5 {
			v := s.tmcSPI.RegWrite(reg, val, s.chainPos, printTime)
			if v == val {
				return true
			}
		}
		return false
	}()
	if !ok {
		panic(gcode.Errorf("Unable to write tmc spi '%s' register %s", s.name, regName))
	}
}

func (s *MCUTMCSPI) TMCFrequency() float64 { return s.tmcFrequency }
func (s *MCUTMCSPI) MCU() *mcu.MCU         { return s.tmcSPI.MCU() }

// ######################################################################
// TMC2130 printer object
// ######################################################################

// TMC2130 is a [tmc2130] section.
type TMC2130 struct {
	fields    *tmc.FieldHelper
	mcuTMC    *MCUTMCSPI
	cmdhelper *tmc.TMCCommandHelper
}

func loadPrefix(config *configfile.Wrapper) any {
	t := &TMC2130{}
	// Setup mcu communication
	t.fields = tmc.NewFieldHelper(Fields(), SignedFields, FieldFormatters, nil)
	t.mcuTMC = NewMCUTMCSPI(config, Registers, t.fields, TMCFrequency)
	// Allow virtual pins to be created
	tmc.NewTMCVirtualPinHelper(config, t.mcuTMC)
	// Register commands
	currentHelper := NewTMCCurrentHelper(config, t.mcuTMC)
	t.cmdhelper = tmc.NewTMCCommandHelper(config, t.mcuTMC, currentHelper)
	t.cmdhelper.SetupRegisterDump(ReadRegisters, nil)
	// Setup basic register values
	tmc.TMCWaveTableHelper(config, t.mcuTMC)
	tmc.TMCStealthchopHelper(config, t.mcuTMC)
	tmc.TMCVcoolthrsHelper(config, t.mcuTMC)
	tmc.TMCVhighHelper(config, t.mcuTMC)
	// Allow other registers to be set from the config
	set := func(name string, def int64) { t.fields.SetConfigField(config, name, def) }
	// CHOPCONF
	set("toff", 4)
	set("hstrt", 0)
	set("hend", 7)
	set("tbl", 1)
	set("vhighfs", 0)
	set("vhighchm", 0)
	// COOLCONF
	set("semin", 0)
	set("seup", 0)
	set("semax", 0)
	set("sedn", 0)
	set("seimin", 0)
	set("sgt", 0)
	set("sfilt", 0)
	// IHOLDIRUN
	set("iholddelay", 8)
	// PWMCONF
	set("pwm_ampl", 128)
	set("pwm_grad", 4)
	set("pwm_freq", 1)
	set("pwm_autoscale", 1)
	set("freewheel", 0)
	// TPOWERDOWN
	set("tpowerdown", 0)
	return t
}

func init() { host.RegisterModule("tmc2130", nil, loadPrefix) }

// PhaseOffset returns the phase offset (nil for unknown) and the number of phases.
func (t *TMC2130) PhaseOffset() (*int64, int64) { return t.cmdhelper.PhaseOffset() }

func (t *TMC2130) Status(eventtime float64) map[string]any { return t.cmdhelper.Status(eventtime) }
