// Package cli is the command line of the Klipper host: it reads the options, sets the start
// arguments, and runs the printer again after each restart.
//
// Ported to Go from main() of klippy/klippy.py of Klipper (https://github.com/Klipper3d/klipper),
// commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f. The G-code pseudo-terminal (-I) is not
// ported: the deck sends G-code through the API socket (-a). The log file does not rotate.
//
//	Copyright (C) 2016-2024  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package cli

import (
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strings"
	"time"

	_ "printdeck/internal/klipper/extras/all"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
)

// KlipperVersion is the Klipper version of the port. The board firmware must be this version.
const KlipperVersion = "v0.13.0-777-g7bc4d0946"

// SoftwareVersion is the version that the host reports.
const SoftwareVersion = KlipperVersion + "-printdeck-go"

const usage = `usage: printdeck klippy [options] <config file>

  -i FILE   read commands from file instead of from tty port
  -a FILE   api server unix domain socket filename
  -l FILE   write log to file instead of stderr
  -v        enable debug messages
  -o FILE   write output to file instead of to serial port
  -d FILE   file to read for mcu protocol dictionary (or NAME=FILE)`

type options struct {
	debuginput, apiserver, logfile, debugoutput string
	verbose                                      bool
	dictionary                                   map[string]string
	args                                         []string
}

func parseArgs(args []string) (options, error) {
	o := options{dictionary: map[string]string{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			o.args = append(o.args, a)
			continue
		}
		name, value, hasValue := strings.Cut(a, "=")
		if !strings.HasPrefix(a, "--") {
			name, value, hasValue = a[:2], a[2:], len(a) > 2
		}
		if name == "-v" || name == "--verbose" {
			o.verbose = true
			continue
		}
		if !hasValue {
			if i+1 >= len(args) {
				return o, fmt.Errorf("%s option requires an argument", name)
			}
			i++
			value = args[i]
		}
		switch name {
		case "-i", "--debuginput":
			o.debuginput = value
		case "-a", "--api-server":
			o.apiserver = value
		case "-l", "--logfile":
			o.logfile = value
		case "-o", "--debugoutput":
			o.debugoutput = value
		case "-d", "--dictionary":
			key, fname := "dictionary", value
			if mcuName, f, ok := strings.Cut(value, "="); ok {
				key, fname = "dictionary_"+mcuName, f
			}
			o.dictionary[key] = fname
		case "-I", "--input-tty":
			return o, fmt.Errorf("the G-code pseudo-terminal is not supported: use the API socket (-a)")
		default:
			return o, fmt.Errorf("no such option: %s", name)
		}
	}
	if len(o.args) != 1 {
		return o, fmt.Errorf("Incorrect number of arguments")
	}
	return o, nil
}

func tryReadFile(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// cpuInfo is util.get_cpu_info: the number of cores and the model name.
func cpuInfo() string {
	data, ok := tryReadFile("/proc/cpuinfo")
	if !ok {
		return "?"
	}
	cores, model := 0, "?"
	for _, l := range strings.Split(data, "\n") {
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if k == "processor" {
			cores++
		}
		// Python's dict() keeps the last value.
		if k == "model name" {
			model = v
		}
	}
	return fmt.Sprintf("%d core %s", cores, model)
}

// deviceInfo is util.get_device_info.
func deviceInfo() string {
	data, ok := tryReadFile("/proc/device-tree/model")
	if !ok {
		if data, ok = tryReadFile("/sys/class/dmi/id/product_name"); !ok {
			return "?"
		}
	}
	return strings.TrimSpace(strings.TrimRight(data, " \x00"))
}

// linuxVersion is util.get_linux_version.
func linuxVersion() string {
	data, ok := tryReadFile("/proc/version")
	if !ok {
		return "?"
	}
	return strings.TrimSpace(data)
}

// Main runs the Klipper host with the command line args, and returns the exit code.
func Main(args []string) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n\nprintdeck klippy: error: %v\n", usage, err)
		return 2
	}
	startArgs := host.StartArgs{"config_file": o.args[0], "apiserver": o.apiserver, "start_reason": "startup"}
	if o.debuginput != "" {
		startArgs["debuginput"] = o.debuginput
		debuginput, err := os.Open(o.debuginput)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer debuginput.Close()
		startArgs["gcode_fd"] = debuginput
	}
	if o.debugoutput != "" {
		startArgs["debugoutput"] = o.debugoutput
		for k, v := range o.dictionary {
			startArgs[k] = v
		}
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	if o.logfile != "" {
		startArgs["log_file"] = o.logfile
		f, err := os.OpenFile(o.logfile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer f.Close()
		log.SetOutput(io.Writer(f))
	}
	log.Print("Starting Klippy...")
	startArgs["software_version"] = SoftwareVersion
	startArgs["cpu_info"] = cpuInfo()
	startArgs["device"] = deviceInfo()
	startArgs["linux_version"] = linuxVersion()
	if o.logfile != "" {
		log.Print(strings.Join([]string{
			fmt.Sprintf("Args: %q", append([]string{"printdeck", "klippy"}, args...)),
			fmt.Sprintf("Git version: '%s'", SoftwareVersion),
			fmt.Sprintf("CPU: %s", startArgs["cpu_info"]),
			fmt.Sprintf("Device: %s", startArgs["device"]),
			fmt.Sprintf("Linux: %s", startArgs["linux_version"]),
			fmt.Sprintf("Go: '%s'", runtime.Version())}, "\n"))
	} else if o.debugoutput == "" {
		log.Print("No log file specified! Severe timing issues may result!")
	}

	// Start Printer() class
	var res string
	for {
		mainReactor := reactor.New()
		printer := host.New(mainReactor, startArgs)
		res = printer.Run()
		if res == "exit" || res == "error_exit" {
			break
		}
		time.Sleep(time.Second)
		log.Print("Restarting printer")
		startArgs["start_reason"] = res
	}
	if res == "error_exit" {
		return 255
	}
	return 0
}
