//go:build js && wasm

package main

import (
	"fmt"
	"strings"
	"syscall/js"

	"printdeck/internal/cloud/wire"
)

// The page of a printer of a project, /projects/<slug>/printers/<printer slug>: a 3D view of the
// printer (assets/viewer3d.js) with the picture of a print over its bed, the numbers of the
// project on this printer, and its prints. A tap on a tile puts the print on the bed, and a second
// tap opens the page of the print. The project page lists its printers with links to these pages.

// printerSlugs returns the slug of each printer of the project v, by ID: the letters and digits
// of its name, in lower case, with "-" between the words. Two printers with the same slug get the
// start of their IDs after it.
func printerSlugs(v *wire.ProjectView) map[string]string {
	out := map[string]string{}
	seen := map[string]int{}
	for _, p := range v.Printers {
		seen[slugOf(p.Name)]++
	}
	for _, p := range v.Printers {
		s := slugOf(p.Name)
		if seen[s] > 1 {
			s += "-" + p.ID[:min(4, len(p.ID))]
		}
		out[p.ID] = s
	}
	return out
}

// slugOf is the letters and digits of name in lower case, with "-" between the words.
func slugOf(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		} else {
			dash = true
		}
	}
	if b.Len() == 0 {
		return "printer"
	}
	return b.String()
}

// printerPath is the address of the page of the printer with the slug in the project slug.
func printerPath(project, printer string) string {
	return "/projects/" + project + "/printers/" + printer
}

// showPrinterPage shows the page of the printer with the slug printer in the project id.
func (a *app) showPrinterPage(id, printer string) {
	var v wire.ProjectView
	if err := a.api("GET", "/projects/"+id, nil, &v); err != nil {
		a.failed(err)
		return
	}
	a.view, a.print = &v, ""
	slugs := printerSlugs(&v)
	var pr *wire.Printer
	for i, p := range v.Printers {
		if slugs[p.ID] == printer {
			pr = &v.Printers[i]
		}
	}
	if pr == nil {
		replaceAddress(projectPath(v.Slug, 0))
		a.drawProject()
		show("vProject")
		return
	}
	doc.Set("title", pr.Name+" · "+v.Name+" · Printdeck")
	byID("ptBack").Set("href", projectPath(v.Slug, 0))
	setText("ptBack", v.Name)
	setText("ptName", pr.Name)
	setText("ptKind", printerKind(*pr))
	var prints []wire.ProjectPrint
	for _, p := range v.Prints {
		if p.PrinterID == pr.ID {
			prints = append(prints, p)
		}
	}
	tilePrinters = nil
	drawPrinterStats(*pr, prints)
	picture := ""
	if len(prints) > 0 {
		picture = prints[0].Model
	}
	show("vPrinter")
	view3d(*pr, picture)
	a.drawPrinterTiles(v, *pr, prints)
}

// printerKind is the process and the model of a printer: "Resin · Phenom".
func printerKind(p wire.Printer) string {
	kind := map[string]string{"fdm": "Filament", "resin": "Resin"}[p.Process]
	if p.Model != "" && p.Model != p.Name {
		kind += " · " + p.Model
	}
	return kind
}

// view3d shows the 3D view of the printer p with the picture of a print over its bed.
func view3d(p wire.Printer, picture string) {
	v := js.Global().Get("pwfView3d")
	if v.IsUndefined() {
		return
	}
	opts := js.Global().Get("Object").New()
	opts.Set("model", p.Model+" "+p.Name)
	opts.Set("process", p.Process)
	opts.Set("picture", picture)
	opts.Set("credit", byID("ptCredit"))
	v.Call("show", byID("ptView"), opts)
}

// drawPrinterStats fills the numbers of the printer page: the prints, the print time, and the
// material.
func drawPrinterStats(p wire.Printer, prints []wire.ProjectPrint) {
	secs, grams, done := 0, 0.0, 0
	for _, x := range prints {
		secs += x.Seconds
		grams += x.Grams
		if x.Result == "complete" {
			done++
		}
	}
	setText("ptPrints", fmt.Sprint(len(prints)))
	setText("ptPrintsSub", fmt.Sprintf("%d complete", done))
	setText("ptTime", fmt.Sprintf("%d:%02d", secs/3600, secs/60%60))
	setText("ptTimeSub", "hours")
	if p.Process == "resin" {
		setText("ptMatLab", "Resin")
		setText("ptMat", "--")
		setText("ptMatSub", "")
	} else {
		setText("ptMatLab", "Filament")
		setText("ptMat", fmt.Sprintf("%.0f g", grams))
		setText("ptMatSub", "")
	}
	setHidden("ptNoPrints", len(prints) > 0)
}

// drawPrinterTiles draws the prints of the printer page. The first tap on a tile puts its picture
// on the bed; the next tap opens the page of the print.
func (a *app) drawPrinterTiles(v wire.ProjectView, pr wire.Printer, prints []wire.ProjectPrint) {
	box := byID("ptPrints2")
	box.Set("textContent", "")
	chosen := ""
	if len(prints) > 0 {
		chosen = prints[0].ID
	}
	var tiles []js.Value
	for i, p := range prints {
		tile := printTile(p, projectPath(v.Slug, p.N))
		tile.Call("setAttribute", "aria-current", fmt.Sprint(p.ID == chosen))
		id, model := p.ID, p.Model
		idx := i
		tile.Call("addEventListener", "click", js.FuncOf(func(_ js.Value, args []js.Value) any {
			if id == chosen {
				return nil // the second tap: the link opens the print
			}
			args[0].Call("preventDefault")
			chosen = id
			for j, t := range tiles {
				t.Call("setAttribute", "aria-current", fmt.Sprint(j == idx))
			}
			view3d(pr, model)
			return nil
		}))
		tiles = append(tiles, tile)
		box.Call("appendChild", tile)
	}
}

// drawProjectPrinters fills the list of the printers of the project page, with a link to the
// page of each printer.
func drawProjectPrinters(v *wire.ProjectView) {
	box := byID("pPrinterList")
	box.Set("textContent", "")
	counts := map[string]int{}
	for _, p := range v.Prints {
		counts[p.PrinterID]++
	}
	slugs := printerSlugs(v)
	n := 0
	for _, p := range v.Printers {
		li := el("li", "", "")
		link := el("a", "", p.Name)
		link.Set("href", printerPath(v.Slug, slugs[p.ID]))
		li.Call("appendChild", link)
		li.Call("appendChild", el("span", "", fmt.Sprintf("%s · %s", printerKind(p), count(counts[p.ID], "print", "prints"))))
		box.Call("appendChild", li)
		n++
	}
	setHidden("pPrinterCard", n == 0)
}
