package ui

import (
	"fmt"
	"net"
	"strings"

	"printdeck/internal/gfx"
)

// Settings, Printers: the printers of the deck (specs/many-printers-on-one-deck.md). A tap on a
// printer changes its name, its label or its colour, or removes it. Add a printer adds a Klipper
// printer on USB, with its own Klipper host, a resin printer on the network, or a Bambu Lab printer
// on the network. A change starts
// to work after a restart of the deck.

// PrinterEntry is a printer in Settings, Printers.
type PrinterEntry struct {
	ID    string // "" for the first printer
	Name  string
	Label string
	Color int    // an index of PrinterColors
	Kind  string // "Klipper", "Resin" or "Bambu Lab"
	Addr  string // the IP address of a printer on the network
}

// PrinterAdmin changes the printers of the deck.
type PrinterAdmin interface {
	List() []PrinterEntry
	AddKlipper(name string) error
	AddResin(name, addr string) error
	// AddBambu adds a Bambu Lab printer at addr, with its serial number and its access code.
	AddBambu(name, addr, serial, code string) error
	Update(p PrinterEntry) error
	Remove(id string) error
	// Changed reports that the list differs from the printers that the deck runs now.
	Changed() bool
	Restart()
}

// MaxPrinters is the most printers on one deck.
const MaxPrinters = 4

// printersLine is the line of the Printers row of Settings.
func printersLine(list []PrinterEntry) string {
	if len(list) <= 1 {
		return "One printer · add a printer"
	}
	var labels []string
	for _, p := range list {
		labels = append(labels, p.Label)
	}
	return fmt.Sprintf("%d printers · %s", len(list), strings.Join(labels, ", "))
}

func printerColorName(i int) string {
	if i >= 0 && i < len(PrinterColorNames) {
		return PrinterColorNames[i]
	}
	return ""
}

// drawPrinters is the page Settings, Printers.
func (a *App) drawPrinters(c *gfx.Canvas) {
	f := a.navPage(c, "Printers")
	list := a.Printers.List()
	var items []menuItem
	if a.Printers.Changed() {
		items = append(items, menuItem{"Restart the deck", "The changes start to work after a restart", text, a.Printers.Restart})
	}
	for _, p := range list {
		p := p
		sub := p.Label + " · " + printerColorName(p.Color) + " · " + p.Kind
		if p.Addr != "" {
			sub += " · " + p.Addr
		}
		items = append(items, menuItem{p.Name, sub, dim, func() { a.askPrinter(p, len(list)) }})
	}
	if len(list) < MaxPrinters {
		items = append(items, menuItem{"Add a printer", "A Klipper printer on USB, or a printer on the network", dim, a.askAddPrinter})
	}
	a.menuRows(c, f.body(), f.wide, 60, 5, items)
}

// askPrinter asks what to change of the printer p.
func (a *App) askPrinter(p PrinterEntry, count int) {
	choices := []menuItem{
		{"Name", p.Name, dim, func() {
			a.editPrinterText("Name of the printer", p.Name, 32, func(v string) { p.Name = v; a.saveDeckPrinter(p) })
		}},
		{"Label", p.Label + " · 2 to 4 characters", dim, func() {
			a.editPrinterText("Label of the printer", p.Label, 4, func(v string) { p.Label = strings.ToUpper(v); a.saveDeckPrinter(p) })
		}},
		{"Colour", printerColorName(p.Color), dim, func() { a.askPrinterColor(p) }},
	}
	if p.ID != "" && count > 1 {
		choices = append(choices, menuItem{"Remove", "Its Klipper host stops. Its files stay on the deck.", dim, func() {
			a.confirm = &confirmBox{question: "Remove " + p.Name + " from this deck?", yes: "Remove", danger: true, onYes: func() {
				a.printerChange(func() error { return a.Printers.Remove(p.ID) })
			}}
		}})
	}
	a.confirm = &confirmBox{question: p.Name, choices: choices}
}

// askPrinterColor asks the colour of the printer p. Two printers cannot have the same colour.
func (a *App) askPrinterColor(p PrinterEntry) {
	used := map[int]string{}
	for _, o := range a.Printers.List() {
		if o.ID != p.ID {
			used[o.Color] = o.Label
		}
	}
	var choices []menuItem
	for i, name := range PrinterColorNames {
		i := i
		sub := "Free" // in the colour itself, as a sample
		if l, ok := used[i]; ok {
			choices = append(choices, menuItem{name, "In use by " + l, dim, nil})
			continue
		}
		if i == p.Color {
			sub = "Now"
		}
		choices = append(choices, menuItem{name, sub, PrinterColors[i], func() { p.Color = i; a.saveDeckPrinter(p) }})
	}
	a.confirm = &confirmBox{question: "Colour of " + p.Name, choices: choices}
}

// askAddPrinter asks the kind of the new printer, then its name, and the address of a printer on
// the network. A Bambu Lab printer also needs its serial number and its access code.
func (a *App) askAddPrinter() {
	a.confirm = &confirmBox{question: "Add a printer", choices: []menuItem{
		{"Klipper printer on USB", "Its own Klipper host. Then set up its board.", dim, func() {
			a.editPrinterText("Name of the new printer", "", 32, func(name string) {
				a.printerChange(func() error { return a.Printers.AddKlipper(name) })
			})
		}},
		{"Resin printer on the network", "A ChiTu board, for example the Phenom", dim, func() {
			a.editPrinterText("Name of the new printer", "", 32, func(name string) {
				a.editPrinterText("IP address of "+name, "", 15, func(addr string) {
					if net.ParseIP(addr).To4() == nil {
						a.Toast("Type an IPv4 address, for example 192.168.1.40")
						return
					}
					a.printerChange(func() error { return a.Printers.AddResin(name, addr) })
				})
			})
		}},
		{"Bambu Lab printer on the network", "For example the X2D. Its screen shows the address and the access code.", dim, a.askAddBambu},
	}}
}

// BambuFound is a Bambu Lab printer that the deck found on the network.
type BambuFound struct {
	Addr, Serial, Name string
}

// bambuFinder is a PrinterAdmin that finds the Bambu Lab printers on the network. It waits a few
// seconds.
type bambuFinder interface{ FindBambu() ([]BambuFound, error) }

// askAddBambu finds the Bambu Lab printers on the network, and asks which one to add: a printer
// that the deck found needs only its name and its access code. With no printer found, or Type the
// address, it asks each value (askBambuByHand).
func (a *App) askAddBambu() {
	bf, ok := a.Printers.(bambuFinder)
	if !ok {
		a.askBambuByHand()
		return
	}
	a.Toast("Looking for Bambu Lab printers on the network…")
	go func() {
		found, err := bf.FindBambu()
		a.post(func() {
			have := map[string]bool{}
			for _, p := range a.Printers.List() {
				have[p.Addr] = true
			}
			var choices []menuItem
			for _, f := range found {
				f := f
				if have[f.Addr] {
					continue
				}
				name := f.Name
				if name == "" {
					name = "Bambu Lab printer"
				}
				choices = append(choices, menuItem{name + " · " + f.Addr, "Serial number " + f.Serial, dim, func() {
					a.editPrinterText("Name of the new printer", name, 32, func(name string) {
						a.askBambuCode(name, f.Addr, f.Serial)
					})
				}})
			}
			if len(choices) == 0 {
				msg := "No new Bambu Lab printer on the network. Type its address."
				if err != nil {
					msg = "The deck could not look on the network: " + err.Error()
				}
				a.Toast(msg)
				a.askBambuByHand()
				return
			}
			choices = append(choices, menuItem{"Type the address", "A printer that is not in the list", dim, a.askBambuByHand})
			a.confirm = &confirmBox{question: "Which Bambu Lab printer?", choices: choices}
		})
	}()
}

// askBambuCode asks the access code of the Bambu Lab printer, and adds it.
func (a *App) askBambuCode(name, addr, serial string) {
	a.editPrinterText("Access code of "+name, "", 16, func(code string) {
		a.printerChange(func() error { return a.Printers.AddBambu(name, addr, serial, code) })
	})
	a.textKB.shift = false // the access code is case-sensitive
}

// askBambuByHand asks the name, the IP address, the serial number and the access code of a Bambu
// Lab printer. The printer shows the address and the access code in its network settings, and the
// serial number in its device settings.
func (a *App) askBambuByHand() {
	a.editPrinterText("Name of the new printer", "", 32, func(name string) {
		a.editPrinterText("IP address of "+name, "", 15, func(addr string) {
			if net.ParseIP(addr).To4() == nil {
				a.Toast("Type an IPv4 address, for example 192.168.1.40")
				return
			}
			a.editPrinterText("Serial number of "+name, "", 20, func(serial string) {
				a.askBambuCode(name, addr, strings.ToUpper(strings.ReplaceAll(serial, " ", "")))
			})
		})
	})
}

// editPrinterText opens the keyboard for a text of a printer.
func (a *App) editPrinterText(title, value string, max int, done func(string)) {
	a.textKB = &keyboard{title: title, value: value, max: max, ok: "Save", cancel: "Back", shift: true,
		back: func() { a.textKB = nil },
		done: func(v string) {
			v = strings.TrimSpace(v)
			if v == "" {
				a.Toast("Type a name")
				return
			}
			a.textKB = nil
			done(v)
		}}
}

func (a *App) saveDeckPrinter(p PrinterEntry) {
	a.printerChange(func() error { return a.Printers.Update(p) })
}

// printerChange runs a change of the printers, and shows its error.
func (a *App) printerChange(f func() error) {
	go func() {
		err := f()
		a.post(func() {
			if err != nil {
				a.Toast("The change was not saved: " + err.Error())
			}
		})
	}()
}
