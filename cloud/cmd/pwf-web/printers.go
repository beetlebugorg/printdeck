//go:build js && wasm

package main

import (
	"fmt"
	"sort"
	"syscall/js"

	"printdeck/internal/cloud/wire"
)

// The printers of a project: a row of keys over the prints, one for each printer, when the prints
// of the project come from two printers or more. A key shows the prints and the numbers of its
// printer. The tiles then name their printer.

// printerFilter is the printer of the key that is down, or "" for all the printers.
var printerFilter string

// stateFilter is the state of the prints that show: "" for all, "ready" for the prints that are
// ready to pick up, or "delivered".
var stateFilter string

// printState is the state of a print for stateFilter: "discarded", "test", "inuse", "delivered",
// "ready", or "". A print in use is not ready to pick up.
func printState(p wire.ProjectPrint) string {
	switch {
	case !p.Discarded.IsZero():
		return "discarded"
	case !p.Test.IsZero():
		return "test"
	case !p.InUse.IsZero():
		return "inuse"
	case !p.Delivered.IsZero():
		return "delivered"
	case !p.Ready.IsZero():
		return "ready"
	}
	return ""
}

// tilePrinters are the names of the printers for the tiles, by ID, when the prints have two
// printers or more; else nil.
var tilePrinters map[string]string

// shownView and shownHref are the project and the addresses of the last drawPrints, for a key.
var (
	shownView *wire.ProjectView
	shownHref func(wire.ProjectPrint) string
)

// drawPrints draws the numbers, the row of the printers and the tiles of the project v, for the
// printer of printerFilter. href is the address of the page of a print.
func drawPrints(v *wire.ProjectView, href func(wire.ProjectPrint) string) {
	shownView, shownHref = v, href
	setTilePhotos(v)
	names := map[string]string{}
	for _, p := range v.Printers {
		names[p.ID] = p.Name
	}
	counts := map[string]int{}
	for _, p := range v.Prints {
		if names[p.PrinterID] != "" {
			counts[p.PrinterID]++
		}
	}
	if counts[printerFilter] == 0 {
		printerFilter = ""
	}
	shown := v.Prints
	if printerFilter != "" {
		shown = nil
		for _, p := range v.Prints {
			if p.PrinterID == printerFilter {
				shown = append(shown, p)
			}
		}
	}
	tilePrinters = nil
	if len(counts) > 1 {
		tilePrinters = names
	}
	drawStats(shown)
	// The keys of the states: all, ready to pick up, delivered. A delivered print shows the newest
	// delivery first.
	states := map[string]int{}
	for _, p := range shown {
		states[printState(p)]++
	}
	if states[stateFilter] == 0 {
		stateFilter = ""
	}
	byState := shown
	if stateFilter != "" {
		byState = nil
		for _, p := range shown {
			if printState(p) == stateFilter {
				byState = append(byState, p)
			}
		}
		if stateFilter == "delivered" {
			sort.SliceStable(byState, func(i, j int) bool { return byState[i].Delivered.After(byState[j].Delivered) })
		}
	}
	srow := byID("pStates")
	srow.Set("textContent", "")
	setHidden("pStates", states["ready"]+states["delivered"]+states["inuse"]+states["test"]+states["discarded"] == 0)
	for _, k := range []struct{ id, label string }{{"", "All"}, {"ready", "Ready for pickup"}, {"delivered", "Delivered"}, {"inuse", "In use"}, {"test", "Test prints"}, {"discarded", "Rejected"}} {
		n := len(shown)
		if k.id != "" {
			n = states[k.id]
			if n == 0 {
				continue
			}
		}
		b := el("button", "", fmt.Sprintf("%s · %d", k.label, n))
		b.Set("type", "button")
		b.Call("setAttribute", "aria-pressed", fmt.Sprint(k.id == stateFilter))
		id := k.id
		b.Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any {
			stateFilter = id
			drawPrints(shownView, shownHref)
			return nil
		}))
		srow.Call("appendChild", b)
	}
	drawTiles(byState, href)

	row := byID("pPrinters")
	row.Set("textContent", "")
	setHidden("pPrinters", len(counts) < 2)
	if len(counts) < 2 {
		return
	}
	ids := make([]string, 0, len(counts))
	for id := range counts {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return names[ids[i]] < names[ids[j]] })
	key := func(id, label string) {
		b := el("button", "", label)
		b.Set("type", "button")
		b.Call("setAttribute", "aria-pressed", fmt.Sprint(id == printerFilter))
		b.Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any {
			printerFilter = id
			drawPrints(shownView, shownHref)
			return nil
		}))
		row.Call("appendChild", b)
	}
	key("", fmt.Sprintf("All printers · %d", len(v.Prints)))
	for _, id := range ids {
		key(id, fmt.Sprintf("%s · %d", names[id], counts[id]))
	}
}

// drawPrinterList fills the card of the printers of the owner on the home page.
func (a *app) drawPrinterList() {
	var list []wire.Printer
	if a.kind != "owner" || a.api("GET", "/printers", nil, &list) != nil {
		setHidden("printersCard", true)
		return
	}
	box := byID("printersList")
	box.Set("textContent", "")
	for _, p := range list {
		li := el("li", "", "")
		li.Call("appendChild", el("strong", "", p.Name))
		kind := map[string]string{"fdm": "Filament", "resin": "Resin"}[p.Process]
		if p.Model != "" && p.Model != p.Name {
			kind += " · " + p.Model
		}
		li.Call("appendChild", el("span", "", kind))
		box.Call("appendChild", li)
	}
	setHidden("printersCard", len(list) == 0)
}
