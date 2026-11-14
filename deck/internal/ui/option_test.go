package ui

import (
	"strings"
	"testing"

	"printdeck/internal/timelapse"
)

// A row of a setting opens the option page. Its toggle or its choices change the setting, and a
// check shows the setting now.
func TestOptionPage(t *testing.T) {
	a, _, cv := newPrintApp(idle)
	tl := &fakeTimeLapse{s: timelapse.DefaultSettings}
	a.TimeLapse = tl
	a.Theme = "day"
	a.pages = []page{pageMachine, pageSettings}
	tapLabel(t, a, cv, "Theme")
	if f := simFrame(t, a, "option-theme"); !strings.Contains(f, "NIGHT") || !strings.Contains(f, "AUTO") {
		t.Error("no choices")
	}
	tapLabel(t, a, cv, "Night")
	if a.Theme != "night" || a.topPage() != pageOption {
		t.Errorf("theme %q, page %v", a.Theme, a.topPage())
	}
	a.Theme = "day"
	a.back()
	tapLabel(t, a, cv, "Time lapse")
	simFrame(t, a, "option-timelapse")
	park := tl.s.Park
	tapLabel(t, a, cv, "Park for the frames")
	simFrame(t, a, "option-park")
	tapLabel(t, a, cv, "Park for the frames")
	if tl.s.Park == park {
		t.Error("the toggle did not change the park")
	}
}
