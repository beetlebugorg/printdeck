package api

import (
	"context"
	"testing"
	"time"

	"printdeck/internal/cloud"
)

// A file of a queue with the path of a plate names its build and its plate (from 0).
func TestQueuedPlates(t *testing.T) {
	items := []cloud.ProjectQueued{
		{Name: "Slate frame", Place: 1, File: "builds/pblfqkpbrjtnreu3-plate-1.gcode"},
		{Name: "Benchy", Place: 2, File: "benchy.gcode"},
		{Name: "Old deck", Place: 3},
	}
	got := queuedPlates(items)
	if len(got) != 1 || got[plateQueueKey("pblfqkpbrjtnreu3", 0)] == nil || got[plateQueueKey("pblfqkpbrjtnreu3", 0)].Place != 1 {
		t.Errorf("queued plates %v", got)
	}
}

// A new build replaces the waiting plates of an older build of the same pull request when it has
// all their parts. A plate with a part that the new build does not have keeps waiting.
func TestReplaceByBuild(t *testing.T) {
	_, s, _ := testServer(t)
	ctx := context.Background()
	p := &Project{ID: newID(13)}
	now := time.Now().UTC()
	old := PlanRecord{ID: newID(10), Kind: cloud.BuildTest, PR: 9, Created: now.Add(-time.Hour)}
	plan := PlanRecord{ID: newID(10), Kind: cloud.BuildTest, PR: 9, Created: now}
	for _, rec := range []PlanRecord{old, plan} {
		if err := s.putJSON(ctx, planKey(p.ID, rec.ID), rec); err != nil {
			t.Fatal(err)
		}
	}
	plate := func(state string, names ...string) plateRecord {
		pr := plateRecord{State: state}
		for _, n := range names {
			pr.Parts = append(pr.Parts, cloud.PlatePart{Name: n, Count: 1})
		}
		return pr
	}
	ob := BuildRecord{ID: newID(10), Plan: old.ID, State: cloud.BuildReady, Plates: []plateRecord{
		plate(cloud.PartWaiting, "Cable clip"), plate(cloud.PartWaiting, "Camera arm"), plate(cloud.PartApproved, "Slate base")}}
	if err := s.putJSON(ctx, buildKey(p.ID, ob.ID), ob); err != nil {
		t.Fatal(err)
	}
	nb := BuildRecord{ID: newID(10), Plan: plan.ID, Plates: []plateRecord{plate("", "Slate base"), plate("", "Cable clip")}}
	if err := s.replaceByBuild(ctx, p, &plan, &nb); err != nil {
		t.Fatal(err)
	}
	b, err := s.loadBuild(ctx, p.ID, ob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b.Plates[0].State != cloud.PartReplaced || b.Plates[1].State != cloud.PartWaiting || b.Plates[2].State != cloud.PartApproved {
		t.Errorf("states %s %s %s", b.Plates[0].State, b.Plates[1].State, b.Plates[2].State)
	}
}
