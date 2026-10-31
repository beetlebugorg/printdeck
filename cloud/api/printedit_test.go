package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"printdeck/internal/cloud"
)

// The owner renames a print, marks it delivered and links its source. A member writes notes,
// and cannot rename. The fields stay when the deck sends the print again. Both write the notes
// of the project.
func TestPrintEdit(t *testing.T) {
	d, base := devService(t)
	base = strings.Replace(base, "127.0.0.1", "localhost", 1)
	d.API.Base, d.Grants.URL = base, base+"/upload"
	ctx := context.Background()
	l := connectedLink(t, d, base)
	id := l.Identity()
	var c cloud.Client
	var oc cloud.OwnerCodeReply
	if err := c.Do(ctx, id, "POST", "/api/v1/owner/code", nil, &oc); err != nil {
		t.Fatal(err)
	}
	tok := register(t, base, newSoftKey(t), "owner", oc.Code, "Jeremy").Token
	sum, err := c.NewProject(ctx, id, "Robot")
	if err != nil {
		t.Fatal(err)
	}
	var inv cloud.InviteReply
	if st := call(t, base, "POST", "/api/v1/projects/"+sum.ID+"/invites", tok, cloud.InviteRequest{Name: "Sam"}, &inv); st != 200 {
		t.Fatalf("invite: %d", st)
	}
	sam := register(t, base, newSoftKey(t), "invite", strings.TrimPrefix(inv.URL, base+"/j#"), "Sam").Token
	up := cloud.PrintUpload{Request: cloud.PrintRequest{Name: "resin_plate_0.05", Result: "complete", Ended: time.Now().UTC(), Seconds: 60, Key: "k@1"}}
	pid, err := c.SendPrint(ctx, id, sum.ID, up)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/projects/" + sum.ID + "/prints/" + pid
	str := func(s string) *string { return &s }
	yes := true
	print := func(token string) cloud.ProjectPrint {
		var v cloud.ProjectView
		if st := call(t, base, "GET", "/api/v1/projects/"+sum.ID, token, nil, &v); st != 200 || len(v.Prints) != 1 {
			t.Fatalf("view: %d %+v", st, v.Prints)
		}
		return v.Prints[0]
	}

	if st := call(t, base, "PATCH", path, sam, cloud.PrintEdit{Title: str("Mine")}, nil); st != 401 {
		t.Errorf("a member renames: %d", st)
	}
	if st := call(t, base, "PATCH", path, tok, cloud.PrintEdit{Source: str("javascript:alert(1)")}, nil); st != 400 {
		t.Errorf("a source with no web address: %d", st)
	}
	src := "https://github.com/beetlebug/robot/blob/3f2a9c1/parts/shoulder.stl"
	if st := call(t, base, "PATCH", path, tok, cloud.PrintEdit{Title: str("Shoulder pieces"), Delivered: &yes, Source: str(src)}, nil); st != 200 {
		t.Fatalf("owner edit: %d", st)
	}
	if st := call(t, base, "PATCH", path, sam, cloud.PrintEdit{Notes: str("  Sanded the left one.\r\nFits.  ")}, nil); st != 200 {
		t.Fatalf("member notes: %d", st)
	}
	p := print(sam)
	if p.Name != "Shoulder pieces" || p.DeckName != "resin_plate_0.05" || p.Delivered.IsZero() || p.Source != src ||
		p.Notes != "Sanded the left one.\nFits." || p.NotesBy != "Sam" {
		t.Fatalf("print %+v", p)
	}

	// The deck sends the print again: the fields of the pages stay.
	if again, err := c.SendPrint(ctx, id, sum.ID, up); err != nil || again != pid {
		t.Fatalf("again %q %v", again, err)
	}
	if p := print(tok); p.Name != "Shoulder pieces" || p.Notes == "" || p.Delivered.IsZero() || p.Source != src {
		t.Fatalf("after the deck sent it again: %+v", p)
	}
	// Not delivered, and the name of the deck again.
	no := false
	if st := call(t, base, "PATCH", path, tok, cloud.PrintEdit{Title: str(""), Delivered: &no}, nil); st != 200 {
		t.Fatal(st)
	}
	if p := print(tok); p.Name != "resin_plate_0.05" || p.DeckName != "" || !p.Delivered.IsZero() {
		t.Fatalf("reset %+v", p)
	}

	// Ready for pickup: the owner only, and it clears again.
	if st := call(t, base, "PATCH", path, sam, cloud.PrintEdit{Ready: &yes}, nil); st != 401 {
		t.Errorf("member ready: %d", st)
	}
	if st := call(t, base, "PATCH", path, tok, cloud.PrintEdit{Ready: &yes}, nil); st != 200 {
		t.Fatal(st)
	}
	if p := print(tok); p.Ready.IsZero() {
		t.Fatalf("not ready: %+v", p)
	}
	if st := call(t, base, "PATCH", path, tok, cloud.PrintEdit{Ready: &no}, nil); st != 200 {
		t.Fatal(st)
	}
	if p := print(tok); !p.Ready.IsZero() {
		t.Fatalf("still ready: %+v", p)
	}

	// A test print: the owner only, and it clears again.
	if st := call(t, base, "PATCH", path, sam, cloud.PrintEdit{Test: &yes}, nil); st != 401 {
		t.Errorf("member test: %d", st)
	}
	if st := call(t, base, "PATCH", path, tok, cloud.PrintEdit{Test: &yes}, nil); st != 200 {
		t.Fatal(st)
	}
	if p := print(tok); p.Test.IsZero() {
		t.Fatalf("not a test print: %+v", p)
	}

	// The notes of the project.
	if st := call(t, base, "PATCH", "/api/v1/projects/"+sum.ID+"/notes", sam, cloud.NotesEdit{Notes: "Paint: matte black"}, nil); st != 200 {
		t.Fatalf("project notes: %d", st)
	}
	var v cloud.ProjectView
	call(t, base, "GET", "/api/v1/projects/"+sum.ID, tok, nil, &v)
	if v.Notes != "Paint: matte black" || v.NotesBy != "Sam" {
		t.Fatalf("project notes %+v", v)
	}
}
