package ui

import (
	"strings"

	"printdeck/internal/cloud"
	"printdeck/internal/gfx"
	"printdeck/kit"
)

// The Cloud page (Settings > Cloud): the link of the deck to its cloud service, for the share
// links and Parts with Friends. Connect takes a code from `pwf enroll-code` on the computer of
// the owner of the service. The deck then signs its requests with its own key.

// CloudLink is the link of the deck to its service (cloud.Link).
type CloudLink interface {
	State() cloud.LinkState
	Connect(domain, code string)
	Check()
	Disconnect()
}

// cloudLine is the line of the Cloud row of Settings.
func cloudLine(s cloud.LinkState) string {
	if s.Connected {
		return "Connected to " + s.Domain
	}
	return "Off · share links and Parts with Friends"
}

// cloudDomain is the domain of the next Connect: the one that the owner typed, or the default.
func (a *App) cloudDomain() string {
	if a.cloudDomainSet != "" {
		return a.cloudDomainSet
	}
	return cloud.DefaultDomain
}

// drawCloud is the Cloud page.
func (a *App) drawCloud(c *gfx.Canvas) {
	f := a.navPage(c, "Cloud")
	s := a.Cloud.State()
	r := kit.Rows(f.body(), 10, kit.Px(80), kit.Fr(1))
	info := r[0]
	window(c, info)
	a.region("State", info)
	state, line := "NOT CONNECTED", "SERVICE: "+strings.ToUpper(a.cloudDomain())
	switch {
	case s.Busy:
		state = "WAITING FOR THE SERVICE"
	case s.Connected:
		state, line = "CONNECTED · "+strings.ToUpper(s.Name), "DECK "+strings.ToUpper(s.Deck)
	}
	if s.Err != "" && !s.Busy {
		line = s.Err
	}
	in := kit.Inset(info, 16, 8, 16, 8)
	rows := kit.Rows(in, 0, kit.Fr(1), kit.Fr(1))
	c.Text(rows[0], gfx.Fit(gfx.Face(true, 17), state, in.Dx()), gfx.Face(true, 17), lcdInk, gfx.Left)
	col := lcdDim
	if s.Err != "" && !s.Busy {
		col = lcdInk
	}
	c.Text(rows[1], gfx.Fit(gfx.Face(true, 14), line, in.Dx()), gfx.Face(true, 14), col, gfx.Left)

	var items []menuItem
	if s.Connected {
		checked := "Ask the service for this deck"
		if !s.Checked.IsZero() {
			checked = "Good at " + clockText(s.Checked, a.Clock24)
		}
		items = []menuItem{
			{"Service", s.Domain + " · since " + fileDate(s.Enrolled.Local(), a.Now(), a.Clock24), dim, nil},
			{"Check the connection", checked, dim, a.Cloud.Check},
			{"Disconnect", "Remove this deck from the service", dim, func() {
				a.confirm = &confirmBox{question: "Disconnect from " + s.Domain + "?", yes: "Disconnect", danger: true,
					onYes: a.Cloud.Disconnect}
			}},
		}
	} else {
		items = []menuItem{
			{"Connect", "Type the code from pwf enroll-code", dim, a.askCloudCode},
			{"Service", a.cloudDomain(), dim, a.editCloudDomain},
		}
	}
	a.menuRows(c, r[1], f.wide, 60, 5, items)
}

// askCloudCode opens the keyboard for the enrollment code.
func (a *App) askCloudCode() {
	a.textKB = &keyboard{title: "Code from pwf enroll-code", max: 20, ok: "Connect", cancel: "Back", shift: true,
		back: func() { a.textKB = nil },
		done: func(v string) {
			a.textKB = nil
			if strings.TrimSpace(v) == "" {
				return
			}
			a.Cloud.Connect(a.cloudDomain(), v)
		}}
}

// editCloudDomain opens the keyboard for the domain of the service.
func (a *App) editCloudDomain() {
	a.textKB = &keyboard{title: "Domain of the service", value: a.cloudDomain(), max: 120, ok: "Save", cancel: "Back",
		back: func() { a.textKB = nil },
		done: func(v string) {
			a.textKB = nil
			a.cloudDomainSet = strings.TrimSpace(v)
		}}
}
