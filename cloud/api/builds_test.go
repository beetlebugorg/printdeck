package api

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"printdeck/internal/cloud"
)

// fakeOIDC takes a token "<repository>|<ref>|<sha>[|<id>[|<job workflow ref>]]" for the tests of
// the builds. With no ID, each token gets a new one. With no workflow, the job is of the slice
// workflow of the repository on main.
type fakeOIDC struct{}

func (fakeOIDC) Verify(_ context.Context, token, audience string) (*GitHubClaims, error) {
	f := strings.Split(token, "|")
	if len(f) < 3 || len(f) > 5 || audience != "localhost" {
		return nil, errors.New("bad token")
	}
	owner, _, _ := strings.Cut(f[0], "/")
	c := &GitHubClaims{Repository: f[0], Ref: f[1], SHA: f[2], RunID: "42", RepositoryOwner: owner,
		JobWorkflowRef: f[0] + "/.github/workflows/printdeck.yml@refs/heads/main"}
	c.ID = newID(10)
	if len(f) > 3 && f[3] != "" {
		c.ID = f[3]
	}
	if len(f) > 4 {
		c.JobWorkflowRef = f[4]
	}
	return c, nil
}

// The flow of a plan: the owner links a repository, the plan run uploads the parts and their
// models, the owner makes a build with plates, the slice run uploads the files, and the owner
// approves one plate and rejects another. A newer plan of the same pull request replaces the
// plates that wait.
func TestPlanFlow(t *testing.T) {
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
	var inv cloud.InviteReply
	call(t, base, "POST", "/api/v1/projects/"+sum.ID+"/invites", tok, cloud.InviteRequest{Name: "Sam"}, &inv)
	sam := register(t, base, newSoftKey(t), "invite", strings.TrimPrefix(inv.URL, base+"/j#"), "Sam").Token
	other, _ := c.NewProject(ctx, l.Identity(), "Other")
	if st := call(t, base, "PUT", "/api/v1/projects/"+sum.ID+"/repo", tok, cloud.RepoRequest{Repo: "bad repo"}, nil); st != 400 {
		t.Errorf("a bad repository: %d", st)
	}
	if st := call(t, base, "PUT", "/api/v1/projects/"+sum.ID+"/repo", tok, cloud.RepoRequest{Repo: "https://github.com/beetlebugorg/bittlex.git"}, nil); st != 200 {
		t.Fatalf("link: %d", st)
	}
	if st := call(t, base, "PUT", "/api/v1/projects/"+other.ID+"/repo", tok, cloud.RepoRequest{Repo: "BeetlebugOrg/Bittlex"}, nil); st != 409 {
		t.Errorf("the same repository in two projects: %d", st)
	}

	sha := strings.Repeat("ab", 20)
	token := "beetlebugorg/bittlex|refs/pull/7/merge|" + strings.Repeat("cd", 20)
	manifest := cloud.PlanManifest{Commit: sha, PR: 7, Parts: []cloud.PlanPart{
		{Name: "Pod body", Files: []string{"stl/pod_body.3mf"}, Printer: "taz", Process: "fdm", Profile: "taz-workhorse-se/pla-0.10-fine@1",
			Material: "roll-4", Changed: true, Models: []string{"pod_body.stl"}, SizeX: 80, SizeY: 100},
		{Name: "Pod door", Files: []string{"stl/pod_door.stl"}, Printer: "taz", Process: "fdm", Profile: "taz-workhorse-se/pla-0.20-standard@1",
			Material: "roll-4", Models: []string{"pod_door.stl"}},
		{Name: "Feet", Files: []string{"stl/foot.stl"}, Count: 4, Printer: "phenom", Process: "resin", Prebuilt: "phenom/feet.cbddlp",
			Material: "Grey resin", Models: []string{"foot.stl"}},
	}}
	if st := call(t, base, "POST", "/api/v1/plans", "elsewhere/repo|refs/heads/main|"+sha, manifest, nil); st != 403 {
		t.Errorf("a plan of another repository: %d", st)
	}
	if st := call(t, base, "POST", "/api/v1/plans", "", manifest, nil); st != 401 {
		t.Errorf("a plan with no token: %d", st)
	}
	model := []byte("solid x\nendsolid x\n")
	plan := func() string {
		var up cloud.UploadReply
		if st := call(t, base, "POST", "/api/v1/plans", token, manifest, &up); st != 200 {
			t.Fatalf("plan: %d", st)
		}
		if st := call(t, base, "POST", "/api/v1/plans/"+up.ID+"/done", token, nil, nil); st != 400 {
			t.Errorf("done before the models: %d", st)
		}
		for _, f := range []string{"pod_body.stl", "pod_door.stl", "foot.stl"} {
			if err := cloud.PostFile(ctx, nil, up.Grant, f, "model/stl", "", model); err != nil {
				t.Fatal(err)
			}
		}
		var pv cloud.ProjectPlan
		if st := call(t, base, "POST", "/api/v1/plans/"+up.ID+"/done", token, nil, &pv); st != 200 || !pv.Ready {
			t.Fatalf("plan done: %d %+v", st, pv)
		}
		if pv.Kind != cloud.BuildTest || pv.PR != 7 || pv.Commit != sha || len(pv.Parts[0].ModelURLs) != 1 || !pv.Parts[0].Changed {
			t.Fatalf("plan %+v", pv)
		}
		return up.ID
	}
	first := plan()

	builds := "/api/v1/projects/" + sum.ID + "/plans/" + first + "/builds"
	plates := func(groups ...[]cloud.PlatePart) cloud.BuildRequest {
		var r cloud.BuildRequest
		for _, g := range groups {
			r.Plates = append(r.Plates, cloud.PlateRequest{Parts: g})
		}
		return r
	}
	if st := call(t, base, "POST", builds, tok, plates([]cloud.PlatePart{{Part: 0, Count: 1}, {Part: 1, Count: 1}}), nil); st != 400 {
		t.Errorf("two profiles on one plate: %d", st)
	}
	// Only the owner chooses the plates and slices them. A member reads the plan and its parts, for
	// the 3D view of the plan page, but cannot make a build.
	if st := call(t, base, "POST", builds, sam, plates([]cloud.PlatePart{{Part: 0, Count: 1}}), nil); st != 401 {
		t.Errorf("a build of a member: %d", st)
	}
	var seen cloud.ProjectPlan
	if st := call(t, base, "GET", "/api/v1/projects/"+sum.ID+"/plans/"+first, sam, nil, &seen); st != 200 || len(seen.Parts) == 0 {
		t.Errorf("a member reads the plan: %d, %d parts", st, len(seen.Parts))
	}
	var pb cloud.ProjectBuild
	if st := call(t, base, "POST", builds, tok, plates([]cloud.PlatePart{{Part: 0, Count: 2}}, []cloud.PlatePart{{Part: 2, Count: 4}}), &pb); st != 200 {
		t.Fatalf("build: %d", st)
	}
	if pb.State != cloud.BuildSlicing || !strings.Contains(pb.Start, "-f build="+pb.ID) || pb.Plates[0].Parts[0].Count != 2 ||
		pb.Plates[0].Parts[0].Name != "Pod body" || pb.Plates[1].Parts[0].Count != 1 {
		t.Fatalf("build %+v", pb)
	}

	// Only the slice workflow on main gets the build, and each token works once.
	for _, wf := range []string{"beetlebugorg/bittlex/.github/workflows/printdeck.yml@refs/heads/feature",
		"beetlebugorg/bittlex/.github/workflows/other.yml@refs/heads/main",
		"beetlebugorg/bittlex/.github/workflows/printdeck.yml@refs/pull/7/merge"} {
		if st := call(t, base, "GET", "/api/v1/builds/"+pb.ID, token+"||"+wf, nil, nil); st != 403 {
			t.Errorf("a slice request of %s: %d", wf, st)
		}
	}
	once := token + "|jti-1"
	if st := call(t, base, "GET", "/api/v1/builds/"+pb.ID, once, nil, nil); st != 200 {
		t.Fatalf("a token: %d", st)
	}
	if st := call(t, base, "GET", "/api/v1/builds/"+pb.ID, once, nil, nil); st != 401 {
		t.Errorf("the same token again: %d", st)
	}

	// The slice run.
	var job cloud.BuildJob
	if st := call(t, base, "GET", "/api/v1/builds/"+pb.ID, token, nil, &job); st != 200 || job.Commit != sha || len(job.Plates) != 2 {
		t.Fatalf("job: %d %+v", st, job)
	}
	gcode := []byte("G1 X10\n")
	h := sha256.Sum256(gcode)
	files := cloud.BuildFiles{Plates: []cloud.BuildPlate{
		{File: "plate-1.gcode", Size: int64(len(gcode)), SHA256: hex.EncodeToString(h[:]), Preview: "plate-1.path.bin", Seconds: 3600, Grams: 30,
			Profile: "taz-workhorse-se/pla-0.10-fine@44606b4e58b6"},
		{File: "plate-2.cbddlp", Size: int64(len(gcode)), SHA256: hex.EncodeToString(h[:])},
	}}
	if st := call(t, base, "POST", "/api/v1/builds/"+pb.ID+"/files", token, cloud.BuildFiles{Plates: files.Plates[:1]}, nil); st != 400 {
		t.Errorf("files for one of two plates: %d", st)
	}
	var up cloud.UploadReply
	if st := call(t, base, "POST", "/api/v1/builds/"+pb.ID+"/files", token, files, &up); st != 200 {
		t.Fatalf("files: %d", st)
	}
	for _, f := range []string{"plate-1.gcode", "plate-1.path.bin", "plate-2.cbddlp"} {
		if err := cloud.PostFile(ctx, nil, up.Grant, f, "application/octet-stream", "", gcode); err != nil {
			t.Fatal(err)
		}
	}
	if st := call(t, base, "POST", "/api/v1/builds/"+pb.ID+"/done", token, nil, &pb); st != 200 || pb.State != cloud.BuildReady ||
		pb.Plates[0].State != cloud.PartWaiting || pb.Plates[0].PreviewURL == "" || pb.Plates[0].Profile != "taz-workhorse-se/pla-0.10-fine@44606b4e58b6" {
		t.Fatalf("done: %+v", pb)
	}

	// The owner approves the first plate and rejects the second, with a reason.
	pl := "/api/v1/projects/" + sum.ID + "/builds/" + pb.ID + "/plates/"
	if st := call(t, base, "POST", pl+"1/reject", tok, cloud.BuildDecision{}, nil); st != 400 {
		t.Errorf("a rejection with no reason: %d", st)
	}
	if st := call(t, base, "POST", pl+"0/approve", sam, nil, nil); st != 401 {
		t.Errorf("an approval of a member: %d", st)
	}
	if st := call(t, base, "POST", pl+"0/approve", tok, nil, nil); st != 200 {
		t.Fatalf("approve: %d", st)
	}
	if st := call(t, base, "POST", pl+"1/reject", tok, cloud.BuildDecision{Reason: "The feet need a chamfer."}, nil); st != 200 {
		t.Fatalf("reject: %d", st)
	}
	var v cloud.ProjectView
	call(t, base, "GET", "/api/v1/projects/"+sum.ID, sam, nil, &v)
	if v.Repo != "beetlebugorg/bittlex" || len(v.Plans) != 1 || len(v.Plans[0].Builds) != 1 ||
		v.Plans[0].Builds[0].Plates[0].State != cloud.PartApproved || v.Plans[0].Builds[0].Plates[1].Reason == "" {
		t.Fatalf("view %+v", v.Plans)
	}

	// A second build of the same plan waits; a newer plan of the pull request replaces it.
	var second cloud.ProjectBuild
	call(t, base, "POST", builds, tok, plates([]cloud.PlatePart{{Part: 1, Count: 1}}), &second)
	call(t, base, "POST", "/api/v1/builds/"+second.ID+"/files", token, cloud.BuildFiles{Plates: files.Plates[:1]}, &up)
	cloud.PostFile(ctx, nil, up.Grant, "plate-1.gcode", "application/octet-stream", "", gcode)
	cloud.PostFile(ctx, nil, up.Grant, "plate-1.path.bin", "application/octet-stream", "", gcode)
	if st := call(t, base, "POST", "/api/v1/builds/"+second.ID+"/done", token, nil, nil); st != 200 {
		t.Fatal(st)
	}
	// A newer plan with no change of the pod door keeps the plate.
	time.Sleep(5 * time.Millisecond)
	plan()
	var b2 cloud.ProjectBuild
	call(t, base, "GET", "/api/v1/projects/"+sum.ID+"/builds/"+second.ID, tok, nil, &b2)
	if b2.Plates[0].State != cloud.PartWaiting {
		t.Fatalf("the plate after a newer plan with no change: %+v", b2.Plates)
	}
	time.Sleep(5 * time.Millisecond)
	manifest.Parts[1].Changed = true
	plan()
	call(t, base, "GET", "/api/v1/projects/"+sum.ID+"/builds/"+second.ID, tok, nil, &b2)
	if b2.Plates[0].State != cloud.PartReplaced {
		t.Fatalf("the plate after a newer plan: %+v", b2.Plates)
	}
	var b1 cloud.ProjectBuild
	call(t, base, "GET", "/api/v1/projects/"+sum.ID+"/builds/"+pb.ID, tok, nil, &b1)
	if b1.Plates[0].State != cloud.PartApproved {
		t.Fatalf("an approved plate after a newer plan: %+v", b1.Plates)
	}
}

// recordDispatch is a GitHub App for the tests: it records the slice runs that it starts.
type recordDispatch struct {
	runs, titles []string
	comments     map[int64]string // the comments on pull requests, by ID
	err          error
	// pulls are the open pull requests: "<base> <head repo> <branch> <sha>" -> the number.
	pulls map[string]int
	// states are the states of the pull requests that are not open, by number.
	states map[int]string
}

func (r *recordDispatch) Comment(_ context.Context, repo string, pr int, id int64, body string) (int64, error) {
	if r.comments == nil {
		r.comments = map[int64]string{}
	}
	if _, ok := r.comments[id]; !ok {
		id = int64(len(r.comments) + 100)
	}
	r.comments[id] = fmt.Sprintf("%s#%d\n%s", repo, pr, body)
	return id, nil
}

func (r *recordDispatch) PullState(_ context.Context, repo string, pr int) (string, error) {
	if s, ok := r.states[pr]; ok {
		return s, nil
	}
	return "open", nil
}

func (r *recordDispatch) OpenPull(_ context.Context, base, headRepo, branch, sha string) (int, error) {
	return r.pulls[base+" "+headRepo+" "+branch+" "+sha], nil
}

func (r *recordDispatch) Dispatch(_ context.Context, repo, build, title string) error {
	r.runs = append(r.runs, repo+" "+build)
	r.titles = append(r.titles, title)
	return r.err
}

// With the GitHub App, a build starts its slice run, and the run tells its ID. A refusal of
// GitHub fails the build with the reason.
func TestBuildDispatch(t *testing.T) {
	d, base := devService(t)
	base = strings.Replace(base, "127.0.0.1", "localhost", 1)
	d.API.Base, d.Grants.URL = base, base+"/upload"
	d.API.OIDC = fakeOIDC{}
	app := &recordDispatch{}
	d.API.GitHub = app
	ctx := context.Background()
	l := connectedLink(t, d, base)
	var c cloud.Client
	var oc cloud.OwnerCodeReply
	c.Do(ctx, l.Identity(), "POST", "/api/v1/owner/code", nil, &oc)
	tok := register(t, base, newSoftKey(t), "owner", oc.Code, "Jeremy").Token
	sum, _ := c.NewProject(ctx, l.Identity(), "Bittlex")
	call(t, base, "PUT", "/api/v1/projects/"+sum.ID+"/repo", tok, cloud.RepoRequest{Repo: "beetlebugorg/bittlex"}, nil)
	token := "beetlebugorg/bittlex|refs/heads/main|" + strings.Repeat("ab", 20)
	var up cloud.UploadReply
	dup := cloud.PlanManifest{Parts: []cloud.PlanPart{
		{Name: "Pod door", Variant: "fine", Files: []string{"a.stl"}, Printer: "taz", Process: "fdm", Profile: "p/f@1"},
		{Name: "Pod door", Variant: "Fine", Files: []string{"b.stl"}, Printer: "taz", Process: "fdm", Profile: "p/f@1"}}}
	if st := call(t, base, "POST", "/api/v1/plans", token, dup, nil); st != 400 {
		t.Errorf("a variant twice: %d", st)
	}
	call(t, base, "POST", "/api/v1/plans", token, cloud.PlanManifest{Parts: []cloud.PlanPart{
		{Name: "Pod door", Variant: "fine", Files: []string{"door_fine.stl"}, Printer: "taz", Process: "fdm", Profile: "p/fine@1", Material: "roll-4"},
		{Name: "Pod door", Variant: "standard", Files: []string{"door.stl"}, Printer: "taz", Process: "fdm", Profile: "p/q@1", Material: "roll-4"}}}, &up)
	if st := call(t, base, "POST", "/api/v1/plans/"+up.ID+"/done", token, nil, nil); st != 200 {
		t.Fatal(st)
	}
	req := cloud.BuildRequest{Plates: []cloud.PlateRequest{{Parts: []cloud.PlatePart{{Part: 1, Count: 2}}}}}
	var b cloud.ProjectBuild
	if st := call(t, base, "POST", "/api/v1/projects/"+sum.ID+"/plans/"+up.ID+"/builds", tok, req, &b); st != 200 {
		t.Fatal(st)
	}
	if pp := b.Plates[0].Parts[0]; pp.Name != "Pod door" || pp.Variant != "standard" {
		t.Errorf("the part of the plate %+v", pp)
	}
	if len(app.runs) != 1 || app.runs[0] != "beetlebugorg/bittlex "+b.ID || b.Start != "" || b.State != cloud.BuildSlicing ||
		app.titles[0] != "Pod door (standard) x2 · main" {
		t.Fatalf("dispatch %v %+v", app.runs, b)
	}
	if st := call(t, base, "POST", "/api/v1/builds/"+b.ID+"/started", token, cloud.BuildStarted{RunID: "41221435684"}, &b); st != 200 ||
		b.Run != "https://github.com/beetlebugorg/bittlex/actions/runs/41221435684" {
		t.Fatalf("started: %d %+v", st, b)
	}
	if st := call(t, base, "POST", "/api/v1/builds/"+b.ID+"/progress", token, cloud.BuildProgress{Text: "Slicing plate 1 of 1: Pod door x2"}, &b); st != 200 ||
		b.Progress != "Slicing plate 1 of 1: Pod door x2" {
		t.Fatalf("progress: %d %+v", st, b)
	}
	app.err = errors.New("the Printdeck GitHub App is not installed on beetlebugorg/bittlex: install it there")
	call(t, base, "POST", "/api/v1/projects/"+sum.ID+"/plans/"+up.ID+"/builds", tok, req, &b)
	if b.State != cloud.BuildFailed || !strings.Contains(b.Error, "not installed") {
		t.Fatalf("a refused dispatch: %+v", b)
	}
}

// A member's fork sends the plan of the member's pull request with the token of the fork. A
// stranger's fork, a member with no GitHub username and a branch with no pull request send no
// plan, and a fork cannot touch a build.
func TestForkPlan(t *testing.T) {
	d, base := devService(t)
	base = strings.Replace(base, "127.0.0.1", "localhost", 1)
	d.API.Base, d.Grants.URL = base, base+"/upload"
	d.API.OIDC = fakeOIDC{}
	head := strings.Repeat("cd", 20)
	gh := &recordDispatch{pulls: map[string]int{
		"beetlebugorg/bittlex sam/bittlex door " + head:          12,
		"beetlebugorg/bittlex beetlebugorg/bittlex feet " + head: 13,
	}}
	d.API.GitHub = gh
	ctx := context.Background()
	l := connectedLink(t, d, base)
	var c cloud.Client
	var oc cloud.OwnerCodeReply
	c.Do(ctx, l.Identity(), "POST", "/api/v1/owner/code", nil, &oc)
	tok := register(t, base, newSoftKey(t), "owner", oc.Code, "Jeremy").Token
	sum, _ := c.NewProject(ctx, l.Identity(), "Bittlex")
	call(t, base, "PUT", "/api/v1/projects/"+sum.ID+"/repo", tok, cloud.RepoRequest{Repo: "beetlebugorg/bittlex"}, nil)
	var inv cloud.InviteReply
	call(t, base, "POST", "/api/v1/projects/"+sum.ID+"/invites", tok, cloud.InviteRequest{Name: "Sam"}, &inv)
	sam := register(t, base, newSoftKey(t), "invite", strings.TrimPrefix(inv.URL, base+"/j#"), "Sam").Token
	manifest := cloud.PlanManifest{Commit: strings.Repeat("ee", 20), PR: 99, Parts: []cloud.PlanPart{
		{Name: "Pod door", Files: []string{"door.stl"}, Printer: "taz", Process: "fdm", Profile: "p/q@1"}}}
	fork := "sam/bittlex|refs/heads/door|" + head
	if st := call(t, base, "POST", "/api/v1/plans", fork, manifest, nil); st != 403 {
		t.Errorf("a member with no GitHub username: %d", st)
	}
	// The owner sets the GitHub username of the member; a member cannot.
	path := "/api/v1/projects/" + sum.ID + "/members/" + inv.Member
	if st := call(t, base, "PATCH", path, sam, cloud.MemberEdit{GitHub: "sam"}, nil); st != 401 {
		t.Errorf("a member sets a username: %d", st)
	}
	if st := call(t, base, "PATCH", path, tok, cloud.MemberEdit{GitHub: "bad name!"}, nil); st != 400 {
		t.Errorf("a bad username: %d", st)
	}
	if st := call(t, base, "PATCH", path, tok, cloud.MemberEdit{GitHub: "@Sam"}, nil); st != 200 {
		t.Fatalf("set the username: %d", st)
	}
	for _, bad := range []string{"eve/bittlex|refs/heads/door|" + head, "sam/bittlex|refs/heads/other|" + head,
		"sam/bittlex|refs/heads/door|" + strings.Repeat("ab", 20), "sam/bittlex|refs/tags/v1|" + head} {
		if st := call(t, base, "POST", "/api/v1/plans", bad, manifest, nil); st != 403 {
			t.Errorf("a plan of %s: %d", bad, st)
		}
	}
	var up cloud.UploadReply
	if st := call(t, base, "POST", "/api/v1/plans", fork, manifest, &up); st != 200 {
		t.Fatalf("the plan of the fork: %d", st)
	}
	var pv cloud.ProjectPlan
	if st := call(t, base, "POST", "/api/v1/plans/"+up.ID+"/done", fork, nil, &pv); st != 200 {
		t.Fatalf("done: %d", st)
	}
	if pv.Kind != cloud.BuildTest || pv.PR != 12 || pv.Commit != head || pv.Fork != "sam/bittlex" || pv.Author != "sam" ||
		pv.Repo != "beetlebugorg/bittlex" || pv.Run != "https://github.com/sam/bittlex/actions/runs/42" {
		t.Fatalf("the plan of the fork %+v", pv)
	}
	if st := call(t, base, "GET", "/api/v1/builds/x1", fork, nil, nil); st != 403 {
		t.Errorf("a fork asks for a build: %d", st)
	}
	// A branch of the repository with a pull request.
	manifest.Commit, manifest.PR = head, 0
	call(t, base, "POST", "/api/v1/plans", "beetlebugorg/bittlex|refs/heads/feet|"+head, manifest, &up)
	var bp cloud.ProjectPlan
	call(t, base, "POST", "/api/v1/plans/"+up.ID+"/done", "beetlebugorg/bittlex|refs/heads/feet|"+head, nil, &bp)
	if bp.Kind != cloud.BuildTest || bp.PR != 13 || bp.Fork != "" {
		t.Fatalf("the plan of a branch with a pull request %+v", bp)
	}
}

// The checker of the tokens of GitHub reads the keys of the issuer, and checks the signature,
// the issuer and the audience.
func TestGitHubOIDC(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	var issuer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/jwks" {
			http.NotFound(w, r)
			return
		}
		enc := base64.RawURLEncoding
		json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{"kty": "RSA", "kid": "k1",
			"n": enc.EncodeToString(key.N.Bytes()), "e": enc.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
	}))
	defer srv.Close()
	issuer = srv.URL
	sign := func(aud string, k *rsa.PrivateKey) string {
		tk := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"iss": issuer, "aud": aud, "exp": time.Now().Add(5 * time.Minute).Unix(),
			"jti": "j1", "repository": "beetlebugorg/bittlex", "ref": "refs/heads/main", "sha": strings.Repeat("ab", 20),
			"job_workflow_ref": "beetlebugorg/bittlex/.github/workflows/printdeck.yml@refs/heads/main"})
		tk.Header["kid"] = "k1"
		s, err := tk.SignedString(k)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	v := &GitHubOIDC{Issuer: issuer}
	ctx := context.Background()
	c, err := v.Verify(ctx, sign("printdeck.beetlebug.org", key), "printdeck.beetlebug.org")
	if err != nil || c.Repository != "beetlebugorg/bittlex" || c.Ref != "refs/heads/main" || c.ID != "j1" || sliceRun(c) != nil {
		t.Fatalf("a good token: %+v %v", c, err)
	}
	if _, err := v.Verify(ctx, sign("elsewhere.org", key), "printdeck.beetlebug.org"); err == nil {
		t.Error("a token for another audience")
	}
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	if _, err := v.Verify(ctx, sign("printdeck.beetlebug.org", other), "printdeck.beetlebug.org"); err == nil {
		t.Error("a token of another key")
	}
}
