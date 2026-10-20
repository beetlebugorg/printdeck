package main

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"sync"

	"printdeck/internal/klipper/clocksync"
	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/serialhdl"
)

// klipperProbe connects to a Klipper board as the Klipper host does, up to the point where the
// host would configure it, and prints what the board reports. It sends only identify, get_config,
// get_uptime and get_clock: no command that sets a pin, heats or moves.
//
//	printdeck klipper-probe DEVICE [BAUD]
//
// Stop the Klipper host first: the port has one user at a time. On an Arduino style board, such
// as the RAMBo, opening the port resets the board, as it does when the Klipper host connects.
func klipperProbe(args []string) {
	if len(args) < 1 {
		log.Fatal("usage: printdeck klipper-probe DEVICE [BAUD]")
	}
	device, baud := args[0], 250000
	if len(args) > 1 {
		b, err := strconv.Atoi(args[1])
		if err != nil {
			log.Fatalf("bad baud rate %q", args[1])
		}
		baud = b
	}
	r := reactor.New()
	failed := false
	r.RegisterCallback(func(float64) any {
		defer r.End()
		if err := probe(r, device, baud); err != nil {
			fmt.Println("FAILED:", err)
			failed = true
		}
		return nil
	}, reactor.Now)
	r.Run()
	if failed {
		os.Exit(1)
	}
}

func probe(r *reactor.Reactor, device string, baud int) error {
	s := serialhdl.New(r, "mcu")
	if err := s.ConnectUART(device, baud, true); err != nil {
		return err
	}
	defer s.Disconnect()

	// The board can go on with the reports of an earlier session (for example its thermistors).
	// Count them, by name.
	var mu sync.Mutex
	unhandled := map[string]int{}
	s.SetDefaultHandler(func(params serialhdl.Params) {
		mu.Lock()
		unhandled[fmt.Sprint(params["#name"])]++
		mu.Unlock()
	})

	p := s.MsgParser()
	version, buildVersions := p.Version()
	kinds := map[string]int{}
	for _, m := range p.Messages() {
		kinds[string(m.Kind)]++
	}
	// The same line as klippy: its count is all the messages of the dictionary.
	fmt.Printf("Loaded MCU 'mcu' %d commands (%s / %s)\n", len(p.Messages()), version, buildVersions)
	fmt.Printf("messages: commands=%d responses=%d output=%d\n", kinds["command"], kinds["response"], kinds["output"])
	consts := p.Constants()
	names := make([]string, 0, len(consts))
	for k := range consts {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		v, _ := p.Constant(k)
		fmt.Printf("config %s=%s\n", k, v)
	}

	cfg, err := s.SendWithResponse("get_config", "config")
	if err != nil {
		return err
	}
	fmt.Printf("get_config: is_config=%d crc=%d is_shutdown=%d move_count=%d\n",
		cfg.Int("is_config"), cfg.Int("crc"), cfg.Int("is_shutdown"), cfg.Int("move_count"))

	cs := clocksync.New(r)
	if err := cs.Connect(s); err != nil {
		return err
	}
	r.Pause(r.Monotonic() + 5.)
	e := cs.Estimate()
	fmt.Printf("clock: freq=%.0f (nominal %.0f, %+.1f ppm) active=%v\n",
		e.Freq, cs.MCUFreq(), (e.Freq-cs.MCUFreq())/cs.MCUFreq()*1e6, cs.IsActive())
	fmt.Println(s.Stats(r.Monotonic()))
	mu.Lock()
	for name, n := range unhandled {
		fmt.Printf("unhandled reports: %s=%d\n", name, n)
	}
	mu.Unlock()
	return nil
}
