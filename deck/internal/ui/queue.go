package ui

import (
	"errors"
	"fmt"
	"image"
	"strings"

	"printdeck/internal/gfx"
	"printdeck/internal/queue"
	"printdeck/kit"
)

// The print queue of the deck. The queue never starts a print by itself: at the end of each print
// the deck asks on each screen, and a press on Start starts the next file. On a resin printer
// that press is the only start of a queued file.

// queueItem is the file path as a file of the queue, with its name, material and time.
func (a *App) queueItem(path, from string) queue.Item {
	m := a.Printer.FileMeta(path)
	it := queue.Item{Path: path, Name: m.Name(path), Project: m.Project, Material: m.Material, MaterialName: m.MaterialName,
		Color: m.Color, Seconds: int(m.PrintTime.Seconds()), From: from}
	// A resin file: the time from its header, and the project of the next print.
	if r, ok := a.Printer.(interface{ FileSeconds(string) int }); ok && it.Seconds == 0 {
		it.Seconds = r.FileSeconds(path)
	}
	if it.Project == "" && a.resin() {
		it.Project = a.Printer.NextProject()
	}
	return it
}

// Enqueue adds the file path to the end of the queue. from is "deck", "slicer" or "build". It is
// safe from any goroutine.
func (a *App) Enqueue(path, from string) error {
	if a.Queue == nil {
		return errors.New("this deck has no queue")
	}
	_, err := a.Queue.Add(a.queueItem(path, from))
	a.Redraw()
	return err
}

// EnqueueAs adds the file path to the end of the queue with the name and the project, for a plate
// of a build of the service. It is safe from any goroutine.
func (a *App) EnqueueAs(path, from, name, project string) error {
	if a.Queue == nil {
		return errors.New("this deck has no queue")
	}
	it := a.queueItem(path, from)
	if name != "" {
		it.Name = name
	}
	if project != "" {
		it.Project = project
	}
	_, err := a.Queue.Add(it)
	a.Redraw()
	return err
}

// queueTime is a time of print for the queue, for example "45 min" or "3 h 20 min".
func queueTime(sec int) string {
	m := (sec + 59) / 60
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	return fmt.Sprintf("%d h %d min", m/60, m%60)
}

// queueLine is the line under the name of a file of the queue: its material and its time.
func queueLine(it queue.Item) string {
	var parts []string
	switch {
	case it.MaterialName != "":
		parts = append(parts, it.MaterialName)
	case it.Material != "":
		parts = append(parts, it.Material)
	}
	if it.Seconds > 0 {
		parts = append(parts, queueTime(it.Seconds))
	}
	if it.Project != "" {
		parts = append(parts, it.Project)
	}
	if len(parts) == 0 {
		return "No time from the slicer"
	}
	return strings.Join(parts, " · ")
}

// queueSummary is the line of the Queue key: "Empty", or "3 files · 6 h 10 min".
func (a *App) queueSummary() string {
	list := a.Queue.List()
	if len(list) == 0 {
		return "Empty"
	}
	s := fmt.Sprintf("%d %s", len(list), plural(len(list), "file", "files"))
	if sec := a.Queue.Seconds(); sec > 0 {
		s += " · " + queueTime(sec)
	}
	return s
}

// startQueued starts the file it of the queue after a press: the material check asks first when
// the material can be wrong. The file leaves the queue when it starts.
func (a *App) startQueued(it queue.Item) {
	if st := a.Printer.State(); st.Busy() || !st.Reachable {
		a.confirm = &confirmBox{question: "The printer is not ready. The file stays in the queue.", yes: "OK", onYes: func() {}}
		return
	}
	start := func() {
		a.Queue.Remove(it.ID)
		a.Printer.PrintFile(it.Path)
		a.pages = nil
	}
	if a.resin() {
		start()
		return
	}
	q, yes, danger, set := a.materialCheck(it.Path)
	if q == "" {
		start()
		return
	}
	a.confirm = &confirmBox{question: q, yes: yes, danger: danger, onYes: func() {
		if set != "" {
			a.setMaterial(set)
		}
		start()
	}}
}

// AskQueueNext asks on each screen to start the next file of the queue: at the end of a print,
// when the printer is free. It does nothing with an empty queue or a print that runs. It is safe
// from any goroutine.
func (a *App) AskQueueNext() {
	a.post(func() {
		if !a.Printer.State().Busy() {
			a.askQueueNext()
		}
	})
}

// askQueueNext asks on each screen to start the next file of the queue.
func (a *App) askQueueNext() {
	{
		if a.Queue == nil {
			return
		}
		it, ok := a.Queue.Next()
		if !ok {
			return
		}
		more := ""
		if n := a.Queue.Len(); n > 1 {
			more = fmt.Sprintf(" · %d more after it", n-1)
		}
		start := "Take the part off the bed first"
		if a.resin() {
			start = "Check the vat and the plate first"
		}
		a.hub.ask(&confirmBox{question: "Next: " + gfx.Fit(gfx.Face(true, 26), it.Name, 360), choices: []menuItem{
			{"Start", start + " · " + queueLine(it), dim, func() { a.startQueued(it) }},
			{"Skip", "Take it out of the queue" + more, dim, func() {
				a.Queue.Remove(it.ID)
				a.askQueueNext()
			}},
			{"Later", "It waits in Machine, Queue", dim, func() {}},
		}})
	}
}

// AskSentFile asks on each screen what to do with a file that a slicer asks to print while the
// queue has files. It is safe from any goroutine.
func (a *App) AskSentFile(path string) {
	a.post(func() { a.askSentFile(path) })
}

// askSentFile asks what to do with a file that a slicer sent to print while the queue has files:
// print it now, add it to the queue, or start the next file of the queue.
func (a *App) askSentFile(path string) {
	it := a.queueItem(path, "slicer")
	next, _ := a.Queue.Next()
	a.hub.ask(&confirmBox{question: "Print " + gfx.Fit(gfx.Face(true, 26), it.Name, 340) + "?", choices: []menuItem{
		{"Print it now", "The queue waits", dim, func() {
			q, yes, danger, set := a.materialCheck(path)
			if q == "" {
				a.Printer.PrintFile(path)
				return
			}
			a.confirm = &confirmBox{question: q, yes: yes, danger: danger, onYes: func() {
				if set != "" {
					a.setMaterial(set)
				}
				a.Printer.PrintFile(path)
			}}
		}},
		{"Add it to the queue", fmt.Sprintf("After %d %s", a.Queue.Len(), plural(a.Queue.Len(), "file", "files")), dim, func() {
			a.Queue.Add(it)
		}},
		{"Start the queue", "Next: " + fitText(next.Name, 240), dim, func() { a.startQueued(next) }},
	}})
}

// drawQueue is the Queue page: the files in their order, each with its material and its time. A
// tap on a file asks: start it now, move it to the front or up, or take it out.
func (a *App) drawQueue(c *gfx.Canvas) {
	list := a.Queue.List()
	title := "Queue"
	if sec := a.Queue.Seconds(); sec > 0 {
		title += " · " + queueTime(sec)
	}
	f := a.navPage(c, title)
	area := a.listArea(f)
	if len(list) == 0 {
		noItems(c, area, "", "No files in the queue.", "A file that a slicer sends during a print goes here.", "On a file, tap Add to queue.")
		return
	}
	l := kit.List{Area: area, N: len(list), RowH: projectRowH, Gap: 10, Scroll: &a.queueScroll}
	a.list(c, l, func(i int) string { return list[i].Name }, func(c *gfx.Canvas, i int, box image.Rectangle) {
		it := list[i]
		plate(c, box)
		h1, h2 := 30, 26
		in := kit.Inset(box, 18, (box.Dy()-h1-h2)/2, 18, 0)
		s := kit.NewStack(in, 0)
		name := fmt.Sprintf("%d. %s", i+1, it.Name)
		c.Text(s.Next(h1), gfx.Fit(gfx.Face(true, 19), name, in.Dx()), gfx.Face(true, 19), text, gfx.Left)
		c.Text(s.Next(h2), gfx.Fit(gfx.Face(false, 16), queueLine(it), in.Dx()), gfx.Face(false, 16), dim, gfx.Left)
	}, func(i int) {
		it := list[i]
		var choices []menuItem
		if !a.Printer.State().Busy() {
			choices = append(choices, menuItem{"Start now", queueLine(it), dim, func() { a.startQueued(it) }})
		}
		if i > 0 {
			choices = append(choices,
				menuItem{"Move to the front", "It prints next", dim, func() { a.Queue.Move(it.ID, -i) }},
				menuItem{"Move up", fmt.Sprintf("To place %d", i), dim, func() { a.Queue.Move(it.ID, -1) }})
		}
		choices = append(choices, menuItem{"Take it out", "The file stays on the deck", dim, func() { a.Queue.Remove(it.ID) }})
		a.confirm = &confirmBox{question: fitText(it.Name, 300), choices: choices}
	})
}
