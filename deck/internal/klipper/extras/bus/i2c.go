// Ported to Go from klippy/extras/bus.py of Klipper (https://github.com/Klipper3d/klipper), commit
// 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018,2019  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package bus

import (
	"fmt"
	"log"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/pins"
	"printdeck/internal/klipper/serialhdl"
	"printdeck/internal/klipper/serialqueue"
)

// MCUI2C is an I2C device of a board (MCU_I2C).
type MCUI2C struct {
	mcu             *mcu.MCU
	bus             string
	i2cAddress      int
	oid             int
	speed           int
	asyncWriteOnly  bool
	configFmt       string
	configFmtTicks  string
	cmdQueue        *serialqueue.CommandQueue
	i2cWriteCmd     *mcu.CommandWrapper
	i2cReadCmd      *mcu.CommandQueryWrapper
	i2cTransferCmd  *mcu.CommandQueryWrapper
	i2cTransferFile *mcu.CommandWrapper // the i2c_transfer command in batch mode
	i2cWriteOnlyCmd *mcu.CommandWrapper
	toWrite         [][]byte
	configured      bool
}

// NewMCUI2C makes an I2C device. bus "" is the default bus; swPins are the scl and sda pins of a
// software bus, or nil.
func NewMCUI2C(m *mcu.MCU, bus string, addr, speed int, swPins []string, asyncWriteOnly bool) *MCUI2C {
	d := &MCUI2C{mcu: m, bus: bus, i2cAddress: addr, speed: speed, asyncWriteOnly: asyncWriteOnly}
	d.oid = m.CreateOID()
	m.AddConfigCmd(fmt.Sprintf("config_i2c oid=%d", d.oid), false, false)
	// Generate I2C bus config message
	if swPins != nil {
		d.configFmt = fmt.Sprintf("i2c_set_software_bus oid=%d"+
			" scl_pin=%s sda_pin=%s rate=%d address=%d", d.oid, swPins[0], swPins[1], speed, addr)
		d.configFmtTicks = fmt.Sprintf("i2c_set_sw_bus oid=%d"+
			" scl_pin=%s sda_pin=%s pulse_ticks=%%d address=%d", d.oid, swPins[0], swPins[1], addr)
	} else {
		d.configFmt = fmt.Sprintf("i2c_set_bus oid=%d i2c_bus=%%s rate=%d address=%d", d.oid, speed, addr)
	}
	d.cmdQueue = m.AllocCommandQueue()
	m.RegisterConfigCallback(d.buildConfig)
	m.Printer().RegisterEventHandler("klippy:connect", func(...any) { d.handleConnect() })
	return d
}

func (d *MCUI2C) handleConnect() {
	for _, data := range d.toWrite {
		d.I2CWrite(data, 0, 0, true)
	}
}

func (d *MCUI2C) OID() int                                { return d.oid }
func (d *MCUI2C) MCU() *mcu.MCU                           { return d.mcu }
func (d *MCUI2C) I2CAddress() int                         { return d.i2cAddress }
func (d *MCUI2C) CommandQueue() *serialqueue.CommandQueue { return d.cmdQueue }

func (d *MCUI2C) asyncWriteStatus(params serialhdl.Params) {
	status := fmt.Sprint(params["i2c_bus_status"])
	if status == "SUCCESS" {
		return
	}
	log.Printf("MCU '%s' I2C request to addr %d reports error %s", d.mcu.Name(), d.i2cAddress, status)
}

func (d *MCUI2C) buildConfig() {
	if strings.Contains(d.configFmt, "%") {
		bus := ResolveBusName(d.mcu, "i2c_bus", d.bus)
		d.configFmt = fmt.Sprintf(d.configFmt, bus)
	}
	if d.configFmtTicks != "" {
		if d.mcu.TryLookupCommand("i2c_set_sw_bus oid=%c scl_pin=%u sda_pin=%u pulse_ticks=%u address=%u") != nil {
			pulseTicks := d.mcu.SecondsToClock(1. / float64(d.speed) / 2)
			d.configFmt = fmt.Sprintf(d.configFmtTicks, pulseTicks)
		} else {
			pconfig := d.mcu.Printer().LookupObject("configfile").(mcuCodeDeprecator)
			pconfig.DeprecateMCUCode(d.mcu.Name(), "i2c_set_sw_bus", fmt.Sprint(d.mcu.Status(0)["mcu_version"]), "")
		}
	}
	d.mcu.AddConfigCmd(d.configFmt, false, false)
	if d.mcu.TryLookupCommand("i2c_read oid=%c reg=%*s read_len=%u") != nil {
		d.i2cWriteCmd = d.mcu.LookupCommand("i2c_write oid=%c data=%*s", d.cmdQueue)
		d.i2cWriteOnlyCmd = d.i2cWriteCmd
		d.i2cReadCmd = d.mcu.LookupQueryCommand("i2c_read oid=%c reg=%*s read_len=%u",
			"i2c_read_response oid=%c response=%*s", int64(d.oid), d.cmdQueue, false)
		pconfig := d.mcu.Printer().LookupObject("configfile").(mcuCodeDeprecator)
		pconfig.DeprecateMCUCode(d.mcu.Name(), "i2c_transfer", fmt.Sprint(d.mcu.Status(0)["mcu_version"]), "")
	} else {
		d.i2cTransferCmd = d.mcu.LookupQueryCommand("i2c_transfer oid=%c write=%*s read_len=%u",
			"i2c_response oid=%c i2c_bus_status=%c response=%*s", int64(d.oid), d.cmdQueue, false)
		d.i2cWriteOnlyCmd = d.mcu.LookupCommand("i2c_transfer oid=%c write=%*s read_len=%u", d.cmdQueue)
		if d.mcu.IsFileoutput() {
			d.i2cTransferFile = d.mcu.LookupCommand("i2c_transfer oid=%c write=%*s read_len=%u", d.cmdQueue)
		}
		if d.asyncWriteOnly {
			d.mcu.RegisterSerialResponse(d.asyncWriteStatus,
				"i2c_response oid=%c i2c_bus_status=%c response=%*s", int64(d.oid))
		}
	}
	d.configured = true
}

// I2CWriteNoack writes bytes, with no wait for the board.
func (d *MCUI2C) I2CWriteNoack(data []byte, minclock, reqclock int64) {
	if d.asyncWriteOnly {
		d.i2cWriteOnlyCmd.Send([]any{d.oid, data, 0}, minclock, reqclock)
		return
	}
	d.i2cWriteCmd.Send([]any{d.oid, data}, minclock, reqclock)
}

// I2CWrite writes bytes. Before the config, the write waits for the connect.
func (d *MCUI2C) I2CWrite(data []byte, minclock, reqclock int64, retry bool) {
	if !d.configured {
		d.toWrite = append(d.toWrite, data)
		return
	}
	if d.asyncWriteOnly {
		d.i2cWriteOnlyCmd.Send([]any{d.oid, data, 0}, minclock, reqclock)
		return
	}
	if d.i2cTransferCmd != nil {
		d.I2CTransfer(data, 0, minclock, reqclock, retry)
		return
	}
	d.i2cWriteCmd.SendWaitAck([]any{d.oid, data}, minclock, reqclock)
}

// I2CRead writes bytes and reads readLen bytes.
func (d *MCUI2C) I2CRead(write []byte, readLen int, retry bool) serialhdl.Params {
	if d.asyncWriteOnly {
		panic(&mcu.Error{Msg: "I2C dev is write only"})
	}
	if d.i2cTransferCmd != nil {
		return d.I2CTransfer(write, readLen, 0, 0, retry)
	}
	return d.i2cReadCmd.Send([]any{d.oid, write, readLen}, 0, 0, retry)
}

// I2CTransfer writes bytes and reads readLen bytes with i2c_transfer. In batch mode it returns
// nil.
func (d *MCUI2C) I2CTransfer(write []byte, readLen int, minclock, reqclock int64, retry bool) serialhdl.Params {
	if d.asyncWriteOnly {
		panic(&mcu.Error{Msg: "I2C dev is write only"})
	}
	if d.mcu.IsFileoutput() {
		d.i2cTransferFile.Send([]any{d.oid, write, readLen}, minclock, reqclock)
		return nil
	}
	param := d.i2cTransferCmd.Send([]any{d.oid, write, readLen}, minclock, reqclock, retry)
	status := fmt.Sprint(param["i2c_bus_status"])
	if status == "SUCCESS" {
		return param
	}
	errMsg := fmt.Sprintf("MCU '%s' I2C request to addr %d reports error %s", d.mcu.Name(), d.i2cAddress, status)
	d.mcu.Printer().InvokeShutdown(errMsg, nil)
	return nil
}

// MCUI2CFromConfig makes an I2C device from the options of a config section. defaultAddr < 0 makes
// i2c_address required.
func MCUI2CFromConfig(config *configfile.Wrapper, defaultAddr, defaultSpeed int, asyncWriteOnly bool) *MCUI2C {
	// Load bus parameters
	printer := host.Of(config)
	i2cMCU := mcu.Lookup(printer, config.Get("i2c_mcu", configfile.Default("mcu")))
	speed := config.GetInt("i2c_speed", configfile.Default(defaultSpeed), configfile.Min(100000))
	var addr int
	if defaultAddr < 0 {
		addr = config.GetInt("i2c_address", configfile.Min(0), configfile.Max(127))
	} else {
		addr = config.GetInt("i2c_address", configfile.Default(defaultAddr), configfile.Min(0), configfile.Max(127))
	}
	// Determine pin from config
	ppins := printer.LookupObject("pins").(*pins.Pins)
	var swPins []string
	bus := ""
	if _, ok := config.GetOptional("i2c_software_scl_pin", configfile.Default(nil)); ok {
		for _, name := range []string{"scl", "sda"} {
			option := fmt.Sprintf("i2c_software_%s_pin", name)
			p := ppins.LookupPin(config.Get(option), false, false, option)
			if p.Chip != pins.Chip(i2cMCU) {
				pinError("%s: i2c pins must be on same mcu", config.Name())
			}
			swPins = append(swPins, p.Pin)
		}
	} else {
		bus, _ = config.GetOptional("i2c_bus", configfile.Default(nil))
	}
	// Create MCU_I2C object
	return NewMCUI2C(i2cMCU, bus, addr, speed, swPins, asyncWriteOnly)
}
