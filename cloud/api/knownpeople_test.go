package api

import (
	"context"
	"strings"
	"testing"

	"printdeck/internal/cloud"
)

// The owner adds a member of one project to another project with no invite. The member's
// session sees the new project at once, and a second add is a conflict. Only the owner lists
// and adds people.
func TestAddKnownMember(t *testing.T) {
	d, base := devService(t)
	base = strings.Replace(base, "127.0.0.1", "localhost", 1)
	d.API.Base, d.Grants.URL = base, base+"/upload"
	ctx := context.Background()
	l := connectedLink(t, d, base)
	var c cloud.Client
	var oc cloud.OwnerCodeReply
	if err := c.Do(ctx, l.Identity(), "POST", "/api/v1/owner/code", nil, &oc); err != nil {
		t.Fatal(err)
	}
	tok := register(t, base, newSoftKey(t), "owner", oc.Code, "Jeremy").Token
	robot, err := c.NewProject(ctx, l.Identity(), "Robot")
	if err != nil {
		t.Fatal(err)
	}
	speaker, err := c.NewProject(ctx, l.Identity(), "Speaker")
	if err != nil {
		t.Fatal(err)
	}
	var inv cloud.InviteReply
	call(t, base, "POST", "/api/v1/projects/"+robot.ID+"/invites", tok, cloud.InviteRequest{Name: "Sam"}, &inv)
	sam := register(t, base, newSoftKey(t), "invite", strings.TrimPrefix(inv.URL, base+"/j#"), "Sam").Token
	// An invite that waits is not a person to add.
	call(t, base, "POST", "/api/v1/projects/"+robot.ID+"/invites", tok, cloud.InviteRequest{Name: "Ada"}, nil)

	names := func(token string) []string {
		t.Helper()
		var list []cloud.ProjectSummary
		if st := call(t, base, "GET", "/api/v1/projects", token, nil, &list); st != 200 {
			t.Fatalf("projects: %d", st)
		}
		var out []string
		for _, p := range list {
			out = append(out, p.Name)
		}
		return out
	}
	if got := names(sam); len(got) != 1 || got[0] != "Robot" {
		t.Fatalf("Sam before the add: %v", got)
	}

	var people []cloud.KnownPerson
	if st := call(t, base, "GET", "/api/v1/projects/"+speaker.ID+"/people", tok, nil, &people); st != 200 {
		t.Fatalf("people: %d", st)
	}
	if len(people) != 1 || people[0].Name != "Sam" || people[0].Member != inv.Member || len(people[0].Projects) != 1 || people[0].Projects[0] != "Robot" {
		t.Fatalf("people %+v", people)
	}
	if st := call(t, base, "GET", "/api/v1/projects/"+robot.ID+"/people", sam, nil, nil); st != 401 {
		t.Errorf("a member lists the people: %d", st)
	}
	add := cloud.MemberAdd{Project: robot.ID, Member: inv.Member}
	if st := call(t, base, "POST", "/api/v1/projects/"+speaker.ID+"/members", sam, add, nil); st != 401 {
		t.Errorf("a member adds a person: %d", st)
	}
	var v cloud.ProjectView
	if st := call(t, base, "POST", "/api/v1/projects/"+speaker.ID+"/members", tok, add, &v); st != 200 {
		t.Fatalf("add: %d", st)
	}
	if len(v.Members) != 1 || v.Members[0].Name != "Sam" || !v.Members[0].Joined {
		t.Fatalf("members %+v", v.Members)
	}

	// The same session of Sam sees the new project.
	if got := names(sam); len(got) != 2 {
		t.Fatalf("Sam after the add: %v", got)
	}
	if st := call(t, base, "GET", "/api/v1/projects/"+speaker.ID, sam, nil, nil); st != 200 {
		t.Fatalf("Sam opens the new project: %d", st)
	}
	if st := call(t, base, "POST", "/api/v1/projects/"+speaker.ID+"/members", tok, add, nil); st != 409 {
		t.Errorf("a second add: %d", st)
	}
	call(t, base, "GET", "/api/v1/projects/"+speaker.ID+"/people", tok, nil, &people)
	if len(people) != 0 {
		t.Errorf("people after the add: %+v", people)
	}

	// The owner removes Sam from the new project: Sam has no access there, and keeps the first.
	if st := call(t, base, "DELETE", "/api/v1/projects/"+speaker.ID+"/members/"+v.Members[0].ID, tok, nil, nil); st != 200 {
		t.Fatalf("remove: %d", st)
	}
	if st := call(t, base, "GET", "/api/v1/projects/"+speaker.ID, sam, nil, nil); st != 401 {
		t.Errorf("Sam after the remove: %d", st)
	}
	if got := names(sam); len(got) != 1 || got[0] != "Robot" {
		t.Errorf("Sam after the remove: %v", got)
	}
}
