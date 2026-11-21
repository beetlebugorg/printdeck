package api

import (
	"context"
	"testing"
	"time"

	"printdeck/internal/cloud"
)

// The public page of a repository has its newest ready builds, with a copy of the G-code of each
// FDM plate, and no names, reasons or states. A build of a fork and a build that slices stay out.
func TestPublishBuilds(t *testing.T) {
	_, s, _ := testServer(t)
	ctx := context.Background()
	p := &Project{ID: newID(13), Repo: "beetlebugorg/mini"}
	now := time.Now().UTC()
	plan := &PlanRecord{ID: newID(10), Repo: "beetlebugorg/mini", Kind: cloud.BuildTest, PR: 6, Created: now}
	fork := &PlanRecord{ID: newID(10), Repo: "sam/mini", Fork: "sam/mini", Kind: cloud.BuildTest, PR: 7, Created: now}
	put := func(b BuildRecord) {
		if err := s.putJSON(ctx, buildKey(p.ID, b.ID), b); err != nil {
			t.Fatal(err)
		}
	}
	plate := func(file, process string) plateRecord {
		return plateRecord{BuildPlate: cloud.BuildPlate{Printer: "taz", Process: process, File: file, Seconds: 60,
			Settings: map[string]string{"infill": "20 %"}}, State: cloud.PartRejected, By: "Sam", Reason: "Too thin", Sent: "pr1"}
	}
	ready := BuildRecord{ID: newID(10), Plan: plan.ID, State: cloud.BuildReady, Created: now,
		Plates: []plateRecord{plate("plate-1.gcode", "fdm"), plate("plate-2.cbddlp", "resin")}}
	put(ready)
	put(BuildRecord{ID: newID(10), Plan: plan.ID, State: cloud.BuildSlicing, Created: now.Add(time.Minute)})
	put(BuildRecord{ID: newID(10), Plan: fork.ID, State: cloud.BuildReady, Created: now.Add(2 * time.Minute)})
	view := &cloud.ProjectView{}
	want, types := map[string]string{}, map[string]string{}
	if err := s.publishBuilds(ctx, p, "p/mini/", view, []*PlanRecord{plan, fork}, want, types); err != nil {
		t.Fatal(err)
	}
	if len(view.Builds) != 1 || view.Builds[0].ID != ready.ID || view.Builds[0].PlanName != "PR #6" {
		t.Fatalf("builds %+v", view.Builds)
	}
	pl := view.Builds[0].Plates
	if len(pl) != 2 || pl[0].FileURL != "builds/"+ready.ID+"/plate-1.gcode" || pl[1].FileURL != "" {
		t.Errorf("plates %+v", pl)
	}
	if pl[0].State != "" || pl[0].By != "" || pl[0].Reason != "" || pl[0].SentTo != "" || pl[0].Settings["infill"] != "20 %" {
		t.Errorf("plate 1 %+v", pl[0])
	}
	if src := want["p/mini/builds/"+ready.ID+"/plate-1.gcode"]; src != buildDir(p.ID, ready.ID)+"plate-1.gcode" || len(want) != 1 {
		t.Errorf("copies %v", want)
	}
}
