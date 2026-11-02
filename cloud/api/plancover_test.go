package api

import (
	"context"
	"strings"
	"testing"

	"printdeck/internal/cloud"
)

// The cover of a plan of the default branch is the picture of the project in the list. A plan of
// another branch does not change it, and a plan of the default branch with no cover removes it.
func TestPlanCover(t *testing.T) {
	d, base := devService(t)
	base = strings.Replace(base, "127.0.0.1", "localhost", 1)
	d.API.Base, d.Grants.URL = base, base+"/upload"
	d.API.OIDC = fakeOIDC{}
	ctx := context.Background()
	l := connectedLink(t, d, base)
	var c cloud.Client
	var oc cloud.OwnerCodeReply
	if err := c.Do(ctx, l.Identity(), "POST", "/api/v1/owner/code", nil, &oc); err != nil {
		t.Fatal(err)
	}
	tok := register(t, base, newSoftKey(t), "owner", oc.Code, "Jeremy").Token
	sum, err := c.NewProject(ctx, l.Identity(), "Bittlex")
	if err != nil {
		t.Fatal(err)
	}
	if st := call(t, base, "PUT", "/api/v1/projects/"+sum.ID+"/repo", tok, cloud.RepoRequest{Repo: "beetlebugorg/bittlex"}, nil); st != 200 {
		t.Fatalf("link: %d", st)
	}
	part := cloud.PlanPart{Name: "Pod door", Files: []string{"stl/pod_door.stl"}, Printer: "taz", Process: "fdm",
		Profile: "taz-workhorse-se/pla-0.20-standard@1", Models: []string{"part1-1.stl"}}
	main := "beetlebugorg/bittlex|refs/heads/main|" + strings.Repeat("ab", 20)
	branch := "beetlebugorg/bittlex|refs/heads/lid|" + strings.Repeat("cd", 20)

	if st := call(t, base, "POST", "/api/v1/plans", main, cloud.PlanManifest{Parts: []cloud.PlanPart{part}, Cover: "../x.png"}, nil); st != 400 {
		t.Errorf("a bad cover name: %d", st)
	}
	plan := func(token, cover string, upload bool) {
		t.Helper()
		var up cloud.UploadReply
		if st := call(t, base, "POST", "/api/v1/plans", token, cloud.PlanManifest{Parts: []cloud.PlanPart{part}, Cover: cover}, &up); st != 200 {
			t.Fatalf("plan: %d", st)
		}
		cloud.PostFile(ctx, nil, up.Grant, "part1-1.stl", "model/stl", "", []byte("abc"))
		if upload {
			cloud.PostFile(ctx, nil, up.Grant, cover, "image/png", "", []byte("png"))
		}
		if st := call(t, base, "POST", "/api/v1/plans/"+up.ID+"/done", token, nil, nil); st != 200 {
			t.Fatalf("done: %d", st)
		}
	}
	cover := func() string {
		t.Helper()
		var list []cloud.ProjectSummary
		if st := call(t, base, "GET", "/api/v1/projects", tok, nil, &list); st != 200 || len(list) != 1 {
			t.Fatalf("list: %d %+v", st, list)
		}
		return list[0].Cover
	}
	if got := cover(); got != "" {
		t.Errorf("a cover before a plan: %q", got)
	}
	plan(main, "cover.png", false) // the picture is not up: no cover
	if got := cover(); got != "" {
		t.Errorf("a cover that is not up: %q", got)
	}
	plan(main, "cover.png", true)
	first := cover()
	if !strings.Contains(first, "/plans/") || !strings.Contains(first, "cover.png") {
		t.Fatalf("the cover of the main plan: %q", first)
	}
	plan(branch, "cover.jpg", true)
	if got := cover(); !strings.Contains(got, "cover.png") {
		t.Errorf("a branch plan changed the cover: %q", got)
	}
	plan(main, "", false)
	if got := cover(); got != "" {
		t.Errorf("a main plan with no cover kept the cover: %q", got)
	}
}
