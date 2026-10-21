package main

import (
	"fmt"
	"log"
	"os"

	"printdeck/internal/board"
	"printdeck/internal/klipper/cli"
)

// boardList probes each USB serial port for a board with Klipper firmware, and prints what it
// finds. Stop the Klipper host first: the port has one user at a time.
//
//	printdeck board
func boardList() {
	ports := board.Ports()
	if len(ports) == 0 {
		fmt.Println("no USB serial ports")
		return
	}
	for _, p := range ports {
		i := board.Probe(p, board.Baud)
		fmt.Printf("%s\n  name: %s\n", i.Device, i.Name)
		if !i.Klipper {
			fmt.Printf("  no Klipper firmware: %s\n", i.Err)
			continue
		}
		state := "old: flash the firmware of this host"
		if board.SameVersion(i.Version, cli.KlipperVersion) {
			state = "the same as this host"
		}
		fmt.Printf("  MCU: %s\n  firmware: %s (%s)\n", i.MCU, i.Version, state)
	}
}

// boardFlash writes the Klipper firmware to the board on a port, through its bootloader. With
// -check, it only reads the chip and writes nothing. FIRMWARE_DIR is the folder of the firmware
// (default /usr/share/printdeck/firmware). Stop the Klipper host first.
//
//	printdeck flash [-check] DEVICE
func boardFlash(args []string) {
	check := len(args) > 0 && args[0] == "-check"
	if check {
		args = args[1:]
	}
	if len(args) != 1 {
		log.Fatal("usage: printdeck flash [-check] DEVICE")
	}
	if d := os.Getenv("FIRMWARE_DIR"); d != "" {
		board.FirmwareDir = d
	}
	if check {
		c, err := board.Check(args[0])
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("chip: %s (bootloader STK500v2), firmware for it: %v\n", c.Name, board.CanFlash(c.Name))
		return
	}
	last := ""
	err := board.Flash(args[0], func(step string, done, total int) {
		if step != last {
			fmt.Println(step)
			last = step
		}
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("done:", board.FirmwareVersion())
}
