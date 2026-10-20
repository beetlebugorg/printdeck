package main

import (
	"fmt"

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
