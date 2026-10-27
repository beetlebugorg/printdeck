package ui

import (
	"strings"
	"testing"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/printer"
)

// runPosted runs the changes that other goroutines posted, as the UI loop does.
func runPosted(a *App, n int) {
	for i := 0; i < n; i++ {
		select {
		case f := <-a.posted:
			f()
		case <-time.After(2 * time.Second):
			return
		}
	}
}

// With [printdeck_sdcard], the Files page has the SD card of the printer: its folders, its G-code
// files, and a tap on a file copies it and asks to print it.
func TestFilesSDCard(t *testing.T) {
	for _, wide := range []bool{false, true} {
		a, pr, _ := newPrintApp(idle)
		a.Wide = wide
		pr.Machine_.Modules = []string{"printdeck_sdcard"}
		pr.SDCard_ = map[string][]printer.SDFile{
			"":      {{Name: "parts", Dir: true}, {Name: "plaque.gcode", Size: 3 << 20}},
			"parts": {{Name: "clip.gcode", Size: 40 << 10}},
		}
		cv := gfx.New(a.Screen().W, a.Screen().H)
		a.pages = []page{pageFiles}
		tapLabel(t, a, cv, "SD card")
		runPosted(a, 1)
		a.Draw(cv)
		if !hasKey(a, "parts") || !hasKey(a, "plaque.gcode") {
			t.Fatalf("wide %v: no folder and file of the card", wide)
		}
		tapLabel(t, a, cv, "parts")
		runPosted(a, 1)
		a.Draw(cv)
		if a.sd.dir != "parts" || !hasKey(a, "clip.gcode") || !hasKey(a, "Up") {
			t.Fatalf("wide %v: folder %q", wide, a.sd.dir)
		}
		tapLabel(t, a, cv, "clip.gcode")
		runPosted(a, 1)
		if a.confirm == nil || !strings.Contains(a.confirm.question, "clip.gcode") {
			t.Errorf("wide %v: no question to print the copy", wide)
		}
	}
}

// With no [printdeck_sdcard], the Files page has no key of the SD card.
func TestFilesNoSDCard(t *testing.T) {
	a, _, cv := newPrintApp(idle)
	a.pages = []page{pageFiles}
	a.Draw(cv)
	if hasKey(a, "SD card") {
		t.Error("an SD card key with no SD card")
	}
}
