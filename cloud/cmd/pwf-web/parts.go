//go:build js && wasm

package main

// The tab Parts of the project page: the parts of printdeck.yml of the newest plan of the
// default branch, each with its picture, its text and its facts, and a 3D view of a part or of an
// assembly. A person turns the view, takes the assembly apart (Explode) and cuts it open (Cut),
// to look at the design (assets/viewer3d.js: inspect, thumbs).

import (
	"fmt"
	"strings"
	"syscall/js"

	"printdeck/internal/cloud/wire"
)

// partsState is the state of the tab Parts.
type partsState struct {
	plan   string   // the ID of the plan on the tab
	shown  string   // "a<n>" for the assembly n, "p<n>" for the part n of the plan
	view   js.Value // the controls of the 3D view (inspect)
	thumbs map[string]string
}

var parts = partsState{thumbs: map[string]string{}}

// partsPlan is the plan of the tab: the newest ready plan of the default branch, else the newest
// ready plan.
func partsPlan(v *wire.ProjectView) *wire.ProjectPlan {
	var best *wire.ProjectPlan
	for i := range v.Plans {
		p := &v.Plans[i]
		if !p.Ready || len(p.Parts) == 0 {
			continue
		}
		if best == nil || (p.Kind == wire.BuildMain) != (best.Kind == wire.BuildMain) && p.Kind == wire.BuildMain ||
			(p.Kind == wire.BuildMain) == (best.Kind == wire.BuildMain) && p.Created.After(best.Created) {
			best = p
		}
	}
	return best
}

// partColor is the color of a part in the views: the color of its material, else orange for
// filament and gray for resin.
func partColor(pp wire.PlanPart) string {
	switch {
	case pp.MaterialColor != "":
		return pp.MaterialColor
	case pp.Process == "resin":
		return "#9aa0a8"
	}
	return "#e8762b"
}

// drawPartsTab fills the tab Parts, when a person opens the tab. The pictures of the cards stay
// for the next draw of the same plan.
func (a *app) drawPartsTab() {
	v := a.view
	pl := partsPlan(v)
	if pl == nil {
		setHidden("ptViewer", true)
		setHidden("ptNone", false)
		byID("ptGroups").Set("textContent", "")
		return
	}
	parts.plan = pl.ID
	setHidden("ptNone", true)
	setHidden("ptViewer", false)
	line := "From the commit " + shortSHA(pl.Commit)
	if pl.Branch != "" {
		line += " of " + pl.Branch
	}
	line += ", " + localWhen(pl.Created)
	setText("ptFrom", line)

	// The keys of the assemblies, over the view.
	keys := byID("ptAsmKeys")
	keys.Set("textContent", "")
	for i, as := range pl.Assemblies {
		b := el("button", "", as.Name)
		b.Set("type", "button")
		b.Get("dataset").Set("show", fmt.Sprintf("a%d", i))
		n := i
		b.Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any {
			go a.showPartsView(pl, fmt.Sprintf("a%d", n))
			return nil
		}))
		keys.Call("appendChild", b)
	}

	// The cards of the parts, in their groups. The variants of a part are one card.
	box := byID("ptGroups")
	box.Set("textContent", "")
	type card struct {
		name  string
		first int   // the index of the first variant in the plan
		all   []int // the indexes of the variants
	}
	var groups []string
	cards := map[string][]*card{}
	byName := map[string]*card{}
	for i, pp := range pl.Parts {
		if c := byName[strings.ToLower(pp.Name)]; c != nil {
			c.all = append(c.all, i)
			continue
		}
		g := pp.Group
		if _, ok := cards[g]; !ok {
			groups = append(groups, g)
		}
		c := &card{name: pp.Name, first: i, all: []int{i}}
		byName[strings.ToLower(pp.Name)] = c
		cards[g] = append(cards[g], c)
	}
	var thumbList []int
	for _, g := range groups {
		sec := el("section", "ptgroup", "")
		if g != "" {
			sec.Call("appendChild", el("h2", "", g))
		}
		grid := el("div", "ptcards", "")
		for _, c := range cards[g] {
			pp := pl.Parts[c.first]
			art := el("article", "ptcard", "")
			art.Set("id", fmt.Sprintf("ptCard%d", c.first))
			pic := el("button", "ptpic", "")
			pic.Set("type", "button")
			pic.Call("setAttribute", "aria-label", "Show "+pp.Name+" in 3D")
			pic.Call("appendChild", el("img", "", ""))
			art.Call("appendChild", pic)
			body := el("div", "ptbody", "")
			body.Call("appendChild", el("h3", "", pp.Name))
			if len(c.all) > 1 {
				chips := el("div", "ptvariants", "")
				for _, i := range c.all {
					chip := el("button", "chip", pl.Parts[i].Variant)
					chip.Set("type", "button")
					n := i
					chip.Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any {
						go a.showPartsView(pl, fmt.Sprintf("p%d", n))
						return nil
					}))
					chips.Call("appendChild", chip)
				}
				body.Call("appendChild", chips)
			}
			if pp.Description != "" {
				body.Call("appendChild", el("p", "ptdesc", pp.Description))
			}
			body.Call("appendChild", el("p", "ptfacts", partFacts(pl, pp)))
			links := el("div", "linkrow", "")
			view := el("button", "", "View in 3D")
			view.Set("type", "button")
			links.Call("appendChild", view)
			if pl.Repo != "" && len(pp.Files) > 0 {
				f := el("a", "", "The file on GitHub")
				f.Set("href", "https://github.com/"+pl.Repo+"/blob/"+pl.Commit+"/"+pp.Files[0])
				f.Set("target", "_blank")
				f.Set("rel", "noopener")
				links.Call("appendChild", f)
			}
			body.Call("appendChild", links)
			art.Call("appendChild", body)
			n := c.first
			show := js.FuncOf(func(js.Value, []js.Value) any {
				go a.showPartsView(pl, fmt.Sprintf("p%d", n))
				byID("ptViewer").Call("scrollIntoView", map[string]any{"behavior": "smooth", "block": "start"})
				return nil
			})
			pic.Call("addEventListener", "click", show)
			view.Call("addEventListener", "click", show)
			grid.Call("appendChild", art)
			thumbList = append(thumbList, c.first)
		}
		sec.Call("appendChild", grid)
		box.Call("appendChild", sec)
	}
	go drawPartThumbs(pl, thumbList)

	first := "p0"
	if len(pl.Assemblies) > 0 {
		first = "a0"
	}
	go a.showPartsView(pl, first)
}

// partFacts is the line of facts of a part: the printer, the material, the size and the count.
func partFacts(pl *wire.ProjectPlan, pp wire.ProjectPlanPart) string {
	var f []string
	f = append(f, printerName(pp.Printer))
	if pp.MaterialName != "" {
		f = append(f, pp.MaterialName)
	} else if pp.Material != "" {
		f = append(f, pp.Material)
	}
	if pp.SizeX > 0 {
		f = append(f, fmt.Sprintf("%.0f × %.0f × %.0f mm", pp.SizeX, pp.SizeY, pp.SizeZ))
	}
	if pp.Count > 1 {
		f = append(f, fmt.Sprintf("%d copies", pp.Count))
	}
	return strings.Join(f, " · ")
}

// drawPartThumbs draws the picture of each card, with one renderer, and keeps the pictures of
// the plan for the next draw.
func drawPartThumbs(pl *wire.ProjectPlan, list []int) {
	v := js.Global().Get("pwfView3d")
	if v.IsUndefined() {
		return
	}
	in := js.Global().Get("Array").New()
	var need []int
	for _, i := range list {
		key := pl.ID + "/" + fmt.Sprint(i)
		if u, ok := parts.thumbs[key]; ok {
			setThumb(i, u)
			continue
		}
		pp := pl.Parts[i]
		o := js.Global().Get("Object").New()
		urls := js.Global().Get("Array").New()
		for _, u := range pp.ModelURLs {
			urls.Call("push", u)
		}
		o.Set("urls", urls)
		o.Set("color", partColor(pp.PlanPart))
		in.Call("push", o)
		need = append(need, i)
	}
	if len(need) == 0 {
		return
	}
	out, err := await(v.Call("thumbs", in, 360))
	if err != nil {
		return
	}
	for k, i := range need {
		u := out.Index(k).String()
		parts.thumbs[pl.ID+"/"+fmt.Sprint(i)] = u
		setThumb(i, u)
	}
}

func setThumb(i int, url string) {
	card := byID(fmt.Sprintf("ptCard%d", i))
	if card.IsNull() || url == "" {
		return
	}
	card.Call("querySelector", ".ptpic img").Set("src", url)
}

// showPartsView shows the assembly ("a<n>") or the part ("p<n>") of the plan in the 3D view.
func (a *app) showPartsView(pl *wire.ProjectPlan, what string) {
	v := js.Global().Get("pwfView3d")
	if v.IsUndefined() {
		return
	}
	parts.shown = what
	var n int
	fmt.Sscanf(what[1:], "%d", &n)
	items := js.Global().Get("Array").New()
	var names []string
	var partOf []int // the part of the plan of each item, or -1
	title, desc := "", ""
	if what[0] == 'a' {
		as := pl.Assemblies[n]
		title, desc = as.Name, as.Description
		labels := map[string]int{}
		for i, pp := range pl.Parts {
			if pp.Variant != "" {
				labels[pp.Name+" ("+pp.Variant+")"] = i
			} else {
				labels[pp.Name] = i
			}
		}
		for i, it := range as.Items {
			o := js.Global().Get("Object").New()
			if i < len(as.ModelURLs) {
				o.Set("url", as.ModelURLs[i])
			}
			pi, ok := labels[it.Part]
			color := it.Color
			if !ok {
				pi = -1
			} else if color == "" {
				color = partColor(pl.Parts[pi].PlanPart)
			}
			if color == "" {
				color = "#8a8f98"
			}
			o.Set("color", color)
			o.Set("name", it.Name)
			if len(it.Explode) == 3 {
				e := js.Global().Get("Array").New()
				e.Call("push", it.Explode[0], it.Explode[1], it.Explode[2])
				o.Set("explode", e)
			}
			items.Call("push", o)
			names = append(names, it.Name)
			partOf = append(partOf, pi)
		}
	} else {
		pp := pl.Parts[n]
		title, desc = partLabel(pp.Name, pp.Variant), pp.Description
		for i, u := range pp.ModelURLs {
			o := js.Global().Get("Object").New()
			o.Set("url", u)
			o.Set("color", partColor(pp.PlanPart))
			o.Set("name", fmt.Sprintf("%s %d", pp.Name, i+1))
			items.Call("push", o)
		}
	}
	setText("ptTitle", title)
	setText("ptDesc", desc)
	setHidden("ptDesc", desc == "")
	assembly := what[0] == 'a'
	setHidden("ptExplodeRow", !assembly || len(names) < 2)
	byID("ptExplode").Set("value", 0)
	byID("ptCut").Set("value", 100)
	for _, b := range jsList(byID("ptAsmKeys").Call("querySelectorAll", "button")) {
		b.Call("setAttribute", "aria-pressed", fmt.Sprint(b.Get("dataset").Get("show").String() == what))
	}

	// The list of the items of an assembly: a tap shows one item, and a second tap all.
	list := byID("ptItems")
	list.Set("textContent", "")
	setHidden("ptItems", !assembly)
	picked := -1
	pick := func(i int) {
		if i == picked {
			i = -1
		}
		picked = i
		if !parts.view.IsUndefined() {
			parts.view.Call("highlight", i)
		}
		for k, li := range jsList(list.Call("querySelectorAll", "li")) {
			li.Call("setAttribute", "aria-pressed", fmt.Sprint(k == i))
		}
	}
	for i, name := range names {
		li := el("li", "", "")
		dot := el("span", "dot", "")
		dot.Get("style").Set("background", items.Index(i).Get("color"))
		li.Call("appendChild", dot)
		li.Call("appendChild", el("span", "", name))
		if partOf[i] < 0 {
			li.Call("appendChild", el("span", "hw", "hardware"))
		}
		li.Set("tabIndex", 0)
		li.Call("setAttribute", "role", "button")
		k := i
		li.Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any {
			pick(k)
			return nil
		}))
		list.Call("appendChild", li)
	}

	opts := js.Global().Get("Object").New()
	opts.Set("items", items)
	opts.Set("onPick", js.FuncOf(func(_ js.Value, args []js.Value) any {
		i := args[0].Int()
		if assembly {
			pick(i)
		}
		return nil
	}))
	parts.view = js.Undefined()
	ctl, err := await(v.Call("inspect", byID("ptModel"), opts))
	if err != nil || parts.shown != what {
		return
	}
	parts.view = ctl
}

// bindParts connects the controls of the 3D view of the tab Parts.
func bindParts() {
	on("ptExplode", "input", func() {
		if !parts.view.IsUndefined() {
			parts.view.Call("explode", byID("ptExplode").Get("valueAsNumber").Float()/100)
		}
	})
	on("ptCut", "input", func() {
		if !parts.view.IsUndefined() {
			parts.view.Call("cut", byID("ptCut").Get("valueAsNumber").Float()/100)
		}
	})
	on("ptReset", "click", func() {
		if !parts.view.IsUndefined() {
			parts.view.Call("reset")
		}
	})
}
