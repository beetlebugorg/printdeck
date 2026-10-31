package api

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"printdeck/internal/cloud"
)

// The builds of a project from its GitHub repository (docs/specs/github-builds.md).
//
//   - The owner links a repository to a project: repos/<owner>/<name>.json has the project.
//   - An Action of the repository sends POST /api/v1/builds with an OIDC token of GitHub and the
//     manifest. The token gives the repository, so the build goes to the project of the
//     repository. The reply has a grant for the files: x/<project>/builds/<id>/.
//   - The Action uploads the files, then sends POST /api/v1/builds/<id>/done. The API checks
//     the files, and the parts wait for the approval of the owner.
//   - x/<project>/builds/<id>.json is the record of the build.

// The limits of the builds.
const (
	MaxBuildFile  = 200 << 20
	MaxBuildParts = 50
	// BuildsShown is the count of the newest builds on the project page.
	BuildsShown   = 30
	buildGrantTTL = time.Hour
)

// GitHubIssuer is the issuer of the OIDC tokens of GitHub Actions.
const GitHubIssuer = "https://token.actions.githubusercontent.com"

// GitHubClaims are the claims of an OIDC token of GitHub Actions that the API uses.
type GitHubClaims struct {
	Repository string `json:"repository"`
	Ref        string `json:"ref"`
	SHA        string `json:"sha"`
	EventName  string `json:"event_name"`
	RunID      string `json:"run_id"`
	jwt.RegisteredClaims
}

// OIDCVerifier checks an OIDC token of GitHub Actions for the audience, and returns its claims.
type OIDCVerifier interface {
	Verify(ctx context.Context, token, audience string) (*GitHubClaims, error)
}

// GitHubOIDC checks the tokens with the keys of GitHub (JWKS), which it keeps for an hour.
type GitHubOIDC struct {
	// Issuer is GitHubIssuer, or the address of a test server.
	Issuer string
	HTTP   *http.Client

	mu    sync.Mutex
	keys  map[string]*rsa.PublicKey
	until time.Time
}

func (g *GitHubOIDC) issuer() string {
	if g.Issuer != "" {
		return g.Issuer
	}
	return GitHubIssuer
}

// key returns the public key with the ID kid, and reads the keys again for a key that it does
// not know.
func (g *GitHubOIDC) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	g.mu.Lock()
	k, ok := g.keys[kid]
	fresh := time.Now().Before(g.until)
	g.mu.Unlock()
	if ok && fresh {
		return k, nil
	}
	keys, err := g.fetch(ctx)
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	g.keys, g.until = keys, time.Now().Add(time.Hour)
	g.mu.Unlock()
	if k, ok := keys[kid]; ok {
		return k, nil
	}
	return nil, errors.New("no such key of GitHub")
}

func (g *GitHubOIDC) fetch(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	hc := g.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.issuer()+"/.well-known/jwks", nil)
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the keys of GitHub: %s", resp.Status)
	}
	var set struct {
		Keys []struct {
			Kty, Kid, N, E string
		} `json:"keys"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20)).Decode(&set); err != nil {
		return nil, err
	}
	out := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" {
			continue
		}
		n, err1 := base64.RawURLEncoding.DecodeString(k.N)
		e, err2 := base64.RawURLEncoding.DecodeString(k.E)
		if err1 != nil || err2 != nil || len(e) > 4 {
			continue
		}
		out[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	return out, nil
}

// Verify checks the signature (RS256), the issuer, the audience and the times of the token.
func (g *GitHubOIDC) Verify(ctx context.Context, token, audience string) (*GitHubClaims, error) {
	var c GitHubClaims
	_, err := jwt.ParseWithClaims(token, &c, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		return g.key(ctx, kid)
	}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(g.issuer()), jwt.WithAudience(audience),
		jwt.WithExpirationRequired(), jwt.WithLeeway(time.Minute))
	if err != nil {
		return nil, err
	}
	if c.Repository == "" {
		return nil, errors.New("the token has no repository")
	}
	return &c, nil
}

// BuildRecord is x/<project>/builds/<id>.json.
type BuildRecord struct {
	ID      string      `json:"id"`
	Repo    string      `json:"repo"`
	Commit  string      `json:"commit"`
	Ref     string      `json:"ref"`
	Kind    string      `json:"kind"`
	PR      int         `json:"pr,omitempty"`
	Tag     string      `json:"tag,omitempty"`
	Branch  string      `json:"branch,omitempty"`
	RunID   string      `json:"run_id,omitempty"`
	Created time.Time   `json:"created"`
	Ready   bool        `json:"ready"`
	Parts   []buildPart `json:"parts"`
}

// buildPart is a part of a build record, with its state.
type buildPart struct {
	cloud.BuildPart
	State  string    `json:"state"`
	By     string    `json:"by,omitempty"`
	At     time.Time `json:"at,omitzero"`
	Reason string    `json:"reason,omitempty"`
}

func buildKey(project, id string) string { return "x/" + project + "/builds/" + id + ".json" }
func buildDir(project, id string) string { return "x/" + project + "/builds/" + id + "/" }

// repoKey is the file of the project of a repository: repos/<owner>/<name>.json, in lower case.
func repoKey(repo string) string { return "repos/" + strings.ToLower(repo) + ".json" }

var (
	repoName = regexp.MustCompile(`^[A-Za-z0-9-]{1,39}/[A-Za-z0-9._-]{1,100}$`)
	// fileName is the name of a file of a build: no folders.
	fileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,120}$`)
	hexSHA   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)
	pullRef  = regexp.MustCompile(`^refs/pull/([0-9]+)/`)
)

// repoRecord is repos/<owner>/<name>.json.
type repoRecord struct {
	Project string `json:"project"`
	Repo    string `json:"repo"`
}

// setRepo links the repository repo to the project p, or removes the link for "".
func (s *Server) setRepo(ctx context.Context, p *Project, repo string) (*Project, error) {
	repo = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(repo), "https://github.com/"), ".git"))
	repo = strings.Trim(repo, "/")
	if repo != "" && !repoName.MatchString(repo) {
		return nil, fail(http.StatusBadRequest, "the repository needs the form owner/name, for example beetlebugorg/bittlex")
	}
	if repo != "" {
		b, _, err := s.Store.Get(ctx, repoKey(repo))
		if err == nil {
			var rec repoRecord
			if json.Unmarshal(b, &rec) == nil && rec.Project != p.ID {
				if _, _, err := s.loadProject(ctx, rec.Project); err == nil {
					return nil, fail(http.StatusConflict, "another project has this repository")
				}
			}
		} else if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
	}
	old := p.Repo
	np, err := s.changeProject(ctx, p.ID, func(q *Project) error {
		q.Repo = repo
		return nil
	})
	if err != nil {
		return nil, err
	}
	if old != "" && !strings.EqualFold(old, repo) {
		s.Store.Delete(ctx, repoKey(old))
	}
	if repo != "" {
		if err := s.putJSON(ctx, repoKey(repo), repoRecord{Project: p.ID, Repo: repo}); err != nil {
			return nil, err
		}
	}
	return np, nil
}

// buildClaims returns the claims of the OIDC token of the request.
func (s *Server) buildClaims(ctx context.Context, r *http.Request) (*GitHubClaims, error) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || tok == "" {
		return nil, fail(http.StatusUnauthorized, "send the OIDC token of GitHub Actions")
	}
	v := s.OIDC
	if v == nil {
		v = defaultOIDC
	}
	u, _ := url.Parse(s.base())
	c, err := v.Verify(ctx, tok, u.Hostname())
	if err != nil {
		return nil, fail(http.StatusUnauthorized, "the token of GitHub does not check: "+err.Error())
	}
	return c, nil
}

var defaultOIDC = &GitHubOIDC{}

// projectOfRepo returns the project of the repository.
func (s *Server) projectOfRepo(ctx context.Context, repo string) (*Project, error) {
	b, _, err := s.Store.Get(ctx, repoKey(repo))
	if errors.Is(err, ErrNotFound) {
		return nil, fail(http.StatusForbidden, "no project has the repository "+repo+": link it on the project page")
	}
	if err != nil {
		return nil, err
	}
	var rec repoRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		return nil, err
	}
	p, _, err := s.loadProject(ctx, rec.Project)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(p.Repo, repo) {
		return nil, fail(http.StatusForbidden, "no project has the repository "+repo)
	}
	return p, nil
}

// routeBuilds serves /api/v1/builds and /api/v1/builds/<id>/done, for an Action of GitHub.
func (s *Server) routeBuilds(ctx context.Context, r *http.Request, body []byte, rest string) (any, error) {
	if r.Method != http.MethodPost {
		return nil, fail(http.StatusMethodNotAllowed, "no such request")
	}
	c, err := s.buildClaims(ctx, r)
	if err != nil {
		return nil, err
	}
	p, err := s.projectOfRepo(ctx, c.Repository)
	if err != nil {
		return nil, err
	}
	switch {
	case rest == "":
		var in cloud.BuildManifest
		if json.Unmarshal(body, &in) != nil {
			return nil, fail(http.StatusBadRequest, "bad JSON")
		}
		return s.newBuild(ctx, p, c, in)
	case strings.HasSuffix(rest, "/done"):
		return s.buildDone(ctx, p, c, strings.TrimSuffix(rest, "/done"))
	}
	return nil, fail(http.StatusMethodNotAllowed, "no such request")
}

// newBuild makes the record of a build of the project p from the claims and the manifest, and
// returns the grant for its files.
func (s *Server) newBuild(ctx context.Context, p *Project, c *GitHubClaims, in cloud.BuildManifest) (any, error) {
	if len(in.Parts) == 0 || len(in.Parts) > MaxBuildParts {
		return nil, fail(http.StatusBadRequest, fmt.Sprintf("a build has 1 to %d parts", MaxBuildParts))
	}
	rec := BuildRecord{ID: newID(10), Repo: c.Repository, Commit: c.SHA, Ref: c.Ref, RunID: clip(c.RunID, 30), Created: s.now().UTC()}
	if in.Commit != "" {
		rec.Commit = in.Commit
	}
	if !hexSHA.MatchString(rec.Commit) {
		return nil, fail(http.StatusBadRequest, "the commit needs 40 hex digits")
	}
	switch {
	case strings.HasPrefix(c.Ref, "refs/tags/"):
		rec.Kind, rec.Tag = cloud.BuildRelease, clip(strings.TrimPrefix(c.Ref, "refs/tags/"), 100)
	case pullRef.MatchString(c.Ref):
		rec.Kind = cloud.BuildTest
		rec.PR, _ = strconv.Atoi(pullRef.FindStringSubmatch(c.Ref)[1])
	case strings.HasPrefix(c.Ref, "refs/heads/"):
		rec.Branch = clip(strings.TrimPrefix(c.Ref, "refs/heads/"), 100)
		rec.Kind = cloud.BuildBranch
		if rec.Branch == "main" || rec.Branch == "master" {
			rec.Kind = cloud.BuildMain
		}
	default:
		return nil, fail(http.StatusBadRequest, "a build needs a branch, a tag or a pull request")
	}
	if in.PR > 0 && rec.Kind == cloud.BuildTest && in.PR != rec.PR {
		return nil, fail(http.StatusBadRequest, "the pull request is not the pull request of the token")
	}
	seen := map[string]bool{}
	for _, bp := range in.Parts {
		bp.Name, bp.Printer, bp.Material, bp.Profile, bp.Slicer =
			clip(strings.TrimSpace(bp.Name), 120), clip(bp.Printer, 40), clip(bp.Material, 80), clip(bp.Profile, 200), clip(bp.Slicer, 80)
		if bp.Name == "" || bp.Printer == "" {
			return nil, fail(http.StatusBadRequest, "each part needs a name and a printer")
		}
		if bp.Process != "fdm" && bp.Process != "resin" {
			return nil, fail(http.StatusBadRequest, "the process of a part is fdm or resin")
		}
		for _, f := range []string{bp.File, bp.Preview, bp.Picture} {
			if f == "" {
				continue
			}
			if !fileName.MatchString(f) || f == "build.json" {
				return nil, fail(http.StatusBadRequest, "a bad file name: "+f)
			}
		}
		if bp.File == "" || seen[bp.File] {
			return nil, fail(http.StatusBadRequest, "each part needs a file of its own")
		}
		seen[bp.File] = true
		if bp.Size <= 0 || bp.Size > MaxBuildFile || !sha256Re.MatchString(bp.SHA256) {
			return nil, fail(http.StatusBadRequest, "each part needs the size (up to 200 MB) and the SHA-256 of its file")
		}
		bp.Count = max(1, min(bp.Count, 100))
		rec.Parts = append(rec.Parts, buildPart{BuildPart: bp, State: cloud.PartWaiting})
	}
	g, err := s.Grants.Grant(ctx, buildDir(p.ID, rec.ID), MaxBuildFile, buildGrantTTL)
	if err != nil {
		return nil, err
	}
	if err := s.putJSON(ctx, buildKey(p.ID, rec.ID), rec); err != nil {
		return nil, err
	}
	return cloud.BuildReply{ID: rec.ID, URL: s.buildURL(p, rec.ID), Grant: g}, nil
}

// buildURL is the build page.
func (s *Server) buildURL(p *Project, id string) string {
	return s.projectURL(p) + "/builds/" + id
}

// buildDone checks the files of the build id, and makes it ready. The older builds of the same
// pull request or branch with parts that wait get the state replaced.
func (s *Server) buildDone(ctx context.Context, p *Project, c *GitHubClaims, id string) (any, error) {
	rec, err := s.loadBuild(ctx, p.ID, id)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(rec.Repo, c.Repository) {
		return nil, fail(http.StatusForbidden, "the build is of another repository")
	}
	keys, err := s.Store.List(ctx, buildDir(p.ID, id))
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, k := range keys {
		have[path.Base(k)] = true
	}
	for _, bp := range rec.Parts {
		for _, f := range []string{bp.File, bp.Preview, bp.Picture} {
			if f != "" && !have[f] {
				return nil, fail(http.StatusBadRequest, "the file "+f+" is not up")
			}
		}
	}
	rec.Ready = true
	if err := s.putJSON(ctx, buildKey(p.ID, id), rec); err != nil {
		return nil, err
	}
	builds, err := s.loadBuilds(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	for _, o := range builds {
		if o.ID == rec.ID || !o.Ready || o.Kind == cloud.BuildRelease || o.Kind != rec.Kind ||
			o.PR != rec.PR || o.Branch != rec.Branch || !o.Created.Before(rec.Created) {
			continue
		}
		changed := false
		for i := range o.Parts {
			if o.Parts[i].State == cloud.PartWaiting {
				o.Parts[i].State, changed = cloud.PartReplaced, true
			}
		}
		if changed {
			if err := s.putJSON(ctx, buildKey(p.ID, o.ID), o); err != nil {
				return nil, err
			}
		}
	}
	return s.projectBuild(ctx, p, rec), nil
}

func (s *Server) loadBuild(ctx context.Context, project, id string) (*BuildRecord, error) {
	if !validID(id) {
		return nil, fail(http.StatusGone, "no such build")
	}
	b, _, err := s.Store.Get(ctx, buildKey(project, id))
	if errors.Is(err, ErrNotFound) {
		return nil, fail(http.StatusGone, "no such build")
	}
	if err != nil {
		return nil, err
	}
	var rec BuildRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// loadBuilds returns the builds of the project, the newest first.
func (s *Server) loadBuilds(ctx context.Context, project string) ([]*BuildRecord, error) {
	keys, err := s.Store.List(ctx, "x/"+project+"/builds/")
	if err != nil {
		return nil, err
	}
	var out []*BuildRecord
	for _, k := range keys {
		rel := strings.TrimPrefix(k, "x/"+project+"/builds/")
		if strings.Contains(rel, "/") || !strings.HasSuffix(rel, ".json") {
			continue
		}
		if rec, err := s.loadBuild(ctx, project, strings.TrimSuffix(rel, ".json")); err == nil {
			out = append(out, rec)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}

// projectBuild is a build for the pages, with the addresses of its files.
func (s *Server) projectBuild(ctx context.Context, p *Project, rec *BuildRecord) cloud.ProjectBuild {
	out := cloud.ProjectBuild{ID: rec.ID, Repo: rec.Repo, Commit: rec.Commit, Ref: rec.Ref, Kind: rec.Kind, PR: rec.PR,
		Tag: rec.Tag, Branch: rec.Branch, Created: rec.Created, Ready: rec.Ready, Parts: []cloud.ProjectBuildPart{}}
	if rec.RunID != "" {
		out.Run = "https://github.com/" + rec.Repo + "/actions/runs/" + rec.RunID
	}
	dir := buildDir(p.ID, rec.ID)
	for _, bp := range rec.Parts {
		pp := cloud.ProjectBuildPart{BuildPart: bp.BuildPart, State: bp.State, By: bp.By, At: bp.At, Reason: bp.Reason}
		if rec.Ready {
			if bp.Preview != "" {
				pp.PreviewURL, _ = s.Grants.FileURL(ctx, dir+bp.Preview, urlTTL)
			}
			if bp.Picture != "" {
				pp.PictureURL, _ = s.Grants.FileURL(ctx, dir+bp.Picture, urlTTL)
			}
		}
		out.Parts = append(out.Parts, pp)
	}
	return out
}

// decideBuildPart approves or rejects the part n of the build id of the project p, for the
// owner.
func (s *Server) decideBuildPart(ctx context.Context, p *Project, w *who, id string, n int, approve bool, reason string) (any, error) {
	if err := ownerOnly(w); err != nil {
		return nil, err
	}
	rec, err := s.loadBuild(ctx, p.ID, id)
	if err != nil {
		return nil, err
	}
	if !rec.Ready {
		return nil, fail(http.StatusConflict, "the files of the build are not all up yet")
	}
	if n < 0 || n >= len(rec.Parts) {
		return nil, fail(http.StatusGone, "no such part")
	}
	part := &rec.Parts[n]
	if part.State == cloud.PartReplaced {
		return nil, fail(http.StatusConflict, "a newer build replaced this build")
	}
	part.State, part.By, part.At, part.Reason = cloud.PartApproved, w.name, s.now().UTC(), ""
	if !approve {
		part.State, part.Reason = cloud.PartRejected, clip(strings.TrimSpace(reason), 1000)
		if part.Reason == "" {
			return nil, fail(http.StatusBadRequest, "a rejection needs a reason")
		}
	}
	if err := s.putJSON(ctx, buildKey(p.ID, id), rec); err != nil {
		return nil, err
	}
	return s.projectBuild(ctx, p, rec), nil
}
