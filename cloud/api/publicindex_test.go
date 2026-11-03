package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"printdeck/internal/cloud"
)

// The list of the public projects follows the public pages: a page that the owner turns on, a
// rename to a new slug, and a page that the owner turns off.
func TestPublicIndex(t *testing.T) {
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
	if _, err := c.NewProject(ctx, l.Identity(), "Private"); err != nil {
		t.Fatal(err)
	}
	var pr cloud.PrintReply
	req := cloud.PrintRequest{Name: "Arm", File: "arm.gcode", Result: "complete", Ended: time.Now().UTC(), Seconds: 60, Key: "arm@1"}
	c.Do(ctx, l.Identity(), "POST", "/api/v1/projects/"+id+"/prints", req, &pr)
	c.Do(ctx, l.Identity(), "POST", "/api/v1/projects/"+id+"/prints/"+pr.Print+"/done", cloud.PrintDone{}, nil)
	index := func() []cloud.PublicProject {
		t.Helper()
		b, _, err := d.Store.Get(ctx, PublicIndex)
		if err != nil {
			t.Fatal(err)
		}
		var out []cloud.PublicProject
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("%v: %s", err, b)
		}
		return out
	}

	if st := call(t, base, "POST", "/api/v1/projects/"+id+"/public", tok, cloud.PublicRequest{On: true}, nil); st != 200 {
		t.Fatalf("public on: %d", st)
	}
	if got := index(); len(got) != 1 || got[0].Slug != "robot" || got[0].Name != "Robot" || got[0].Prints != 1 {
		t.Fatalf("after public on: %+v", got)
	}

	if st := call(t, base, "PATCH", "/api/v1/projects/"+id, tok, cloud.ProjectRequest{Name: "Arm robot", Note: "Two arms."}, nil); st != 200 {
		t.Fatalf("rename: %d", st)
	}
	if got := index(); len(got) != 1 || got[0].Slug != "arm-robot" || got[0].Name != "Arm robot" || got[0].Note != "Two arms." {
		t.Fatalf("after the rename: %+v", got)
	}

	if st := call(t, base, "POST", "/api/v1/projects/"+id+"/public", tok, cloud.PublicRequest{On: false}, nil); st != 200 {
		t.Fatalf("public off: %d", st)
	}
	if _, _, err := d.Store.Get(ctx, PublicIndex); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a list with no public projects: %v", err)
	}
}
