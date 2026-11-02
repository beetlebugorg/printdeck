package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"printdeck/internal/cloud"
)

// A member deletes their account, and the owner deletes the data of a second member: the
// passkeys, the photos, their public copies and the GitHub username go, the names on the notes
// become FormerMember, and the project still loads. Then the owner deletes the project, and no
// file of it stays.
func TestForgetMember(t *testing.T) {
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
	var sum cloud.ProjectSummary
	if st := call(t, base, "POST", "/api/v1/projects", tok, cloud.ProjectRequest{Name: "Robot"}, &sum); st != 200 {
		t.Fatalf("new project: %d", st)
	}
	id := sum.ID
	join := func(name string) (*softKey, cloud.SignInReply, string) {
		var inv cloud.InviteReply
		if st := call(t, base, "POST", "/api/v1/projects/"+id+"/invites", tok, cloud.InviteRequest{Name: name}, &inv); st != 200 {
			t.Fatalf("invite: %d", st)
		}
		k := newSoftKey(t)
		in := register(t, base, k, "invite", strings.TrimPrefix(inv.URL, base+"/j#"), name)
		return k, in, inv.Member
	}
	samKey, sam, samID := join("Samantha")
	_, max, maxID := join("Maxwell")
	if st := call(t, base, "PATCH", "/api/v1/projects/"+id+"/members/"+samID, tok, cloud.MemberEdit{GitHub: new("samgh")}, nil); st != 200 {
		t.Fatalf("GitHub username: %d", st)
	}

	// A print of the deck, with notes of Samantha on it and on the project.
	var pr cloud.PrintReply
	req := cloud.PrintRequest{Name: "Arm", File: "arm.gcode", Result: "complete", Ended: time.Now().UTC(), Seconds: 60, Key: "arm@1"}
	if err := c.Do(ctx, l.Identity(), "POST", "/api/v1/projects/"+id+"/prints", req, &pr); err != nil {
		t.Fatal(err)
	}
	if err := c.Do(ctx, l.Identity(), "POST", "/api/v1/projects/"+id+"/prints/"+pr.Print+"/done", cloud.PrintDone{}, nil); err != nil {
		t.Fatal(err)
	}
	notes := "Print the arm in PETG."
	if st := call(t, base, "PATCH", "/api/v1/projects/"+id+"/prints/"+pr.Print, sam.Token, cloud.PrintEdit{Notes: &notes}, nil); st != 200 {
		t.Fatalf("print notes: %d", st)
	}
	if st := call(t, base, "PATCH", "/api/v1/projects/"+id+"/notes", sam.Token, cloud.NotesEdit{Notes: "Two arms."}, nil); st != 200 {
		t.Fatalf("project notes: %d", st)
	}
	photo := func(tok string) string {
		var ph cloud.PhotoReply
		if st := call(t, base, "POST", "/api/v1/projects/"+id+"/photos", tok, nil, &ph); st != 200 {
			t.Fatalf("photo grant: %d", st)
		}
		if err := cloud.PostFile(ctx, nil, ph.Grant, "photo", "image/jpeg", "no-store", exifJPEG(t, 64, 48, 1)); err != nil {
			t.Fatal(err)
		}
		if st := call(t, base, "POST", "/api/v1/projects/"+id+"/photos/"+ph.Photo+"/done", tok, map[string]string{"print": pr.Print}, nil); st != 200 {
			t.Fatalf("photo done: %d", st)
		}
		return ph.Photo
	}
	samPhoto, maxPhoto := photo(sam.Token), photo(max.Token)
	if st := call(t, base, "POST", "/api/v1/projects/"+id+"/public", tok, cloud.PublicRequest{On: true}, nil); st != 200 {
		t.Fatalf("public on: %d", st)
	}
	// The public copy of the photo in a comment on a pull request, a fork plan of Samantha, and a
	// removal from a queue that waits for the deck.
	gh := "gh/0123456789abcdef0123456789abcdef.jpg"
	d.Store.Put(ctx, gh, []byte("jpeg"), PutOptions{})
	d.API.putJSON(ctx, prKey(id, 7), prRecord{Comment: 1, Photos: map[string]string{samPhoto: base + "/" + gh}})
	d.API.putJSON(ctx, planKey(id, "plana2b3"), PlanRecord{ID: "plana2b3", Fork: "samgh/robot", Author: "samgh", Created: time.Now()})
	d.API.putJSON(ctx, printerKey("pr1"), cloud.Printer{ID: "pr1", Name: "TAZ"})
	rmKey := removeDir("pr1") + "abcdef01.json"
	d.API.putJSON(ctx, rmKey, cloud.QueueRemove{Item: "abcdef01", Project: id, Name: "arm.gcode", By: "Samantha", At: time.Now()})

	// Samantha deletes her account.
	if st := call(t, base, "DELETE", "/api/v1/me", sam.Token, nil, nil); st != 204 {
		t.Fatalf("delete me: %d", st)
	}
	if st := call(t, base, "GET", "/api/v1/projects/"+id, sam.Token, nil, nil); st != 401 {
		t.Errorf("the session after the delete: %d", st)
	}
	if st, _ := login(t, base, samKey); st != 401 {
		t.Errorf("the passkey after the delete: %d", st)
	}
	var view cloud.ProjectView
	if st := call(t, base, "GET", "/api/v1/projects/"+id, tok, nil, &view); st != 200 {
		t.Fatalf("the project after the delete: %d", st)
	}
	if len(view.Members) != 1 || view.Members[0].ID != maxID || len(view.Prints) != 1 || len(view.Photos) != 1 || view.Photos[0].ID != maxPhoto {
		t.Errorf("the project after the delete: %+v", view)
	}
	if view.NotesBy != FormerMember || view.Notes != "Two arms." || view.Prints[0].NotesBy != FormerMember || view.Prints[0].Notes != notes {
		t.Errorf("the notes after the delete: %q %q, %q %q", view.Notes, view.NotesBy, view.Prints[0].Notes, view.Prints[0].NotesBy)
	}
	keys, _ := d.Store.List(ctx, "")
	for _, k := range keys {
		if strings.Contains(k, samPhoto) || k == gh || k == memberKeyFile(samKey.id) {
			t.Errorf("a file of Samantha stays: %s", k)
		}
		b, _, _ := d.Store.Get(ctx, k)
		if strings.Contains(string(b), "Samantha") || strings.Contains(string(b), "samgh\"") {
			t.Errorf("%s has Samantha: %s", k, b)
		}
	}
	if b, _, _ := d.Store.Get(ctx, "p/robot/page.json"); !strings.Contains(string(b), maxPhoto) {
		t.Errorf("the public page lost the photo of Maxwell: %s", b)
	}

	// The owner deletes the data of Maxwell.
	view = cloud.ProjectView{}
	if st := call(t, base, "DELETE", "/api/v1/projects/"+id+"/members/"+maxID+"?data=delete", tok, nil, &view); st != 200 {
		t.Fatalf("delete the data of a member: %d", st)
	}
	if len(view.Members) != 0 || len(view.Photos) != 0 || len(view.Prints) != 1 {
		t.Errorf("the project after the second delete: %+v", view)
	}
	if st := call(t, base, "GET", "/api/v1/projects/"+id, max.Token, nil, nil); st != 401 {
		t.Errorf("the session of Maxwell: %d", st)
	}
	if keys, _ := d.Store.List(ctx, "members/"); len(keys) != 0 {
		t.Errorf("passkeys of members stay: %v", keys)
	}
	if keys, _ := d.Store.List(ctx, "p/robot/photos/"); len(keys) != 0 {
		t.Errorf("public copies of the photos stay: %v", keys)
	}
	if st := call(t, base, "DELETE", "/api/v1/me", tok, nil, nil); st != 400 {
		t.Errorf("the owner deletes the owner: %d", st)
	}

	// The owner deletes the project: the pictures of the comments and the removals go too.
	gh2 := "gh/fedcba9876543210fedcba9876543210.png"
	d.Store.Put(ctx, gh2, []byte("png"), PutOptions{})
	d.API.putJSON(ctx, buildKey(id, "b1"), BuildRecord{ID: "b1", Plates: []plateRecord{{Pic: base + "/" + gh2}}})
	d.API.putJSON(ctx, rmKey, cloud.QueueRemove{Item: "abcdef01", Project: id, By: FormerMember, At: time.Now()})
	if st := call(t, base, "DELETE", "/api/v1/projects/"+id, tok, nil, nil); st != 204 {
		t.Fatalf("delete the project: %d", st)
	}
	for _, prefix := range []string{"x/", "p/", "gh/", "invites/", removeDir("pr1")} {
		if keys, _ := d.Store.List(ctx, prefix); len(keys) != 0 {
			t.Errorf("files after the delete of the project: %v", keys)
		}
	}
}

// The owner lists the passkeys and removes one: its sessions end. The last passkey stays.
func TestOwnerPasskeys(t *testing.T) {
	d, base := devService(t)
	base = strings.Replace(base, "127.0.0.1", "localhost", 1)
	d.API.Base, d.Grants.URL = base, base+"/upload"
	ctx := context.Background()
	l := connectedLink(t, d, base)
	var c cloud.Client
	code := func() string {
		var oc cloud.OwnerCodeReply
		if err := c.Do(ctx, l.Identity(), "POST", "/api/v1/owner/code", nil, &oc); err != nil {
			t.Fatal(err)
		}
		return oc.Code
	}
	phone := register(t, base, newSoftKey(t), "owner", code(), "Jeremy").Token
	laptop := register(t, base, newSoftKey(t), "owner", code(), "").Token
	var list []cloud.PasskeyInfo
	if st := call(t, base, "GET", "/api/v1/me/passkeys", phone, nil, &list); st != 200 || len(list) != 2 || !list[0].This || list[1].This {
		t.Fatalf("passkeys: %d %+v", st, list)
	}
	if st := call(t, base, "DELETE", "/api/v1/me/passkeys/"+list[1].ID, phone, nil, nil); st != 204 {
		t.Fatalf("remove a passkey: %d", st)
	}
	if st := call(t, base, "GET", "/api/v1/projects", laptop, nil, nil); st != 401 {
		t.Errorf("the session of a removed passkey: %d", st)
	}
	if st := call(t, base, "DELETE", "/api/v1/me/passkeys/"+list[0].ID, phone, nil, nil); st != 409 {
		t.Errorf("remove the last passkey: %d", st)
	}
	if st := call(t, base, "GET", "/api/v1/projects", phone, nil, nil); st != 200 {
		t.Errorf("the session of the last passkey: %d", st)
	}
}
