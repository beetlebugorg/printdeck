package main

import (
	"fmt"
	"os"
	"path/filepath"

	"printdeck/internal/system"
)

// systemMain runs a system program of Printdeck OS when the binary runs under its name (a link in
// /usr/bin), and reports that it did. The programs were shell scripts with these names.
//
//	printdeck-init                         the first program at boot
//	printdeck-update BUNDLE                write an update bundle to the other slot, and restart
//	printdeck-commit                       make the new slot the default after an update
//	printdeck-credentials                  make the password and the API key at the first start
//	printdeck-settings                     apply the settings of the config partition
//	printdeck-wifi SSID PASSWORD [COUNTRY] set the Wi-Fi network
func systemMain(name string, args []string) bool {
	var err error
	switch name {
	case "printdeck-init":
		system.Init()
	case "printdeck-update":
		if len(args) != 1 {
			usage("printdeck-update printdeck-update.tar.xz")
		}
		var slot string
		if slot, err = system.Update(args[0], os.Stdout); err == nil {
			fmt.Printf("Restarting into slot %s.\n", slot)
			err = system.RebootTryboot()
		}
	case "printdeck-commit":
		err = system.Commit(os.Stdout)
	case "printdeck-credentials":
		err = system.Credentials()
	case "printdeck-settings":
		err = system.Settings(os.Stderr)
	case "printdeck-wifi":
		if len(args) < 1 || args[0] == "" {
			usage("printdeck-wifi SSID PASSWORD [COUNTRY]")
		}
		pass, country := "", ""
		if len(args) > 1 {
			pass = args[1]
		}
		if len(args) > 2 {
			country = args[2]
		}
		err = system.SetWifi(args[0], pass, country)
	default:
		return false
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return true
}

func usage(u string) {
	fmt.Fprintln(os.Stderr, "Usage:", u)
	os.Exit(2)
}

// programName is the name that the binary runs under.
func programName() string { return filepath.Base(os.Args[0]) }
