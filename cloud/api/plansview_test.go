package api

import (
	"context"
	"testing"
	"time"

	"printdeck/internal/cloud"
)

// The Builds tab lists the plans of pull requests and releases. Many merges to main do not push
// them out of the view, and the count is the count of those plans.
func TestPlansViewListed(t *testing.T) {
	_, s, _ := testServer(t)
	ctx := context.Background()
	p := &Project{ID: newID(13)}
	now := time.Now().UTC()
	put := func(i int, kind string) {
		rec := PlanRecord{ID: newID(10), Kind: kind, Created: now.Add(-time.Duration(i) * time.Minute)}
		if kind == cloud.BuildTest {
			rec.PR = i
		}
		if err := s.putJSON(ctx, planKey(p.ID, rec.ID), rec); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 12 {
		put(i, cloud.BuildMain) // the 12 newest plans: merges to main
	}
	put(12, cloud.BuildTest)
	put(13, cloud.BuildBranch)
	put(14, cloud.BuildRelease)
	put(15, cloud.BuildTest)
	put(16, cloud.BuildTest)
	plans, total, _, _ := s.plansView(ctx, p, PlansShown)
	if total != 4 {
		t.Errorf("total %d, not 4", total)
	}
	listed := 0
	for _, pv := range plans {
		if pv.Kind == cloud.BuildTest || pv.Kind == cloud.BuildRelease {
			listed++
		}
	}
	if len(plans) != PlansShown+listedShown || listed != listedShown || plans[0].Kind != cloud.BuildMain {
		t.Errorf("%d plans, %d listed", len(plans), listed)
	}
}

// A build of a plan of main that is older than the PlansShown newest plans is in the view, with
// its plan.
func TestPlansViewOldBuild(t *testing.T) {
	_, s, _ := testServer(t)
	ctx := context.Background()
	p := &Project{ID: newID(13)}
	now := time.Now().UTC()
	var old string
	for i := range PlansShown + 5 {
		rec := PlanRecord{ID: newID(10), Kind: cloud.BuildMain, Created: now.Add(-time.Duration(i) * time.Hour)}
		if err := s.putJSON(ctx, planKey(p.ID, rec.ID), rec); err != nil {
			t.Fatal(err)
		}
		old = rec.ID // the oldest plan
	}
	b := BuildRecord{ID: newID(10), Plan: old, Created: now, State: cloud.BuildReady}
	if err := s.putJSON(ctx, buildKey(p.ID, b.ID), b); err != nil {
		t.Fatal(err)
	}
	plans, _, _, _ := s.plansView(ctx, p, PlansShown)
	if len(plans) != PlansShown+1 {
		t.Fatalf("%d plans, not %d", len(plans), PlansShown+1)
	}
	last := plans[len(plans)-1]
	if last.ID != old || len(last.Builds) != 1 || last.Builds[0].ID != b.ID {
		t.Errorf("the old plan %s: %+v", old, last)
	}
}

// A build that slices for more than BuildStale shows as failed. The record stays.
func TestStaleBuild(t *testing.T) {
	_, s, _ := testServer(t)
	ctx := context.Background()
	p := &Project{ID: newID(13)}
	for _, c := range []struct {
		age   time.Duration
		state string
	}{{time.Minute, cloud.BuildSlicing}, {BuildStale + time.Minute, cloud.BuildFailed}} {
		b := &BuildRecord{ID: newID(10), Created: time.Now().UTC().Add(-c.age), State: cloud.BuildSlicing, Progress: "Slicing plate 1"}
		got := s.projectBuild(ctx, p, b)
		if got.State != c.state || (c.state == cloud.BuildFailed) != (got.Error != "") {
			t.Errorf("age %v: state %q, error %q", c.age, got.State, got.Error)
		}
		if b.State != cloud.BuildSlicing {
			t.Errorf("the record changed: %q", b.State)
		}
	}
}

// The Builds tab has the plans of pull requests and releases, and a plan of main with a build.
func TestBuildsTabPlans(t *testing.T) {
	all := []cloud.ProjectPlan{
		{ID: "a", Kind: cloud.BuildMain},
		{ID: "b", Kind: cloud.BuildMain, Builds: []cloud.ProjectBuild{{ID: "x"}}},
		{ID: "c", Kind: cloud.BuildTest},
		{ID: "d", Kind: cloud.BuildBranch},
		{ID: "e", Kind: cloud.BuildRelease},
	}
	var ids string
	for _, p := range buildsTabPlans(all) {
		ids += p.ID
	}
	if ids != "bce" {
		t.Errorf("plans %q, not bce", ids)
	}
}
