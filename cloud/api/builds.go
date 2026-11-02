package api

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// The link of a project to its GitHub repository, and the check of the OIDC tokens of GitHub
// Actions (docs/specs/github-builds.md). The plans and the builds are in plans.go.
//
//   - The owner links a repository to a project: repos/<owner>/<name>.json has the project.
//   - A request of an Action has an OIDC token of GitHub. The token gives the repository, so the
//     request goes to the project of the repository, and to no other project.

// The limits of the plans and the builds.
const (
	MaxBuildFile = 200 << 20
	MaxPlanParts = 50
	MaxPlanDocs  = 10
	// MaxAssemblies is the most assemblies of a plan, and MaxAssemblyItems the most items of one.
	MaxAssemblies    = 10
	MaxAssemblyItems = 60
	MaxDocFile       = 50 << 20
	MaxPlates        = 20
	MaxPartCount     = 100
	// PlansShown is the count of the newest plans that the view of a project has: the project
	// page shows some of them, and the page of all the plans gets all of them.
	PlansShown    = 10
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
	// RepositoryOwner is the owner of the repository of the run: the member for a fork.
	RepositoryOwner string `json:"repository_owner"`
	// JobWorkflowRef is the workflow file of the job and its ref, for example
	// "beetlebugorg/bittlex/.github/workflows/printdeck.yml@refs/heads/main".
	JobWorkflowRef       string `json:"job_workflow_ref"`
	jwt.RegisteredClaims        // ID is the "jti" of the token
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

// githubLogin is a GitHub username.
var githubLogin = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)

// forkPlan is the project and the pull request of a plan of a member's fork.
type forkPlan struct {
	project *Project
	pr      int
	branch  string
}

// forkProject returns the project of the plan of a fork's token. The token must be of a fork of
// the repository of a project, by a member of that project, from a branch with an open pull
// request into that repository whose head is the commit of the token:
//
//  1. The owner of the fork (repository_owner) is a member of a project with a repository, with
//     that GitHub username, which the owner of the project set. This check needs no call to
//     GitHub, so a stranger stops here.
//  2. GitHub has an open pull request into the repository of the project, from that branch of
//     that fork, with that commit as its head. The pull request proves that the fork is of that
//     repository.
func (s *Server) forkProject(ctx context.Context, c *GitHubClaims) (*forkPlan, error) {
	branch, ok := strings.CutPrefix(c.Ref, "refs/heads/")
	owner := c.RepositoryOwner
	if !ok || branch == "" || !githubLogin.MatchString(owner) || !hexSHA.MatchString(c.SHA) {
		return nil, fail(http.StatusForbidden, "no project has the repository "+c.Repository)
	}
	keys, err := s.Store.List(ctx, "repos/")
	if err != nil {
		return nil, err
	}
	var candidates []*Project
	for _, k := range keys {
		b, _, err := s.Store.Get(ctx, k)
		if err != nil {
			continue
		}
		var rec repoRecord
		if json.Unmarshal(b, &rec) != nil {
			continue
		}
		p, _, err := s.loadProject(ctx, rec.Project)
		if err != nil || !strings.EqualFold(p.Repo, rec.Repo) || strings.EqualFold(p.Repo, c.Repository) {
			continue
		}
		for _, m := range p.Members {
			if !m.Joined.IsZero() && m.GitHub != "" && strings.EqualFold(m.GitHub, owner) {
				candidates = append(candidates, p)
				break
			}
		}
	}
	if len(candidates) == 0 {
		return nil, fail(http.StatusForbidden, "@"+owner+" is not a member of a Printdeck project with a GitHub repository")
	}
	if s.GitHub == nil {
		return nil, fail(http.StatusForbidden, "the plan of a fork needs the GitHub App of the service")
	}
	for _, p := range candidates {
		n, err := s.GitHub.OpenPull(ctx, p.Repo, c.Repository, branch, c.SHA)
		if err != nil || n <= 0 {
			continue
		}
		return &forkPlan{project: p, pr: n, branch: branch}, nil
	}
	return nil, fail(http.StatusForbidden, "no open pull request of "+c.Repository+" ("+branch+") has the commit "+c.SHA[:7])
}

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
	if err := s.useToken(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// tokenKey is the record of a token that the API took: oidc/<SHA-256 of its ID>. A lifecycle rule
// of the bucket deletes the records after a day, when the tokens ended long ago.
func tokenKey(id string) string {
	sum := sha256.Sum256([]byte(id))
	return "oidc/" + hex.EncodeToString(sum[:])
}

// useToken records the ID of the token, and refuses a token that the API took already: each
// token works for one request. The Action asks GitHub for a new token for each request.
func (s *Server) useToken(ctx context.Context, c *GitHubClaims) error {
	if c.ID == "" {
		return fail(http.StatusUnauthorized, "the token of GitHub has no ID")
	}
	_, err := s.Store.Put(ctx, tokenKey(c.ID), []byte(c.Repository), PutOptions{ContentType: "text/plain", IfNoneMatch: true})
	if errors.Is(err, ErrConflict) {
		return fail(http.StatusUnauthorized, "this token of GitHub was used already: send a new token")
	}
	return err
}

// SliceWorkflow is the workflow file that may slice the builds of a repository, on its default
// branch. A run of another file, or of the file on another branch, gets no build.
const SliceWorkflow = ".github/workflows/printdeck.yml"

// sliceRun checks that the token is of the slice workflow of the repository on main (or master):
// the file that the owner reviewed, and not a copy on a branch of a pull request.
func sliceRun(c *GitHubClaims) error {
	ref := strings.ToLower(c.JobWorkflowRef)
	base := strings.ToLower(c.Repository + "/" + SliceWorkflow + "@refs/heads/")
	if ref == base+"main" || ref == base+"master" {
		return nil
	}
	return fail(http.StatusForbidden, "only the workflow "+SliceWorkflow+" on the default branch slices a build, and this job is of "+
		c.JobWorkflowRef)
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
