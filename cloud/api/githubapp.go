package api

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// GitHubApp starts the slice runs of the builds with the GitHub App of the service
// (docs/specs/github-builds.md). The App has one permission on a repository: Actions, read and
// write. The owner installs it on the repositories of the projects.
//
// For each build, the service signs a JWT of the App (10 minutes), finds the installation of the
// App on the repository, gets a token of the installation for that one repository, and starts
// the workflow printdeck.yml on the default branch with the input build.
type GitHubApp struct {
	AppID int64
	Key   *rsa.PrivateKey
	// API is the address of the API of GitHub, or "" for https://api.github.com.
	API  string
	HTTP *http.Client
	// Now is the clock, or nil for time.Now.
	Now func() time.Time
}

// GitHubAppSecret is the JSON of the secret of the App in Secrets Manager.
type GitHubAppSecret struct {
	AppID      int64  `json:"app_id"`
	PrivateKey string `json:"private_key"` // PEM
}

func (g *GitHubApp) api() string {
	if g.API != "" {
		return strings.TrimSuffix(g.API, "/")
	}
	return "https://api.github.com"
}

func (g *GitHubApp) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

// appToken is the JWT of the App: 60 s before now (for a clock that runs ahead) to 9 minutes
// after now. GitHub takes at most 10 minutes.
func (g *GitHubApp) appToken() (string, error) {
	now := g.now()
	t := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": strconv.FormatInt(g.AppID, 10),
	})
	return t.SignedString(g.Key)
}

// call sends a request to the API of GitHub with the token, and reads the reply into out.
func (g *GitHubApp) call(ctx context.Context, method, path, token string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.api()+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := g.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		var e struct {
			Message string `json:"message"`
		}
		json.Unmarshal(data, &e)
		if e.Message == "" {
			e.Message = resp.Status
		}
		return &gitHubError{status: resp.StatusCode, msg: e.Message}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

type gitHubError struct {
	status int
	msg    string
}

func (e *gitHubError) Error() string { return fmt.Sprintf("GitHub %d: %s", e.status, e.msg) }

// installationToken returns a token of the installation of the App on repo, for that one
// repository and with only the permissions perms.
func (g *GitHubApp) installationToken(ctx context.Context, repo string, perms map[string]string) (string, error) {
	app, err := g.appToken()
	if err != nil {
		return "", err
	}
	var inst struct {
		ID int64 `json:"id"`
	}
	if err := g.call(ctx, http.MethodGet, "/repos/"+repo+"/installation", app, nil, &inst); err != nil {
		if ge, ok := err.(*gitHubError); ok && ge.status == http.StatusNotFound {
			return "", fmt.Errorf("the Printdeck GitHub App is not installed on %s: install it there", repo)
		}
		return "", err
	}
	_, name, _ := strings.Cut(repo, "/")
	var tok struct {
		Token string `json:"token"`
	}
	req := map[string]any{"repositories": []string{name}, "permissions": perms}
	if err := g.call(ctx, http.MethodPost, fmt.Sprintf("/app/installations/%d/access_tokens", inst.ID), app, req, &tok); err != nil {
		return "", err
	}
	return tok.Token, nil
}

// OpenPull returns the number of the open pull request into base from the branch of headRepo
// whose head is the commit sha, or 0 for none.
func (g *GitHubApp) OpenPull(ctx context.Context, base, headRepo, branch, sha string) (int, error) {
	token, err := g.installationToken(ctx, base, map[string]string{"pull_requests": "read", "metadata": "read"})
	if err != nil {
		return 0, err
	}
	owner, _, _ := strings.Cut(headRepo, "/")
	var pulls []struct {
		Number int `json:"number"`
		Head   struct {
			SHA  string `json:"sha"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
	}
	q := "/repos/" + base + "/pulls?state=open&per_page=100&head=" + url.QueryEscape(owner+":"+branch)
	if err := g.call(ctx, http.MethodGet, q, token, nil, &pulls); err != nil {
		return 0, err
	}
	for _, p := range pulls {
		if p.Head.SHA == sha && strings.EqualFold(p.Head.Repo.FullName, headRepo) {
			return p.Number, nil
		}
	}
	return 0, nil
}

// Dispatch starts the slice run of the build in the repository repo ("owner/name"), with the
// title of the run. A workflow from before the input title gets the build only.
func (g *GitHubApp) Dispatch(ctx context.Context, repo, build, title string) error {
	// A token for this one repository, with only the permission to start workflows.
	token, err := g.installationToken(ctx, repo, map[string]string{"actions": "write", "metadata": "read"})
	if err != nil {
		return err
	}
	tok := struct{ Token string }{token}
	var r struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := g.call(ctx, http.MethodGet, "/repos/"+repo, tok.Token, nil, &r); err != nil {
		return err
	}
	if r.DefaultBranch == "" {
		r.DefaultBranch = "main"
	}
	return g.dispatch(ctx, repo, build, title, tok.Token, r.DefaultBranch)
}

// Repo returns the number, the default branch and the visibility of repo. A reply with no
// field "private" or with another visibility than "public" gives a repository that is not public.
func (g *GitHubApp) Repo(ctx context.Context, repo string) (RepoInfo, error) {
	token, err := g.installationToken(ctx, repo, map[string]string{"metadata": "read"})
	if err != nil {
		return RepoInfo{}, err
	}
	var r struct {
		ID            int64  `json:"id"`
		DefaultBranch string `json:"default_branch"`
		Private       *bool  `json:"private"`
		Visibility    string `json:"visibility"`
	}
	if err := g.call(ctx, http.MethodGet, "/repos/"+repo, token, nil, &r); err != nil {
		return RepoInfo{}, err
	}
	if r.ID == 0 || r.DefaultBranch == "" {
		return RepoInfo{}, fmt.Errorf("%s has no number or no default branch", repo)
	}
	return RepoInfo{ID: strconv.FormatInt(r.ID, 10), DefaultBranch: r.DefaultBranch,
		Public: r.Private != nil && !*r.Private && r.Visibility == "public"}, nil
}

// dispatch starts the workflow on the branch.
func (g *GitHubApp) dispatch(ctx context.Context, repo, build, title, token, branch string) error {
	path := "/repos/" + repo + "/actions/workflows/printdeck.yml/dispatches"
	in := map[string]any{"ref": branch, "inputs": map[string]string{"build": build, "title": title}}
	err := g.call(ctx, http.MethodPost, path, token, in, nil)
	if ge, ok := err.(*gitHubError); ok && ge.status == http.StatusUnprocessableEntity && strings.Contains(ge.msg, "Unexpected inputs") {
		in["inputs"] = map[string]string{"build": build}
		err = g.call(ctx, http.MethodPost, path, token, in, nil)
	}
	if err != nil {
		if ge, ok := err.(*gitHubError); ok && ge.status == http.StatusNotFound {
			return fmt.Errorf("%s has no workflow .github/workflows/printdeck.yml on %s", repo, branch)
		}
		return err
	}
	return nil
}

// PullState returns the state of the pull request pr of repo: "open", "closed" or "merged".
func (g *GitHubApp) PullState(ctx context.Context, repo string, pr int) (string, error) {
	token, err := g.installationToken(ctx, repo, map[string]string{"pull_requests": "read", "metadata": "read"})
	if err != nil {
		return "", err
	}
	var out struct {
		State  string `json:"state"`
		Merged bool   `json:"merged"`
	}
	if err := g.call(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/pulls/%d", repo, pr), token, nil, &out); err != nil {
		return "", err
	}
	if out.Merged {
		return "merged", nil
	}
	return out.State, nil
}

// Comment writes the comment of Printdeck on the pull request pr of repo: it changes the comment
// id, or makes a new comment for 0 or a comment that is gone. It returns the ID of the comment.
func (g *GitHubApp) Comment(ctx context.Context, repo string, pr int, id int64, body string) (int64, error) {
	token, err := g.installationToken(ctx, repo, map[string]string{"pull_requests": "write", "metadata": "read"})
	if err != nil {
		return 0, err
	}
	var out struct {
		ID int64 `json:"id"`
	}
	in := map[string]string{"body": body}
	if id != 0 {
		err := g.call(ctx, http.MethodPatch, fmt.Sprintf("/repos/%s/issues/comments/%d", repo, id), token, in, &out)
		if ge, ok := err.(*gitHubError); !ok || ge.status != http.StatusNotFound {
			return out.ID, err
		}
	}
	err = g.call(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/issues/%d/comments", repo, pr), token, in, &out)
	return out.ID, err
}
