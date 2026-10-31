package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"printdeck/internal/cloud"
)

// loginInvite signs in with the passkey k and the invite code: the passkey joins the project of
// the invite. It returns the status and the reply.
func loginInvite(t *testing.T, base string, k *softKey, code string) (int, cloud.SignInReply) {
	t.Helper()
	var begin cloud.PasskeyBegin
	if st := call(t, base, "POST", "/api/v1/auth/login/begin", "", nil, &begin); st != 200 {
		t.Fatalf("login begin: %d", st)
	}
	var out cloud.SignInReply
	st := call(t, base, "POST", "/api/v1/auth/login/finish", "",
		cloud.PasskeyRequest{Code: code, State: begin.State, Credential: k.get(t, begin.Options, base)}, &out)
	return st, out
}

// One passkey of a friend works for two projects: the second invite joins with a sign-in. A
// removal from one project keeps the other, and a removal from both removes the passkey. The
// passkey starts as a file from before the passkeys of many projects.
func TestPasskeyOfManyProjects(t *testing.T) {
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
	project := func(name string) string {
		var sum cloud.ProjectSummary
		if st := call(t, base, "POST", "/api/v1/projects", tok, cloud.ProjectRequest{Name: name}, &sum); st != 200 {
			t.Fatalf("new project: %d", st)
		}
		return sum.ID
	}
	invite := func(p string) (code, member string) {
		var inv cloud.InviteReply
		if st := call(t, base, "POST", "/api/v1/projects/"+p+"/invites", tok, cloud.InviteRequest{Name: "Sam"}, &inv); st != 200 {
			t.Fatalf("invite: %d", st)
		}
		return strings.TrimPrefix(inv.URL, base+"/j#"), inv.Member
	}
	a, b := project("Robot"), project("Garden")
	codeA, samA := invite(a)
	sam := newSoftKey(t)
	register(t, base, sam, "invite", codeA, "Sam")

	// The passkey file as the service wrote it before: one project in Project and Member.
	file := memberKeyFile(sam.id)
	raw, _, err := d.Store.Get(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	var k Passkey
	json.Unmarshal(raw, &k)
	old := map[string]any{"credential": k.Credential, "added": k.Added, "project": a, "member": samA}
	if err := d.API.putJSON(ctx, file, old); err != nil {
		t.Fatal(err)
	}
	if st, in := login(t, base, sam); st != 200 || in.Project != a {
		t.Fatalf("login with an old file: %d %+v", st, in)
	}

	// The second invite: a sign-in with the passkey joins.
	codeB, samB := invite(b)
	st, in := loginInvite(t, base, sam, codeB)
	if st != 200 || in.Kind != KindMember || in.Project != "" || in.Name != "Sam" {
		t.Fatalf("join with a sign-in: %d %+v", st, in)
	}
	var list []cloud.ProjectSummary
	if st := call(t, base, "GET", "/api/v1/projects", in.Token, nil, &list); st != 200 || len(list) != 2 {
		t.Fatalf("projects of the member: %d %+v", st, list)
	}
	for _, p := range []string{a, b} {
		if st := call(t, base, "GET", "/api/v1/projects/"+p, in.Token, nil, nil); st != 200 {
			t.Errorf("project %s: %d", p, st)
		}
	}
	// An invite to a project of the passkey does not join again, and still works.
	codeA2, _ := invite(a)
	if st, _ := loginInvite(t, base, sam, codeA2); st != 409 {
		t.Errorf("a second join of the same project: %d", st)
	}
	if st := call(t, base, "POST", "/api/v1/invites/view", "", cloud.PasskeyRequest{Code: codeA2}, nil); st != 200 {
		t.Errorf("the invite after a refused join: %d", st)
	}

	// A removal from one project keeps the other.
	if st := call(t, base, "DELETE", "/api/v1/projects/"+a+"/members/"+samA, tok, nil, nil); st != 200 {
		t.Fatalf("remove from a: %d", st)
	}
	if st := call(t, base, "GET", "/api/v1/projects/"+a, in.Token, nil, nil); st != 401 {
		t.Errorf("a removed member with the old session: %d", st)
	}
	st, in = login(t, base, sam)
	if st != 200 || in.Project != b {
		t.Fatalf("login after a removal: %d %+v", st, in)
	}
	if st := call(t, base, "GET", "/api/v1/projects/"+a, in.Token, nil, nil); st != 401 {
		t.Errorf("project a after the removal: %d", st)
	}

	// A removal from the last project removes the passkey.
	if st := call(t, base, "DELETE", "/api/v1/projects/"+b+"/members/"+samB, tok, nil, nil); st != 200 {
		t.Fatalf("remove from b: %d", st)
	}
	if st, _ := login(t, base, sam); st != 401 {
		t.Errorf("login with no project: %d", st)
	}
	if _, _, err := d.Store.Get(ctx, file); err == nil {
		t.Error("the passkey file stays with no project")
	}
}
