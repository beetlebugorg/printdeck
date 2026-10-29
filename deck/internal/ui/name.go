package ui

import (
	"strings"

	"printdeck/internal/system"
)

// Namer is a SetupHost that can change the name of the deck: the name that slicers show, and the
// host name for <host>.local, made from it.
type Namer interface {
	Name() string
	SetName(name string) error
}

// nameLine is the line of the Name row: the name and its address, for example
// "Workhorse · workhorse.local".
func nameLine(name string) string {
	if h := system.HostLabel(name); h != "" {
		return name + " · " + h + ".local"
	}
	return name
}

// editName opens the keyboard for the name of the deck. The system sets it in the background.
func (a *App) editName(nm Namer) {
	a.textKB = &keyboard{title: "Name of this Printdeck", value: nm.Name(), max: system.MaxName, ok: "Save", cancel: "Back",
		shift: true, back: func() { a.textKB = nil },
		done: func(v string) {
			name, err := system.CheckName(v)
			if err != nil {
				a.Toast(err.Error())
				return
			}
			a.textKB = nil
			if name == strings.TrimSpace(nm.Name()) {
				return
			}
			go func() {
				err := nm.SetName(name)
				a.post(func() {
					if err != nil {
						a.Toast("The name was not changed: " + err.Error())
						return
					}
					a.Toast("The name is " + name)
				})
			}()
		}}
}
