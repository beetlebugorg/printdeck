package ui

import (
	"strings"
	"testing"
	"time"
)

type fakeAdmin struct{ added []string }

func (f *fakeAdmin) List() []PrinterEntry         { return nil }
func (f *fakeAdmin) AddKlipper(name string) error { return nil }
func (f *fakeAdmin) AddResin(name, addr string) error {
	f.added = append(f.added, name+"@"+addr)
	return nil
}
func (f *fakeAdmin) AddBambu(name, addr, s, c string) error { return nil }
func (f *fakeAdmin) Update(p PrinterEntry) error            { return nil }
func (f *fakeAdmin) Remove(id string) error                 { return nil }
func (f *fakeAdmin) Changed() bool                          { return true }
func (f *fakeAdmin) Restart()                               {}

// Add a resin printer: its name, then its address.
func TestAddResinFlow(t *testing.T) {
	a := New()
	fa := &fakeAdmin{}
	a.Printers = fa
	a.Printer = &DemoPrinter{View: -1}
	a.pages = []page{pagePrinters}
	frameText(a)
	a.askAddPrinter()
	frameText(a)
	r, ok := a.KeyRect("Resin printer on the network")
	if !ok {
		t.Fatal("no resin choice")
	}
	tap(a, r)
	frameText(a)
	if a.textKB == nil {
		t.Fatal("no name keyboard")
	}
	a.textKB.value = "Phenom"
	frameText(a)
	save, ok := a.KeyRect("Save")
	if !ok {
		t.Fatal("no save")
	}
	tap(a, save)
	f := frameText(a)
	if a.textKB == nil || !strings.Contains(a.textKB.title, "IP address") {
		t.Fatalf("after the name: kb %+v toast? %v", a.textKB, strings.Contains(f, "Type"))
	}
	a.textKB.value = "192.168.76.58"
	frameText(a)
	save, _ = a.KeyRect("Save")
	tap(a, save)
	frameText(a)
	if len(fa.added) != 1 || fa.added[0] != "Phenom@192.168.76.58" || a.textKB != nil {
		t.Errorf("added %v, keyboard %v", fa.added, a.textKB)
	}
}

// On a deck with many printers, a keyboard shows its title: the status bar waits until the
// keyboard closes.
func TestKeyboardOverStatusBar(t *testing.T) {
	s, _ := fleetDeck(t)
	a := s.Active()
	a.editPrinterText("Name of the new printer", "", 32, func(string) {})
	f := frameText(a)
	if strings.Contains(f, "43% · 1:12") {
		t.Error("the status bar shows over the keyboard")
	}
	shot(t, a, "kb-fleet")
	a.textKB = nil
	if f := frameText(a); !strings.Contains(f, "43% · 1:12") {
		t.Error("no status bar after the keyboard")
	}
}

// finderAdmin finds the X2D on the network.
type finderAdmin struct {
	fakeAdmin
	bambu []string
}

func (f *finderAdmin) FindBambu() ([]BambuFound, error) {
	return []BambuFound{{Addr: "192.168.77.171", Serial: "20P5AJ000000001", Name: "X2D"}}, nil
}
func (f *finderAdmin) AddBambu(name, addr, serial, code string) error {
	f.bambu = append(f.bambu, name+"@"+addr+"/"+serial+"/"+code)
	return nil
}

// Add a Bambu Lab printer that the deck found: pick it, keep its name, and type the access code.
func TestAddBambuFound(t *testing.T) {
	a := New()
	fa := &finderAdmin{}
	a.Printers = fa
	a.Printer = &DemoPrinter{View: -1}
	a.askAddBambu()
	// The search posts the toast, then the list of the printers.
	for a.confirm == nil {
		select {
		case f := <-a.posted:
			f()
		case <-time.After(2 * time.Second):
			t.Fatal("no list of the printers")
		}
	}
	if len(a.confirm.choices) != 2 || a.confirm.choices[0].title != "X2D · 192.168.77.171" {
		t.Fatalf("choices %+v", a.confirm.choices)
	}
	a.confirm.choices[0].tap()
	if a.textKB == nil || a.textKB.value != "X2D" {
		t.Fatalf("name keyboard %+v", a.textKB)
	}
	a.textKB.done("X2D")
	if a.textKB == nil || !strings.HasPrefix(a.textKB.title, "Access code") {
		t.Fatalf("code keyboard %+v", a.textKB)
	}
	a.textKB.done("8d86abcd")
	deadline := time.After(2 * time.Second)
	for len(fa.bambu) == 0 {
		select {
		case f := <-a.posted:
			f()
		case <-deadline:
			t.Fatal("no AddBambu")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if fa.bambu[0] != "X2D@192.168.77.171/20P5AJ000000001/8d86abcd" {
		t.Errorf("added %v", fa.bambu)
	}
}
