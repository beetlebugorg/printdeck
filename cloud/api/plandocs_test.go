package api

import (
	"context"
	"strings"
	"testing"

	"printdeck/internal/cloud"
)

func TestPlanDocs(t *testing.T) {
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
	sum256 := strings.Repeat("0f", 32)
	part := cloud.PlanPart{Name: "Pod door", Files: []string{"stl/pod_door.stl"}, Printer: "taz", Process: "fdm",
		Profile: "taz-workhorse-se/pla-0.20-standard@1", Models: []string{"pod_door.stl"}}
	guide := cloud.PlanDoc{Name: "Build guide", File: "bittle pi/guide.pdf", PDF: "doc1.pdf", Thumb: "doc1.png", Size: 3, Pages: 12, SHA256: sum256}
	spec := cloud.PlanDoc{Name: "Spec", File: "spec.pdf", PDF: "doc2.pdf", Thumb: "doc2.png", Size: 3, Pages: 4, SHA256: sum256}

	// Bad docs.
	main := "beetlebugorg/bittlex|refs/heads/main|" + strings.Repeat("ab", 20)
	for _, bad := range []cloud.PlanDoc{
		{Name: "", File: "a.pdf", PDF: "doc1.pdf", Size: 3, SHA256: sum256},
		{Name: "A", File: "a.pdf", PDF: "doc1.png", Size: 3, SHA256: sum256},
		{Name: "A", File: "a.pdf", PDF: "../doc1.pdf", Size: 3, SHA256: sum256},
		{Name: "A", File: "a.pdf", PDF: "doc1.pdf", Thumb: "doc1.pdf", Size: 3, SHA256: sum256},
		{Name: "A", File: "a.pdf", PDF: "doc1.pdf", Size: MaxDocFile + 1, SHA256: sum256},
		{Name: "A", File: "a.pdf", PDF: "doc1.pdf", Size: 3, SHA256: "x"},
	} {
		m := cloud.PlanManifest{Parts: []cloud.PlanPart{part}, Docs: []cloud.PlanDoc{bad}}
		if st := call(t, base, "POST", "/api/v1/plans", main, m, nil); st != 400 {
			t.Errorf("a bad doc %+v: %d", bad, st)
		}
	}

	plan := func(token string, docs []cloud.PlanDoc, files ...string) cloud.ProjectPlan {
		var up cloud.UploadReply
		if st := call(t, base, "POST", "/api/v1/plans", token, cloud.PlanManifest{Parts: []cloud.PlanPart{part}, Docs: docs}, &up); st != 200 {
			t.Fatalf("plan: %d", st)
		}
		for _, f := range append([]string{"pod_door.stl"}, files...) {
			if err := cloud.PostFile(ctx, nil, up.Grant, f, "application/octet-stream", "", []byte("abc")); err != nil {
				t.Fatal(err)
			}
		}
		var pv cloud.ProjectPlan
		if st := call(t, base, "POST", "/api/v1/plans/"+up.ID+"/done", token, nil, &pv); st != 200 {
			t.Fatalf("done: %d", st)
		}
		return pv
	}
	// The PDF must be up before the plan is ready.
	var up cloud.UploadReply
	call(t, base, "POST", "/api/v1/plans", main, cloud.PlanManifest{Parts: []cloud.PlanPart{part}, Docs: []cloud.PlanDoc{guide}}, &up)
	cloud.PostFile(ctx, nil, up.Grant, "pod_door.stl", "model/stl", "", []byte("abc"))
	if st := call(t, base, "POST", "/api/v1/plans/"+up.ID+"/done", main, nil, nil); st != 400 {
		t.Errorf("done with no PDF: %d", st)
	}

	// The picture of the spec is not up: the spec has no picture.
	pv := plan(main, []cloud.PlanDoc{guide, spec}, "doc1.pdf", "doc1.png", "doc2.pdf")
	if len(pv.Docs) != 2 || pv.Docs[0].URL == "" || pv.Docs[0].ThumbURL == "" || pv.Docs[1].ThumbURL != "" || pv.Docs[1].Thumb != "" {
		t.Fatalf("docs %+v", pv.Docs)
	}
	if want := "https://github.com/beetlebugorg/bittlex/blob/" + strings.Repeat("ab", 20) + "/bittle%20pi/guide.pdf"; pv.Docs[0].RepoURL != want {
		t.Errorf("repo link %q", pv.Docs[0].RepoURL)
	}
	// A later plan of a pull request does not change the docs of the project.
	plan("beetlebugorg/bittlex|refs/pull/7/merge|"+strings.Repeat("cd", 20), []cloud.PlanDoc{spec}, "doc2.pdf", "doc2.png")
	var v cloud.ProjectView
	if st := call(t, base, "GET", "/api/v1/projects/"+sum.ID, tok, nil, &v); st != 200 {
		t.Fatalf("view: %d", st)
	}
	if len(v.Docs) != 2 || v.Docs[0].Name != "Build guide" || v.Docs[0].Plan != pv.ID || v.Docs[0].Pages != 12 {
		t.Fatalf("project docs %+v", v.Docs)
	}
	// A newer plan of main with no docs: the project has no docs.
	plan(main, nil)
	v = cloud.ProjectView{}
	call(t, base, "GET", "/api/v1/projects/"+sum.ID, tok, nil, &v)
	if len(v.Docs) != 0 {
		t.Fatalf("docs after a plan with none: %+v", v.Docs)
	}
}
