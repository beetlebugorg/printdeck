// Package bus has the helpers of the SPI devices of a board: the bus name, the config commands,
// and the spi_send and spi_transfer commands.
//
// Ported to Go from klippy/extras/bus.py of Klipper (https://github.com/Klipper3d/klipper), commit
// 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f. The I2C part and MCU_bus_digital_out are not ported
// yet.
//
//	Copyright (C) 2018,2019  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package bus

import (
	"fmt"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
	"printdeck/internal/klipper/serialhdl"
	"printdeck/internal/klipper/serialqueue"
)

func pinError(format string, args ...any) { panic(&pins.Error{Msg: fmt.Sprintf(format, args...)}) }

// ResolveBusName returns the bus name of an enumeration of the board, and reserves the pins of the
// bus. An empty bus is the bus with the value 0.
func ResolveBusName(m *mcu.MCU, param, bus string) string {
	// Find enumerations for the given bus
	enumerations := m.Enumerations()
	enums, ok := enumerations[param]
	if !ok {
		enums, ok = enumerations["bus"]
	}
	if !ok {
		if bus == "" {
			return "0"
		}
		return bus
	}
	// Verify bus is a valid enumeration
	ppins := m.Printer().LookupObject("pins").(*pins.Pins)
	if bus == "" {
		found := false
		for k, v := range enums {
			if v == 0 {
				bus, found = k, true
				break
			}
		}
		if !found {
			pinError("Must specify %s on mcu '%s'", param, m.Name())
		}
	}
	if _, ok := enums[bus]; !ok {
		pinError("Unknown %s '%s'", param, bus)
	}
	// Check for reserved bus pins
	resolver := ppins.PinResolver(m.Name())
	if reservePins, ok := m.Constants()["BUS_PINS_"+bus]; ok {
		for _, pin := range strings.Split(fmt.Sprint(reservePins), ",") {
			resolver.ReservePin(pin, bus)
		}
	}
	return bus
}

// SPI

// MCUSPI is a device on an SPI bus of a board (MCU_SPI).
type MCUSPI struct {
	mcu            *mcu.MCU
	bus            string
	speed          int
	oid            int
	configFmt      string
	configFmtTicks string
	cmdQueue       *serialqueue.CommandQueue
	spiSendCmd     *mcu.CommandWrapper
	spiTransferCmd *mcu.CommandQueryWrapper
}

// NewMCUSPI makes an SPI device. pin "" is a device with no chip select. swPins are the miso,
// mosi and sclk pins of a software bus, or nil.
func NewMCUSPI(m *mcu.MCU, bus, pin string, mode, speed int, swPins []string, csActiveHigh bool) *MCUSPI {
	s := &MCUSPI{mcu: m, bus: bus, speed: speed}
	// Config SPI object (set all CS pins high before spi_set_bus commands)
	s.oid = m.CreateOID()
	if pin == "" {
		m.AddConfigCmd(fmt.Sprintf("config_spi_without_cs oid=%d", s.oid), false, false)
	} else {
		active := 0
		if csActiveHigh {
			active = 1
		}
		m.AddConfigCmd(fmt.Sprintf("config_spi oid=%d pin=%s cs_active_high=%d", s.oid, pin, active), false, false)
	}
	// Generate SPI bus config message
	if swPins != nil {
		s.configFmt = fmt.Sprintf("spi_set_software_bus oid=%d miso_pin=%s mosi_pin=%s sclk_pin=%s mode=%d rate=%d",
			s.oid, swPins[0], swPins[1], swPins[2], mode, speed)
		s.configFmtTicks = fmt.Sprintf("spi_set_sw_bus oid=%d miso_pin=%s mosi_pin=%s sclk_pin=%s mode=%d pulse_ticks=%%d",
			s.oid, swPins[0], swPins[1], swPins[2], mode)
	} else {
		s.configFmt = fmt.Sprintf("spi_set_bus oid=%d spi_bus=%%s mode=%d rate=%d", s.oid, mode, speed)
	}
	s.cmdQueue = m.AllocCommandQueue()
	m.RegisterConfigCallback(s.buildConfig)
	return s
}

type mcuCodeDeprecator interface {
	DeprecateMCUCode(mcuName, feature, mcuVersion, msg string)
}

// SetupShutdownMsg sets the bytes that the board sends to the device at a shutdown.
func (s *MCUSPI) SetupShutdownMsg(shutdownSeq []byte) {
	s.mcu.AddConfigCmd(fmt.Sprintf("config_spi_shutdown oid=%d spi_oid=%d shutdown_msg=%x",
		s.mcu.CreateOID(), s.oid, shutdownSeq), false, false)
}

func (s *MCUSPI) OID() int                                { return s.oid }
func (s *MCUSPI) MCU() *mcu.MCU                           { return s.mcu }
func (s *MCUSPI) CommandQueue() *serialqueue.CommandQueue { return s.cmdQueue }

func (s *MCUSPI) buildConfig() {
	if strings.Contains(s.configFmt, "%") {
		bus := ResolveBusName(s.mcu, "spi_bus", s.bus)
		s.configFmt = fmt.Sprintf(s.configFmt, bus)
	}
	if s.configFmtTicks != "" {
		if s.mcu.TryLookupCommand("spi_set_sw_bus oid=%c miso_pin=%u mosi_pin=%u sclk_pin=%u mode=%u pulse_ticks=%u") != nil {
			pulseTicks := s.mcu.SecondsToClock(1. / float64(s.speed))
			s.configFmt = fmt.Sprintf(s.configFmtTicks, pulseTicks)
		} else {
			pconfig := s.mcu.Printer().LookupObject("configfile").(mcuCodeDeprecator)
			pconfig.DeprecateMCUCode(s.mcu.Name(), "spi_set_sw_bus", fmt.Sprint(s.mcu.Status(0)["mcu_version"]), "")
		}
	}
	s.mcu.AddConfigCmd(s.configFmt, false, false)
	s.spiSendCmd = s.mcu.LookupCommand("spi_send oid=%c data=%*s", s.cmdQueue)
	s.spiTransferCmd = s.mcu.LookupQueryCommand("spi_transfer oid=%c data=%*s",
		"spi_transfer_response oid=%c response=%*s", int64(s.oid), s.cmdQueue, false)
}

// SPISend sends bytes to the device. Before the config, it is an init command.
func (s *MCUSPI) SPISend(data []byte, minclock, reqclock int64) {
	if s.spiSendCmd == nil {
		// Send setup message via mcu initialization
		s.mcu.AddConfigCmd(fmt.Sprintf("spi_send oid=%d data=%x", s.oid, data), true, false)
		return
	}
	s.spiSendCmd.Send([]any{s.oid, data}, minclock, reqclock)
}

// SPITransfer sends bytes and returns the response of the device.
func (s *MCUSPI) SPITransfer(data []byte, minclock, reqclock int64) serialhdl.Params {
	return s.spiTransferCmd.Send([]any{s.oid, data}, minclock, reqclock, true)
}

// SPITransferWithPreface sends a preface and then a transfer, in one block.
func (s *MCUSPI) SPITransferWithPreface(prefaceData, data []byte, minclock, reqclock int64) serialhdl.Params {
	return s.spiTransferCmd.SendWithPreface(s.spiSendCmd, []any{s.oid, prefaceData}, []any{s.oid, data},
		minclock, reqclock, true)
}

// MCUSPIFromConfig makes an SPI device from the options of a config section. pinOption is the
// option of the chip select pin, and shareType the share type of the pin ("" for none).
func MCUSPIFromConfig(config *configfile.Wrapper, mode int, pinOption string, defaultSpeed int,
	shareType string, csActiveHigh bool) *MCUSPI {
	// Determine pin from config
	ppins := host.Of(config).LookupObject("pins").(*pins.Pins)
	csPin := config.Get(pinOption)
	csParams := ppins.LookupPin(csPin, false, false, shareType)
	pin := csParams.Pin
	if pin == "None" {
		ppins.ResetPinSharing(csParams)
		pin = ""
	}
	// Load bus parameters
	m, ok := csParams.Chip.(*mcu.MCU)
	if !ok {
		pinError("%s: spi pins must be on a board", config.Name())
	}
	speed := config.GetInt("spi_speed", configfile.Default(defaultSpeed), configfile.Min(100000))
	var swPins []string
	bus := ""
	if _, ok := config.GetOptional("spi_software_sclk_pin", configfile.Default(nil)); ok {
		for _, name := range []string{"miso", "mosi", "sclk"} {
			option := "spi_software_" + name + "_pin"
			params := ppins.LookupPin(config.Get(option), false, false, option)
			if params.Chip != csParams.Chip {
				pinError("%s: spi pins must be on same mcu", config.Name())
			}
			swPins = append(swPins, params.Pin)
		}
	} else {
		bus, _ = config.GetOptional("spi_bus", configfile.Default(nil))
	}
	// Create MCU_SPI object
	return NewMCUSPI(m, bus, pin, mode, speed, swPins, csActiveHigh)
}

// MCUBusDigitalOut is a digital output pin that changes on a command queue (MCU_bus_digital_out).
type MCUBusDigitalOut struct {
	mcu          *mcu.MCU
	oid          int
	cmdQueue     *serialqueue.CommandQueue
	updatePinCmd *mcu.CommandWrapper
}

// NewMCUBusDigitalOut makes the pin. A nil cmdQueue is a new queue.
func NewMCUBusDigitalOut(m *mcu.MCU, pinDesc string, cmdQueue *serialqueue.CommandQueue, value int) *MCUBusDigitalOut {
	d := &MCUBusDigitalOut{mcu: m, oid: m.CreateOID()}
	ppins := m.Printer().LookupObject("pins").(*pins.Pins)
	params := ppins.LookupPin(pinDesc, false, false, "")
	if params.Chip != pins.Chip(m) {
		pinError("Pin %s must be on mcu %s", pinDesc, m.Name())
	}
	m.AddConfigCmd(fmt.Sprintf("config_digital_out oid=%d pin=%s value=%d default_value=%d max_duration=%d",
		d.oid, params.Pin, value, value, 0), false, false)
	m.RegisterConfigCallback(d.buildConfig)
	if cmdQueue == nil {
		cmdQueue = m.AllocCommandQueue()
	}
	d.cmdQueue = cmdQueue
	return d
}

func (d *MCUBusDigitalOut) OID() int                                { return d.oid }
func (d *MCUBusDigitalOut) MCU() *mcu.MCU                           { return d.mcu }
func (d *MCUBusDigitalOut) CommandQueue() *serialqueue.CommandQueue { return d.cmdQueue }

func (d *MCUBusDigitalOut) buildConfig() {
	d.updatePinCmd = d.mcu.LookupCommand("update_digital_out oid=%c value=%c", d.cmdQueue)
}

// UpdateDigitalOut sets the pin. Before the config, it is a config command, with the "%c" format
// of Klipper's code.
func (d *MCUBusDigitalOut) UpdateDigitalOut(value bool, minclock, reqclock int64) {
	v := 0
	if value {
		v = 1
	}
	if d.updatePinCmd == nil {
		// Send setup message via mcu initialization
		d.mcu.AddConfigCmd(fmt.Sprintf("update_digital_out oid=%c value=%c", rune(d.oid), rune(v)), false, false)
		return
	}
	d.updatePinCmd.Send([]any{d.oid, v}, minclock, reqclock)
}
