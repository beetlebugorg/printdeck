//go:build js && wasm

package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"syscall/js"
	"time"

	"printdeck/internal/cloud/platepack"
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
	// The newest plans of the pull requests and the releases, and a link to all of them. The
	// Overview shows the newest plan of main.
	var shown []wire.ProjectPlan
	for _, p := range v.Plans {
		if (p.Kind == wire.BuildTest || p.Kind == wire.BuildRelease) && len(shown) < plansOnProject {
			shown = append(shown, p)
		}
	}
	drawPlanList(list, v.Slug, shown)
	if v.PlansTotal > len(shown) {
		li := el("li", "", "")
		more := el("a", "more", fmt.Sprintf("All %d plans", v.PlansTotal))
		more.Set("href", "/projects/"+v.Slug+"/plans")
		li.Call("appendChild", more)
		list.Call("appendChild", li)
	}
	if v.Repo != "" && len(shown) == 0 {
		list.Call("appendChild", el("li", "dim small", "No plans of pull requests or releases yet. The Action sends a plan at each push."))
	}
}

// plansOnProject is the count of the newest plans on the project page.
const plansOnProject = 3

// showPlansPage shows all the plans of the project id.
func (a *app) showPlansPage(id string) {
	var v wire.ProjectView
	if err := a.api("GET", "/projects/"+id, nil, &v); err != nil {
		a.failed(err)
		return
	}
	var plans []wire.ProjectPlan
	if err := a.api("GET", "/projects/"+id+"/plans", nil, &plans); err != nil {
		a.failed(err)
		return
	}
	a.view = &v
	doc.Set("title", "Plans · "+v.Name+" · Printdeck")
	byID("psBack").Set("href", projectPath(v.Slug, 0))
	setText("psBack", v.Name)
	setText("psSub", "github.com/"+v.Repo+" · "+count(len(plans), "plan", "plans")+", the newest first")
	list := byID("psList")
	list.Set("textContent", "")
	rows := drawPlanList(list, v.Slug, plans)
	show("vPlans")
	go describePlans(plans, rows)
}

// drawPlanList adds a row for each plan to list: its name and commit, the parts that changed,
// and its builds.
func drawPlanList(list js.Value, slug string, plans []wire.ProjectPlan) []js.Value {
	var rows []js.Value
	for _, p := range plans {
		li := el("li", "", "")
		link := el("a", "", "")
		link.Set("href", planPath(slug, p.ID))
		changed := 0
		for _, pp := range p.Parts {
			if pp.Changed {
				changed++
			}
		}
		name := planName(p) + " · " + shortSHA(p.Commit)
		if p.Author != "" {
			name += " · @" + p.Author
		}
		title := el("strong", "", name)
		link.Call("appendChild", title)
		sub := fmt.Sprintf("%s, %d changed · %s", count(len(p.Parts), "part", "parts"), changed, localWhen(p.Created))
		if n := len(p.Builds); n > 0 {
			text, _ := buildSummary(p.Builds[0])
			sub += " · " + count(n, "build", "builds") + ", the last " + strings.ToLower(text)
		}
		if !p.Ready {
			sub += " · the models are not up yet"
		}
		link.Call("appendChild", el("span", "sub", sub))
		li.Call("appendChild", link)
		list.Call("appendChild", li)
		rows = append(rows, link)
	}
	return rows
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
	a.layouts = nil
	a.planChoices = make([]planChoice, len(p.Parts))
	for i, pp := range p.Parts {
		// A release plan suggests all the parts. Another plan suggests the parts that changed.
		// A part that does not slice is not checked: it cannot go on a plate.
		a.planChoices[i] = planChoice{on: (pp.Changed || p.Kind == wire.BuildRelease) && pp.SliceError == "", count: max(1, pp.Count)}
	}
	a.firstChoice()
	// The kind of print: the kind of the first part that changed, else the kind of the first part.
	a.planProcess = p.Parts[0].Process
	for _, pp := range p.Parts {
		if pp.Changed {
			a.planProcess = pp.Process
			break
		}
	}
	a.keepProcess()
	doc.Set("title", planName(p)+" · "+v.Name+" · Printdeck")
	byID("plBack").Set("href", projectPath(v.Slug, 0))
	setText("plBack", v.Name)
	setText("plTitle", planName(p)+" · "+shortSHA(p.Commit))
	sub := fmt.Sprintf("github.com/%s · %s", p.Repo, localWhen(p.Created))
	if p.Fork != "" {
		sub = fmt.Sprintf("by @%s, from the fork %s · %s", p.Author, p.Fork, localWhen(p.Created))
	}
	setText("plSub", sub)
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
	setHidden("plKeys", !v.CanSlice || !p.Ready)
	byID("plHint").Set("textContent", planHint(v.CanSlice))
	say("plMsg", nil, "")
	if !p.Ready {
		say("plMsg", nil, "The Action is still sending the models of this plan.")
	}
	show("vPlan")
	setHidden("plDiffCard", true)
	go a.drawPlanDiff(projectID, p.ID)
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

// keepProcess turns off the parts of the other kind of print: a plate prints on one printer.
func (a *app) keepProcess() {
	for i, pp := range a.plan.Parts {
		if pp.Process != a.planProcess {
			a.planChoices[i].on = false
		}
	}
}

// drawProcess draws the choice of the kind of print: FDM or resin, when the plan has both.
func (a *app) drawProcess() {
	box := byID("plProcess")
	box.Set("textContent", "")
	kinds := []string{}
	printer := map[string]string{}
	for _, pp := range a.plan.Parts {
		if _, ok := printer[pp.Process]; !ok {
			kinds = append(kinds, pp.Process)
			printer[pp.Process] = printerName(pp.Printer)
		}
	}
	box.Set("hidden", len(kinds) < 2)
	for _, k := range []string{"fdm", "resin"} {
		k := k
		if _, ok := printer[k]; !ok {
			continue
		}
		name := map[string]string{"fdm": "FDM", "resin": "Resin"}[k]
		b := el("button", "chip", name+" · "+printer[k])
		b.Set("type", "button")
		b.Call("setAttribute", "aria-pressed", strconv.FormatBool(a.planProcess == k))
		b.Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any {
			if a.planProcess == k {
				return nil
			}
			a.planProcess = k
			a.keepProcess()
			a.planPlates = nil
			a.drawPlanParts()
			a.drawPlates()
			return nil
		}))
		box.Call("appendChild", b)
	}
}

// partGroups returns the parts of the plan by their name: the indexes of the variants of each
// part, in the order of printdeck.yml. A part with no variants has one index.
func (a *app) partGroups() [][]int {
	var out [][]int
	at := map[string]int{}
	for i, pp := range a.plan.Parts {
		k := strings.ToLower(pp.Name)
		if g, ok := at[k]; ok {
			out[g] = append(out[g], i)
			continue
		}
		at[k] = len(out)
		out = append(out, []int{i})
	}
	return out
}

// firstChoice keeps one variant of each part on: a release plan or the first look of a plan has
// all the changed variants on, and a test round tests one quality at a time. The first variant
// that changed stays on.
func (a *app) firstChoice() {
	for _, g := range a.partGroups() {
		kept := false
		for _, i := range g {
			if a.planChoices[i].on {
				if kept {
					a.planChoices[i].on = false
				}
				kept = true
			}
		}
	}
}

// planHint is the text over the parts of a plan. The owner, and a member that the owner lets
// slice, choose the parts and the plates, and slice them (the API checks it with canSlice).
// Another member sees the parts in 3D.
func planHint(slice bool) string {
	if slice {
		return "The parts that changed are checked. Check the parts to print, and set the copies."
	}
	return "Tap a part to see it in 3D. To slice plates, ask the owner to turn on Can slice for you."
}

// drawPlanParts draws the list of the parts of the plan: one row for each part, with a check box,
// the chips of its variants, and the copies.
func (a *app) drawPlanParts() {
	p := a.plan
	list := byID("plParts")
	list.Set("textContent", "")
	owner := a.view.CanSlice && p.Ready
	changedPlates := func() {
		a.planPlates = nil // the suggestion again, with the new parts
		a.drawPlanParts()
		a.drawPlates()
	}
	a.drawProcess()
	for _, g := range a.partGroups() {
		g := g
		first := p.Parts[g[0]]
		if first.Process != a.planProcess {
			continue // the other kind of print
		}
		on, changed := false, false
		shownHere := false
		for _, i := range g {
			on = on || a.planChoices[i].on
			changed = changed || p.Parts[i].Changed
			shownHere = shownHere || i == a.planShown
		}
		// The variant that the row describes: the first chosen one, else the first.
		cur := g[0]
		for _, i := range g {
			if a.planChoices[i].on {
				cur = i
				break
			}
		}
		pp := p.Parts[cur]
		li := el("li", "", "")
		li.Get("dataset").Set("part", strings.ToLower(first.Name))
		if on {
			li.Get("classList").Call("add", "on")
		}
		if shownHere {
			li.Get("classList").Call("add", "shown")
		}
		box := el("input", "", "")
		box.Set("type", "checkbox")
		box.Set("checked", on)
		broken := true
		for _, i := range g {
			broken = broken && p.Parts[i].SliceError != ""
		}
		box.Set("disabled", !owner || broken)
		box.Call("setAttribute", "aria-label", "Print "+first.Name)
		box.Call("addEventListener", "change", js.FuncOf(func(js.Value, []js.Value) any {
			if box.Get("checked").Bool() {
				// The variant that changed, else the first variant.
				pick := g[0]
				for _, i := range g {
					if p.Parts[i].Changed {
						pick = i
						break
					}
				}
				if p.Parts[pick].SliceError != "" {
					for _, i := range g {
						if p.Parts[i].SliceError == "" {
							pick = i // a variant that slices
							break
						}
					}
				}
				a.planChoices[pick].on = true
			} else {
				for _, i := range g {
					a.planChoices[i].on = false
				}
			}
			changedPlates()
			return nil
		}))
		li.Call("appendChild", box)
		what := el("div", "what", "")
		name := el("strong", "", first.Name+" ")
		if changed {
			name.Call("appendChild", el("span", "tag changed", "Changed"))
		}
		what.Call("appendChild", name)
		// The chips of the variants: each one turns its variant on or off.
		if len(g) > 1 || first.Variant != "" {
			chips := el("div", "chips variants", "")
			for _, i := range g {
				i := i
				v := p.Parts[i]
				chip := el("button", "chip", v.Variant)
				chip.Set("type", "button")
				chip.Set("disabled", !owner || v.SliceError != "")
				chip.Call("setAttribute", "aria-pressed", strconv.FormatBool(a.planChoices[i].on))
				title := profileName(v.Profile)
				if v.SliceError != "" {
					title += ", does not slice"
				}
				if v.Changed {
					chip.Get("classList").Call("add", "changed")
					title += ", changed"
				}
				chip.Set("title", title)
				chip.Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any {
					a.planChoices[i].on = !a.planChoices[i].on
					a.showPlanModel(i)
					changedPlates()
					return nil
				}))
				chips.Call("appendChild", chip)
			}
			what.Call("appendChild", chips)
		}
		sub := el("span", "sub", "")
		head := printerName(pp.Printer)
		if pp.Prebuilt != "" {
			head += " · a ready file: " + pp.Prebuilt
		} else if pp.Profile != "" {
			head += " · " + profileName(pp.Profile)
		}
		sub.Call("appendChild", doc.Call("createTextNode", head+" · "))
		sub.Call("appendChild", materialEl(pp.MaterialName, pp.MaterialColor, pp.Material))
		withReorder(sub, pp.MaterialURL, pp.MaterialLinks)
		tail := ""
		if pp.SizeX > 0 {
			tail += fmt.Sprintf(" · %.0f x %.0f x %.0f mm", pp.SizeX, pp.SizeY, pp.SizeZ)
		}
		if pp.Alone {
			tail += " · prints alone"
		}
		tail += sliceFacts(pp.Seconds, pp.Grams, pp.ML)
		sub.Call("appendChild", doc.Call("createTextNode", tail))
		what.Call("appendChild", sub)
		if pp.SliceError != "" {
			what.Call("appendChild", el("span", "warn", "Does not slice: "+pp.SliceError))
		}
		what.Call("addEventListener", "click", js.FuncOf(func(_ js.Value, args []js.Value) any {
			if args[0].Get("target").Get("tagName").String() != "BUTTON" {
				a.showPlanModel(cur)
			}
			return nil
		}))
		li.Call("appendChild", what)
		count := el("input", "", "")
		count.Set("type", "number")
		count.Set("min", "1")
		count.Set("max", "100")
		count.Set("value", strconv.Itoa(a.planChoices[cur].count))
		count.Set("disabled", !owner || pp.Prebuilt != "")
		count.Call("setAttribute", "aria-label", "Copies of "+first.Name)
		count.Call("addEventListener", "change", js.FuncOf(func(js.Value, []js.Value) any {
			n, err := strconv.Atoi(count.Get("value").String())
			if err != nil || n < 1 {
				n = 1
			}
			for _, i := range g {
				a.planChoices[i].count = min(n, 100)
			}
			a.drawPlates()
			return nil
		}))
		li.Call("appendChild", count)
		list.Call("appendChild", li)
	}
}

// materialEl is the material of a part or a plate: a dot of its color and its name, for example
// "MatterHackers PLA Black". With no name, it is the material of printdeck.yml, for example roll-4.
func materialEl(name, color, material string) js.Value {
	span := el("span", "material", "")
	if color != "" {
		dot := el("i", "dot", "")
		dot.Get("style").Set("background", color)
		span.Call("appendChild", dot)
	}
	text := name
	if text == "" {
		text = material
	}
	span.Call("appendChild", doc.Call("createTextNode", text))
	if name != "" && material != "" && material != name {
		span.Set("title", material)
	}
	return span
}

// reorderLink is a link to the shop page of a material, or null for an address that is not
// https. It opens in a new tab, where a phone can pay with its wallet.
func reorderLink(url, text string) js.Value {
	if !strings.HasPrefix(url, "https://") {
		return js.Null()
	}
	a := el("a", "reorder", text)
	a.Set("href", url)
	a.Set("target", "_blank")
	a.Set("rel", "noopener noreferrer")
	return a
}

// withReorder adds the Reorder links of a material to box: "Reorder", or "Reorder: 2.85 mm ·
// 1.75 mm" with labels.
func withReorder(box js.Value, url string, links []wire.MaterialLink) {
	if len(links) == 0 && url != "" {
		links = []wire.MaterialLink{{URL: url}}
	}
	first := true
	for _, l := range links {
		text := l.Label
		if text == "" {
			text = "Reorder"
		}
		a := reorderLink(l.URL, text)
		if a.IsNull() {
			continue
		}
		sep := " · "
		if first {
			sep = " "
			if l.Label != "" {
				sep = " Reorder: "
			}
			first = false
		}
		box.Call("appendChild", doc.Call("createTextNode", sep))
		box.Call("appendChild", a)
	}
}

// partLabel is the name of a part with its variant: "Pod door · fine".
func partLabel(name, variant string) string {
	if variant == "" {
		return name
	}
	return name + " · " + variant
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
	setText("plViewTitle", partLabel(pp.Name, pp.Variant))
	facts := strings.Join(pp.Files, ", ")
	setText("plViewFacts", facts)
	for _, li := range jsList(byID("plParts").Call("querySelectorAll", "li")) {
		li.Get("classList").Call("toggle", "shown", li.Get("dataset").Get("part").String() == strings.ToLower(pp.Name))
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
	opts.Set("printer", pp.Printer) // the real bed of the TAZ, or the vat of the Phenom
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

// plateLayout is the layout of the plate of the parts, as the slice makes it (platepack, a copy
// of platepack.py of printdeck-action): an outline for each object, its place, the number of
// objects that fit, and the bed. The places are nil when the objects do not fit. A part with no
// bed fits. The result stays in a cache, by the parts and their counts.
type plateLayout struct {
	outlines []platepack.Outline
	names    []string
	places   []platepack.Place
	fit      int
	bx, by   float64
}

func (a *app) plateLayout(parts []int) *plateLayout {
	key := fmt.Sprint(a.plan.ID)
	for _, i := range parts {
		key += fmt.Sprintf(" %d:%d", i, a.planChoices[i].count)
	}
	if l, ok := a.layouts[key]; ok {
		return l
	}
	l := &plateLayout{}
	var margin float64
	for _, i := range parts {
		pp := a.plan.Parts[i]
		h := platepack.Outline(pp.Outline)
		if len(h) != platepack.N {
			h = platepack.RectOutline(pp.SizeX, pp.SizeY)
		}
		for k := 0; k < a.planChoices[i].count; k++ {
			l.outlines = append(l.outlines, h)
			l.names = append(l.names, partLabel(pp.Name, pp.Variant))
		}
		margin = math.Max(margin, pp.Margin)
		if pp.BedX > 0 {
			l.bx, l.by = pp.BedX, pp.BedY
		}
	}
	if l.bx > 0 {
		l.places, l.fit = platepack.Plate(l.outlines, l.bx, l.by, margin)
	}
	if a.layouts == nil {
		a.layouts = map[string]*plateLayout{}
	}
	a.layouts[key] = l
	return l
}

// plateFit reports whether the parts fit on the bed of their printer, the number of objects,
// and how many objects fit.
func (a *app) plateFit(parts []int) (bool, int, int) {
	l := a.plateLayout(parts)
	n := len(l.outlines)
	if l.bx == 0 || n == 0 {
		return true, n, n
	}
	return l.places != nil, n, l.fit
}

// sliceFacts is the print time and the filament of the slice of a part in the plan, for example
// " · 0:43:49 · 8.5 g", or "" for a part with no slice.
func sliceFacts(sec int, grams, ml float64) string {
	out := ""
	if sec > 0 {
		out += " · " + clock(sec)
	}
	if ml > 0 {
		out += fmt.Sprintf(" · %.1f ml", ml)
	} else if grams > 0 {
		out += fmt.Sprintf(" · %.1f g", grams)
	}
	return out
}

// plateEstimate is the print time and the filament of the plate pl from the slices of its parts
// in the plan: the sum of each part times its count. The slice of the plate can differ, so the
// text says "about". It is "" when a part has no slice.
func (a *app) plateEstimate(pl []int) string {
	sec, grams, ml := 0, 0.0, 0.0
	for _, i := range pl {
		pp := a.plan.Parts[i]
		if pp.Seconds <= 0 {
			return ""
		}
		c := a.planChoices[i].count
		sec, grams, ml = sec+pp.Seconds*c, grams+pp.Grams*float64(c), ml+pp.ML*float64(c)
	}
	return " · about" + sliceFacts(sec, grams, ml)[2:]
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
		head := profileName(first.Profile)
		if first.Prebuilt != "" {
			head = "a ready file"
		}
		sub := el("span", "sub", head+" · ")
		sub.Call("appendChild", materialEl(first.MaterialName, first.MaterialColor, first.Material))
		sub.Call("appendChild", doc.Call("createTextNode", " · "+count(objects, "object", "objects")+a.plateEstimate(pl)))
		card.Call("appendChild", sub)
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
			li.Call("appendChild", el("span", "", fmt.Sprintf("%s × %d", partLabel(pp.Name, pp.Variant), a.planChoices[i].count)))
			if a.view.CanSlice {
				sel := el("select", "", "")
				sel.Call("setAttribute", "aria-label", "The plate of "+partLabel(pp.Name, pp.Variant))
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

// drawPlateBed draws the bed of a plate from above, with the objects at the places of the slice.
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
	// The objects at the places of the slice, with their turns. The back of the bed is at the
	// top. With no places (the parts do not fit), the drawing has only the bed.
	l := a.plateLayout(pl)
	if l.places == nil {
		return
	}
	colors := []string{"#e8762b", "#f2a65a", "#8b5cf6", "#3aa05a", "#2a7fc0"}
	names := map[string]int{}
	for k, p := range l.places {
		if _, ok := names[l.names[k]]; !ok {
			names[l.names[k]] = len(names)
		}
		ctx.Set("fillStyle", colors[names[l.names[k]]%len(colors)])
		ctx.Call("beginPath")
		for j, c := range platepack.Corners(platepack.Turned(l.outlines[k], p.K), p.X, p.Y) {
			x, y := (bx/2+c[0])*scale, (by/2-c[1])*scale
			if j == 0 {
				ctx.Call("moveTo", x, y)
			} else {
				ctx.Call("lineTo", x, y)
			}
		}
		ctx.Call("closePath")
		ctx.Call("fill")
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
		text, class := buildSummary(b)
		title.Call("appendChild", el("span", "tag "+class, text))
		link.Call("appendChild", title)
		var parts []string
		for _, pl := range b.Plates {
			for _, pp := range pl.Parts {
				parts = append(parts, fmt.Sprintf("%s × %d", partLabel(pp.Name, pp.Variant), pp.Count))
			}
		}
		link.Call("appendChild", el("span", "sub", strings.Join(parts, ", ")))
		li.Call("appendChild", link)
		list.Call("appendChild", li)
	}
}

// buildSummary is the state of a build for a list: the state of the slice, or, for a build with
// its files, what the owner did with its plates. It returns the text and the class of the tag.
func buildSummary(b wire.ProjectBuild) (string, string) {
	if b.State != wire.BuildReady {
		return buildStateText(b.State), b.State
	}
	n := map[string]int{}
	for _, pl := range b.Plates {
		n[pl.State]++
	}
	total := len(b.Plates)
	switch {
	case n[wire.PartWaiting] > 0:
		return buildStateText(wire.PartWaiting), wire.PartWaiting
	case n[wire.PartApproved] == total:
		return buildStateText(wire.PartApproved), wire.PartApproved
	case n[wire.PartRejected] == total:
		return buildStateText(wire.PartRejected), wire.PartRejected
	case n[wire.PartReplaced] == total:
		return buildStateText(wire.PartReplaced), wire.PartReplaced
	}
	var parts []string
	for _, st := range []string{wire.PartApproved, wire.PartRejected, wire.PartReplaced} {
		if n[st] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n[st], strings.ToLower(buildStateText(st))))
		}
	}
	return strings.Join(parts, ", "), "ready"
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

// bedOf is the bed of a printer of printdeck.yml in mm.
func bedOf(printer string) (float64, float64) {
	switch strings.ToLower(printer) {
	case "phenom":
		return 276, 155
	}
	return 280, 280
}

// plateView returns the 3D view of the plate n of the build b: a box with the G-code viewer and its
// controls. The viewer loads when the box comes near the screen, so a build with many plates
// does not read all its G-code at once. A new draw of the build keeps the boxes, so a view keeps
// its turn and its layer.
func (a *app) plateView(b wire.ProjectBuild, n int) js.Value {
	pl := b.Plates[n]
	url, _, _ := strings.Cut(pl.FileURL, "?")
	key := fmt.Sprintf("%s/%d/%s", b.ID, n, url)
	if box, ok := a.plateViews[key]; ok {
		return box
	}
	box := el("div", "gcode-view", "")
	a.plateViews[key] = box
	start := func() {
		v := js.Global().Get("pwfView3d")
		if v.IsUndefined() {
			return
		}
		opts := js.Global().Get("Object").New()
		opts.Set("url", pl.FileURL)
		bx, by := bedOf(pl.Printer)
		bed := js.Global().Get("Array").New()
		bed.Call("push", bx, by)
		opts.Set("bed", bed)
		opts.Set("printer", pl.Printer)
		opts.Set("many", true)
		v.Call("showGcode", box, opts)
	}
	io := js.Global().Get("IntersectionObserver")
	if io.IsUndefined() {
		start()
		return box
	}
	var seen js.Func
	var obs js.Value
	seen = js.FuncOf(func(_ js.Value, args []js.Value) any {
		entries := args[0]
		for i := 0; i < entries.Length(); i++ {
			if entries.Index(i).Get("isIntersecting").Bool() {
				obs.Call("disconnect")
				start()
				seen.Release()
				return nil
			}
		}
		return nil
	})
	o := js.Global().Get("Object").New()
	o.Set("rootMargin", "300px")
	obs = io.New(seen, o)
	obs.Call("observe", box)
	return box
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
// again each 3 seconds.
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
	ownerPrinters = nil
	if v.IsOwner {
		a.api("GET", "/printers", nil, &ownerPrinters)
	}
	a.view = &v
	a.playing++
	playing := a.playing
	doc.Set("title", buildTitle(&v, b)+" · "+v.Name+" · Printdeck")
	byID("bdBack").Set("href", planPath(v.Slug, b.Plan))
	setText("bdBack", "The plan")
	show("vBuild")
	a.drawBuild(b)
	// The clock of the slice goes on each second, between the loads of the build.
	go func() {
		for a.playing == playing && a.lastBuild.State == wire.BuildSlicing {
			drawSliceClock(a.lastBuild)
			time.Sleep(time.Second)
		}
	}()
	if watchBuild(b) {
		go func() {
			for {
				wait := 3 * time.Second
				if b.State != wire.BuildSlicing {
					wait = 20 * time.Second
				}
				time.Sleep(wait)
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
				b = nb
				if !watchBuild(nb) {
					return
				}
			}
		}()
	}
}

// watchBuild reports that the build page loads the build again: while it slices, and while a
// plate is sent to a printer or is in its queue, so a change on the deck shows.
func watchBuild(b wire.ProjectBuild) bool {
	if b.State == wire.BuildSlicing {
		return true
	}
	for _, pl := range b.Plates {
		if pl.Queued != nil || pl.SentTo != "" && pl.TakenAt.IsZero() {
			return true
		}
	}
	return false
}

// buildTitle is the title of the build page: the plan (the pull request, the tag or the branch)
// and the parts of the build, for example "PR #9 · Slate frame, Slate body and 2 more".
func buildTitle(v *wire.ProjectView, b wire.ProjectBuild) string {
	var parts []string
	seen := map[string]bool{}
	for _, pl := range b.Plates {
		for _, pp := range pl.Parts {
			if l := partLabel(pp.Name, pp.Variant); !seen[l] {
				seen[l] = true
				parts = append(parts, l)
			}
		}
	}
	names := strings.Join(parts, ", ")
	if len(parts) > 3 {
		names = strings.Join(parts[:3], ", ") + fmt.Sprintf(" and %d more", len(parts)-3)
	} else if len(parts) > 1 {
		names = strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
	if names == "" {
		names = "A build of " + count(len(b.Plates), "plate", "plates")
	}
	if v != nil {
		for _, p := range v.Plans {
			if p.ID == b.Plan {
				return planName(p) + " · " + names
			}
		}
	}
	return names
}

// buildFacts is the size of the build: the plates, the print time and the material of all the
// plates, for example "3 plates · 11:02:00 · 125 g".
func buildFacts(b wire.ProjectBuild) string {
	sec, grams, ml := 0, 0.0, 0.0
	for _, pl := range b.Plates {
		sec, grams, ml = sec+pl.Seconds, grams+pl.Grams, ml+pl.ML
	}
	out := count(len(b.Plates), "plate", "plates")
	if sec > 0 {
		out += " · " + clock(sec)
	}
	if grams > 0 {
		out += fmt.Sprintf(" · %.0f g", grams)
	}
	if ml > 0 {
		out += fmt.Sprintf(" · %.0f ml", ml)
	}
	return out
}

// drawBuild draws the plates of the build b, with the keys of the owner.
// ownerPrinters are the printers of the owner, for the keys Send to of the build page.
var ownerPrinters []wire.Printer

func (a *app) drawBuild(b wire.ProjectBuild) {
	v := a.view
	setText("bdTitle", buildTitle(v, b))
	setText("bdSub", buildFacts(b)+" · commit "+shortSHA(b.Commit)+" · "+localWhen(b.Created)+" · "+buildStateText(b.State))
	drawSlicing(b)
	setHidden("bdStartCard", b.Start == "" || !v.IsOwner)
	setText("bdStart", b.Start)
	setHidden("bdError", b.Error == "")
	setText("bdError", b.Error)
	setHidden("bdRun", b.Run == "")
	byID("bdRun").Set("href", b.Run)
	setHidden("bdAgain", !v.CanSlice || b.State != wire.BuildFailed)
	a.lastBuild = b
	box := byID("bdPlates")
	box.Set("textContent", "")
	// Each plate card has a 3D view of its own (plateView). A view stays when the page draws the
	// build again, and goes with the page.
	if a.plateViews == nil || a.plateBuild != b.ID {
		a.plateViews, a.plateBuild = map[string]js.Value{}, b.ID
	}
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
			parts = append(parts, fmt.Sprintf("%s × %d", partLabel(pp.Name, pp.Variant), pp.Count))
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
		if pl.Material != "" || pl.MaterialName != "" {
			d := el("div", "", "")
			d.Call("appendChild", el("dt", "", "Material"))
			dd := el("dd", "", "")
			dd.Call("appendChild", materialEl(pl.MaterialName, pl.MaterialColor, pl.Material))
			withReorder(dd, pl.MaterialURL, pl.MaterialLinks)
			d.Call("appendChild", dd)
			facts.Call("appendChild", d)
		}
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
		if pl.FileURL != "" && pl.Process == "fdm" {
			card.Call("appendChild", a.plateView(b, n))
			card.Call("appendChild", el("p", "small dim", "Drag to turn the plate, also under the bed. Pinch or scroll to zoom. Click the view, then the arrow keys move the layer."))
		} else if pl.PictureURL != "" {
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
			line := "Approved by " + pl.By + ", " + localWhen(pl.At) + "."
			q := pl.Queued
			switch {
			case q != nil && q.Removing:
				line += " In the queue of the " + q.Printer + ": its deck takes it out within a minute."
			case q != nil:
				line += fmt.Sprintf(" In the queue of the %s, place %d. A press on the deck starts it.", q.Printer, q.Place)
			case !pl.TakenAt.IsZero():
				line += " It was in the queue of the " + pl.SentTo + ", and is not in a queue now. Send it again to print it."
			case pl.SentTo != "" && pl.Process == "resin":
				line += " Sent to the " + pl.SentTo + ": its deck puts it in its queue within a minute, and sends the file to the printer when no print runs."
			case pl.SentTo != "":
				line += " Sent to the " + pl.SentTo + ": its deck puts it in its queue within a minute."
			default:
				line += " Send it to the queue of a printer."
			}
			card.Call("appendChild", el("p", "small dim", line))
			// In a queue: the key Remove from the queue. Else the keys Send to: the printers of the
			// process of the plate. A removal on the deck shows the keys again at its next report.
			if v.IsOwner && q != nil && q.ID != "" && q.PrinterID != "" && !q.Removing {
				keys := el("div", "row-keys", "")
				msg := el("p", "msg", "")
				btn := el("button", "", "Remove from the queue")
				btn.Set("type", "button")
				path := "/projects/" + v.ID + "/queue/" + q.PrinterID + "/" + q.ID
				btn.Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any {
					go func() {
						if err := a.api("DELETE", path, nil, nil); err != nil {
							msg.Set("textContent", err.Error())
							msg.Get("classList").Call("add", "err")
							return
						}
						var nb wire.ProjectBuild
						if a.api("GET", "/projects/"+v.ID+"/builds/"+b.ID, nil, &nb) == nil {
							a.drawBuild(nb)
						}
					}()
					return nil
				}))
				keys.Call("appendChild", btn)
				card.Call("appendChild", keys)
				card.Call("appendChild", msg)
			}
			if v.IsOwner && pl.File != "" && q == nil {
				keys := el("div", "row-keys", "")
				msg := el("p", "msg", "")
				for _, pr := range ownerPrinters {
					if pr.Process != pl.Process {
						continue
					}
					label := "Send to the " + pr.Name
					if pl.SentTo == pr.Name {
						label = "Send again to the " + pr.Name
					}
					btn := el("button", "primary", label)
					btn.Set("type", "button")
					printerID := pr.ID
					btn.Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any {
						go func() {
							var nb wire.ProjectBuild
							path := fmt.Sprintf("/projects/%s/builds/%s/plates/%d/send", v.ID, b.ID, n)
							if err := a.api("POST", path, wire.PlateSend{Printer: printerID}, &nb); err != nil {
								msg.Set("textContent", err.Error())
								msg.Get("classList").Call("add", "err")
								return
							}
							a.drawBuild(nb)
						}()
						return nil
					}))
					keys.Call("appendChild", btn)
				}
				card.Call("appendChild", keys)
				card.Call("appendChild", msg)
			}
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

// The steps of a slice run, for the card of the build page.
var sliceSteps = []string{"GitHub starts the slice run", "Get the commit and the slicer", "Slice the plates", "Send the files"}

// sliceStep is the step of the slice run of the build b, from 0 (GitHub starts the run) to 3
// (send the files). An older Action sends only a text, with no phase.
func sliceStep(b wire.ProjectBuild) int {
	if b.Run == "" && b.Phase == "" {
		return 0
	}
	switch {
	case b.Phase == wire.PhaseSlice, b.Phase == "" && strings.HasPrefix(b.Progress, "Slicing"):
		return 2
	case b.Phase == wire.PhaseUpload, b.Phase == "" && strings.HasPrefix(b.Progress, "Sending"):
		return 3
	}
	return 1
}

// sliceDone is the part of the slice run that is done, from 0 to 1, for the bar.
func sliceDone(b wire.ProjectBuild) float64 {
	switch sliceStep(b) {
	case 0:
		return 0.04
	case 1:
		return 0.2
	case 2:
		n := len(b.Plates)
		if b.Plate < 1 || n == 0 {
			return 0.35
		}
		return 0.35 + 0.5*float64(b.Plate-1)/float64(n)
	}
	return 0.9
}

// drawSlicing shows the card of the slice while the build slices: the steps, a bar and a clock.
func drawSlicing(b wire.ProjectBuild) {
	slicing := b.State == wire.BuildSlicing
	setHidden("bdSlicing", !slicing)
	if !slicing {
		return
	}
	byID("bdBar").Get("style").Set("width", fmt.Sprintf("%.0f%%", 100*sliceDone(b)))
	now := sliceStep(b)
	list := byID("bdSteps")
	list.Set("textContent", "")
	for i, text := range sliceSteps {
		if i == 2 && len(b.Plates) == 1 {
			text = "Slice the plate"
		}
		if i == now && b.Progress != "" && i > 0 {
			text = b.Progress
		}
		class := "todo"
		switch {
		case i < now:
			class = "done"
		case i == now:
			class = "now"
		}
		li := el("li", class, text)
		if i == now {
			li.Set("ariaCurrent", "step")
		}
		list.Call("appendChild", li)
	}
	drawSliceClock(b)
}

// drawSliceClock shows the time since the person asked for the slice.
func drawSliceClock(b wire.ProjectBuild) {
	sec := int(time.Since(b.Created).Seconds())
	if sec < 0 {
		sec = 0
	}
	setText("bdClock", fmt.Sprintf("%d:%02d", sec/60, sec%60))
}
