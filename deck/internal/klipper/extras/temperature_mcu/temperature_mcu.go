// Package temperature_mcu is the temperature sensor of the chip of a board (temperature_mcu).
//
// Ported to Go from klippy/extras/temperature_mcu.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2020-2024  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package temperature_mcu

import (
	"fmt"
	"log"
	"sort"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/adc_temperature"
	"printdeck/internal/klipper/extras/heaters"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
	"printdeck/internal/klipper/serialhdl"
)

const (
	SampleTime      = 0.001
	SampleCount     = 8
	ReportTime      = 0.300
	RangeCheckCount = 4
)

// PrinterTemperatureMCU is a temperature_mcu sensor.
type PrinterTemperatureMCU struct {
	printer             *host.Printer
	baseTemperature     float64
	slope               float64
	temp1, adc1         *float64
	temp2, adc2         *float64
	minTemp, maxTemp    float64
	debugReadCmd        *mcu.CommandQueryWrapper
	mcuADC              *mcu.ADC
	diagHelper          *adc_temperature.HelperTemperatureDiagnostics
	temperatureCallback func(readTime, temp float64)
	mcuType             string
}

func newPrinterTemperatureMCU(config *configfile.Wrapper) heaters.Sensor {
	t := &PrinterTemperatureMCU{printer: host.Of(config)}
	// Read config
	mcuName := config.Get("sensor_mcu", configfile.Default("mcu"))
	if v, ok := config.GetFloatOptional("sensor_temperature1", configfile.Default(nil)); ok {
		t.temp1 = &v
		a1 := config.GetFloat("sensor_adc1", configfile.Min(0.), configfile.Max(1.))
		t.adc1 = &a1
		if v2, ok := config.GetFloatOptional("sensor_temperature2", configfile.Default(nil)); ok {
			t.temp2 = &v2
			a2 := config.GetFloat("sensor_adc2", configfile.Min(0.), configfile.Max(1.))
			t.adc2 = &a2
		}
	}
	// Setup ADC port
	ppins := t.printer.LookupObject("pins").(*pins.Pins)
	t.mcuADC = ppins.SetupPin("adc", fmt.Sprintf("%s:ADC_TEMPERATURE", mcuName)).(*mcu.ADC)
	t.mcuADC.SetupADCCallback(t.adcCallback)
	t.diagHelper = adc_temperature.NewHelperTemperatureDiagnostics(config, t.mcuADC, t.calcTemp)
	// Register callbacks
	if _, ok := t.printer.StartArgs().Get("debugoutput"); ok {
		t.mcuADC.SetupADCSample(ReportTime, SampleTime, SampleCount, 1, 0., 1., 0)
		return t
	}
	t.printer.RegisterEventHandler("klippy:mcu_identify", func(...any) { t.handleMCUIdentify() })
	return t
}

// Temperature interface

func (t *PrinterTemperatureMCU) SetupCallback(cb func(readTime, temp float64)) {
	t.temperatureCallback = cb
}
func (t *PrinterTemperatureMCU) ReportTimeDelta() float64 { return ReportTime }
func (t *PrinterTemperatureMCU) SetupMinMax(minTemp, maxTemp float64) {
	t.minTemp = minTemp
	t.maxTemp = maxTemp
}

// Internal code

func (t *PrinterTemperatureMCU) adcCallback(samples []mcu.Sample) {
	last := samples[len(samples)-1]
	temp := t.baseTemperature + last.Value*t.slope
	t.temperatureCallback(last.Time+SampleCount*SampleTime, temp)
}

func (t *PrinterTemperatureMCU) calcTemp(adc float64) float64 { return t.baseTemperature + adc*t.slope }
func (t *PrinterTemperatureMCU) calcADC(temp float64) float64 {
	return (temp - t.baseTemperature) / t.slope
}
func (t *PrinterTemperatureMCU) calcBase(temp, adc float64) float64 { return temp - adc*t.slope }

func (t *PrinterTemperatureMCU) handleMCUIdentify() {
	// Obtain mcu information
	m := t.mcuADC.MCU()
	t.debugReadCmd = m.LookupQueryCommand("debug_read order=%c addr=%u", "debug_result val=%u",
		serialhdl.NoOID, nil, false)
	t.mcuType, _ = m.Constants()["MCU"].(string)
	// Run MCU specific configuration
	cfgFuncs := []struct {
		name string
		fn   func()
	}{
		{"rp2", t.configRP2040},
		{"sam3", t.configSam3}, {"sam4", t.configSam4},
		{"same70", t.configSame70}, {"samd21", func() { t.configSamd21(0x00806030) }},
		{"samd51", t.configSamd51}, {"same5", t.configSamd51},
		{"stm32f1", t.configSTM32F1}, {"stm32f2", t.configSTM32F2},
		{"stm32f4", func() { t.configSTM32F4(0x1FFF7A2C, 0x1FFF7A2E) }},
		{"stm32f042", t.configSTM32F0x2},
		{"stm32f070", t.configSTM32F070},
		{"stm32f072", t.configSTM32F0x2},
		{"stm32g0", t.configSTM32G0},
		{"stm32g4", t.configSTM32G0},
		{"stm32l4", t.configSTM32G0},
		{"stm32h723", t.configSTM32H723},
		{"stm32h7", t.configSTM32H7},
		{"", t.configUnknown},
	}
	for _, c := range cfgFuncs {
		if strings.HasPrefix(t.mcuType, c.name) {
			c.fn()
			break
		}
	}
	log.Printf("mcu_temperature '%s' nominal base=%.6f slope=%.6f", m.Name(), t.baseTemperature, t.slope)
	// Setup manual base/slope override
	if t.temp1 != nil {
		if t.temp2 != nil {
			t.slope = (*t.temp2 - *t.temp1) / (*t.adc2 - *t.adc1)
		}
		t.baseTemperature = t.calcBase(*t.temp1, *t.adc1)
	}
	// Setup min/max checks
	arange := []float64{t.calcADC(t.minTemp), t.calcADC(t.maxTemp)}
	sort.Float64s(arange)
	minADC, maxADC := arange[0], arange[1]
	t.mcuADC.SetupADCSample(ReportTime, SampleTime, SampleCount, 1, minADC, maxADC, RangeCheckCount)
	t.diagHelper.SetupDiagMinMax(t.minTemp, t.maxTemp, minADC, maxADC)
}

func (t *PrinterTemperatureMCU) configUnknown() {
	configfile.Raise("MCU temperature not supported on %s", t.mcuType)
}

func (t *PrinterTemperatureMCU) configRP2040() {
	t.slope = 3.3 / -0.001721
	t.baseTemperature = t.calcBase(27., 0.706/3.3)
}

func (t *PrinterTemperatureMCU) configSam3() {
	t.slope = 3.3 / .002650
	t.baseTemperature = t.calcBase(27., 0.8/3.3)
}

func (t *PrinterTemperatureMCU) configSam4() {
	t.slope = 3.3 / .004700
	t.baseTemperature = t.calcBase(27., 1.44/3.3)
}

func (t *PrinterTemperatureMCU) configSame70() {
	t.slope = 3.3 / .002330
	t.baseTemperature = t.calcBase(25., 0.72/3.3)
}

func (t *PrinterTemperatureMCU) configSamd21(addr int64) {
	get1v := func(val int64) float64 {
		if val&0x80 != 0 {
			val = 0x100 - val
		}
		return 1. - float64(val)/1000.
	}
	cal1 := t.read32(addr)
	cal2 := t.read32(addr + 4)
	roomTemp := float64((cal1>>0)&0xff) + float64((cal1>>8)&0xf)/10.
	hotTemp := float64((cal1>>12)&0xff) + float64((cal1>>20)&0xf)/10.
	room1v := get1v((cal1 >> 24) & 0xff)
	hot1v := get1v((cal2 >> 0) & 0xff)
	roomADC := float64((cal2>>8)&0xfff) * room1v / (3.3 * 4095.)
	hotADC := float64((cal2>>20)&0xfff) * hot1v / (3.3 * 4095.)
	t.slope = (hotTemp - roomTemp) / (hotADC - roomADC)
	t.baseTemperature = t.calcBase(roomTemp, roomADC)
}

func (t *PrinterTemperatureMCU) configSamd51() { t.configSamd21(0x00800100) }

func (t *PrinterTemperatureMCU) configSTM32F1() {
	t.slope = 3.3 / -.004300
	t.baseTemperature = t.calcBase(25., 1.43/3.3)
}

func (t *PrinterTemperatureMCU) configSTM32F2() {
	t.slope = 3.3 / .002500
	t.baseTemperature = t.calcBase(25., .76/3.3)
}

func (t *PrinterTemperatureMCU) configSTM32F4(addr1, addr2 int64) {
	calADC30 := float64(t.read16(addr1)) / 4095.
	calADC110 := float64(t.read16(addr2)) / 4095.
	t.slope = (110. - 30.) / (calADC110 - calADC30)
	t.baseTemperature = t.calcBase(30., calADC30)
}

func (t *PrinterTemperatureMCU) configSTM32F0x2() { t.configSTM32F4(0x1FFFF7B8, 0x1FFFF7C2) }

func (t *PrinterTemperatureMCU) configSTM32F070() {
	t.slope = 3.3 / -.004300
	calADC30 := float64(t.read16(0x1FFFF7B8)) / 4095.
	t.baseTemperature = t.calcBase(30., calADC30)
}

func (t *PrinterTemperatureMCU) configSTM32G0() {
	calADC30 := float64(t.read16(0x1FFF75A8)) * 3.0 / (3.3 * 4095.)
	calADC130 := float64(t.read16(0x1FFF75CA)) * 3.0 / (3.3 * 4095.)
	t.slope = (130. - 30.) / (calADC130 - calADC30)
	t.baseTemperature = t.calcBase(30., calADC30)
}

func (t *PrinterTemperatureMCU) configSTM32H723() {
	calADC30 := float64(t.read16(0x1FF1E820)) / 4095.
	calADC130 := float64(t.read16(0x1FF1E840)) / 4095.
	t.slope = (130. - 30.) / (calADC130 - calADC30)
	t.baseTemperature = t.calcBase(30., calADC30)
}

func (t *PrinterTemperatureMCU) configSTM32H7() {
	calADC30 := float64(t.read16(0x1FF1E820)) / 65535.
	calADC110 := float64(t.read16(0x1FF1E840)) / 65535.
	t.slope = (110. - 30.) / (calADC110 - calADC30)
	t.baseTemperature = t.calcBase(30., calADC30)
}

func (t *PrinterTemperatureMCU) read16(addr int64) int64 {
	params := t.debugReadCmd.Send([]any{1, addr}, 0, 0, true)
	return params.Int("val")
}

func (t *PrinterTemperatureMCU) read32(addr int64) int64 {
	params := t.debugReadCmd.Send([]any{2, addr}, 0, 0, true)
	return params.Int("val")
}

func load(config *configfile.Wrapper) any {
	pheaters := heaters.Load(config)
	pheaters.AddSensorFactory("temperature_mcu", newPrinterTemperatureMCU)
	return nil
}

func init() { host.RegisterModule("temperature_mcu", load, nil) }
