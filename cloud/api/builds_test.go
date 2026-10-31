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
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"printdeck/internal/cloud"
)

// fakeOIDC takes a token "<repository>|<ref>|<sha>" for the tests of the builds.
type fakeOIDC struct{}

func (fakeOIDC) Verify(_ context.Context, token, audience string) (*GitHubClaims, error) {
	f := strings.Split(token, "|")
	if len(f) != 3 || audience != "localhost" {
		return nil, errors.New("bad token")
	}
	return &GitHubClaims{Repository: f[0], Ref: f[1], SHA: f[2], RunID: "42"}, nil
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
	if st := call(t, base, "POST", builds, sam, plates([]cloud.PlatePart{{Part: 0, Count: 1}}), nil); st != 401 {
		t.Errorf("a build of a member: %d", st)
	}
	var pb cloud.ProjectBuild
	if st := call(t, base, "POST", builds, tok, plates([]cloud.PlatePart{{Part: 0, Count: 2}}, []cloud.PlatePart{{Part: 2, Count: 4}}), &pb); st != 200 {
		t.Fatalf("build: %d", st)
	}
	if pb.State != cloud.BuildSlicing || !strings.Contains(pb.Start, "-f build="+pb.ID) || pb.Plates[0].Parts[0].Count != 2 ||
		pb.Plates[0].Parts[0].Name != "Pod body" || pb.Plates[1].Parts[0].Count != 1 {
		t.Fatalf("build %+v", pb)
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
	time.Sleep(5 * time.Millisecond)
	plan()
	var b2 cloud.ProjectBuild
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
			"repository": "beetlebugorg/bittlex", "ref": "refs/heads/main", "sha": strings.Repeat("ab", 20)})
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
	if err != nil || c.Repository != "beetlebugorg/bittlex" || c.Ref != "refs/heads/main" {
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
