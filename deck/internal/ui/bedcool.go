package ui

import (
	"fmt"
	"maps"

	"printdeck/internal/bedcool"
	"printdeck/internal/gfx"
)

// The Bed cool page of Maintenance: the deck sets the bed to a hold temperature a little before
// the end of a print, so the part can come off sooner (package bedcool).

// BedCool is the early cool of the bed.
type BedCool interface {
	Settings() bedcool.Settings
	SetSettings(bedcool.Settings)
	Info() bedcool.Info
}

// bedCoolLine is the line of the Bed cool row.
func bedCoolLine(s bedcool.Settings) string {
	if !s.On {
		return "Off · the bed cools after the print"
	}
	var on []string
	for _, m := range materials {
		if s.Hold[m.Name] > 0 {
			on = append(on, m.Name)
		}
	}
	if len(on) == 0 {
		return "On · no material has a hold temperature"
	}
	return "On for " + joinAnd(on)
}

// joinAnd joins words as "A, B and C".
func joinAnd(w []string) string {
	switch len(w) {
	case 0:
		return ""
	case 1:
		return w[0]
	}
	s := w[0]
	for _, x := range w[1 : len(w)-1] {
		s += ", " + x
	}
	return s + " and " + w[len(w)-1]
}

// holdLine is the line of the row of a material: its hold, and the time that the bed needs to
// cool to it from the bed temperature of the material.
func holdLine(s bedcool.Settings, m material) string {
	h := s.Hold[m.Name]
	if h <= 0 {
		return "Off"
	}
	d := bedcool.CoolTime(s.Tau, float64(m.Bed), float64(h))
	if d <= 0 {
		return fmt.Sprintf("Hold %d° · no cool from %d°", h, m.Bed)
	}
	return fmt.Sprintf("Hold %d° · from %d° in about %d min", h, m.Bed, int(d.Minutes()+0.5))
}

// changeBedCool changes the settings of the bed cool.
func (a *App) changeBedCool(change func(s *bedcool.Settings)) {
	s := a.BedCool.Settings()
	s.Hold = maps.Clone(s.Hold)
	change(&s)
	a.BedCool.SetSettings(s)
}

// openHold opens the choices of the hold temperature of a material.
func (a *App) openHold(m material) {
	var choices []menuItem
	for _, h := range bedcool.Holds {
		if h >= m.Bed {
			break
		}
		if h == 0 {
			choices = append(choices, menuItem{title: "Off", sub: "The bed stays hot until the end"})
		} else {
			choices = append(choices, menuItem{title: fmt.Sprintf("%d°", h)})
		}
	}
	a.openOption(&option{title: m.Name + " hold", choices: choices,
		note: "The bed goes to this temperature a little before the end. A lower hold saves more time, " +
			"and the part can let go or warp.",
		current: func() int {
			h := a.BedCool.Settings().Hold[m.Name]
			for i, v := range bedcool.Holds {
				if v == h {
					return i
				}
			}
			return -1
		},
		pick: func(i int) { a.changeBedCool(func(s *bedcool.Settings) { s.Hold[m.Name] = bedcool.Holds[i] }) }})
}

// drawBedCool is the Bed cool page.
func (a *App) drawBedCool(c *gfx.Canvas) {
	f := a.navPage(c, "Bed cool")
	s := a.BedCool.Settings()
	toggle := menuGroup{items: []menuItem{{"Cool before the end", bedCoolLine(s), dim, func() {
		a.changeBedCool(func(s *bedcool.Settings) { s.On = !s.On })
	}}}, checks: []bool{s.On}}
	var items []menuItem
	for _, m := range materials {
		m := m
		items = append(items, menuItem{m.Name, holdLine(s, m), dim, func() { a.openHold(m) }})
	}
	curve := fmt.Sprintf("Not learned yet · %d min to cool by half", int(s.Tau*0.693/60+0.5))
	if s.Samples > 0 {
		curve = fmt.Sprintf("%d min to cool by half · from %d samples", int(s.Tau*0.693/60+0.5), s.Samples)
	}
	info := []menuItem{{"Cooling of the bed", curve, dim, nil}}
	if last := a.BedCool.Info().Last; last != "" {
		info = append(info, menuItem{"Last print", last, dim, nil})
	}
	a.menuGroups(c, f.body(), f.wide, 60, 5, []menuGroup{toggle, {heading: "HOLD", items: items,
		note: "Not before half of the print, and not for a part that is tall on a small base."}, {items: info}})
}
