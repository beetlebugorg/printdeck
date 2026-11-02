package api

import (
	"context"
	"strings"
	"testing"

	"printdeck/internal/cloud"
)

// A plan with assemblies: the service checks the items, waits for their models, and gives the
// project view the address of each model.
func TestPlanAssemblies(t *testing.T) {
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
	part := cloud.PlanPart{Name: "Pod door", Variant: "fine", Files: []string{"stl/pod_door.stl"}, Printer: "taz", Process: "fdm",
		Profile: "taz-workhorse-se/pla-0.20-standard@1", Models: []string{"part1-1.stl"},
		Description: "  The door of the pod.  ", Group: "The pod"}
	door := cloud.PlanAssemblyItem{Part: "pod door (FINE)", Model: "asm1-1.stl", Explode: []float64{0, -1, 0}}
	pi := cloud.PlanAssemblyItem{Name: "Raspberry Pi 5", Model: "asm1-2.stl", Color: "#1b7f3a", Explode: []float64{0, 0, 0}}
	main := "beetlebugorg/bittlex|refs/heads/main|" + strings.Repeat("ab", 20)

	for _, bad := range [][]cloud.PlanAssemblyItem{
		{{Part: "Pod lid", Model: "asm1-1.stl"}},                   // no such part
		{{Model: "asm1-1.stl"}},                                    // no name, no part
		{{Name: "A", Model: "../asm1-1.stl"}},                      // a bad file name
		{{Name: "A", Model: "part1-1.stl"}},                        // the model of a part
		{{Name: "A", Model: "a.stl"}, {Name: "B", Model: "a.stl"}}, // the same model two times
	} {
		m := cloud.PlanManifest{Parts: []cloud.PlanPart{part}, Assemblies: []cloud.PlanAssembly{{Name: "Pod", Items: bad}}}
		if st := call(t, base, "POST", "/api/v1/plans", main, m, nil); st != 400 {
			t.Errorf("a bad assembly %+v: %d", bad, st)
		}
	}

	m := cloud.PlanManifest{Parts: []cloud.PlanPart{part},
		Assemblies: []cloud.PlanAssembly{{Name: "The pod", Description: "The door on the Pi.", Items: []cloud.PlanAssemblyItem{door, pi}}}}
	var up cloud.UploadReply
	if st := call(t, base, "POST", "/api/v1/plans", main, m, &up); st != 200 {
		t.Fatalf("plan: %d", st)
	}
	for _, f := range []string{"part1-1.stl", "asm1-1.stl"} {
		if err := cloud.PostFile(ctx, nil, up.Grant, f, "model/stl", "", []byte("abc")); err != nil {
			t.Fatal(err)
		}
	}
	if st := call(t, base, "POST", "/api/v1/plans/"+up.ID+"/done", main, nil, nil); st != 400 {
		t.Errorf("done with a model of the assembly not up: %d", st)
	}
	cloud.PostFile(ctx, nil, up.Grant, "asm1-2.stl", "model/stl", "", []byte("abc"))
	if st := call(t, base, "POST", "/api/v1/plans/"+up.ID+"/done", main, nil, nil); st != 200 {
		t.Fatalf("done: %d", st)
	}
	var v cloud.ProjectView
	if st := call(t, base, "GET", "/api/v1/projects/"+sum.ID, tok, nil, &v); st != 200 || len(v.Plans) == 0 {
		t.Fatalf("view: %d %+v", st, v.Plans)
	}
	pl := v.Plans[0]
	if pp := pl.Parts[0]; pp.Description != "The door of the pod." || pp.Group != "The pod" {
		t.Errorf("the text of the part: %+v", pp.PlanPart)
	}
	if len(pl.Assemblies) != 1 || len(pl.Assemblies[0].ModelURLs) != 2 || pl.Assemblies[0].ModelURLs[1] == "" {
		t.Fatalf("assemblies %+v", pl.Assemblies)
	}
	a := pl.Assemblies[0]
	if a.Items[0].Name != "Pod door (fine)" || a.Items[0].Part != "Pod door (fine)" || a.Items[1].Color != "#1b7f3a" || a.Items[1].Explode != nil || len(a.Items[0].Explode) != 3 {
		t.Errorf("items %+v", a.Items)
	}
}
