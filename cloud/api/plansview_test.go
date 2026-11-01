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
	plans, total, _ := s.plansView(ctx, p, PlansShown)
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
