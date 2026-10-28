package cloud

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// A signature checks with the right key, method, path, body and time, and fails with any
// change.
func TestSignVerify(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(nil)
	now := time.Unix(1_800_000_000, 0)
	h := http.Header{}
	Sign(h, key, "deck1", "POST", "/api/v1/shares?x=1", []byte(`{"a":1}`), now)
	if err := Verify(pub, h, "POST", "/api/v1/shares?x=1", []byte(`{"a":1}`), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"body":   Verify(pub, h, "POST", "/api/v1/shares?x=1", []byte(`{"a":2}`), now),
		"path":   Verify(pub, h, "POST", "/api/v1/shares", []byte(`{"a":1}`), now),
		"method": Verify(pub, h, "PUT", "/api/v1/shares?x=1", []byte(`{"a":1}`), now),
		"clock":  Verify(pub, h, "POST", "/api/v1/shares?x=1", []byte(`{"a":1}`), now.Add(6*time.Minute)),
	} {
		if err == nil {
			t.Errorf("%s: a changed request checks", name)
		}
	}
	other, _, _ := ed25519.GenerateKey(nil)
	if err := Verify(other, h, "POST", "/api/v1/shares?x=1", []byte(`{"a":1}`), now); !errors.Is(err, ErrBadSign) {
		t.Errorf("other key: %v", err)
	}
	if err := Verify(pub, http.Header{}, "GET", "/", nil, now); !errors.Is(err, ErrUnsigned) {
		t.Errorf("no headers: %v", err)
	}
	h2 := http.Header{}
	Sign(h2, key, "deck1", "POST", "/api/v1/shares?x=1", []byte(`{"a":1}`), now)
	if h2.Get(HeaderRequest) == h.Get(HeaderRequest) {
		t.Error("two requests with one request ID")
	}
}

// Enroll sends the code and the public key, and the signed requests after it check with that
// key. The identity file reads back the same.
func TestEnrollAndSelf(t *testing.T) {
	var pub ed25519.PublicKey
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/api/v1/decks":
			var in EnrollRequest
			json.Unmarshal(body, &in)
			if in.Code != "ABCD-1234" || in.Name != "Workhorse" {
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(ErrorReply{Error: "bad code"})
				return
			}
			b, _ := base64.StdEncoding.DecodeString(in.PublicKey)
			pub = b
			json.NewEncoder(w).Encode(EnrollReply{Deck: "d-1"})
		case "/api/v1/decks/self":
			if err := Verify(pub, r.Header, r.Method, r.URL.RequestURI(), body, time.Now()); err != nil {
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(ErrorReply{Error: err.Error()})
				return
			}
			json.NewEncoder(w).Encode(DeckInfo{Deck: "d-1", Name: "Workhorse"})
		}
	}))
	defer srv.Close()
	var c Client
	ctx := context.Background()
	if _, err := c.Enroll(ctx, srv.URL, "WRONG", "Workhorse"); err == nil {
		t.Fatal("a wrong code enrolls")
	} else if e, ok := err.(*APIError); !ok || e.Status != 403 || e.Msg != "bad code" {
		t.Fatalf("wrong code: %v", err)
	}
	id, err := c.Enroll(ctx, srv.URL, " ABCD-1234 ", "Workhorse")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cloud.json")
	if err := id.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := LoadIdentity(path)
	if err != nil || back.Deck != "d-1" || string(back.Key) != string(id.Key) {
		t.Fatalf("load: %+v %v", back, err)
	}
	info, err := c.Self(ctx, back)
	if err != nil || info.Deck != "d-1" {
		t.Fatalf("self: %+v %v", info, err)
	}
	if none, err := LoadIdentity(filepath.Join(t.TempDir(), "none.json")); none != nil || err != nil {
		t.Fatalf("no file: %v %v", none, err)
	}
}
