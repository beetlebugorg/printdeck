package api

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

// The App signs its JWT, gets a token of its installation for the one repository with only
// Actions, and starts printdeck.yml on the default branch with the build.
func TestGitHubAppDispatch(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	var dispatched map[string]any
	oldWorkflow := false
	var asked map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		switch {
		case r.URL.Path == "/repos/beetlebugorg/bittlex/installation":
			tk, err := jwt.Parse(auth, func(*jwt.Token) (any, error) { return &key.PublicKey, nil })
			if err != nil || tk.Claims.(jwt.MapClaims)["iss"] != "123" {
				http.Error(w, `{"message":"bad JWT"}`, 401)
				return
			}
			w.Write([]byte(`{"id": 77}`))
		case r.URL.Path == "/repos/other/repo/installation":
			http.Error(w, `{"message":"Not Found"}`, 404)
		case r.URL.Path == "/app/installations/77/access_tokens" && r.Method == "POST":
			json.NewDecoder(r.Body).Decode(&asked)
			w.Write([]byte(`{"token": "inst-token"}`))
		case r.URL.Path == "/repos/beetlebugorg/bittlex" && auth == "inst-token":
			w.Write([]byte(`{"default_branch": "main"}`))
		case r.URL.Path == "/repos/beetlebugorg/bittlex/actions/workflows/printdeck.yml/dispatches" && auth == "inst-token":
			dispatched = nil
			json.NewDecoder(r.Body).Decode(&dispatched)
			if _, ok := dispatched["inputs"].(map[string]any)["title"]; ok && oldWorkflow {
				http.Error(w, `{"message":"Unexpected inputs provided: [\"title\"]"}`, 422)
				return
			}
			w.WriteHeader(204)
		default:
			http.Error(w, `{"message":"unexpected"}`, 500)
		}
	}))
	defer srv.Close()
	g := &GitHubApp{AppID: 123, Key: key, API: srv.URL}
	if err := g.Dispatch(context.Background(), "beetlebugorg/bittlex", "b1", "Pod door · PR #7"); err != nil {
		t.Fatal(err)
	}
	if in := dispatched["inputs"].(map[string]any); dispatched["ref"] != "main" || in["build"] != "b1" || in["title"] != "Pod door · PR #7" {
		t.Errorf("dispatch %v", dispatched)
	}
	// A workflow from before the input title.
	oldWorkflow = true
	if err := g.Dispatch(context.Background(), "beetlebugorg/bittlex", "b2", "x"); err != nil {
		t.Fatalf("an old workflow: %v", err)
	}
	if in := dispatched["inputs"].(map[string]any); in["build"] != "b2" || in["title"] != nil {
		t.Errorf("the dispatch for an old workflow %v", dispatched)
	}
	if p := asked["permissions"].(map[string]any); p["actions"] != "write" || len(p) != 2 ||
		asked["repositories"].([]any)[0] != "bittlex" {
		t.Errorf("token request %v", asked)
	}
	if err := g.Dispatch(context.Background(), "other/repo", "b1", ""); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("a repository with no App: %v", err)
	}
}
