package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"printdeck/internal/cloud"
)

// A plan keeps only the files that it names. A plan that is not ready after PlanStale loses its
// files at the next plan. A project takes at most PlansPerDay plans in a day.
func TestPlanLimits(t *testing.T) {
	d, base := devService(t)
	base = strings.Replace(base, "127.0.0.1", "localhost", 1)
	d.API.Base, d.Grants.URL = base, base+"/upload"
	d.API.OIDC = fakeOIDC{}
	ctx := context.Background()
	l := connectedLink(t, d, base)
	var c cloud.Client
	var oc cloud.OwnerCodeReply
	c.Do(ctx, l.Identity(), "POST", "/api/v1/owner/code", nil, &oc)
	tok := register(t, base, newSoftKey(t), "owner", oc.Code, "Jeremy").Token
	sum, _ := c.NewProject(ctx, l.Identity(), "Bittlex")
	call(t, base, "PUT", "/api/v1/projects/"+sum.ID+"/repo", tok, cloud.RepoRequest{Repo: "beetlebugorg/bittlex"}, nil)
	now := time.Now().UTC()
	d.API.Now = func() time.Time { return now }
	token := "beetlebugorg/bittlex|refs/heads/main|" + strings.Repeat("ab", 20)
	m := cloud.PlanManifest{Parts: []cloud.PlanPart{{Name: "A", Files: []string{"a.stl"}, Printer: "taz", Process: "fdm",
		Profile: "p/f@1", Models: []string{"part1-1.stl"}}}}

	// An extra file goes when the plan is done.
	var up cloud.UploadReply
	if st := call(t, base, "POST", "/api/v1/plans", token, m, &up); st != 200 {
		t.Fatalf("plan: %d", st)
	}
	dir := planDir(sum.ID, up.ID)
	for _, f := range []string{"part1-1.stl", "evil.html"} {
		if err := cloud.PostFile(ctx, nil, up.Grant, f, "model/stl", "", []byte("abc")); err != nil {
			t.Fatal(err)
		}
	}
	if st := call(t, base, "POST", "/api/v1/plans/"+up.ID+"/done", token, nil, nil); st != 200 {
		t.Fatalf("done: %d", st)
	}
	if keys, _ := d.Store.List(ctx, dir); len(keys) != 1 || !strings.HasSuffix(keys[0], "part1-1.stl") {
		t.Errorf("the files of the plan after done: %v", keys)
	}

	// A plan that is never done loses its files at a plan after PlanStale.
	var stale cloud.UploadReply
	call(t, base, "POST", "/api/v1/plans", token, m, &stale)
	cloud.PostFile(ctx, nil, stale.Grant, "big.bin", "application/octet-stream", "", []byte("abc"))
	now = now.Add(PlanStale + time.Minute)
	call(t, base, "POST", "/api/v1/plans", token, m, nil)
	if keys, _ := d.Store.List(ctx, planDir(sum.ID, stale.ID)); len(keys) != 0 {
		t.Errorf("the files of a stale plan: %v", keys)
	}
	if keys, _ := d.Store.List(ctx, dir); len(keys) != 1 {
		t.Errorf("a sweep took the files of a ready plan: %v", keys)
	}

	// The limit of the day.
	day, _ := json.Marshal(planDay{Repo: PlansPerDay})
	d.Store.Put(ctx, planDayKey(sum.ID, now), day, PutOptions{})
	if st := call(t, base, "POST", "/api/v1/plans", token, m, nil); st != 429 {
		t.Errorf("a plan over the limit of the day: %d", st)
	}
	now = now.Add(24 * time.Hour)
	if st := call(t, base, "POST", "/api/v1/plans", token, m, nil); st != 200 {
		t.Errorf("a plan on the next day: %d", st)
	}
}
