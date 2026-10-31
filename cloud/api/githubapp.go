package api

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// Dispatch starts the slice run of the build in the repository repo ("owner/name").
func (g *GitHubApp) Dispatch(ctx context.Context, repo, build string) error {
	app, err := g.appToken()
	if err != nil {
		return err
	}
	var inst struct {
		ID int64 `json:"id"`
	}
	if err := g.call(ctx, http.MethodGet, "/repos/"+repo+"/installation", app, nil, &inst); err != nil {
		if ge, ok := err.(*gitHubError); ok && ge.status == http.StatusNotFound {
			return fmt.Errorf("the Printdeck GitHub App is not installed on %s: install it there", repo)
		}
		return err
	}
	_, name, _ := strings.Cut(repo, "/")
	var tok struct {
		Token string `json:"token"`
	}
	// A token for this one repository, with only the permission to start workflows.
	req := map[string]any{"repositories": []string{name}, "permissions": map[string]string{"actions": "write", "metadata": "read"}}
	if err := g.call(ctx, http.MethodPost, fmt.Sprintf("/app/installations/%d/access_tokens", inst.ID), app, req, &tok); err != nil {
		return err
	}
	var r struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := g.call(ctx, http.MethodGet, "/repos/"+repo, tok.Token, nil, &r); err != nil {
		return err
	}
	if r.DefaultBranch == "" {
		r.DefaultBranch = "main"
	}
	in := map[string]any{"ref": r.DefaultBranch, "inputs": map[string]string{"build": build}}
	if err := g.call(ctx, http.MethodPost, "/repos/"+repo+"/actions/workflows/printdeck.yml/dispatches", tok.Token, in, nil); err != nil {
		if ge, ok := err.(*gitHubError); ok && ge.status == http.StatusNotFound {
			return fmt.Errorf("%s has no workflow .github/workflows/printdeck.yml on %s", repo, r.DefaultBranch)
		}
		return err
	}
	return nil
}
