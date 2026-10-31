//go:build js && wasm

package main

import (
	"fmt"
	"sort"
	"strings"
	"syscall/js"

	"printdeck/internal/cloud/wire"
)

// The dashboard of the project page: the links to the parts of the page, what waits for the
// owner, the pull requests, and the folds of the long lists.

// drawDashboard draws the cards of the dashboard from the view of the project.
func (a *app) drawDashboard(v *wire.ProjectView) {
	setText("pPrintsCount", fmt.Sprintf("%d", len(v.Prints)))
	setText("pPhotosCount", fmt.Sprintf("%d", len(v.Photos)))
	setText("lkPrints", fmt.Sprintf("Prints (%d)", len(v.Prints)))
	setText("lkPhotos", fmt.Sprintf("Photos (%d)", len(v.Photos)))
	fold("prints", "pPrintsMore", 6, a.foldOpen["prints"], "prints")
	fold("photos", "pPhotosMore", 8, a.foldOpen["photos"], "photos")
	a.drawPulls(v)
	a.drawWaiting(v)
}

// fold shows the first n items of the list box, and a key that shows all of them.
func fold(box, key string, n int, open bool, what string) {
	items := byID(box).Get("children")
	total := items.Get("length").Int()
	for i := 0; i < total; i++ {
		items.Index(i).Get("classList").Call("toggle", "folded", !open && i >= n)
	}
	setHidden(key, total <= n)
	if open {
		setText(key, "Show fewer "+what)
	} else {
		setText(key, fmt.Sprintf("Show all %d %s", total, what))
	}
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

// drawPulls draws the card of the pull requests: each with its newest plan, its author, the
// parts that changed, and its newest build.
func (a *app) drawPulls(v *wire.ProjectView) {
	pulls := pullPlans(v)
	setHidden("pPullsCard", len(pulls) == 0)
	setHidden("lkPulls", len(pulls) == 0)
	list := byID("pPulls")
	list.Set("textContent", "")
	for _, p := range pulls {
		changed := 0
		for _, pp := range p.Parts {
			if pp.Changed {
				changed++
			}
		}
		title := fmt.Sprintf("PR #%d", p.PR)
		if p.Author != "" {
			title += " · @" + p.Author
		}
		sub := fmt.Sprintf("%s · %s changed · %s", shortSHA(p.Commit), count(changed, "part", "parts"), localWhen(p.Created))
		tag, class := "No build yet", ""
		if len(p.Builds) > 0 {
			tag, class = buildSummary(p.Builds[0])
		}
		list.Call("appendChild", dashRow(planPath(v.Slug, p.ID), title, sub, tag, class))
	}
}

// drawWaiting draws the card of what waits for the owner: the plates to approve, and the pull
// requests with a plan and no build.
func (a *app) drawWaiting(v *wire.ProjectView) {
	list := byID("pWait")
	list.Set("textContent", "")
	n := 0
	if v.IsOwner {
		for _, p := range v.Plans {
			for _, b := range p.Builds {
				waiting := 0
				var parts []string
				for _, pl := range b.Plates {
					if pl.State == wire.PartWaiting {
						waiting++
						for _, pp := range pl.Parts {
							parts = append(parts, partLabel(pp.Name, pp.Variant))
						}
					}
				}
				if waiting == 0 {
					continue
				}
				title := fmt.Sprintf("Approve %s · %s", count(waiting, "plate", "plates"), planName(p))
				list.Call("appendChild", dashRow(buildPath(v.Slug, b.ID), title, strings.Join(parts, ", "), "Waiting", wire.PartWaiting))
				n++
			}
		}
		for _, p := range pullPlans(v) {
			if len(p.Builds) > 0 || !p.Ready {
				continue
			}
			title := fmt.Sprintf("Choose the plates · PR #%d", p.PR)
			sub := "A new plan"
			if p.Author != "" {
				sub += " of @" + p.Author
			}
			list.Call("appendChild", dashRow(planPath(v.Slug, p.ID), title, sub+" · "+localWhen(p.Created), "Plan", "changed"))
			n++
		}
	}
	setHidden("pWaitCard", n == 0)
}

// dashRow is a row of a card of the dashboard: a link with a title, a line under it, and a tag.
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
