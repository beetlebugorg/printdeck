package ui

import (
	"fmt"
	"image"
	"strconv"

	"printdeck/internal/gfx"
	"printdeck/internal/maxmode"
)

// The Max mode page (Settings > Max mode): the [max_mode] section of printer.cfg. In max mode the
// printer ignores the F speeds of the G-code, and gives each move a speed from the flow limit of
// the hotend and the speeds of the features (package maxmode). The page changes a copy of the
// settings. Save writes the section and restarts Klipper.

// MaxModer is a printer with max mode (printer.Klippy).
type MaxModer interface {
	// MaxMode returns the settings of the [max_mode] section, and false for a printer.cfg with no
	// section.
	MaxMode() (maxmode.Settings, bool)
	// SetMaxMode writes the settings to printer.cfg and restarts Klipper.
	SetMaxMode(maxmode.Settings) error
}

// featureTitles are the names of the features (maxmode.Feature) on the page.
var featureTitles = [maxmode.NumFeatures]string{"Other lines", "Outer wall", "Inner wall", "Overhang", "Bridge",
	"Top surface", "Solid infill", "Infill", "Support", "Skirt and brim"}

// maxModer returns the printer with max mode, or false for a printer with no max mode.
func (a *App) maxModer() (MaxModer, bool) {
	if a.resin() {
		return nil, false
	}
	m, ok := a.Printer.(MaxModer)
	return m, ok
}

// maxModeLine is the line of the Max mode row of the Settings page.
func maxModeLine(s maxmode.Settings, section bool) string {
	if section && s.Enable {
		return fmt.Sprintf("On · max flow %s mm³/s", num(s.MaxFlow))
	}
	return "Off · the speeds of the G-code"
}

// num is a number with no zeros at the end: 12, 12.5.
func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// speedLine is a speed in mm/s, or zero as the text of zero.
func speedLine(v float64, zero string) string {
	if v == 0 {
		return zero
	}
	return num(v) + " mm/s"
}

// openMaxMode opens the Max mode page with a copy of the settings of the printer.
func (a *App) openMaxMode() {
	m, ok := a.maxModer()
	if !ok {
		return
	}
	s, _ := m.MaxMode()
	a.mmSaved, a.mmDraft = s, s
	a.open(pageMaxMode)
}

// editMaxMode opens a decimal keypad for one number of the copy of the settings.
func (a *App) editMaxMode(title, unit string, v *float64, hi float64) {
	a.keypad = &keypad{title: title, digits: num(*v), unit: unit, decimal: true, lo: 0, hi: hi,
		setF: func(x float64) { *v = x; a.Redraw() }}
}

// saveMaxMode asks to write the copy of the settings to printer.cfg.
func (a *App) saveMaxMode() {
	m, ok := a.maxModer()
	if !ok {
		return
	}
	if a.mmDraft == a.mmSaved {
		a.Toast("No changes to save")
		return
	}
	if a.mmDraft.MaxFlow <= 0 {
		a.Toast("Max flow must be more than 0")
		return
	}
	s := a.mmDraft
	a.confirm = &confirmBox{question: "Save max mode to printer.cfg? Klipper restarts.", yes: "Save",
		onYes: func() {
			a.do(func() error {
				if err := m.SetMaxMode(s); err != nil {
					return err
				}
				a.mmSaved = s
				a.Toast("Max mode saved. Klipper restarts.")
				return nil
			})
		}}
}

// drawMaxMode is the Max mode page.
func (a *App) drawMaxMode(c *gfx.Canvas) {
	f := a.navPage(c, "Max mode")
	b := backRect(f.head)
	fill, fg := raised, text
	if a.mmDraft != a.mmSaved {
		fill, fg = orange, onAcc
	}
	a.printerKey(c, b.Sub(image.Pt(b.Dx()+12, 0)), "Save", fill, fg, a.saveMaxMode)

	s := &a.mmDraft
	speed := func(title string, v *float64, zero string) menuItem {
		return menuItem{title, speedLine(*v, zero), dim, func() { a.editMaxMode(title, " mm/s", v, 1000) }}
	}
	flow := menuItem{"Max flow", num(s.MaxFlow) + " mm³/s · the melt rate of the hotend", dim,
		func() { a.editMaxMode("Max flow", " mm³/s", &s.MaxFlow, 100) }}
	layerTime := menuItem{"Min layer time", "No minimum", dim,
		func() { a.editMaxMode("Min layer time", " s", &s.MinLayerTime, 600) }}
	if s.MinLayerTime > 0 {
		layerTime.sub = num(s.MinLayerTime) + " s · a short layer prints slower"
	}
	limits := menuGroup{heading: "Limits", items: []menuItem{flow,
		speed("First layer speed", &s.FirstLayerSpeed, "No limit"),
		speed("Travel speed", &s.TravelSpeed, "The max velocity of the printer"),
		layerTime,
		speed("Min speed", &s.MinSpeed, "No minimum")}}
	features := menuGroup{heading: "Feature speeds", note: "0 is no limit: the flow limit sets the speed."}
	for i := range maxmode.NumFeatures {
		features.items = append(features.items, speed(featureTitles[i], &s.FeatureSpeed[i], "No limit"))
	}
	on := menuGroup{items: []menuItem{{"Max mode", "The flow and the feature speeds set the speed of each move", dim,
		func() { s.Enable = !s.Enable; a.Redraw() }}}, checks: []bool{s.Enable},
		note: "Run a flow test before you turn it on. Save writes [max_mode] in printer.cfg and restarts Klipper."}
	a.menuGroups(c, f.body(), f.wide, 60, 5, []menuGroup{on, limits, features})
}
