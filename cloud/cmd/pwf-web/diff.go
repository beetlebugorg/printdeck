//go:build js && wasm

package main

import (
	"path"
	"syscall/js"

	"printdeck/internal/cloud/wire"
)

// What changed in the models of a plan: each changed part against the same part in an older
// plan. The browser compares the STL files (viewer3d.js: describeDiff, showDiff).

// diffPair is a model of a part that changed, and the same model in an older plan.
type diffPair struct {
	label          string
	oldURL, newURL string
}

// basePlan returns the older plan to compare the plan at idx with: the newest older plan of
// main or of a tag, else the newest older plan. plans are the newest first.
func basePlan(plans []wire.ProjectPlan, idx int) (wire.ProjectPlan, bool) {
	for _, p := range plans[idx+1:] {
		if p.Ready && (p.Kind == wire.BuildMain || p.Kind == wire.BuildRelease) {
			return p, true
		}
	}
	for _, p := range plans[idx+1:] {
		if p.Ready {
			return p, true
		}
	}
	return wire.ProjectPlan{}, false
}

// diffPairs returns the models of the parts that changed in the plan at idx, each with the same
// file in the base plan. A file of more than one variant of a part shows once.
func diffPairs(plans []wire.ProjectPlan, idx int) ([]diffPair, wire.ProjectPlan) {
	base, ok := basePlan(plans, idx)
	if !ok {
		return nil, base
	}
	var out []diffPair
	seen := map[string]bool{}
	for _, pp := range plans[idx].Parts {
		if !pp.Changed || len(pp.ModelURLs) != len(pp.Files) {
			continue
		}
		var old *wire.ProjectPlanPart
		for i := range base.Parts {
			bp := &base.Parts[i]
			if bp.Name == pp.Name && len(bp.ModelURLs) == len(bp.Files) && (old == nil || bp.Variant == pp.Variant) {
				old = bp
			}
		}
		if old == nil {
			continue
		}
		for j, f := range pp.Files {
			for k, of := range old.Files {
				if of != f || seen[pp.Name+"|"+f] {
					continue
				}
				seen[pp.Name+"|"+f] = true
				label := pp.Name
				if len(pp.Files) > 1 {
					label += " · " + path.Base(f)
				}
				out = append(out, diffPair{label: label, oldURL: old.ModelURLs[k], newURL: pp.ModelURLs[j]})
			}
		}
	}
	return out, base
}

// describeDiff returns the text of the changes of a model.
func describeDiff(d diffPair) string {
	v := js.Global().Get("pwfView3d")
	if v.IsUndefined() {
		return ""
	}
	t, err := await(v.Call("describeDiff", d.oldURL, d.newURL))
	if err != nil {
		return "The models did not load"
	}
	return t.String()
}

// describePlans adds a line for each changed model to the rows of the newest plans.
func describePlans(plans []wire.ProjectPlan, rows []js.Value) {
	for i := range plans {
		if i >= 8 || i >= len(rows) {
			return
		}
		pairs, _ := diffPairs(plans, i)
		for _, d := range pairs {
			line := el("span", "diffline", d.label+": …")
			rows[i].Call("appendChild", line)
			line.Set("textContent", d.label+": "+describeDiff(d))
		}
	}
}

// drawPlanDiff fills the card What changed of the plan page: each changed model, its changes
// since the base plan, and a key that shows them in 3D.
func (a *app) drawPlanDiff(projectID, id string) {
	var plans []wire.ProjectPlan
	if err := a.api("GET", "/projects/"+projectID+"/plans", nil, &plans); err != nil {
		return
	}
	idx := -1
	for i, p := range plans {
		if p.ID == id {
			idx = i
		}
	}
	if idx < 0 || a.plan == nil || a.plan.ID != id {
		return
	}
	pairs, base := diffPairs(plans, idx)
	setHidden("plDiffCard", len(pairs) == 0)
	if len(pairs) == 0 {
		return
	}
	setText("plDiffBase", "Since "+planName(base)+" · "+shortSHA(base.Commit)+". Green is new material, red is material that went.")
	list := byID("plDiff")
	list.Set("textContent", "")
	for _, d := range pairs {
		li := el("li", "", "")
		txt := el("div", "txt", "")
		txt.Call("appendChild", el("strong", "", d.label))
		sum := el("span", "sub", "Comparing…")
		txt.Call("appendChild", sum)
		li.Call("appendChild", txt)
		see := el("button", "", "See in 3D")
		see.Set("type", "button")
		pair := d
		see.Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any {
			setText("plViewTitle", pair.label+" · what changed")
			setText("plViewFacts", "")
			box := byID("plModel")
			box.Call("scrollIntoView", map[string]any{"behavior": "smooth", "block": "center"})
			js.Global().Get("pwfView3d").Call("showDiff", box, pair.oldURL, pair.newURL)
			return nil
		}))
		li.Call("appendChild", see)
		list.Call("appendChild", li)
		go func() { sum.Set("textContent", describeDiff(pair)) }()
	}
}
