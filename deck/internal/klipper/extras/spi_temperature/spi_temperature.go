// Package spi_temperature has the common SPI thermocouple and RTD temperature sensors: MAX6675,
// MAX31855, MAX31856 and MAX31865.
//
// Ported to Go from klippy/extras/spi_temperature.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018  Petri Honkala <cruwaller@gmail.com>
//	Copyright (C) 2018  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package spi_temperature

import (
	"fmt"
	"log"
	"math"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/bus"
	"printdeck/internal/klipper/extras/heaters"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/serialhdl"
)

// ######################################################################
// SensorBase
// ######################################################################

const (
	ReportTime      = 0.300
	MaxInvalidCount = 3
)

// chip is the conversion of one sensor type.
type chip interface {
	calcTemp(adc int64) float64
	calcADC(temp float64) int64
	handleFault(adc, fault int64)
}

// SensorBase is the SPI link and the reports of a sensor.
type SensorBase struct {
	printer        *host.Printer
	chipType       string
	chip           chip
	callback       func(readTime, temp float64)
	minSampleValue int64
	maxSampleValue int64
	reportClock    int64
	spi            *bus.MCUSPI
	mcu            *mcu.MCU
	oid            int
}

func newSensorBase(config *configfile.Wrapper, chipType string, c chip, configCmd []byte, spiMode int) *SensorBase {
	s := &SensorBase{printer: host.Of(config), chipType: chipType, chip: c}
	s.spi = bus.MCUSPIFromConfig(config, spiMode, "sensor_pin", 4000000, "", false)
	if configCmd != nil {
		s.spi.SPISend(configCmd, 0, 0)
	}
	s.mcu = s.spi.MCU()
	// Reader chip configuration
	s.oid = s.mcu.CreateOID()
	s.mcu.RegisterSerialResponse(s.handleSPIResponse,
		"thermocouple_result oid=%c next_clock=%u value=%u fault=%c", int64(s.oid))
	s.mcu.RegisterConfigCallback(s.buildConfig)
	return s
}

func (s *SensorBase) SetupMinMax(minTemp, maxTemp float64) {
	a, b := s.chip.calcADC(minTemp), s.chip.calcADC(maxTemp)
	s.minSampleValue = min(a, b)
	s.maxSampleValue = max(a, b)
}

func (s *SensorBase) SetupCallback(cb func(readTime, temp float64)) { s.callback = cb }
func (s *SensorBase) ReportTimeDelta() float64                      { return ReportTime }

func (s *SensorBase) buildConfig() {
	s.mcu.AddConfigCmd(fmt.Sprintf("config_thermocouple oid=%d spi_oid=%d thermocouple_type=%s",
		s.oid, s.spi.OID(), s.chipType), false, false)
	clock := s.mcu.GetQuerySlot(s.oid)
	s.reportClock = s.mcu.SecondsToClock(ReportTime)
	s.mcu.AddConfigCmd(fmt.Sprintf("query_thermocouple oid=%d clock=%d rest_ticks=%d"+
		" min_value=%d max_value=%d max_invalid_count=%d", s.oid, clock, s.reportClock,
		s.minSampleValue, s.maxSampleValue, MaxInvalidCount), true, false)
}

func (s *SensorBase) handleSPIResponse(params serialhdl.Params) {
	if params.Int("fault") != 0 {
		s.chip.handleFault(params.Int("value"), params.Int("fault"))
		return
	}
	temp := s.chip.calcTemp(params.Int("value"))
	nextClock := s.mcu.Clock32ToClock64(uint32(params.Int("next_clock")))
	lastReadClock := nextClock - s.reportClock
	lastReadTime := s.mcu.ClockToPrintTime(float64(lastReadClock))
	s.callback(lastReadTime, temp)
}

func reportFault(msg string) { log.Printf("%s", msg) }

func clampInt(v, lo, hi int64) int64 { return max(lo, min(hi, v)) }

// ######################################################################
// MAX31856 thermocouple
// ######################################################################

const (
	max31856CR0Reg         = 0x00
	max31856CR0Autoconvert = 0x80
	max31856CR0Filt50Hz    = 0x01

	max31856CR1AvgSel1  = 0x00
	max31856CR1AvgSel2  = 0x10
	max31856CR1AvgSel4  = 0x20
	max31856CR1AvgSel8  = 0x30
	max31856CR1AvgSel16 = 0x70

	max31856MaskVoltageUnderOverFault = 0x02
	max31856MaskThermocoupleOpenFault = 0x01

	max31856FaultCJRange = 0x80 // Cold Junction out of range
	max31856FaultTCRange = 0x40 // Thermocouple out of range
	max31856FaultCJHigh  = 0x20 // Cold Junction High
	max31856FaultCJLow   = 0x10 // Cold Junction Low
	max31856FaultTCHigh  = 0x08 // Thermocouple Low
	max31856FaultTCLow   = 0x04 // Thermocouple Low
	max31856FaultOVUV    = 0x02 // Under Over Voltage
	max31856FaultOpen    = 0x01

	max31856Scale = 5
	max31856Mult  = 0.0078125
)

type max31856 struct{}

func (max31856) handleFault(adc, fault int64) {
	if fault&max31856FaultCJRange != 0 {
		reportFault("Max31856: Cold Junction Range Fault")
	}
	if fault&max31856FaultTCRange != 0 {
		reportFault("Max31856: Thermocouple Range Fault")
	}
	if fault&max31856FaultCJHigh != 0 {
		reportFault("Max31856: Cold Junction High Fault")
	}
	if fault&max31856FaultCJLow != 0 {
		reportFault("Max31856: Cold Junction Low Fault")
	}
	if fault&max31856FaultTCHigh != 0 {
		reportFault("Max31856: Thermocouple High Fault")
	}
	if fault&max31856FaultTCLow != 0 {
		reportFault("Max31856: Thermocouple Low Fault")
	}
	if fault&max31856FaultOVUV != 0 {
		reportFault("Max31856: Over/Under Voltage Fault")
	}
	if fault&max31856FaultOpen != 0 {
		reportFault("Max31856: Thermocouple Open Fault")
	}
}

func (max31856) calcTemp(adc int64) float64 {
	adc = adc >> max31856Scale
	// Fix sign bit:
	if adc&0x40000 != 0 {
		adc = ((adc & 0x3FFFF) + 1) * -1
	}
	return max31856Mult * float64(adc)
}

func (max31856) calcADC(temp float64) int64 {
	adc := int64(temp/max31856Mult + 0.5) // convert to ADC value
	return clampInt(adc, 0, 0x3FFFF) << max31856Scale
}

func max31856SPIInit(config *configfile.Wrapper) []byte {
	var cmds []byte
	value := byte(max31856CR0Autoconvert)
	if config.GetBool("tc_use_50Hz_filter", configfile.Default(false)) {
		value |= max31856CR0Filt50Hz
	}
	cmds = append(cmds, 0x80+max31856CR0Reg, value)

	types := map[string]any{"B": 0b0000, "E": 0b0001, "J": 0b0010, "K": 0b0011, "N": 0b0100,
		"R": 0b0101, "S": 0b0110, "T": 0b0111}
	v := config.GetChoice("tc_type", types, configfile.Default("K")).(int)
	averages := map[int]any{1: max31856CR1AvgSel1, 2: max31856CR1AvgSel2, 4: max31856CR1AvgSel4,
		8: max31856CR1AvgSel8, 16: max31856CR1AvgSel16}
	v |= config.GetIntChoice("tc_averaging_count", averages, configfile.Default(1)).(int)
	cmds = append(cmds, byte(v))
	cmds = append(cmds, max31856MaskVoltageUnderOverFault|max31856MaskThermocoupleOpenFault)
	return cmds
}

// ######################################################################
// MAX31855 thermocouple
// ######################################################################

const (
	max31855Scale = 18
	max31855Mult  = 0.25
)

type max31855 struct{}

func (max31855) handleFault(adc, fault int64) {
	if fault&0x1 != 0 {
		reportFault("MAX31855 : Open Circuit")
	}
	if fault&0x2 != 0 {
		reportFault("MAX31855 : Short to GND")
	}
	if fault&0x4 != 0 {
		reportFault("MAX31855 : Short to Vcc")
	}
}

func (max31855) calcTemp(adc int64) float64 {
	adc = adc >> max31855Scale
	// Fix sign bit:
	if adc&0x2000 != 0 {
		adc = ((adc & 0x1FFF) + 1) * -1
	}
	return max31855Mult * float64(adc)
}

func (max31855) calcADC(temp float64) int64 {
	adc := int64(temp/max31855Mult + 0.5) // convert to ADC value
	return clampInt(adc, 0, 0x1FFF) << max31855Scale
}

// ######################################################################
// MAX6675 thermocouple
// ######################################################################

const (
	max6675Scale = 3
	max6675Mult  = 0.25
)

type max6675 struct{}

func (max6675) handleFault(adc, fault int64) {
	if fault&0x02 != 0 {
		reportFault("Max6675 : Device ID error")
	}
	if fault&0x04 != 0 {
		reportFault("Max6675 : Thermocouple Open Fault")
	}
}

func (max6675) calcTemp(adc int64) float64 {
	adc = adc >> max6675Scale
	// Fix sign bit:
	if adc&0x2000 != 0 {
		adc = ((adc & 0x1FFF) + 1) * -1
	}
	return max6675Mult * float64(adc)
}

func (max6675) calcADC(temp float64) int64 {
	adc := int64(temp/max6675Mult + 0.5) // convert to ADC value
	return clampInt(adc, 0, 0x1FFF) << max6675Scale
}

// ######################################################################
// MAX31865 (RTD sensor)
// ######################################################################

const (
	max31865ConfigReg        = 0x00
	max31865ConfigBias       = 0x80
	max31865ConfigModeAuto   = 0x40
	max31865Config3Wire      = 0x10
	max31865ConfigFaultClear = 0x02
	max31865ConfigFilt50Hz   = 0x01

	max31865ADCMax = 1 << 15

	// Callendar-Van Dusen constants for platinum resistance thermometers (RTD)
	cvdA = 3.9083e-3
	cvdB = -5.775e-7
)

type max31865 struct {
	adcToResistDivNominal float64
	configReg             []byte
	base                  *SensorBase
}

func (m *max31865) handleFault(adc, fault int64) {
	if fault&0x80 != 0 {
		reportFault("Max31865 RTD input is disconnected")
	}
	if fault&0x40 != 0 {
		reportFault("Max31865 RTD input is shorted")
	}
	if fault&0x20 != 0 {
		reportFault("Max31865 VREF- is greater than 0.85 * VBIAS, FORCE- open")
	}
	if fault&0x10 != 0 {
		reportFault("Max31865 VREF- is less than 0.85 * VBIAS, FORCE- open")
	}
	if fault&0x08 != 0 {
		reportFault("Max31865 VRTD- is less than 0.85 * VBIAS, FORCE- open")
	}
	if fault&0x04 != 0 {
		reportFault("Max31865 Overvoltage or undervoltage fault")
	}
	if fault&0xfc == 0 {
		reportFault("Max31865 Unspecified error")
	}
	// Attempt to clear the fault
	m.base.spi.SPISend(m.configReg, 0, 0)
}

func (m *max31865) calcTemp(adc int64) float64 {
	adc = adc >> 1 // remove fault bit
	rDivNominal := float64(adc) * m.adcToResistDivNominal
	// Resistance (relative to rtd_nominal_r) is calculated using:
	//  R_div_nominal = 1. + CVD_A * temp + CVD_B * temp**2
	// Solve for temp using quadratic equation:
	//  temp = (-b +- sqrt(b**2 - 4ac)) / 2a
	discriminant := math.Sqrt(cvdA*cvdA - 4.*cvdB*(1.-rDivNominal))
	return (-cvdA + discriminant) / (2. * cvdB)
}

func (m *max31865) calcADC(temp float64) int64 {
	// Calculate relative resistance via Callendar-Van Dusen formula:
	//  resistance = rtd_nominal_r * (1 + CVD_A * temp + CVD_B * temp**2)
	temp = math.Min(temp, 1768.3) // Melting point of platinum
	rDivNominal := 1. + cvdA*temp + cvdB*temp*temp
	adc := int64(rDivNominal/m.adcToResistDivNominal + 0.5)
	adc = clampInt(adc, 0, max31865ADCMax-1)
	return adc << 1 // Add fault bit
}

func newMAX31865(config *configfile.Wrapper) heaters.Sensor {
	rtdNominalR := config.GetFloat("rtd_nominal_r", configfile.Default(100.), configfile.Above(0.))
	rtdReferenceR := config.GetFloat("rtd_reference_r", configfile.Default(430.), configfile.Above(0.))
	adcToResist := rtdReferenceR / float64(max31865ADCMax)
	m := &max31865{adcToResistDivNominal: adcToResist / rtdNominalR}
	value := byte(max31865ConfigBias | max31865ConfigModeAuto | max31865ConfigFaultClear)
	if config.GetBool("rtd_use_50Hz_filter", configfile.Default(false)) {
		value |= max31865ConfigFilt50Hz
	}
	if config.GetInt("rtd_num_of_wires", configfile.Default(2)) == 3 {
		value |= max31865Config3Wire
	}
	m.configReg = []byte{0x80 + max31865ConfigReg, value}
	m.base = newSensorBase(config, "MAX31865", m, m.configReg, 1)
	return m.base
}

// ######################################################################
// Sensor registration
// ######################################################################

func load(config *configfile.Wrapper) any {
	// Register sensors
	pheaters := heaters.Load(config)
	pheaters.AddSensorFactory("MAX6675", func(c *configfile.Wrapper) heaters.Sensor {
		return newSensorBase(c, "MAX6675", max6675{}, nil, 0)
	})
	pheaters.AddSensorFactory("MAX31855", func(c *configfile.Wrapper) heaters.Sensor {
		return newSensorBase(c, "MAX31855", max31855{}, nil, 0)
	})
	pheaters.AddSensorFactory("MAX31856", func(c *configfile.Wrapper) heaters.Sensor {
		return newSensorBase(c, "MAX31856", max31856{}, max31856SPIInit(c), 1)
	})
	pheaters.AddSensorFactory("MAX31865", newMAX31865)
	return nil
}

func init() { host.RegisterModule("spi_temperature", load, nil) }
