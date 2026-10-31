//go:build js && wasm

package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"syscall/js"
	"time"

	"printdeck/internal/cloud/wire"
)

// The builds from GitHub (docs/specs/github-builds.md): the card "Builds from GitHub" of the
// project page, the plan page (the parts of a commit, and the plates of a build), and the build
// page (the sliced plates, for the approval).

func planPath(slug, id string) string  { return "/projects/" + slug + "/plans/" + id }
func buildPath(slug, id string) string { return "/projects/" + slug + "/builds/" + id }

// planName is the short name of a plan: "PR #7", "v1.2", or the branch.
func planName(p wire.ProjectPlan) string {
	switch p.Kind {
	case wire.BuildTest:
		return fmt.Sprintf("PR #%d", p.PR)
	case wire.BuildRelease:
		return p.Tag
	}
	return p.Branch
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// profileName is the profile without its folder and version: "pla-0.10-fine".
func profileName(p string) string {
	p, _, _ = strings.Cut(p, "@")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		p = p[i+1:]
	}
	return p
}

// drawBuilds draws the card "Builds from GitHub" of the project page.
func (a *app) drawBuilds(v *wire.ProjectView) {
	setHidden("pBuildsCard", v.Repo == "" && !v.IsOwner)
	setHidden("pRepoForm", true)
	setHidden("pRepoEdit", !v.IsOwner)
	say("pRepoMsg", nil, "")
	if v.Repo == "" {
		setText("pRepoLine", "Link the GitHub repository of the project. Its Action then sends a plan of the parts at each push.")
		setText("pRepoEdit", "Link a repository")
	} else {
		setText("pRepoLine", "The repository github.com/"+v.Repo+". Each push sends a plan of the parts.")
		setText("pRepoEdit", "Change the repository")
	}
	list := byID("pPlans")
	list.Set("textContent", "")
	for _, p := range v.Plans {
		li := el("li", "", "")
		link := el("a", "", "")
		link.Set("href", planPath(v.Slug, p.ID))
		changed := 0
		for _, pp := range p.Parts {
			if pp.Changed {
				changed++
			}
		}
		title := el("strong", "", planName(p)+" · "+shortSHA(p.Commit))
		link.Call("appendChild", title)
		sub := fmt.Sprintf("%s, %d changed · %s", count(len(p.Parts), "part", "parts"), changed, localWhen(p.Created))
		if n := len(p.Builds); n > 0 {
			sub += " · " + count(n, "build", "builds") + ", the last " + strings.ToLower(buildStateText(p.Builds[0].State))
		}
		if !p.Ready {
			sub += " · the models are not up yet"
		}
		link.Call("appendChild", el("span", "sub", sub))
		li.Call("appendChild", link)
		list.Call("appendChild", li)
	}
	if v.Repo != "" && len(v.Plans) == 0 {
		list.Call("appendChild", el("li", "dim small", "No plans yet. The Action sends a plan at the next push."))
	}
}

func (a *app) bindBuilds() {
	on("pRepoEdit", "click", func() {
		byID("pRepoInput").Set("value", a.view.Repo)
		setHidden("pRepoForm", false)
		setHidden("pRepoEdit", true)
		byID("pRepoInput").Call("focus")
	})
	on("pRepoCancel", "click", func() { setHidden("pRepoForm", true); setHidden("pRepoEdit", false) })
	onSubmit("pRepoForm", func() {
		repo := strings.TrimSpace(byID("pRepoInput").Get("value").String())
		go func() {
			say("pRepoMsg", nil, "Saving…")
			if err := a.api("PUT", "/projects/"+a.view.ID+"/repo", wire.RepoRequest{Repo: repo}, nil); err != nil {
				say("pRepoMsg", err, "")
				return
			}
			a.reload()
		}()
	})
	on("plSlice", "click", func() { go a.slicePlan() })
	on("plSuggest", "click", func() {
		a.planPlates = nil
		a.drawPlates()
	})
	on("bdCopy", "click", func() { go copyText(byID("bdStart").Get("textContent").String(), "bdCopyMsg") })
	on("bdAgain", "click", func() { go a.sliceAgain() })
}

// The plan page.

// planChoice is the choice of a part on the plan page: checked, and its copies.
type planChoice struct {
	on    bool
	count int
}

// showPlanPage shows the plan id of the project.
func (a *app) showPlanPage(projectID, id string) {
	var v wire.ProjectView
	if err := a.api("GET", "/projects/"+projectID, nil, &v); err != nil {
		a.failed(err)
		return
	}
	var p wire.ProjectPlan
	if err := a.api("GET", "/projects/"+projectID+"/plans/"+id, nil, &p); err != nil {
		a.failed(err)
		return
	}
	a.view, a.plan, a.planPlates = &v, &p, nil
	a.planChoices = make([]planChoice, len(p.Parts))
	for i, pp := range p.Parts {
		// A release plan suggests all the parts. Another plan suggests the parts that changed.
		a.planChoices[i] = planChoice{on: pp.Changed || p.Kind == wire.BuildRelease, count: max(1, pp.Count)}
	}
	doc.Set("title", planName(p)+" · "+v.Name+" · Printdeck")
	byID("plBack").Set("href", projectPath(v.Slug, 0))
	setText("plBack", v.Name)
	setText("plTitle", planName(p)+" · "+shortSHA(p.Commit))
	setText("plSub", fmt.Sprintf("github.com/%s · %s", p.Repo, localWhen(p.Created)))
	links := byID("plLinks")
	links.Set("textContent", "")
	add := func(text, href string) {
		l := el("a", "", text)
		l.Set("href", href)
		l.Set("target", "_blank")
		l.Set("rel", "noopener")
		links.Call("appendChild", l)
	}
	if p.PR > 0 {
		add("The pull request", fmt.Sprintf("https://github.com/%s/pull/%d", p.Repo, p.PR))
	}
	add("The commit", "https://github.com/"+p.Repo+"/commit/"+p.Commit)
	if p.Run != "" {
		add("The run", p.Run)
	}
	setHidden("plKeys", !v.IsOwner || !p.Ready)
	say("plMsg", nil, "")
	if !p.Ready {
		say("plMsg", nil, "The Action is still sending the models of this plan.")
	}
	show("vPlan")
	a.drawPlanParts()
	a.drawPlates()
	a.drawPlanBuilds()
	for i, pp := range p.Parts {
		if a.planChoices[i].on || i == len(p.Parts)-1 {
			a.showPlanModel(i)
			break
		}
		_ = pp
	}
}

// drawPlanParts draws the list of the parts of the plan, with a check box and the copies each.
func (a *app) drawPlanParts() {
	p := a.plan
	list := byID("plParts")
	list.Set("textContent", "")
	owner := a.view.IsOwner
	for i, pp := range p.Parts {
		i, pp := i, pp
		li := el("li", "", "")
		if a.planChoices[i].on {
			li.Get("classList").Call("add", "on")
		}
		if i == a.planShown {
			li.Get("classList").Call("add", "shown")
		}
		box := el("input", "", "")
		box.Set("type", "checkbox")
		box.Set("checked", a.planChoices[i].on)
		box.Set("disabled", !owner || !p.Ready)
		box.Call("setAttribute", "aria-label", "Print "+pp.Name)
		box.Call("addEventListener", "change", js.FuncOf(func(js.Value, []js.Value) any {
			a.planChoices[i].on = box.Get("checked").Bool()
			a.planPlates = nil // the suggestion again, with the new parts
			a.drawPlanParts()
			a.drawPlates()
			return nil
		}))
		li.Call("appendChild", box)
		what := el("div", "what", "")
		name := el("strong", "", pp.Name+" ")
		if pp.Changed {
			name.Call("appendChild", el("span", "tag changed", "Changed"))
		}
		what.Call("appendChild", name)
		sub := printerName(pp.Printer)
		if pp.Prebuilt != "" {
			sub += " · a ready file: " + pp.Prebuilt
		} else if pp.Profile != "" {
			sub += " · " + profileName(pp.Profile)
		}
		if pp.Material != "" {
			sub += " · " + pp.Material
		}
		if pp.SizeX > 0 {
			sub += fmt.Sprintf(" · %.0f x %.0f x %.0f mm", pp.SizeX, pp.SizeY, pp.SizeZ)
		}
		if pp.Alone {
			sub += " · prints alone"
		}
		what.Call("appendChild", el("span", "sub", sub))
		what.Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any {
			a.showPlanModel(i)
			return nil
		}))
		li.Call("appendChild", what)
		count := el("input", "", "")
		count.Set("type", "number")
		count.Set("min", "1")
		count.Set("max", "100")
		count.Set("value", strconv.Itoa(a.planChoices[i].count))
		count.Set("disabled", !owner || pp.Prebuilt != "")
		count.Call("setAttribute", "aria-label", "Copies of "+pp.Name)
		count.Call("addEventListener", "change", js.FuncOf(func(js.Value, []js.Value) any {
			n, err := strconv.Atoi(count.Get("value").String())
			if err != nil || n < 1 {
				n = 1
			}
			a.planChoices[i].count = min(n, 100)
			a.drawPlates()
			return nil
		}))
		li.Call("appendChild", count)
		list.Call("appendChild", li)
	}
}

// printerName is the name of a printer of printdeck.yml for the page.
func printerName(p string) string {
	switch strings.ToLower(p) {
	case "taz":
		return "TAZ Workhorse"
	case "phenom":
		return "Peopoly Phenom"
	}
	return p
}

// showPlanModel shows the models of the part i in the 3D view.
func (a *app) showPlanModel(i int) {
	if i < 0 || i >= len(a.plan.Parts) {
		return
	}
	a.planShown = i
	pp := a.plan.Parts[i]
	setText("plViewTitle", pp.Name)
	facts := strings.Join(pp.Files, ", ")
	setText("plViewFacts", facts)
	for _, li := range jsList(byID("plParts").Call("querySelectorAll", "li")) {
		li.Get("classList").Call("remove", "shown")
	}
	if li := byID("plParts").Get("children").Index(i); !li.IsUndefined() && !li.IsNull() {
		li.Get("classList").Call("add", "shown")
	}
	v := js.Global().Get("pwfView3d")
	box := byID("plModel")
	if v.IsUndefined() || len(pp.ModelURLs) == 0 {
		box.Set("textContent", "")
		return
	}
	urls := js.Global().Get("Array").New()
	for _, u := range pp.ModelURLs {
		urls.Call("push", u)
	}
	opts := js.Global().Get("Object").New()
	opts.Set("urls", urls)
	color := "#e8762b"
	if pp.Process == "resin" {
		color = "#9aa0a8"
	}
	opts.Set("color", color)
	if pp.BedX > 0 {
		bed := js.Global().Get("Array").New()
		bed.Call("push", pp.BedX, pp.BedY)
		opts.Set("bed", bed)
	}
	v.Call("showModels", box, opts)
}

func jsList(nl js.Value) []js.Value {
	var out []js.Value
	for i := 0; i < nl.Get("length").Int(); i++ {
		out = append(out, nl.Index(i))
	}
	return out
}

// plateKey is the key of the parts that can share a plate: the printer, the profile and the
// material. A part that prints alone has a key of its own.
func plateKey(pp wire.ProjectPlanPart, i int) string {
	if pp.Alone || pp.Prebuilt != "" {
		return "alone:" + strconv.Itoa(i)
	}
	prof, _, _ := strings.Cut(pp.Profile, "@")
	return pp.Printer + "|" + prof + "|" + pp.Material
}

// plateFit reports whether the parts fit on the bed of their printer, and how many objects fit,
// with the grid of curaslice.py (printdeck-action): objects in rows, the room for the brim around each, 10 mm
// from the edges. The bed and the room come from the plan; a part with no bed fits.
func (a *app) plateFit(parts []int) (bool, int, int) {
	var n int
	var cw, cd, bx, by float64
	for _, i := range parts {
		pp := a.plan.Parts[i]
		n += a.planChoices[i].count
		m := pp.Margin
		cw = math.Max(cw, pp.SizeX+2*m)
		cd = math.Max(cd, pp.SizeY+2*m)
		if pp.BedX > 0 {
			bx, by = pp.BedX, pp.BedY
		}
	}
	if bx == 0 || cw == 0 {
		return true, n, n
	}
	if n == 1 {
		return cw-2*a.plan.Parts[parts[0]].Margin <= bx && cd-2*a.plan.Parts[parts[0]].Margin <= by, 1, 1
	}
	cols := math.Max(1, math.Floor((bx-20)/cw))
	rows := math.Ceil(float64(n) / cols)
	fit := int(cols * math.Max(1, math.Floor((by-20)/cd)))
	return rows*cd <= by-20, n, fit
}

// suggestPlates puts the checked parts on plates: the parts with the same key on one plate while
// they fit, and a new plate for a part that does not fit.
func (a *app) suggestPlates() [][]int {
	var plates [][]int
	keys := []string{}
	for i, pp := range a.plan.Parts {
		if !a.planChoices[i].on {
			continue
		}
		k := plateKey(pp, i)
		placed := false
		for j := range plates {
			if keys[j] != k {
				continue
			}
			if ok, _, _ := a.plateFit(append(append([]int(nil), plates[j]...), i)); ok {
				plates[j] = append(plates[j], i)
				placed = true
				break
			}
		}
		if !placed {
			plates = append(plates, []int{i})
			keys = append(keys, k)
		}
	}
	return plates
}

// drawPlates draws the plates: the suggestion, or the plates that the owner changed.
func (a *app) drawPlates() {
	if a.planPlates == nil {
		a.planPlates = a.suggestPlates()
	}
	// The plates keep only the checked parts.
	var plates [][]int
	for _, pl := range a.planPlates {
		var keep []int
		for _, i := range pl {
			if a.planChoices[i].on {
				keep = append(keep, i)
			}
		}
		if len(keep) > 0 {
			plates = append(plates, keep)
		}
	}
	a.planPlates = plates
	box := byID("plPlates")
	box.Set("textContent", "")
	allFit := true
	for n, pl := range plates {
		n, pl := n, pl
		first := a.plan.Parts[pl[0]]
		ok, objects, fit := a.plateFit(pl)
		allFit = allFit && ok
		card := el("div", "plate", "")
		if !ok {
			card.Get("classList").Call("add", "bad")
		}
		h := el("h3", "", fmt.Sprintf("Plate %d · %s", n+1, printerName(first.Printer)))
		card.Call("appendChild", h)
		sub := profileName(first.Profile)
		if first.Prebuilt != "" {
			sub = "a ready file"
		}
		if first.Material != "" {
			sub += " · " + first.Material
		}
		sub += " · " + count(objects, "object", "objects")
		card.Call("appendChild", el("span", "sub", sub))
		_ = objects
		if !ok {
			card.Call("appendChild", el("span", "warn", fmt.Sprintf("Too much for the bed: %d objects fit. Move parts to another plate, or print fewer copies.", fit)))
		}
		canvas := el("canvas", "", "")
		card.Call("appendChild", canvas)
		list := el("ul", "", "")
		for _, i := range pl {
			i := i
			pp := a.plan.Parts[i]
			li := el("li", "", "")
			li.Call("appendChild", el("span", "", fmt.Sprintf("%s × %d", pp.Name, a.planChoices[i].count)))
			if a.view.IsOwner {
				sel := el("select", "", "")
				sel.Call("setAttribute", "aria-label", "The plate of "+pp.Name)
				for m, other := range plates {
					if m != n && plateKey(a.plan.Parts[other[0]], other[0]) != plateKey(pp, i) {
						continue
					}
					o := el("option", "", fmt.Sprintf("Plate %d", m+1))
					o.Set("value", strconv.Itoa(m))
					o.Set("selected", m == n)
					sel.Call("appendChild", o)
				}
				if len(pl) > 1 {
					o := el("option", "", "A new plate")
					o.Set("value", "new")
					sel.Call("appendChild", o)
				}
				sel.Call("addEventListener", "change", js.FuncOf(func(js.Value, []js.Value) any {
					a.movePart(i, n, sel.Get("value").String())
					return nil
				}))
				li.Call("appendChild", sel)
			}
			list.Call("appendChild", li)
		}
		card.Call("appendChild", list)
		box.Call("appendChild", card)
		a.drawPlateBed(canvas, pl)
	}
	if len(plates) == 0 {
		box.Call("appendChild", el("p", "dim", "No parts are checked."))
	}
	byID("plSlice").Set("disabled", len(plates) == 0 || !allFit)
	setText("plSlice", "Slice "+count(len(plates), "plate", "plates"))
}

// movePart moves the part i from the plate from to the plate to ("new" for a new plate).
func (a *app) movePart(i, from int, to string) {
	plates := a.planPlates
	var rest []int
	for _, x := range plates[from] {
		if x != i {
			rest = append(rest, x)
		}
	}
	plates[from] = rest
	if to == "new" {
		plates = append(plates, []int{i})
	} else if m, err := strconv.Atoi(to); err == nil && m >= 0 && m < len(plates) {
		plates[m] = append(plates[m], i)
	}
	a.planPlates = plates
	a.drawPlates()
}

// drawPlateBed draws the bed of a plate from above, with the objects at the places of the grid
// of the slicer.
func (a *app) drawPlateBed(c js.Value, pl []int) {
	first := a.plan.Parts[pl[0]]
	bx, by := first.BedX, first.BedY
	if bx == 0 {
		bx, by = 280, 280
	}
	px := 300.0
	c.Set("width", px)
	c.Set("height", px)
	ctx := c.Call("getContext", "2d")
	scale := px / math.Max(bx, by)
	ctx.Set("fillStyle", "#2b2d31")
	ctx.Call("fillRect", 0, 0, bx*scale, by*scale)
	ctx.Set("strokeStyle", "#3a3d42")
	ctx.Set("lineWidth", 1)
	for x := 10.0; x < bx; x += 10 {
		ctx.Call("beginPath")
		ctx.Call("moveTo", x*scale, 0)
		ctx.Call("lineTo", x*scale, by*scale)
		ctx.Call("stroke")
	}
	for y := 10.0; y < by; y += 10 {
		ctx.Call("beginPath")
		ctx.Call("moveTo", 0, y*scale)
		ctx.Call("lineTo", bx*scale, y*scale)
		ctx.Call("stroke")
	}
	// The objects, in the order of the slicer, in a grid at the middle.
	type obj struct {
		w, d float64
		name string
	}
	var objs []obj
	var cw, cd float64
	for _, i := range pl {
		pp := a.plan.Parts[i]
		for k := 0; k < a.planChoices[i].count; k++ {
			objs = append(objs, obj{pp.SizeX, pp.SizeY, pp.Name})
		}
		cw = math.Max(cw, pp.SizeX+2*pp.Margin)
		cd = math.Max(cd, pp.SizeY+2*pp.Margin)
	}
	if cw == 0 {
		return
	}
	cols := math.Max(1, math.Floor((bx-20)/cw))
	if len(objs) == 1 {
		cols = 1
	}
	cols = math.Min(cols, float64(len(objs)))
	rows := math.Ceil(float64(len(objs)) / cols)
	colors := []string{"#e8762b", "#f2a65a", "#8b5cf6", "#3aa05a", "#2a7fc0"}
	names := map[string]int{}
	for k, o := range objs {
		r, cl := k/int(cols), k%int(cols)
		cx := bx/2 + (float64(cl)-(cols-1)/2)*cw
		cy := by/2 + (float64(r)-(rows-1)/2)*cd
		if _, ok := names[o.name]; !ok {
			names[o.name] = len(names)
		}
		ctx.Set("fillStyle", colors[names[o.name]%len(colors)])
		ctx.Call("fillRect", (cx-o.w/2)*scale, (cy-o.d/2)*scale, math.Max(2, o.w*scale), math.Max(2, o.d*scale))
	}
}

// slicePlan makes a build of the plan with the plates, and opens its page.
func (a *app) slicePlan() {
	var req wire.BuildRequest
	for _, pl := range a.planPlates {
		var plate wire.PlateRequest
		for _, i := range pl {
			plate.Parts = append(plate.Parts, wire.PlatePart{Part: i, Count: a.planChoices[i].count})
		}
		req.Plates = append(req.Plates, plate)
	}
	say("plMsg", nil, "Starting the slice…")
	var b wire.ProjectBuild
	if err := a.api("POST", "/projects/"+a.view.ID+"/plans/"+a.plan.ID+"/builds", req, &b); err != nil {
		say("plMsg", err, "")
		return
	}
	say("plMsg", nil, "")
	openAddress(buildPath(a.view.Slug, b.ID))
}

// drawPlanBuilds draws the builds of the plan.
func (a *app) drawPlanBuilds() {
	list := byID("plBuilds")
	list.Set("textContent", "")
	setHidden("plBuildsCard", len(a.plan.Builds) == 0)
	for _, b := range a.plan.Builds {
		li := el("li", "", "")
		link := el("a", "", "")
		link.Set("href", buildPath(a.view.Slug, b.ID))
		title := el("strong", "", count(len(b.Plates), "plate", "plates")+" · "+localWhen(b.Created)+" ")
		title.Call("appendChild", el("span", "tag "+b.State, buildStateText(b.State)))
		link.Call("appendChild", title)
		var parts []string
		for _, pl := range b.Plates {
			for _, pp := range pl.Parts {
				parts = append(parts, fmt.Sprintf("%s × %d", pp.Name, pp.Count))
			}
		}
		link.Call("appendChild", el("span", "sub", strings.Join(parts, ", ")))
		li.Call("appendChild", link)
		list.Call("appendChild", li)
	}
}

func buildStateText(s string) string {
	switch s {
	case wire.BuildSlicing:
		return "Slicing"
	case wire.BuildReady:
		return "Ready"
	case wire.BuildFailed:
		return "Failed"
	case wire.PartWaiting:
		return "Waiting for the approval"
	case wire.PartApproved:
		return "Approved"
	case wire.PartRejected:
		return "Rejected"
	case wire.PartReplaced:
		return "Replaced"
	}
	return s
}

// sliceAgain makes a new build of the plan with the plates of the build that failed.
func (a *app) sliceAgain() {
	b := a.lastBuild
	var req wire.BuildRequest
	for _, pl := range b.Plates {
		var plate wire.PlateRequest
		for _, pp := range pl.Parts {
			plate.Parts = append(plate.Parts, wire.PlatePart{Part: pp.Part, Count: pp.Count})
		}
		req.Plates = append(req.Plates, plate)
	}
	say("bdAgainMsg", nil, "Starting the slice…")
	var nb wire.ProjectBuild
	if err := a.api("POST", "/projects/"+a.view.ID+"/plans/"+b.Plan+"/builds", req, &nb); err != nil {
		say("bdAgainMsg", err, "")
		return
	}
	say("bdAgainMsg", nil, "")
	openAddress(buildPath(a.view.Slug, nb.ID))
}

// The build page.

// showBuildPage shows the build id of the project. While the build slices, the page looks
// again each 10 seconds.
func (a *app) showBuildPage(projectID, id string) {
	var v wire.ProjectView
	if err := a.api("GET", "/projects/"+projectID, nil, &v); err != nil {
		a.failed(err)
		return
	}
	var b wire.ProjectBuild
	if err := a.api("GET", "/projects/"+projectID+"/builds/"+id, nil, &b); err != nil {
		a.failed(err)
		return
	}
	a.view = &v
	a.playing++
	playing := a.playing
	doc.Set("title", "A build · "+v.Name+" · Printdeck")
	byID("bdBack").Set("href", planPath(v.Slug, b.Plan))
	setText("bdBack", "The plan")
	show("vBuild")
	a.drawBuild(b)
	if b.State == wire.BuildSlicing {
		go func() {
			for {
				time.Sleep(10 * time.Second)
				if a.playing != playing {
					return
				}
				var nb wire.ProjectBuild
				if a.api("GET", "/projects/"+projectID+"/builds/"+id, nil, &nb) != nil {
					continue
				}
				if a.playing != playing {
					return
				}
				a.drawBuild(nb)
				if nb.State != wire.BuildSlicing {
					return
				}
			}
		}()
	}
}

// drawBuild draws the plates of the build b, with the keys of the owner.
func (a *app) drawBuild(b wire.ProjectBuild) {
	v := a.view
	setText("bdTitle", "A build of "+count(len(b.Plates), "plate", "plates"))
	setText("bdSub", "Commit "+shortSHA(b.Commit)+" · "+localWhen(b.Created)+" · "+buildStateText(b.State))
	setHidden("bdStartCard", b.Start == "" || !v.IsOwner)
	setText("bdStart", b.Start)
	setHidden("bdError", b.Error == "")
	setText("bdError", b.Error)
	setHidden("bdRun", b.Run == "")
	byID("bdRun").Set("href", b.Run)
	setHidden("bdAgain", !v.IsOwner || b.State != wire.BuildFailed)
	a.lastBuild = b
	box := byID("bdPlates")
	box.Set("textContent", "")
	for n, pl := range b.Plates {
		n, pl := n, pl
		card := el("section", "card", "")
		h := el("h2", "", fmt.Sprintf("Plate %d · %s ", n+1, printerName(pl.Printer)))
		if pl.State != "" {
			h.Call("appendChild", el("span", "tag "+pl.State, buildStateText(pl.State)))
		}
		card.Call("appendChild", h)
		var parts []string
		for _, pp := range pl.Parts {
			parts = append(parts, fmt.Sprintf("%s × %d", pp.Name, pp.Count))
		}
		card.Call("appendChild", el("p", "", strings.Join(parts, ", ")))
		facts := el("dl", "facts", "")
		fact := func(k, val string) {
			if val == "" {
				return
			}
			d := el("div", "", "")
			d.Call("appendChild", el("dt", "", k))
			d.Call("appendChild", el("dd", "", val))
			facts.Call("appendChild", d)
		}
		fact("Profile", pl.Profile)
		fact("Material", pl.Material)
		if pl.Seconds > 0 {
			fact("Print time", clock(pl.Seconds))
		}
		if pl.Grams > 0 {
			fact("Filament", fmt.Sprintf("%.0f g", pl.Grams))
		}
		if pl.ML > 0 {
			fact("Resin", fmt.Sprintf("%.0f ml", pl.ML))
		}
		if pl.Layers > 0 {
			fact("Layers", fmt.Sprintf("%d of %.2f mm", pl.Layers, pl.LayerHeight))
		}
		fact("File", pl.File)
		fact("Slicer", pl.Slicer)
		card.Call("appendChild", facts)
		if pl.PictureURL != "" {
			img := el("img", "", "")
			img.Set("src", pl.PictureURL)
			img.Set("alt", "The plate")
			img.Set("style", "max-width:100%;border-radius:8px")
			card.Call("appendChild", img)
		}
		if pl.State == wire.PartRejected && pl.Reason != "" {
			card.Call("appendChild", el("p", "", "Rejected by "+pl.By+": "+pl.Reason))
		}
		if pl.State == wire.PartApproved {
			card.Call("appendChild", el("p", "small dim", "Approved by "+pl.By+", "+localWhen(pl.At)+". The deck of the printer shows it next (phase 2)."))
		}
		if v.IsOwner && pl.State == wire.PartWaiting {
			keys := el("div", "row-keys", "")
			ok := el("button", "primary", "Approve")
			ok.Set("type", "button")
			no := el("button", "", "Reject")
			no.Set("type", "button")
			reason := el("input", "", "")
			reason.Set("placeholder", "The reason, for the pull request")
			reason.Set("maxLength", 1000)
			reason.Set("hidden", true)
			msg := el("p", "msg", "")
			decide := func(approve bool) {
				body := wire.BuildDecision{Reason: strings.TrimSpace(reason.Get("value").String())}
				path := fmt.Sprintf("/projects/%s/builds/%s/plates/%d/", v.ID, b.ID, n)
				if approve {
					path += "approve"
				} else {
					path += "reject"
				}
				var nb wire.ProjectBuild
				if err := a.api("POST", path, body, &nb); err != nil {
					msg.Set("textContent", err.Error())
					msg.Get("classList").Call("add", "err")
					return
				}
				a.drawBuild(nb)
			}
			ok.Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any { go decide(true); return nil }))
			no.Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any {
				if reason.Get("hidden").Bool() {
					reason.Set("hidden", false)
					reason.Call("focus")
					no.Set("textContent", "Send the rejection")
					return nil
				}
				go decide(false)
				return nil
			}))
			keys.Call("appendChild", ok)
			keys.Call("appendChild", no)
			card.Call("appendChild", keys)
			card.Call("appendChild", reason)
			card.Call("appendChild", msg)
		}
		box.Call("appendChild", card)
	}
}
