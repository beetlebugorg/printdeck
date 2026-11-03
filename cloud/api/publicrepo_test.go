package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"printdeck/internal/cloud"
)

// A public repository gives the project a public page with the content of the repository: the
// parts, the assemblies, the docs and the cover of the newest plan of the default branch. The
// prints, the photos, the notes and the names stay members-only until the owner turns the
// public page on. A private repository gives no page, or a page with no content of the
// repository.
func TestPublicRepo(t *testing.T) {
	d, base := devService(t)
	base = strings.Replace(base, "127.0.0.1", "localhost", 1)
	d.API.Base, d.Grants.URL = base, base+"/upload"
	d.API.OIDC = fakeOIDC{}
	gh := &recordDispatch{public: true}
	d.API.GitHub = gh
	ms := d.Store.(*MemStore)
	ctx := context.Background()
	d.Store.Put(ctx, PublicPage, []byte("<!doctype html><title>Project · Printdeck</title><!-- preview -->"), PutOptions{ContentType: "text/html"})
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
	id := sum.ID

	// A member with notes on the project and on a print, and a photo.
	var inv cloud.InviteReply
	if st := call(t, base, "POST", "/api/v1/projects/"+id+"/invites", tok, cloud.InviteRequest{Name: "Samantha"}, &inv); st != 200 {
		t.Fatalf("invite: %d", st)
	}
	sam := register(t, base, newSoftKey(t), "invite", strings.TrimPrefix(inv.URL, base+"/j#"), "Samantha")
	var pr cloud.PrintReply
	req := cloud.PrintRequest{Name: "Arm", File: "arm.gcode", Result: "complete", Ended: time.Now().UTC(), Seconds: 60, Key: "arm@1"}
	if err := c.Do(ctx, l.Identity(), "POST", "/api/v1/projects/"+id+"/prints", req, &pr); err != nil {
		t.Fatal(err)
	}
	if err := c.Do(ctx, l.Identity(), "POST", "/api/v1/projects/"+id+"/prints/"+pr.Print+"/done", cloud.PrintDone{}, nil); err != nil {
		t.Fatal(err)
	}
	printNotes, projectNotes := "Print the arm in PETG.", "Two arms for the pod."
	if st := call(t, base, "PATCH", "/api/v1/projects/"+id+"/prints/"+pr.Print, sam.Token, cloud.PrintEdit{Notes: &printNotes}, nil); st != 200 {
		t.Fatalf("print notes: %d", st)
	}
	if st := call(t, base, "PATCH", "/api/v1/projects/"+id+"/notes", sam.Token, cloud.NotesEdit{Notes: projectNotes}, nil); st != 200 {
		t.Fatalf("project notes: %d", st)
	}
	var ph cloud.PhotoReply
	if st := call(t, base, "POST", "/api/v1/projects/"+id+"/photos", sam.Token, nil, &ph); st != 200 {
		t.Fatalf("photo grant: %d", st)
	}
	if err := cloud.PostFile(ctx, nil, ph.Grant, "photo", "image/jpeg", "no-store", exifJPEG(t, 64, 48, 1)); err != nil {
		t.Fatal(err)
	}
	if st := call(t, base, "POST", "/api/v1/projects/"+id+"/photos/"+ph.Photo+"/done", sam.Token, nil, nil); st != 200 {
		t.Fatalf("photo done: %d", st)
	}

	page := func() (cloud.ProjectView, string, bool) {
		t.Helper()
		b, _, err := d.Store.Get(ctx, "p/bittlex/page.json")
		if errors.Is(err, ErrNotFound) {
			return cloud.ProjectView{}, "", false
		}
		var v cloud.ProjectView
		if err != nil || json.Unmarshal(b, &v) != nil {
			t.Fatalf("page.json: %v %s", err, b)
		}
		return v, string(b), true
	}
	keys := func() []string {
		t.Helper()
		k, _ := d.Store.List(ctx, "p/bittlex")
		return k
	}
	// noMembers checks that no public file has a name of a member, and that a page of the
	// repository only has no content of the members.
	noMembers := func(repoOnly bool) {
		t.Helper()
		for _, k := range keys() {
			b, _, _ := d.Store.Get(ctx, k)
			if strings.Contains(string(b), "Samantha") {
				t.Errorf("%s has the name of a member: %s", k, b)
			}
			if !repoOnly {
				continue
			}
			for _, bad := range []string{"Jeremy", printNotes, projectNotes, "Arm", ph.Photo, pr.Print} {
				if strings.Contains(string(b), bad) {
					t.Errorf("%s of a page of the repository has %q: %s", k, bad, b)
				}
			}
			if strings.Contains(k, "/prints/") || strings.Contains(k, "/photos/") {
				t.Errorf("a copy of a members-only file: %s", k)
			}
		}
	}

	// A link to a public repository makes the page, with the link and no plan yet.
	if st := call(t, base, "PUT", "/api/v1/projects/"+id+"/repo", tok, cloud.RepoRequest{Repo: "beetlebugorg/bittlex"}, nil); st != 200 {
		t.Fatalf("link: %d", st)
	}
	v, _, ok := page()
	if !ok || !v.RepoOnly || v.Repo != "beetlebugorg/bittlex" || len(v.Plans) != 0 || len(v.Prints) != 0 || len(v.Photos) != 0 {
		t.Fatalf("the page of a public repository with no plan: %v %+v", ok, v)
	}
	noMembers(true)
	var view cloud.ProjectView
	if call(t, base, "GET", "/api/v1/projects/"+id, tok, nil, &view); view.Public != "" {
		t.Errorf("the owner's public page is on with only a public repository: %q", view.Public)
	}

	sum256 := strings.Repeat("0f", 32)
	part := cloud.PlanPart{Name: "Pod door", Variant: "fine", Files: []string{"stl/pod_door.stl"}, Printer: "taz", Process: "fdm",
		Profile: "taz-workhorse-se/pla-0.20-standard@1", Material: "roll-4", MaterialName: "PLA Black", MaterialColor: "#1a1a1a",
		Description: "The door of the pod.", Group: "Pod", Count: 2, SizeX: 40, SizeY: 30, SizeZ: 5, Models: []string{"part1-1.stl"}}
	asm := cloud.PlanAssembly{Name: "The pod", Description: "The door on the Pi.", Items: []cloud.PlanAssemblyItem{
		{Name: "Door", Part: "Pod door (fine)", Model: "asm1-1.stl", Explode: []float64{0, 0, 1}},
		{Name: "Pi 5", Model: "asm1-2.stl", Color: "#22aa44"}}}
	guide := cloud.PlanDoc{Name: "Build guide", File: "docs/guide.pdf", PDF: "doc1.pdf", Thumb: "doc1.png", Size: 3, Pages: 12, SHA256: sum256}
	n := 0
	plan := func(ref string) string {
		t.Helper()
		n++
		token := "beetlebugorg/bittlex|" + ref + "|" + strings.Repeat("ab", 19) + strings.Repeat(string(rune('a'+n%6)), 2)
		var up cloud.UploadReply
		m := cloud.PlanManifest{Parts: []cloud.PlanPart{part}, Assemblies: []cloud.PlanAssembly{asm},
			Docs: []cloud.PlanDoc{guide}, Cover: "cover.png"}
		if st := call(t, base, "POST", "/api/v1/plans", token, m, &up); st != 200 {
			t.Fatalf("plan: %d", st)
		}
		for _, f := range []string{"part1-1.stl", "asm1-1.stl", "asm1-2.stl", "doc1.pdf", "doc1.png", "cover.png"} {
			if err := cloud.PostFile(ctx, nil, up.Grant, f, "text/html", "", []byte("data of "+f)); err != nil {
				t.Fatal(err)
			}
		}
		if st := call(t, base, "POST", "/api/v1/plans/"+up.ID+"/done", token, nil, nil); st != 200 {
			t.Fatalf("done: %d", st)
		}
		return up.ID
	}

	// A plan of the default branch: the parts, the assemblies, the docs and the cover, with
	// copies of their files under repo/<plan>/.
	first := plan("refs/heads/main")
	v, raw, _ := page()
	rel := "repo/" + first + "/"
	if len(v.Plans) != 1 || v.Plans[0].ID != first || len(v.Plans[0].Parts) != 1 || len(v.Plans[0].Assemblies) != 1 {
		t.Fatalf("the plan of the page: %s", raw)
	}
	got := v.Plans[0].Parts[0]
	if got.Name != "Pod door" || got.Variant != "fine" || got.Group != "Pod" || got.Description != "The door of the pod." ||
		got.MaterialName != "PLA Black" || got.MaterialColor != "#1a1a1a" || got.SizeX != 40 || got.Count != 2 ||
		got.Printer != "taz" || len(got.ModelURLs) != 1 || got.ModelURLs[0] != rel+"part1-1.stl" {
		t.Errorf("the part of the page: %+v", got)
	}
	if got.Profile != "" || got.Material != "" || got.Digest != "" {
		t.Errorf("the part of the page has the facts of the slicer: %+v", got)
	}
	a := v.Plans[0].Assemblies[0]
	if a.Name != "The pod" || len(a.Items) != 2 || a.Items[0].Explode == nil || a.Items[1].Color != "#22aa44" ||
		len(a.ModelURLs) != 2 || a.ModelURLs[1] != rel+"asm1-2.stl" {
		t.Errorf("the assembly of the page: %+v", a)
	}
	if len(v.Docs) != 1 || v.Docs[0].URL != rel+"doc1.pdf" || v.Docs[0].ThumbURL != rel+"doc1.png" ||
		!strings.HasPrefix(v.Docs[0].RepoURL, "https://github.com/beetlebugorg/bittlex/blob/") {
		t.Errorf("the docs of the page: %+v", v.Docs)
	}
	if v.Cover != rel+"cover.png" || v.Owner != "" || v.Notes != "" || v.NotesBy != "" || len(v.Members) != 0 {
		t.Errorf("the page of a repository: %s", raw)
	}
	for f, want := range map[string]string{"part1-1.stl": "model/stl", "asm1-1.stl": "model/stl", "asm1-2.stl": "model/stl",
		"doc1.pdf": "application/pdf", "doc1.png": "image/png", "cover.png": "image/png"} {
		k := "p/bittlex/" + rel + f
		_, _, err := d.Store.Get(ctx, k)
		if err != nil || ms.ContentType(k) != want || !strings.Contains(ms.CacheControl(k), "immutable") {
			t.Errorf("the copy of %s: %v %q %q", f, err, ms.ContentType(k), ms.CacheControl(k))
		}
	}
	if cc := ms.CacheControl("p/bittlex/page.json"); cc != "public, max-age=60" {
		t.Errorf("the cache of page.json: %q", cc)
	}
	html, _, _ := d.Store.Get(ctx, "p/bittlex")
	if !strings.Contains(string(html), `<meta property="og:image" content="`+base+"/p/bittlex/"+rel+`cover.png">`) ||
		!strings.Contains(string(html), `og:title" content="Bittlex"`) {
		t.Errorf("the preview tags: %s", html)
	}
	noMembers(true)

	// A preview bot gets the page at the project address.
	bot := func() int {
		t.Helper()
		r, _ := http.NewRequest("GET", base+"/projects/bittlex", nil)
		r.Header.Set("User-Agent", "Slackbot-LinkExpanding 1.0")
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if st := bot(); st != 200 {
		t.Errorf("the preview of a page of a public repository: %d", st)
	}

	// A plan of another branch does not change the page.
	plan("refs/heads/lid")
	if v, _, _ := page(); len(v.Plans) != 1 || v.Plans[0].ID != first {
		t.Errorf("a branch plan changed the page: %+v", v.Plans)
	}

	// A new plan of the default branch gets new copies, and the old copies go.
	second := plan("refs/heads/main")
	if v, _, _ := page(); len(v.Plans) != 1 || v.Plans[0].ID != second {
		t.Errorf("the page after a new plan: %+v", v.Plans)
	}
	for _, k := range keys() {
		if strings.Contains(k, first) {
			t.Errorf("a copy of the old plan stays: %s", k)
		}
	}

	// The owner turns the public page on: the page has both. The notes stay, with no names.
	var pub cloud.PublicReply
	if st := call(t, base, "POST", "/api/v1/projects/"+id+"/public", tok, cloud.PublicRequest{On: true}, &pub); st != 200 ||
		pub.URL != base+"/projects/bittlex" {
		t.Fatalf("public on: %d %+v", st, pub)
	}
	v, raw, _ = page()
	if v.RepoOnly || len(v.Prints) != 1 || len(v.Photos) != 1 || len(v.Plans) != 1 || len(v.Docs) != 1 || v.Cover == "" ||
		v.Notes != projectNotes || v.Prints[0].Notes != printNotes || v.Owner != "Jeremy" {
		t.Errorf("the owner's page with a public repository: %s", raw)
	}
	if v.NotesBy != "" || v.Prints[0].NotesBy != "" || v.Photos[0].By != "" {
		t.Errorf("names on the owner's public page: %s", raw)
	}
	noMembers(false)

	// The repository becomes private: the next plan of the default branch takes the content of
	// the repository off the page, and deletes its copies.
	gh.public = false
	plan("refs/heads/main")
	v, raw, ok = page()
	if !ok || v.RepoOnly || len(v.Plans) != 0 || len(v.Docs) != 0 || v.Cover != "" || v.Repo != "" || len(v.Prints) != 1 {
		t.Errorf("the owner's page with a private repository: %s", raw)
	}
	for _, k := range keys() {
		if strings.Contains(k, "/repo/") {
			t.Errorf("a copy of a private repository stays: %s", k)
		}
	}
	if html, _, _ := d.Store.Get(ctx, "p/bittlex"); strings.Contains(string(html), "og:image") {
		t.Errorf("the preview of a private repository has the cover: %s", html)
	}

	// The owner turns the page off. The repository is private, so the page goes.
	if st := call(t, base, "POST", "/api/v1/projects/"+id+"/public", tok, cloud.PublicRequest{On: false}, nil); st != 200 {
		t.Fatalf("public off: %d", st)
	}
	if k := keys(); len(k) != 0 {
		t.Errorf("the files of the page after public off: %v", k)
	}
	if st := bot(); st != 404 {
		t.Errorf("the preview of a private project: %d", st)
	}

	// Public again, then the owner turns the public page on and off: the page of the repository
	// stays, with no content of the members.
	gh.public = true
	plan("refs/heads/main")
	call(t, base, "POST", "/api/v1/projects/"+id+"/public", tok, cloud.PublicRequest{On: true}, nil)
	call(t, base, "POST", "/api/v1/projects/"+id+"/public", tok, cloud.PublicRequest{On: false}, nil)
	if v, raw, ok := page(); !ok || !v.RepoOnly || len(v.Plans) != 1 || len(v.Prints) != 0 {
		t.Errorf("the page of the repository after public off: %s", raw)
	}
	noMembers(true)

	// An error of GitHub leaves the visibility as it was.
	gh.repoErr = errors.New("GitHub is down")
	calls := gh.repos
	last := plan("refs/heads/main")
	if gh.repos != calls+1 {
		t.Errorf("%d calls of GitHub for one plan", gh.repos-calls)
	}
	if v, _, ok := page(); !ok || len(v.Plans) != 1 || v.Plans[0].ID != last {
		t.Errorf("the page after an error of GitHub: %v %+v", ok, v.Plans)
	}
	gh.repoErr = nil

	// The repository becomes private: the page goes, with all its copies.
	gh.public = false
	plan("refs/heads/main")
	if k := keys(); len(k) != 0 {
		t.Errorf("the files of the page of a private repository: %v", k)
	}
	p, _, _ := d.API.loadProject(ctx, id)
	if p.Public != "" || p.RepoOnly || p.RepoPublic {
		t.Errorf("the project after the repository became private: %+v", p)
	}

	// Public again, and the owner removes the link: the page goes.
	gh.public = true
	plan("refs/heads/main")
	if _, _, ok := page(); !ok {
		t.Fatal("no page for a public repository")
	}
	if st := call(t, base, "PUT", "/api/v1/projects/"+id+"/repo", tok, cloud.RepoRequest{Repo: ""}, nil); st != 200 {
		t.Fatalf("unlink: %d", st)
	}
	if k := keys(); len(k) != 0 {
		t.Errorf("the files of the page after the link went: %v", k)
	}
}
