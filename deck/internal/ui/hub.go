package ui

import "sync"

// hub is what the App of the deck and its sessions (the browsers, the Cura tab, the window of the
// desktop app) share: a question from outside, such as a slicer that asks for a key or a print
// that asks for the material. Each screen shows the question, and the first answer closes it on
// all of them.
type hub struct {
	mu   sync.Mutex
	q    *confirmBox
	apps map[*App]bool
}

func newHub() *hub { return &hub{apps: map[*App]bool{}} }

// join adds an App that draws frames; leave removes it.
func (h *hub) join(a *App) {
	h.mu.Lock()
	h.apps[a] = true
	h.mu.Unlock()
}

func (h *hub) leave(a *App) {
	h.mu.Lock()
	delete(h.apps, a)
	h.mu.Unlock()
}

// question is the open question, or nil.
func (h *hub) question() *confirmBox {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.q
}

// redraw draws a new frame on each screen.
func (h *hub) redraw() {
	h.mu.Lock()
	apps := make([]*App, 0, len(h.apps))
	for a := range h.apps {
		apps = append(apps, a)
	}
	h.mu.Unlock()
	for _, a := range apps {
		a.Redraw()
	}
}

// ask shows cf on each screen. An answer on one screen closes it on all of them. It is safe to
// call from any goroutine.
func (h *hub) ask(cf *confirmBox) {
	cf.shared = true
	yes, no := cf.onYes, cf.onNo
	done := func() {
		h.mu.Lock()
		if h.q == cf {
			h.q = nil
		}
		h.mu.Unlock()
		h.redraw()
	}
	cf.onYes = func() {
		done()
		if yes != nil {
			yes()
		}
	}
	cf.onNo = func() {
		done()
		if no != nil {
			no()
		}
	}
	h.mu.Lock()
	h.q = cf
	h.mu.Unlock()
	h.redraw()
}

// showShared shows the question of the hub as the dialog of this screen, and closes it when
// another screen answered it. Draw calls it at the start of a frame.
func (a *App) showShared() {
	q := a.hub.question()
	switch {
	case a.confirm != nil && a.confirm.shared && a.confirm != q:
		a.confirm = nil // answered on another screen
	case a.confirm == nil && q != nil:
		a.confirm = q
	}
}
