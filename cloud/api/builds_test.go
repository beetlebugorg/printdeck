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

// buildCall sends a request of an Action with the token.
func buildCall(t *testing.T, base, path, token string, in, out any) int {
	t.Helper()
	return call(t, base, "POST", path, token, in, out)
}

// The owner links a repository. An Action uploads a build of a pull request, the owner approves
// one part and rejects the other, and a newer build of the same pull request replaces the
// waiting parts of the older one.
func TestBuildFlow(t *testing.T) {
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
	file := []byte("G1 X10 Y10\n")
	sum256 := sha256.Sum256(file)
	manifest := cloud.BuildManifest{Commit: sha, PR: 7, Parts: []cloud.BuildPart{
		{Name: "Pod body", Printer: "taz", Process: "fdm", Material: "PLA", File: "pod_body.gcode", Size: int64(len(file)),
			SHA256: hex.EncodeToString(sum256[:]), Preview: "pod_body.path.bin", Seconds: 3600, Grams: 30},
		{Name: "Feet", Printer: "phenom", Process: "resin", Count: 4, File: "feet.cbddlp", Size: int64(len(file)),
			SHA256: hex.EncodeToString(sum256[:])},
	}}
	token := "beetlebugorg/bittlex|refs/pull/7/merge|" + strings.Repeat("cd", 20)
	if st := buildCall(t, base, "/api/v1/builds", "elsewhere/repo|refs/heads/main|"+sha, manifest, nil); st != 403 {
		t.Errorf("a build of another repository: %d", st)
	}
	if st := buildCall(t, base, "/api/v1/builds", "", manifest, nil); st != 401 {
		t.Errorf("a build with no token: %d", st)
	}
	bad := manifest
	bad.Parts = []cloud.BuildPart{{Name: "x", Printer: "taz", Process: "fdm", File: "../escape", Size: 1, SHA256: hex.EncodeToString(sum256[:])}}
	if st := buildCall(t, base, "/api/v1/builds", token, bad, nil); st != 400 {
		t.Errorf("a file name with a folder: %d", st)
	}
	upload := func() string {
		var br cloud.BuildReply
		if st := buildCall(t, base, "/api/v1/builds", token, manifest, &br); st != 200 {
			t.Fatalf("build: %d", st)
		}
		if !strings.HasSuffix(br.URL, "/projects/bittlex/builds/"+br.ID) {
			t.Errorf("build page %q", br.URL)
		}
		if st := buildCall(t, base, "/api/v1/builds/"+br.ID+"/done", token, nil, nil); st != 400 {
			t.Errorf("done before the files: %d", st)
		}
		for _, f := range []string{"pod_body.gcode", "pod_body.path.bin", "feet.cbddlp"} {
			if err := cloud.PostFile(ctx, nil, br.Grant, f, "application/octet-stream", "", file); err != nil {
				t.Fatal(err)
			}
		}
		var pb cloud.ProjectBuild
		if st := buildCall(t, base, "/api/v1/builds/"+br.ID+"/done", token, nil, &pb); st != 200 || !pb.Ready {
			t.Fatalf("done: %d %+v", st, pb)
		}
		if pb.Kind != cloud.BuildTest || pb.PR != 7 || pb.Commit != sha || pb.Parts[1].Count != 4 || pb.Parts[0].PreviewURL == "" ||
			pb.Run != "https://github.com/beetlebugorg/bittlex/actions/runs/42" {
			t.Fatalf("build %+v", pb)
		}
		return br.ID
	}
	first := upload()

	// The owner approves the FDM part and rejects the resin part, with a reason.
	parts := "/api/v1/projects/" + sum.ID + "/builds/" + first + "/parts/"
	if st := call(t, base, "POST", parts+"1/reject", tok, cloud.BuildDecision{}, nil); st != 400 {
		t.Errorf("a rejection with no reason: %d", st)
	}
	if st := call(t, base, "POST", parts+"0/approve", tok, nil, nil); st != 200 {
		t.Fatalf("approve: %d", st)
	}
	if st := call(t, base, "POST", parts+"1/reject", tok, cloud.BuildDecision{Reason: "The feet need a chamfer."}, nil); st != 200 {
		t.Fatalf("reject: %d", st)
	}
	var v cloud.ProjectView
	call(t, base, "GET", "/api/v1/projects/"+sum.ID, tok, nil, &v)
	if v.Repo != "beetlebugorg/bittlex" || len(v.Builds) != 1 || v.Builds[0].Parts[0].State != cloud.PartApproved ||
		v.Builds[0].Parts[1].State != cloud.PartRejected || v.Builds[0].Parts[1].By != "Jeremy" {
		t.Fatalf("view %+v", v.Builds)
	}

	// A newer build of the pull request replaces the parts that wait.
	time.Sleep(5 * time.Millisecond)
	second := upload()
	if st := call(t, base, "POST", "/api/v1/projects/"+sum.ID+"/builds/"+second+"/parts/0/approve", tok, nil, nil); st != 200 {
		t.Fatal(st)
	}
	time.Sleep(5 * time.Millisecond)
	upload()
	var pb cloud.ProjectBuild
	call(t, base, "GET", "/api/v1/projects/"+sum.ID+"/builds/"+second, tok, nil, &pb)
	if pb.Parts[0].State != cloud.PartApproved || pb.Parts[1].State != cloud.PartReplaced {
		t.Fatalf("the second build after the third: %+v", pb.Parts)
	}
	if st := call(t, base, "POST", "/api/v1/projects/"+sum.ID+"/builds/"+second+"/parts/1/approve", tok, nil, nil); st != 409 {
		t.Errorf("approve a replaced part: %d", st)
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
