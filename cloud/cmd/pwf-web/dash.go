//go:build js && wasm

package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"syscall/js"
	"time"

	"printdeck/internal/cloud/wire"
)

// The overview of the project page, and its tabs. The overview shows what happens now, the
// numbers, a chart of the last 12 weeks, the newest prints, a feed of what happened, and a
// column with what waits for the owner, the pull requests, the printers, the people, the notes
// and the newest photos. The tabs Prints, Photos and Settings show the full lists.

// projectTabs are the tabs with an address of their own: /projects/<slug>/<tab>.
var projectTabs = map[string]bool{"prints": true, "photos": true, "settings": true}

// tabPath is the address of the tab of the project.
func tabPath(slug, tab string) string {
	if !projectTabs[tab] {
		return "/projects/" + slug
	}
	return "/projects/" + slug + "/" + tab
}

// setTab shows the tab of the project page. The CSS hides each part of the page with another tab
// in its data-on.
func setTab(tab string) {
	byID("vProject").Get("dataset").Set("tab", tab)
	links := doc.Call("querySelectorAll", "#pTabs a[data-tab]")
	for i := 0; i < links.Get("length").Int(); i++ {
		l := links.Index(i)
		if l.Get("dataset").Get("tab").String() == tab {
			l.Call("setAttribute", "aria-current", "page")
		} else {
			l.Call("removeAttribute", "aria-current")
		}
	}
}

// bindTabs makes a click on a tab, or on a link to a tab in a card, show the tab with no new load
// of the project. The tab Builds is a plain link to the page of the plans.
func (a *app) bindTabs() {
	byID("vProject").Call("addEventListener", "click", js.FuncOf(func(_ js.Value, args []js.Value) any {
		e := args[0]
		if e.Get("button").Int() != 0 || e.Get("metaKey").Bool() || e.Get("ctrlKey").Bool() ||
			e.Get("shiftKey").Bool() || e.Get("altKey").Bool() || a.view == nil {
			return nil
		}
		link := e.Get("target").Call("closest", "a[data-tab]")
		if link.IsNull() {
			return nil
		}
		tab := link.Get("dataset").Get("tab").String()
		if tab == "builds" {
			return nil
		}
		e.Call("preventDefault")
		e.Call("stopPropagation")
		a.tab = tab
		js.Global().Get("history").Call("pushState", nil, "", tabPath(a.view.Slug, tab))
		lastView = viewKey()
		if tab == "overview" {
			// The chips of the printers on the tab Prints change the numbers.
			drawStats(a.view.Prints)
			drawRate(a.view.Prints)
		}
		setTab(tab)
		js.Global().Call("scrollTo", 0, 0)
		return nil
	}))
}

// drawDashboard draws the tabs and the overview. With full false, it draws only the parts that
// change with the time: the line of what happens now, and the printers. The other parts have
// pictures, and a new draw loads them again.
func (a *app) drawDashboard(v *wire.ProjectView, full bool) {
	a.drawPulse(v)
	drawRigs(v)
	if !full {
		return
	}
	// The tabs.
	links := doc.Call("querySelectorAll", "#vProject a[data-tab]")
	for i := 0; i < links.Get("length").Int(); i++ {
		l := links.Index(i)
		tab := l.Get("dataset").Get("tab").String()
		if tab == "builds" {
			l.Set("href", "/projects/"+v.Slug+"/plans")
		} else {
			l.Set("href", tabPath(v.Slug, tab))
		}
	}
	setText("tbPrintsN", countText(len(v.Prints)))
	setText("tbPhotosN", countText(len(v.Photos)))
	setText("tbBuildsN", countText(v.PlansTotal))
	setHidden("tbBuilds", v.Repo == "")
	// The head: the faces of the people, the repository and the public page.
	drawFaces(v)
	setHidden("pRepoChip", v.Repo == "")
	byID("pRepoChip").Set("href", "https://github.com/"+v.Repo)
	setText("pRepoChip", v.Repo)
	setHidden("pPublicChip", v.Public == "")
	byID("pPublicChip").Set("href", v.Public)

	drawStats(v.Prints)
	drawRate(v.Prints)
	drawChart(v.Prints)
	drawMaterials(v.Prints)
	drawRecent(v)
	a.drawFeed(v)
	a.drawWaiting(v)
	a.drawGit(v)
	a.drawPeople(v)
	drawMosaic(v)
}

// countText is a count for a tab, or "" for none.
func countText(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprint(n)
}

// ago is the time from t to now in words: "just now", "5 min ago", "3 h ago", "yesterday",
// "4 days ago", or the date.
func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	case d < 48*time.Hour:
		return "yesterday"
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
	return shortDate(t)
}

// newestPrints returns the prints, the newest first.
func newestPrints(prints []wire.ProjectPrint) []wire.ProjectPrint {
	out := append([]wire.ProjectPrint(nil), prints...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Ended.After(out[j].Ended) })
	return out
}

// drawRate fills the share of the prints that finished.
func drawRate(prints []wire.ProjectPrint) {
	done := 0
	for _, p := range prints {
		if p.Result == "complete" {
			done++
		}
	}
	if len(prints) == 0 {
		setText("stRate", "–")
		setText("stRateSub", "")
		return
	}
	setText("stRate", fmt.Sprintf("%d%%", int(math.Round(100*float64(done)/float64(len(prints))))))
	setText("stRateSub", fmt.Sprintf("%d of %d finished", done, len(prints)))
}

// The line of what happens now.

// waitItem is a build with plates that wait for the approval of the owner.
type waitItem struct {
	plan  wire.ProjectPlan
	build wire.ProjectBuild
	n     int
	parts []string
}

// waitingPlates returns the builds with plates that wait for the owner, the newest first.
func waitingPlates(v *wire.ProjectView) []waitItem {
	var out []waitItem
	if !v.IsOwner {
		return nil
	}
	for _, p := range v.Plans {
		for _, b := range p.Builds {
			w := waitItem{plan: p, build: b}
			for _, pl := range b.Plates {
				if pl.State != wire.PartWaiting {
					continue
				}
				w.n++
				for _, pp := range pl.Parts {
					w.parts = append(w.parts, partLabel(pp.Name, pp.Variant))
				}
			}
			if w.n > 0 {
				out = append(out, w)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].build.Created.After(out[j].build.Created) })
	return out
}

// drawPulse draws the line at the top of the overview: the print that prints now, or the time of
// the last print; the plates that wait; the open pull requests.
func (a *app) drawPulse(v *wire.ProjectView) {
	box := byID("pPulse")
	box.Set("textContent", "")
	item := func(class, text, href string) {
		var e js.Value
		if href != "" {
			e = el("a", "pi "+class, "")
			e.Set("href", href)
		} else {
			e = el("span", "pi "+class, "")
		}
		e.Call("appendChild", el("i", "", ""))
		e.Call("appendChild", el("span", "", text))
		box.Call("appendChild", e)
	}
	var printing []wire.ProjectLive
	for _, l := range v.Live {
		if l.Result == "" {
			printing = append(printing, l)
		}
	}
	switch {
	case len(printing) > 0:
		l := printing[0]
		text := "Printing " + l.Name
		if l.Printer != "" {
			text += " on " + article(l.Printer) + " " + l.Printer
		}
		if l.Percent > 0 {
			text += fmt.Sprintf(" · %.0f%%", l.Percent)
		}
		if l.Left > 0 {
			text += " · done " + localWhen(time.Now().Add(time.Duration(l.Left)*time.Second))
		}
		if len(printing) > 1 {
			text += fmt.Sprintf(" · %d more", len(printing)-1)
		}
		item("on", text, l.URL)
	case len(v.Prints) > 0:
		last := newestPrints(v.Prints)[0]
		item("idle", "Idle · the last print ended "+ago(last.Ended), projectPath(v.Slug, last.N))
	default:
		item("idle", "No prints yet", "")
	}
	if w := waitingPlates(v); len(w) > 0 {
		n := 0
		for _, x := range w {
			n += x.n
		}
		item("warn", count(n, "plate waits", "plates wait")+" for your approval", buildPath(v.Slug, w[0].build.ID))
	}
	if pulls := pullPlans(v); len(pulls) > 0 {
		item("", count(len(pulls), "open pull request", "open pull requests"), "/projects/"+v.Slug+"/plans")
	}
}

// The chart of the last 12 weeks.

// niceMax is the top of the axis of a chart: a round number at or above m.
func niceMax(m float64) float64 {
	if m <= 0 {
		return 1
	}
	p := math.Pow(10, math.Floor(math.Log10(m)))
	for _, f := range []float64{1, 2, 2.5, 5, 10} {
		if f*p >= m {
			return f * p
		}
	}
	return 10 * p
}

// drawChart draws the hours of print of each of the last 12 weeks: the finished prints, with
// the stopped prints over them.
func drawChart(prints []wire.ProjectPrint) {
	const weeks = 12
	week := 7 * 24 * time.Hour
	now := time.Now()
	var ok, stop [weeks]float64
	var n [weeks]int
	total, totalN := 0.0, 0
	for _, p := range prints {
		age := now.Sub(p.Ended)
		if age < 0 || age >= weeks*week {
			continue
		}
		i := weeks - 1 - int(age/week)
		h := float64(p.Seconds) / 3600
		if p.Result == "complete" {
			ok[i] += h
		} else {
			stop[i] += h
		}
		n[i]++
		total += h
		totalN++
	}
	top := 0.0
	for i := range ok {
		top = math.Max(top, ok[i]+stop[i])
	}
	top = niceMax(top)
	const w, h, left, bottom, upper = 600.0, 170.0, 38.0, 24.0, 10.0
	plotH := h - bottom - upper
	slot := (w - left) / weeks
	bw := slot * 0.62
	var b strings.Builder
	fmt.Fprintf(&b, `<svg viewBox="0 0 %g %g" width="100%%" role="img" aria-label="The hours of print of each week">`, w, h)
	for _, f := range []float64{0, 0.5, 1} {
		y := upper + plotH*(1-f)
		fmt.Fprintf(&b, `<line class="grid" x1="%g" x2="%g" y1="%.1f" y2="%.1f"/>`, left, w, y, y)
		fmt.Fprintf(&b, `<text class="ax" x="%g" y="%.1f" text-anchor="end">%s h</text>`, left-6, y+4, trimFloat(top*f))
	}
	for i := 0; i < weeks; i++ {
		x := left + slot*float64(i) + (slot-bw)/2
		start := now.Add(-time.Duration(weeks-i) * week)
		title := fmt.Sprintf("%s to %s: %s, %s", shortDate(start.Add(24*time.Hour)), shortDate(start.Add(week)),
			count(n[i], "print", "prints"), hoursMinutes(int((ok[i]+stop[i])*3600)))
		y := upper + plotH
		if ok[i]+stop[i] == 0 {
			fmt.Fprintf(&b, `<rect class="b-none" x="%.1f" y="%.1f" width="%.1f" height="3" rx="1.5"><title>%s</title></rect>`, x, y-3, bw, title)
		}
		for _, part := range []struct {
			v     float64
			class string
		}{{ok[i], "b-ok"}, {stop[i], "b-stop"}} {
			if part.v <= 0 {
				continue
			}
			bh := math.Max(plotH*part.v/top, 3)
			y -= bh
			fmt.Fprintf(&b, `<rect class="%s" x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="3"><title>%s</title></rect>`, part.class, x, y, bw, bh, title)
		}
		if i%3 == 2 || i == weeks-1 {
			label := shortDate(start.Add(24 * time.Hour))
			if i == weeks-1 {
				label = "This week"
			}
			fmt.Fprintf(&b, `<text class="ax" x="%.1f" y="%g" text-anchor="middle">%s</text>`, x+bw/2, h-6, label)
		}
	}
	b.WriteString(`</svg>`)
	byID("pChart").Set("innerHTML", b.String())
	if totalN == 0 {
		setText("pChartSum", "No prints in this time")
	} else {
		setText("pChartSum", hoursMinutes(int(total*3600))+" in "+count(totalN, "print", "prints"))
	}
}

// trimFloat is a number with no zeros at its end: 2.5, 10.
func trimFloat(f float64) string {
	s := fmt.Sprintf("%.1f", f)
	return strings.TrimSuffix(strings.TrimSuffix(s, "0"), ".")
}

// matColors are the colors of the materials with no color of their own.
var matColors = []string{"#ff5a1f", "#3d7bd9", "#2f9e5b", "#c94fb7", "#d9a520", "#7a7a7a"}

// drawMaterials draws the grams of each material as one bar, with a key under it.
func drawMaterials(prints []wire.ProjectPrint) {
	type mat struct {
		name, color string
		g           float64
	}
	byName := map[string]*mat{}
	var mats []*mat
	total := 0.0
	for _, p := range prints {
		if p.Grams <= 0 {
			continue
		}
		name, color := p.Material, ""
		if f := p.Filament; f != nil {
			if f.Name != "" {
				name = f.Name
			} else if f.Type != "" {
				name = f.Type
			}
			color = f.Color
		}
		if name == "" {
			name = "Other"
		}
		m := byName[name]
		if m == nil {
			m = &mat{name: name, color: color}
			byName[name] = m
			mats = append(mats, m)
		}
		m.g += p.Grams
		total += p.Grams
	}
	sort.SliceStable(mats, func(i, j int) bool { return mats[i].g > mats[j].g })
	if len(mats) > 6 {
		rest := &mat{name: "Other"}
		for _, m := range mats[5:] {
			rest.g += m.g
		}
		mats = append(mats[:5], rest)
	}
	bar, key := byID("pMatBar"), byID("pMatLegend")
	bar.Set("textContent", "")
	key.Set("textContent", "")
	setHidden("pMatBar", total == 0)
	for i, m := range mats {
		color := m.color
		if color == "" {
			color = matColors[i%len(matColors)]
		}
		seg := el("i", "", "")
		seg.Get("style").Set("width", fmt.Sprintf("%.2f%%", 100*m.g/total))
		seg.Get("style").Set("background", color)
		seg.Set("title", m.name+" · "+grams0(m.g))
		bar.Call("appendChild", seg)
		k := el("span", "", "")
		dot := el("i", "", "")
		dot.Get("style").Set("background", color)
		k.Call("appendChild", dot)
		k.Call("appendChild", el("span", "", m.name+" "))
		k.Call("appendChild", el("b", "", grams0(m.g)))
		key.Call("appendChild", k)
	}
}

// The newest prints, the feed and the photos.

// drawRecent draws the 4 newest prints.
func drawRecent(v *wire.ProjectView) {
	box := byID("pRecent")
	box.Set("textContent", "")
	prints := newestPrints(v.Prints)
	setHidden("pRecentNone", len(prints) > 0)
	setHidden("pRecentAll", len(prints) <= 4)
	setText("pRecentAll", fmt.Sprintf("All %d prints →", len(prints)))
	if len(prints) > 4 {
		prints = prints[:4]
	}
	for _, p := range prints {
		box.Call("appendChild", printTile(p, projectPath(v.Slug, p.N)))
	}
}

// event is a line of the feed.
type event struct {
	at              time.Time
	kind, text, sub string
	href            string
}

// drawFeed draws what happened in the project, the newest first, with a heading for each day.
func (a *app) drawFeed(v *wire.ProjectView) {
	var evs []event
	for _, p := range v.Prints {
		href := projectPath(v.Slug, p.N)
		sub := clock(p.Seconds)
		if p.Grams > 0 {
			sub += " · " + grams(p.Grams)
		}
		if p.Printer != "" {
			sub += " · " + p.Printer
		}
		switch p.Result {
		case "complete":
			evs = append(evs, event{p.Ended, "ok", "Finished " + p.Name, sub, href})
		case "cancelled":
			evs = append(evs, event{p.Ended, "stop", "Cancelled " + p.Name, sub, href})
		default:
			evs = append(evs, event{p.Ended, "stop", p.Name + " stopped", sub, href})
		}
		if !p.Ready.IsZero() {
			evs = append(evs, event{p.Ready, "ready", p.Name + " is ready to pick up", p.ReadyNote, href})
		}
		if !p.Delivered.IsZero() {
			evs = append(evs, event{p.Delivered, "ok", "Delivered " + p.Name, "", href})
		}
	}
	// The photos of one person on one day make one line.
	photos := append([]wire.ProjectPhoto(nil), v.Photos...)
	sort.SliceStable(photos, func(i, j int) bool { return photos[i].Added.After(photos[j].Added) })
	for i := 0; i < len(photos); {
		j := i + 1
		for j < len(photos) && photos[j].By == photos[i].By && dayKey(photos[j].Added) == dayKey(photos[i].Added) {
			j++
		}
		evs = append(evs, event{photos[i].Added, "photo", photos[i].By + " added " + count(j-i, "photo", "photos"), "", tabPath(v.Slug, "photos")})
		i = j
	}
	for _, p := range v.Plans {
		who := ""
		if p.Author != "" {
			who = " of @" + p.Author
		}
		changed := 0
		for _, pp := range p.Parts {
			if pp.Changed {
				changed++
			}
		}
		evs = append(evs, event{p.Created, "git", "New plan" + who + " · " + planName(p),
			shortSHA(p.Commit) + " · " + count(changed, "part", "parts") + " changed", planPath(v.Slug, p.ID)})
		for _, b := range p.Builds {
			evs = append(evs, event{b.Created, "slice", "Slice of " + count(len(b.Plates), "plate", "plates") + " · " + planName(p),
				strings.ToLower(buildStateText(b.State)), buildPath(v.Slug, b.ID)})
			for i, pl := range b.Plates {
				switch pl.State {
				case wire.PartApproved:
					evs = append(evs, event{pl.At, "ok", fmt.Sprintf("%s approved plate %d · %s", pl.By, i+1, planName(p)), "", buildPath(v.Slug, b.ID)})
				case wire.PartRejected:
					evs = append(evs, event{pl.At, "stop", fmt.Sprintf("%s rejected plate %d · %s", pl.By, i+1, planName(p)), pl.Reason, buildPath(v.Slug, b.ID)})
				}
			}
		}
	}
	if !v.NotesAt.IsZero() {
		evs = append(evs, event{v.NotesAt, "note", v.NotesBy + " changed the notes", "", ""})
	}
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].at.After(evs[j].at) })
	if len(evs) > 12 {
		evs = evs[:12]
	}
	list := byID("pFeed")
	list.Set("textContent", "")
	setHidden("pFeedNone", len(evs) > 0)
	day := ""
	for _, e := range evs {
		if e.at.IsZero() {
			continue
		}
		if d := dayLabel(e.at); d != day {
			day = d
			list.Call("appendChild", el("li", "day", d))
		}
		li := el("li", "ev "+e.kind, "")
		var body js.Value
		if e.href != "" {
			body = el("a", "", "")
			body.Set("href", e.href)
		} else {
			body = el("div", "", "")
		}
		body.Call("appendChild", el("span", "dot", ""))
		txt := el("span", "txt", "")
		txt.Call("appendChild", el("strong", "", e.text))
		if e.sub != "" {
			txt.Call("appendChild", el("span", "sub", e.sub))
		}
		body.Call("appendChild", txt)
		body.Call("appendChild", el("time", "", localTime(e.at)))
		li.Call("appendChild", body)
		list.Call("appendChild", li)
	}
}

// dayKey is the day of t for the viewer.
func dayKey(t time.Time) string {
	return js.Global().Get("Date").New(float64(t.UnixMilli())).Call("toDateString").String()
}

// dayLabel is the heading of the day of t in the feed: "Today", "Yesterday", or the date.
func dayLabel(t time.Time) string {
	switch dayKey(t) {
	case dayKey(time.Now()):
		return "Today"
	case dayKey(time.Now().Add(-24 * time.Hour)):
		return "Yesterday"
	}
	return shortDate(t)
}

// drawMosaic draws the 6 newest photos.
func drawMosaic(v *wire.ProjectView) {
	box := byID("pMosaic")
	box.Set("textContent", "")
	photos := append([]wire.ProjectPhoto(nil), v.Photos...)
	sort.SliceStable(photos, func(i, j int) bool { return photos[i].Added.After(photos[j].Added) })
	setHidden("pMosaic", len(photos) == 0)
	if len(photos) == 0 {
		setText("pMosaicAll", "Add photos →")
	} else {
		setText("pMosaicAll", fmt.Sprintf("All %d →", len(photos)))
	}
	if len(photos) > 6 {
		photos = photos[:6]
	}
	for _, ph := range photos {
		box.Call("appendChild", photoFigure(ph))
	}
}

// The column: what waits, GitHub, the printers and the people.

// drawWaiting draws the plates that wait for the approval of the owner, and the pull requests
// with a plan and no build.
func (a *app) drawWaiting(v *wire.ProjectView) {
	list := byID("pWait")
	list.Set("textContent", "")
	n := 0
	for _, w := range waitingPlates(v) {
		title := fmt.Sprintf("Approve %s · %s", count(w.n, "plate", "plates"), planName(w.plan))
		list.Call("appendChild", dashRow(buildPath(v.Slug, w.build.ID), title, strings.Join(w.parts, ", "), "", ""))
		n++
	}
	if v.IsOwner {
		for _, p := range pullPlans(v) {
			if len(p.Builds) > 0 || !p.Ready {
				continue
			}
			sub := "A new plan"
			if p.Author != "" {
				sub += " of @" + p.Author
			}
			list.Call("appendChild", dashRow(planPath(v.Slug, p.ID), fmt.Sprintf("Choose the plates · PR #%d", p.PR), sub+" · "+ago(p.Created), "", ""))
			n++
		}
	}
	setText("pWaitN", fmt.Sprint(n))
	setHidden("pWaitCard", n == 0)
}

// pullPlans returns the newest plan of each pull request, the newest first.
func pullPlans(v *wire.ProjectView) []wire.ProjectPlan {
	seen := map[int]bool{}
	var out []wire.ProjectPlan
	plans := append([]wire.ProjectPlan(nil), v.Plans...)
	sort.SliceStable(plans, func(i, j int) bool { return plans[i].Created.After(plans[j].Created) })
	for _, p := range plans {
		if p.Kind != wire.BuildTest || seen[p.PR] {
			continue
		}
		seen[p.PR] = true
		out = append(out, p)
	}
	return out
}

// steps are the states of the three steps of a plan: the plan, the slice and the approval. Each
// is "done", "now", "fail" or "".
func steps(p wire.ProjectPlan) [3]string {
	var s [3]string
	if !p.Ready {
		s[0] = "now"
		return s
	}
	s[0] = "done"
	if len(p.Builds) == 0 {
		return s
	}
	b := p.Builds[0]
	switch b.State {
	case wire.BuildSlicing:
		s[1] = "now"
		return s
	case wire.BuildFailed:
		s[1] = "fail"
		return s
	}
	s[1] = "done"
	n := map[string]int{}
	for _, pl := range b.Plates {
		n[pl.State]++
	}
	switch {
	case n[wire.PartRejected] > 0:
		s[2] = "fail"
	case n[wire.PartWaiting] > 0:
		s[2] = "now"
	case n[wire.PartApproved] > 0:
		s[2] = "done"
	}
	return s
}

// drawGit draws the card of GitHub: the repository, the newest plan of main, and each open pull
// request with its steps.
func (a *app) drawGit(v *wire.ProjectView) {
	setHidden("pGitCard", v.Repo == "")
	if v.Repo == "" {
		return
	}
	repo := byID("pGitRepo")
	repo.Set("href", "https://github.com/"+v.Repo)
	setText("pGitRepo", v.Repo)
	setText("pGitAll", fmt.Sprintf("All %d builds →", v.PlansTotal))
	// The newest plan of main.
	mainEl := byID("pGitMain")
	mainEl.Set("textContent", "")
	setHidden("pGitMain", true)
	for _, p := range v.Plans {
		if p.Kind != wire.BuildMain && p.Kind != wire.BuildRelease {
			continue
		}
		link := el("a", "", "")
		link.Set("href", planPath(v.Slug, p.ID))
		link.Call("appendChild", el("strong", "", planName(p)))
		link.Call("appendChild", el("span", "sub", shortSHA(p.Commit)+" · "+ago(p.Created)))
		if len(p.Builds) > 0 {
			text, class := buildSummary(p.Builds[0])
			link.Call("appendChild", el("span", "tag "+class, text))
		}
		mainEl.Call("appendChild", link)
		setHidden("pGitMain", false)
		break
	}
	list := byID("pPulls")
	list.Set("textContent", "")
	pulls := pullPlans(v)
	setHidden("pPullsNone", len(pulls) > 0)
	for _, p := range pulls {
		li := el("li", "", "")
		link := el("a", "", "")
		link.Set("href", planPath(v.Slug, p.ID))
		title := fmt.Sprintf("PR #%d", p.PR)
		if p.Author != "" {
			title += " · @" + p.Author
		}
		head := el("div", "phd", "")
		head.Call("appendChild", el("strong", "", title))
		head.Call("appendChild", el("span", "sub", ago(p.Created)))
		link.Call("appendChild", head)
		changed := 0
		for _, pp := range p.Parts {
			if pp.Changed {
				changed++
			}
		}
		link.Call("appendChild", el("span", "sub", shortSHA(p.Commit)+" · "+count(changed, "part", "parts")+" changed"))
		row := el("div", "steps", "")
		for i, st := range steps(p) {
			s := el("span", "step "+st, []string{"Plan", "Slice", "Approve"}[i])
			row.Call("appendChild", s)
		}
		link.Call("appendChild", row)
		li.Call("appendChild", link)
		list.Call("appendChild", li)
	}
}

// drawRigs draws the printers of the project: what each one does now, and its prints.
func drawRigs(v *wire.ProjectView) {
	list := byID("pRigs")
	list.Set("textContent", "")
	slugs := printerSlugs(v)
	known := map[string]bool{}
	for _, pr := range v.Printers {
		known[pr.ID] = true
	}
	rows := 0
	last := map[string]time.Time{}
	counts := map[string]int{}
	for _, p := range v.Prints {
		counts[p.PrinterID]++
		if p.Ended.After(last[p.PrinterID]) {
			last[p.PrinterID] = p.Ended
		}
	}
	for _, pr := range v.Printers {
		state, sub := "idle", "Idle"
		if t := last[pr.ID]; !t.IsZero() {
			sub = "Idle · the last print " + ago(t)
		}
		for _, l := range v.Live {
			if l.PrinterID == pr.ID && l.Result == "" {
				state, sub = "on", fmt.Sprintf("Printing %s · %.0f%%", l.Name, l.Percent)
				if l.Paused {
					state, sub = "warn", "Paused · "+l.Name
				}
			}
		}
		li := el("li", state, "")
		link := el("a", "", "")
		link.Set("href", printerPath(v.Slug, slugs[pr.ID]))
		link.Call("appendChild", el("span", "dot", ""))
		txt := el("span", "txt", "")
		txt.Call("appendChild", el("strong", "", pr.Name))
		txt.Call("appendChild", el("span", "sub", sub))
		link.Call("appendChild", txt)
		link.Call("appendChild", el("span", "num", count(counts[pr.ID], "print", "prints")))
		li.Call("appendChild", link)
		list.Call("appendChild", li)
		rows++
	}
	// A deck from before the printers sends its live print with no printer ID: its row has the
	// name of the printer from the deck, and a link to the live page.
	for _, l := range v.Live {
		if l.Result != "" || known[l.PrinterID] {
			continue
		}
		name := l.Printer
		if name == "" {
			name = "A printer"
		}
		state, sub := "on", fmt.Sprintf("Printing %s · %.0f%%", l.Name, l.Percent)
		if l.Paused {
			state, sub = "warn", "Paused · "+l.Name
		}
		li := el("li", state, "")
		link := el("a", "", "")
		if l.URL != "" {
			link.Set("href", l.URL)
		}
		link.Call("appendChild", el("span", "dot", ""))
		txt := el("span", "txt", "")
		txt.Call("appendChild", el("strong", "", name))
		txt.Call("appendChild", el("span", "sub", sub))
		link.Call("appendChild", txt)
		n := 0
		for _, p := range v.Prints {
			if p.PrinterID == "" && p.Printer == l.Printer {
				n++
			}
		}
		link.Call("appendChild", el("span", "num", count(n, "print", "prints")))
		li.Call("appendChild", link)
		list.Call("appendChild", li)
		rows++
	}
	setHidden("pRigCard", rows == 0)
}

// faceColors are the colors of the circles of the people.
var faceColors = []string{"#2f6fbd", "#2f9e5b", "#c0392b", "#8e44ad", "#d35400", "#16a085", "#b8860b", "#5d6d7e"}

// faceColor is the color of the circle of a person, from the name.
func faceColor(name string) string {
	h := uint32(2166136261)
	for _, r := range name {
		h = (h ^ uint32(r)) * 16777619
	}
	return faceColors[h%uint32(len(faceColors))]
}

// face is the circle of a person, with the first letter of the name.
func face(name, class string) js.Value {
	letter := "?"
	if name != "" {
		letter = strings.ToUpper(string([]rune(name)[:1]))
	}
	f := el("span", "face "+class, letter)
	f.Get("style").Set("background", faceColor(name))
	f.Set("title", name)
	return f
}

// drawFaces draws the circles of the people in the head of the page.
func drawFaces(v *wire.ProjectView) {
	box := byID("pFaces")
	box.Set("textContent", "")
	box.Call("appendChild", face(v.Owner, ""))
	n := 0
	for _, m := range v.Members {
		if !m.Joined {
			continue
		}
		if n == 5 {
			box.Call("appendChild", el("span", "face more", "+"))
			break
		}
		box.Call("appendChild", face(m.Name, ""))
		n++
	}
}

// drawPeople draws the card of the people: the owner, the members and the invites that wait.
// The owner taps Edit to set the GitHub usernames, remove people and invite friends.
func (a *app) drawPeople(v *wire.ProjectView) {
	edit := v.IsOwner && a.peopleEdit
	list := byID("pPeople")
	list.Set("textContent", "")
	list.Get("classList").Call("toggle", "editing", edit)
	setHidden("pPeopleEdit", !v.IsOwner)
	setText("pPeopleEdit", map[bool]string{true: "Done", false: "Edit"}[edit])
	setHidden("peopleInviteForm", !edit)
	row := func(name, role string) (js.Value, js.Value) {
		li := el("li", "", "")
		li.Call("appendChild", face(name, "sm"))
		txt := el("span", "txt", "")
		txt.Call("appendChild", el("strong", "", name))
		if role != "" {
			txt.Call("appendChild", el("span", "sub", role))
		}
		li.Call("appendChild", txt)
		list.Call("appendChild", li)
		return li, txt
	}
	remove := func(li js.Value, m wire.ProjectMember, label string) {
		rm := el("button", "link rm", label)
		rm.Set("type", "button")
		rm.Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any {
			go a.removeMember(m.ID, m.Name)
			return nil
		}))
		li.Call("appendChild", rm)
	}
	row(v.Owner, map[bool]string{true: "Owner", false: ""}[edit])
	invites := 0
	for _, m := range v.Members {
		if !m.Joined {
			invites++
			if edit {
				li, _ := row(m.Name, "Invited "+localDay(m.Invited)+" · not joined yet")
				remove(li, m, "Cancel")
			}
			continue
		}
		role := ""
		if m.GitHub != "" && !edit {
			role = "@" + m.GitHub
		}
		li, txt := row(m.Name, role)
		if edit {
			txt.Call("appendChild", a.githubInput(m, "peopleMsg"))
			remove(li, m, "Remove")
		}
	}
	if invites > 0 && !edit {
		list.Call("appendChild", el("li", "dim small", count(invites, "invite waits", "invites wait")))
	}
}

// githubInput is the field of the GitHub username of the member m: the plans of the member's
// fork need it. A change saves it. msg is the line of the messages.
func (a *app) githubInput(m wire.ProjectMember, msg string) js.Value {
	gh := el("input", "github", "")
	gh.Set("value", m.GitHub)
	gh.Set("placeholder", "GitHub username")
	gh.Set("maxLength", 39)
	gh.Set("autocomplete", "off")
	gh.Set("spellcheck", false)
	gh.Call("setAttribute", "aria-label", "The GitHub username of "+m.Name)
	gh.Set("title", "The pull requests of this GitHub user's fork send plans to this project")
	id := m.ID
	gh.Call("addEventListener", "change", js.FuncOf(func(js.Value, []js.Value) any {
		val := strings.TrimSpace(gh.Get("value").String())
		go func() {
			if err := a.api("PATCH", "/projects/"+a.view.ID+"/members/"+id, wire.MemberEdit{GitHub: val}, nil); err != nil {
				say(msg, err, "")
				return
			}
			for i := range a.view.Members {
				if a.view.Members[i].ID == id {
					a.view.Members[i].GitHub = val
				}
			}
			say(msg, nil, "Saved the GitHub username of "+m.Name+".")
		}()
		return nil
	}))
	return gh
}

// dashRow is a row of a card of the overview: a link with a title, a line under it, and a tag.
func dashRow(href, title, sub, tag, class string) js.Value {
	li := el("li", "", "")
	link := el("a", "", "")
	link.Set("href", href)
	t := el("strong", "", title+" ")
	if tag != "" {
		t.Call("appendChild", el("span", "tag "+class, tag))
	}
	link.Call("appendChild", t)
	link.Call("appendChild", el("span", "sub", sub))
	li.Call("appendChild", link)
	return li
}

// planKey is a key of the plans and the states of their builds: a change of the key draws the
// overview again.
func planKey(plans []wire.ProjectPlan) string {
	var b strings.Builder
	for _, p := range plans {
		fmt.Fprintf(&b, "%s%v;", p.ID, p.Ready)
		for _, bd := range p.Builds {
			b.WriteString(bd.ID + bd.State)
			for _, pl := range bd.Plates {
				b.WriteString(pl.State)
			}
		}
	}
	return b.String()
}
