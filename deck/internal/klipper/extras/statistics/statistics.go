// Package statistics logs the statistics of the printer objects once a second, and has the
// system statistics (load, CPU time, free memory).
//
// Ported to Go from klippy/extras/statistics.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018-2021  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package statistics

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"syscall"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
)

// PrinterSysStats are the statistics of the host system.
type PrinterSysStats struct {
	lastProcessTime, totalProcessTime float64
	lastLoadAvg                       float64
	lastMemAvail                      int64
}

// processTime is the CPU time of the process (time.process_time).
func processTime() float64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	tv := func(t syscall.Timeval) float64 { return float64(t.Sec) + float64(t.Usec)/1e6 }
	return tv(ru.Utime) + tv(ru.Stime)
}

// Stats returns the statistics line of the system.
func (s *PrinterSysStats) Stats(eventtime float64) (bool, string) {
	// Get core usage stats
	ptime := processTime()
	pdiff := ptime - s.lastProcessTime
	s.lastProcessTime = ptime
	if pdiff > 0. {
		s.totalProcessTime += pdiff
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			s.lastLoadAvg, _ = strconv.ParseFloat(f[0], 64)
		}
	}
	msg := fmt.Sprintf("sysload=%.2f cputime=%.3f", s.lastLoadAvg, s.totalProcessTime)
	// Get available system memory
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "MemAvailable:") {
				if f := strings.Fields(line); len(f) > 1 {
					s.lastMemAvail, _ = strconv.ParseInt(f[1], 10, 64)
					msg = fmt.Sprintf("%s memavail=%d", msg, s.lastMemAvail)
				}
				break
			}
		}
	}
	return false, msg
}

// Status returns the system statistics.
func (s *PrinterSysStats) Status(eventtime float64) map[string]any {
	return map[string]any{"sysload": s.lastLoadAvg, "cputime": s.totalProcessTime, "memavail": s.lastMemAvail}
}

// statser is a printer object with a statistics line.
type statser interface {
	Stats(eventtime float64) (bool, string)
}

// PrinterStats logs the statistics once a second.
type PrinterStats struct {
	printer    *host.Printer
	reactor    *reactor.Reactor
	statsTimer *reactor.Timer
	statsCB    []statser
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	printer.AddObject("system_stats", &PrinterSysStats{})
	ps := &PrinterStats{printer: printer, reactor: printer.Reactor()}
	ps.statsTimer = ps.reactor.RegisterTimer(ps.generateStats, reactor.Never)
	printer.RegisterEventHandler("klippy:ready", func(...any) { ps.handleReady() })
	return ps
}

func init() { host.RegisterModule("statistics", load, nil) }

func (ps *PrinterStats) handleReady() {
	ps.statsCB = nil
	for _, o := range ps.printer.LookupObjects("") {
		if s, ok := o.Obj.(statser); ok {
			ps.statsCB = append(ps.statsCB, s)
		}
	}
	if _, ok := ps.printer.StartArgs().Get("debugoutput"); !ok {
		ps.reactor.UpdateTimer(ps.statsTimer, reactor.Now)
	}
}

func (ps *PrinterStats) generateStats(eventtime float64) float64 {
	active := false
	var parts []string
	ps.reactor.AssertNoPause(func() {
		for _, cb := range ps.statsCB {
			a, s := cb.Stats(eventtime)
			active = active || a
			if s != "" {
				parts = append(parts, s)
			}
		}
	})
	if active {
		log.Printf("Stats %.1f: %s", eventtime, strings.Join(parts, " "))
	}
	return eventtime + 1.
}
