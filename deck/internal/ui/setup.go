package ui

import (
	"fmt"
	"image"
	"os"
	"strings"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/wifi"
)

// SetupHost is the system side of the setup screen: the Wi-Fi network and the login.
type SetupHost interface {
	Scan() error
	Networks() ([]wifi.Network, error)
	Status() (wifi.Status, error)
	SetWifi(ssid, password string) error
	// Login is the user and password for SSH, sudo and the remote screen, which the system made at
	// the first start. They are empty when the system has none.
	Login() (user, password string)
	Done() // the setup is finished, so the deck does not open it again at the next start
}

// setupFlow is the setup screen, over everything: 1 Wi-Fi, 2 the summary with the login. The Help
// screen is the summary alone.
type setupFlow struct {
	step   int
	nets   []wifi.Network
	status wifi.Status
	scroll int
	busy   bool
	msg    string
	ssid   string // the network that is joining
	host   string
	help   bool // opened as the Help screen
	kb     *keyboard
	scans  int // the number of the last scan; a timer for an older scan does nothing
}

const (
	setupWifi = iota
	setupJoin
	setupDone
)

const setupRowH = 64

// OpenSetup shows the setup screen. It does nothing when the deck has no SetupHost.
func (a *App) OpenSetup() {
	if a.Setup == nil {
		return
	}
	host, _ := os.Hostname()
	a.setup = &setupFlow{host: host}
	a.setupScan()
	a.Redraw()
}

// OpenHelp shows the addresses of the Printdeck and its login. It does nothing when the deck has no
// SetupHost.
func (a *App) OpenHelp() {
	if a.Setup == nil {
		return
	}
	host, _ := os.Hostname()
	s := &setupFlow{host: host, step: setupDone, help: true}
	a.setup = s
	h := a.Setup
	go func() {
		st, _ := h.Status()
		a.post(func() { s.status = st })
	}()
	a.Redraw()
}

// The network list scans again by itself: soon after an error, because wpa_supplicant can start
// after the deck, and then now and then, so that the list stays current.
const (
	setupRetry   = 3 * time.Second
	setupRefresh = 15 * time.Second
)

// setupScan scans now, for the Scan button, and shows "Scanning…".
func (a *App) setupScan() { a.scanNetworks(false) }

// scanNetworks scans and reads the results a few seconds later. An automatic scan (quiet) does not
// show "Scanning…", and keeps the list when it fails. Each scan starts the timer of the next one.
func (a *App) scanNetworks(quiet bool) {
	s := a.setup
	s.scans++
	gen := s.scans
	if !quiet {
		s.busy, s.msg = true, "Scanning…"
	}
	h := a.Setup
	go func() {
		err := h.Scan()
		if err == nil {
			time.Sleep(4 * time.Second)
		}
		nets, nerr := h.Networks()
		st, _ := h.Status()
		a.post(func() {
			s.status = st
			if !quiet {
				s.busy = false
			}
			if err == nil {
				err = nerr
			}
			next := setupRefresh
			switch {
			case err != nil && quiet:
				next = setupRetry
				if len(s.nets) == 0 {
					s.msg = "Waiting for Wi-Fi…"
				}
			case err != nil:
				next = setupRetry
				s.msg = "Wi-Fi: " + err.Error()
			default:
				s.nets, s.msg = nets, ""
				if len(nets) == 0 {
					next, s.msg = setupRetry, "No networks yet. Scanning again."
				}
			}
			a.nextScan(s, gen, next)
		})
	}()
}

// nextScan starts an automatic scan after d. A newer scan, or a closed setup screen, cancels it.
// While the keyboard is open or the network is joining, it waits.
func (a *App) nextScan(s *setupFlow, gen int, d time.Duration) {
	time.AfterFunc(d, func() {
		a.post(func() {
			switch {
			case a.setup != s || s.scans != gen:
			case s.step != setupWifi || s.kb != nil || s.busy:
				a.nextScan(s, gen, setupRetry)
			default:
				a.scanNetworks(true)
			}
		})
	})
}

// setupJoin sets the network, then waits up to 30 seconds for an address.
func (a *App) setupJoin(ssid, pass string) {
	s := a.setup
	s.kb, s.step, s.ssid, s.busy, s.msg = nil, setupJoin, ssid, true, ""
	h := a.Setup
	go func() {
		if err := h.SetWifi(ssid, pass); err != nil {
			a.post(func() { s.step, s.busy, s.msg = setupWifi, false, err.Error() })
			return
		}
		for i := 0; i < 30; i++ {
			time.Sleep(time.Second)
			st, _ := h.Status()
			if st.Connected() && st.SSID == ssid {
				a.post(func() {
					s.status, s.busy, s.step = st, false, setupDone
				})
				return
			}
		}
		a.post(func() {
			s.step, s.busy = setupWifi, false
			s.msg = "Could not join " + ssid + ". Check the password."
		})
	}()
}

func (a *App) drawSetup(c *gfx.Canvas) {
	s := a.setup
	a.hits = a.hits[:0]
	if s.kb != nil {
		a.drawKeyboard(c, s.kb)
		a.setupMessage(c, image.Rect(24, 222, W-24, 252))
		return
	}
	box := image.Rect(12, 12, W-12, H-12)
	title, name := []string{"1/2 Wi-Fi", "1/2 Wi-Fi", "2/2 Ready"}[s.step], "SETUP"
	if s.help && s.step == setupDone {
		title, name = "Help", "HELP"
	}
	panel(c, box, name)
	c.Fill(image.Rect(box.Min.X+24, box.Min.Y+30, box.Min.X+32, box.Min.Y+46), orange)
	c.Text(image.Rect(box.Min.X+42, box.Min.Y+20, box.Max.X-24, box.Min.Y+56), strings.ToUpper(title), gfx.Face(true, 22), text, gfx.Left)

	switch s.step {
	case setupWifi:
		a.drawSetupWifi(c, box)
	case setupJoin:
		label(c, image.Rect(24, 300, W-24, 340), "Joining "+s.ssid+"…", gfx.Center)
	case setupDone:
		a.drawSetupDone(c, box)
	}
	a.setupMessage(c, image.Rect(24, box.Max.Y-130, W-24, box.Max.Y-100))
}

func (a *App) setupMessage(c *gfx.Canvas, r image.Rectangle) {
	if m := a.setup.msg; m != "" {
		c.Text(r, gfx.Fit(gfx.Face(false, 16), m, r.Dx()), gfx.Face(false, 16), dim, gfx.Center)
	}
}

func (a *App) drawSetupWifi(c *gfx.Canvas, box image.Rectangle) {
	s := a.setup
	head := "Pick the Wi-Fi network."
	if s.status.Connected() {
		head = "On " + s.status.SSID + " at " + s.status.IP + "."
	}
	label(c, image.Rect(24, 74, W-24, 100), gfx.Fit(gfx.Face(false, 16), head, W-48), gfx.Left)

	// The list of networks. It scrolls when it is longer than the window.
	list := image.Rect(24, 112, W-24, box.Max.Y-140)
	window(c, list)
	maxScroll := max(0, len(s.nets)*setupRowH-list.Dy())
	s.scroll = max(0, min(s.scroll, maxScroll))
	for i, n := range s.nets {
		y := list.Min.Y + i*setupRowH - s.scroll
		if y < list.Min.Y || y+setupRowH > list.Max.Y {
			continue // only whole rows
		}
		row := image.Rect(list.Min.X, y, list.Max.X, y+setupRowH)
		if i > 0 {
			c.Fill(image.Rect(row.Min.X+12, y, row.Max.X-12, y+1), lcdCell)
		}
		if n.SSID == s.status.SSID && s.status.Connected() {
			led(c, row.Min.X+20, row.Min.Y+setupRowH/2, true)
		}
		face := gfx.Face(true, 20)
		c.Text(image.Rect(row.Min.X+38, row.Min.Y, row.Max.X-110, row.Max.Y), gfx.Fit(face, n.SSID, row.Dx()-150), face, lcdInk, gfx.Left)
		if n.Secure {
			c.Text(image.Rect(row.Max.X-110, row.Min.Y, row.Max.X-62, row.Max.Y), "LOCK", gfx.Face(true, 12), lcdDim, gfx.Right)
		}
		signalBars(c, image.Pt(row.Max.X-50, row.Min.Y+setupRowH/2+12), n.Signal)
	}
	a.addHit(hit{r: list, scroll: func(dy int) { s.scroll += dy }, tapAt: func(p image.Point) {
		i := (p.Y - list.Min.Y + s.scroll) / setupRowH
		if i < 0 || i >= len(s.nets) || s.busy {
			return
		}
		n := s.nets[i]
		if !n.Secure {
			a.setupJoin(n.SSID, "")
			return
		}
		a.setupPassword(n.SSID)
	}})

	// Scan, Hidden, and Next (or Skip when there is no Wi-Fi)
	y := box.Max.Y - 92
	bw := (W - 48 - 16) / 3
	if s.busy {
		key(c, image.Rect(24, y, 24+bw, y+68), "Scan", surface, dim, false)
	} else {
		a.button(c, image.Rect(24, y, 24+bw, y+68), "Scan", raised, text, a.setupScan)
	}
	a.button(c, image.Rect(24+bw+8, y, 24+2*bw+8, y+68), "Hidden", raised, text, func() {
		s.kb = &keyboard{title: "Name of the network", max: 32, ok: "Next", cancel: "Back",
			back: func() { s.kb = nil },
			done: func(ssid string) {
				if ssid != "" {
					a.setupPassword(ssid)
				}
			}}
	})
	next := "Skip"
	if s.status.Connected() {
		next = "Next"
	}
	a.button(c, image.Rect(W-24-bw, y, W-24, y+68), next, green, onAcc, func() { s.step = setupDone })
}

// setupPassword asks for the password of a network. An empty password is for an open network.
func (a *App) setupPassword(ssid string) {
	s := a.setup
	s.msg = ""
	s.kb = &keyboard{title: "Password for " + ssid, secret: true, max: 63, ok: "Join", cancel: "Back",
		back: func() { s.kb = nil },
		done: func(pass string) {
			if pass != "" && len(pass) < 8 {
				s.msg = "A Wi-Fi password has 8 to 63 characters."
				return
			}
			a.setupJoin(ssid, pass)
		}}
}

func (a *App) drawSetupDone(c *gfx.Canvas, box image.Rectangle) {
	s := a.setup
	var lines []string // a label, then its value
	if s.status.Connected() {
		lines = append(lines, "Wi-Fi", s.status.SSID+"  "+s.status.IP)
	} else {
		lines = append(lines, "Wi-Fi", "Not connected. Ethernet works with no setup.")
	}
	lines = append(lines, "Remote screen and slicers", fmt.Sprintf("http://%s.local", s.host))
	if user, pass := a.Setup.Login(); user != "" {
		lines = append(lines, "User (SSH, sudo, remote screen)", user, "Password", pass,
			"SSH", fmt.Sprintf("ssh %s@%s.local", user, s.host))
	}
	const rowH = 30
	win := image.Rect(24, 80, W-24, 80+len(lines)/2*(2*rowH+10)+14)
	window(c, win)
	for i := 0; i < len(lines); i += 2 {
		y := win.Min.Y + 12 + i/2*(2*rowH+10)
		label, value := image.Rect(win.Min.X+16, y, win.Max.X-16, y+rowH), image.Rect(win.Min.X+16, y+rowH, win.Max.X-16, y+2*rowH)
		c.Text(label, strings.ToUpper(lines[i]), gfx.Face(true, 13), lcdDim, gfx.Left)
		face := gfx.Face(true, 20)
		c.Text(value, gfx.Fit(face, lines[i+1], value.Dx()), face, lcdInk, gfx.Left)
	}
	y := box.Max.Y - 92
	half := (W - 48 - 8) / 2
	if s.help {
		a.button(c, image.Rect(24, y, 24+half, y+68), "Wi-Fi", raised, text, func() { s.step = setupWifi; a.setupScan() })
		a.button(c, image.Rect(W-24-half, y, W-24, y+68), "Close", green, onAcc, func() { a.setup = nil })
		return
	}
	a.button(c, image.Rect(24, y, 24+half, y+68), "Back", raised, text, func() { s.step = setupWifi; a.setupScan() })
	a.button(c, image.Rect(W-24-half, y, W-24, y+68), "Finish", green, onAcc, func() {
		a.Setup.Done()
		a.setup = nil
	})
}

// signalBars draws a signal level in dBm as one to four bars, from the bottom left corner p.
func signalBars(c *gfx.Canvas, p image.Point, dbm int) {
	n := 1
	switch {
	case dbm >= -55:
		n = 4
	case dbm >= -65:
		n = 3
	case dbm >= -75:
		n = 2
	}
	for i := 0; i < 4; i++ {
		col := lcdCell
		if i < n {
			col = lcdInk
		}
		h := 6 + i*6
		c.Fill(image.Rect(p.X+i*9, p.Y-h, p.X+i*9+6, p.Y), col)
	}
}
