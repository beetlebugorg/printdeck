package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"printdeck/internal/cloud"
)

// testServer returns a store, and the URL of the API on it.
func testServer(t *testing.T) (*MemStore, *Server, string) {
	st := NewMemStore()
	s := &Server{Store: st}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return st, s, srv.URL
}

// A deck enrolls with a code once. Its signed requests then work, and stop working after
// Disconnect.
func TestEnrollFlow(t *testing.T) {
	st, _, url := testServer(t)
	ctx := context.Background()
	code, err := NewCode(ctx, st, "deck", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 14 || strings.Count(code, "-") != 2 {
		t.Fatalf("code %q", code)
	}
	var c cloud.Client
	id, err := c.Enroll(ctx, url, strings.ToLower(code), "Workhorse")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Enroll(ctx, url, code, "Workhorse"); status(err) != 401 {
		t.Fatalf("second use of a code: %v", err)
	}
	info, err := c.Self(ctx, id)
	if err != nil || info.Deck != id.Deck || info.Name != "Workhorse" {
		t.Fatalf("self: %+v %v", info, err)
	}
	decks, _ := Decks(ctx, st)
	if len(decks) != 1 || decks[0].ID != id.Deck {
		t.Fatalf("decks: %+v", decks)
	}
	if err := c.Disconnect(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Self(ctx, id); status(err) != 401 {
		t.Fatalf("after disconnect: %v", err)
	}
}

// A request with a bad signature, an old time, or a used request ID gets 401.
func TestBadRequests(t *testing.T) {
	st, s, url := testServer(t)
	ctx := context.Background()
	code, _ := NewCode(ctx, st, "deck", time.Now())
	var c cloud.Client
	id, err := c.Enroll(ctx, url, code, "Mini")
	if err != nil {
		t.Fatal(err)
	}
	// Another key, the same deck ID.
	other, _ := c.Enroll(ctx, url, mustCode(t, st), "Other")
	forged := *id
	forged.Key = other.Key
	if _, err := c.Self(ctx, &forged); status(err) != 401 {
		t.Errorf("forged key: %v", err)
	}
	late := cloud.Client{Now: func() time.Time { return time.Now().Add(-10 * time.Minute) }}
	if _, err := late.Self(ctx, id); status(err) != 401 || !strings.Contains(err.Error(), "clock") {
		t.Errorf("old time: %v", err)
	}
	// A replay of one signed request.
	req, _ := http.NewRequest("GET", url+"/api/v1/decks/self", nil)
	cloud.Sign(req.Header, id.PrivateKey(), id.Deck, "GET", "/api/v1/decks/self", nil, time.Now())
	for i, want := range []int{200, 401} {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("send %d: %d, want %d", i, resp.StatusCode, want)
		}
	}
	// An expired code.
	s.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	old, _ := NewCode(ctx, st, "deck", time.Now())
	if _, err := c.Enroll(ctx, url, old, "Late"); status(err) != 401 {
		t.Errorf("expired code: %v", err)
	}
	// The API never replies 403 or 404.
	for _, p := range []string{"/api/v1/nothing", "/api/v1/decks/self"} {
		resp, _ := http.Get(url + p)
		resp.Body.Close()
		if resp.StatusCode == 403 || resp.StatusCode == 404 {
			t.Errorf("%s: %d", p, resp.StatusCode)
		}
	}
}

func mustCode(t *testing.T, st Store) string {
	code, err := NewCode(context.Background(), st, "deck", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func status(err error) int {
	var e *cloud.APIError
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}
