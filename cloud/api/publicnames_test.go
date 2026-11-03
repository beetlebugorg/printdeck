package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"printdeck/internal/cloud"
)

// The owner's public page has the notes of the owner's choice, and no names of members: no
// name on the notes, the print notes or the photos.
func TestPublicPageNoNames(t *testing.T) {
	d, base := devService(t)
	base = strings.Replace(base, "127.0.0.1", "localhost", 1)
	d.API.Base, d.Grants.URL = base, base+"/upload"
	ctx := context.Background()
	d.Store.Put(ctx, PublicPage, []byte("<!doctype html><title>Project · Printdeck</title><!-- preview -->"), PutOptions{ContentType: "text/html"})
	l := connectedLink(t, d, base)
	var c cloud.Client
	var oc cloud.OwnerCodeReply
	if err := c.Do(ctx, l.Identity(), "POST", "/api/v1/owner/code", nil, &oc); err != nil {
		t.Fatal(err)
	}
	tok := register(t, base, newSoftKey(t), "owner", oc.Code, "Jeremy").Token
	sum, err := c.NewProject(ctx, l.Identity(), "Robot")
	if err != nil {
		t.Fatal(err)
	}
	id := sum.ID
	var inv cloud.InviteReply
	call(t, base, "POST", "/api/v1/projects/"+id+"/invites", tok, cloud.InviteRequest{Name: "Maxwell"}, &inv)
	max := register(t, base, newSoftKey(t), "invite", strings.TrimPrefix(inv.URL, base+"/j#"), "Maxwell")
	var pr cloud.PrintReply
	req := cloud.PrintRequest{Name: "Arm", File: "arm.gcode", Result: "complete", Ended: time.Now().UTC(), Seconds: 60, Key: "arm@1"}
	c.Do(ctx, l.Identity(), "POST", "/api/v1/projects/"+id+"/prints", req, &pr)
	c.Do(ctx, l.Identity(), "POST", "/api/v1/projects/"+id+"/prints/"+pr.Print+"/done", cloud.PrintDone{}, nil)
	notes := "The arm is too thin."
	call(t, base, "PATCH", "/api/v1/projects/"+id+"/prints/"+pr.Print, max.Token, cloud.PrintEdit{Notes: &notes}, nil)
	call(t, base, "PATCH", "/api/v1/projects/"+id+"/notes", max.Token, cloud.NotesEdit{Notes: "Paint it red."}, nil)
	var ph cloud.PhotoReply
	call(t, base, "POST", "/api/v1/projects/"+id+"/photos", max.Token, nil, &ph)
	cloud.PostFile(ctx, nil, ph.Grant, "photo", "image/jpeg", "no-store", exifJPEG(t, 64, 48, 1))
	call(t, base, "POST", "/api/v1/projects/"+id+"/photos/"+ph.Photo+"/done", max.Token, nil, nil)
	if st := call(t, base, "POST", "/api/v1/projects/"+id+"/public", tok, cloud.PublicRequest{On: true}, nil); st != 200 {
		t.Fatalf("public on: %d", st)
	}
	b, _, err := d.Store.Get(ctx, "p/robot/page.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "Maxwell") {
		t.Errorf("a name of a member on the public page: %s", b)
	}
	var v cloud.ProjectView
	json.Unmarshal(b, &v)
	if v.Notes != "Paint it red." || len(v.Prints) != 1 || v.Prints[0].Notes != notes || len(v.Photos) != 1 || v.Repo != "" {
		t.Errorf("the owner's public page: %s", b)
	}
	// With no GitHub App, a linked repository is not public: the page has no plans.
	if st := call(t, base, "PUT", "/api/v1/projects/"+id+"/repo", tok, cloud.RepoRequest{Repo: "beetlebugorg/robot"}, nil); st != 200 {
		t.Fatalf("link: %d", st)
	}
	b, _, _ = d.Store.Get(ctx, "p/robot/page.json")
	if strings.Contains(string(b), "beetlebugorg") || strings.Contains(string(b), `"plans"`) {
		t.Errorf("the content of a repository with no GitHub App: %s", b)
	}
}
