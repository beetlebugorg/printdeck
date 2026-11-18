package octoapi

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"printdeck/internal/signing"
)

func sigKey(b byte) ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{b}, ed25519.SeedSize))
}

func pubText(k ed25519.PrivateKey) string {
	return signing.PublicText(k.Public().(ed25519.PublicKey))
}

// signed signs body as zslice does (src/gcode_sign.zig).
func signed(k ed25519.PrivateKey, body string) string {
	h := sha256.Sum256([]byte(body))
	id := signing.KeyID(k.Public().(ed25519.PublicKey))
	return body + signing.LinePrefix + "ed25519 " + id + " " + base64.StdEncoding.EncodeToString(ed25519.Sign(k, h[:])) + "\n"
}

// newSigned is a test server with "Signed prints only" on and the key of seed 1 trusted.
func newSigned(t *testing.T) (*Server, string, *fakePrinter, *[]string) {
	t.Helper()
	s, ts, p := newTest(t)
	s.Signing = &signing.Store{Path: filepath.Join(t.TempDir(), "signing.json")}
	if _, err := s.Signing.Add("zslice", pubText(sigKey(1)), time.Now()); err != nil {
		t.Fatal(err)
	}
	s.Signing.SetSignedOnly(true)
	var shown []string
	s.Rejected = func(name, reason string) { shown = append(shown, name+": "+reason) }
	return s, ts.URL, p, &shown
}

func TestSignedOnlyRejectsAnUnsignedFile(t *testing.T) {
	s, url, p, shown := newSigned(t)
	r, m := upload(t, url, mainKey, "a.gcode", "G28\n", map[string]string{"print": "true"})
	want := "Rejected: not signed by a trusted slicer (the file has no signature)"
	if r.StatusCode != http.StatusUnprocessableEntity || m["error"] != want {
		t.Fatalf("upload %d %v", r.StatusCode, m)
	}
	if len(*shown) != 1 || (*shown)[0] != "a.gcode: "+want {
		t.Errorf("screen %v", *shown)
	}
	if len(p.printed) != 0 || len(s.Store.Files()) != 0 {
		t.Errorf("printed %v, files %v", p.printed, s.Store.Files())
	}
}

func TestSignedOnlyRejectsAnUnknownKeyAndAChangedFile(t *testing.T) {
	s, url, p, shown := newSigned(t)
	other := signed(sigKey(2), "G28\n")
	id := signing.KeyID(sigKey(2).Public().(ed25519.PublicKey))
	r, m := upload(t, url, mainKey, "b.gcode", other, nil)
	if r.StatusCode != http.StatusUnprocessableEntity || m["error"] != "Rejected: not signed by a trusted slicer (key "+id+")" {
		t.Fatalf("unknown key: %d %v", r.StatusCode, m)
	}
	changed := strings.Replace(signed(sigKey(1), "G28\nM104 S200\n"), "S200", "S300", 1)
	r, m = upload(t, url, mainKey, "c.gcode", changed, map[string]string{"print": "true"})
	if r.StatusCode != http.StatusUnprocessableEntity || !strings.HasPrefix(m["error"].(string), signing.Rejected) {
		t.Fatalf("changed: %d %v", r.StatusCode, m)
	}
	if len(*shown) != 2 || len(p.printed) != 0 || len(s.Store.Files()) != 0 {
		t.Errorf("screen %v, printed %v, files %v", *shown, p.printed, s.Store.Files())
	}
}

// A rejected upload leaves a good file of the same name as it was.
func TestSignedOnlyKeepsTheOldFile(t *testing.T) {
	s, url, _, _ := newSigned(t)
	good := signed(sigKey(1), "G28\n")
	if r, m := upload(t, url, mainKey, "d.gcode", good, nil); r.StatusCode != http.StatusCreated {
		t.Fatalf("good: %d %v", r.StatusCode, m)
	}
	if r, _ := upload(t, url, mainKey, "d.gcode", "G1 X999\n", nil); r.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("bad: %d", r.StatusCode)
	}
	full, _ := s.Store.FullPath("d.gcode")
	if b, _ := os.ReadFile(full); string(b) != good {
		t.Errorf("the file changed: %q", b)
	}
}

func TestSignedOnlyPrintsASignedFile(t *testing.T) {
	_, url, p, shown := newSigned(t)
	r, m := upload(t, url, mainKey, "e.gcode", signed(sigKey(1), "G28\nG1 X10\n"), map[string]string{"print": "true"})
	if r.StatusCode != http.StatusCreated || m["effectivePrint"] != true {
		t.Fatalf("upload %d %v", r.StatusCode, m)
	}
	if len(p.printed) != 1 || len(*shown) != 0 {
		t.Errorf("printed %v, screen %v", p.printed, *shown)
	}
}

// With the setting on, a start of a file that came in before (with the setting off) is checked.
func TestSignedOnlyChecksAStartOfAStoredFile(t *testing.T) {
	s, url, p, _ := newSigned(t)
	s.Signing.SetSignedOnly(false)
	if r, _ := upload(t, url, mainKey, "old.gcode", "G28\n", nil); r.StatusCode != http.StatusCreated {
		t.Fatalf("upload with the setting off: %d", r.StatusCode)
	}
	s.Signing.SetSignedOnly(true)
	r, m := do(t, "POST", url+"/api/files/local/old.gcode", mainKey, strings.NewReader(`{"command":"select","print":true}`), "application/json")
	if r.StatusCode != http.StatusUnprocessableEntity || !strings.HasPrefix(m["error"].(string), signing.Rejected) {
		t.Fatalf("start: %d %v", r.StatusCode, m)
	}
	if len(p.printed) != 0 {
		t.Errorf("printed %v", p.printed)
	}
}

func TestSignedOnlyRefusesGcodeCommands(t *testing.T) {
	s, url, p, _ := newSigned(t)
	r, _ := do(t, "POST", url+"/api/printer/command", mainKey, strings.NewReader(`{"commands":["M104 S250"]}`), "application/json")
	if r.StatusCode != http.StatusForbidden || len(p.gcode) != 0 {
		t.Fatalf("command: %d %v", r.StatusCode, p.gcode)
	}
	s.Signing.SetSignedOnly(false)
	if r, _ := do(t, "POST", url+"/api/printer/command", mainKey, strings.NewReader(`{"commands":["M104 S250"]}`), "application/json"); r.StatusCode != http.StatusNoContent {
		t.Errorf("setting off: %d", r.StatusCode)
	}
}

// The setting is off by default: an unsigned file goes in, as before.
func TestSigningOffTakesUnsignedFiles(t *testing.T) {
	s, ts, p := newTest(t)
	s.Signing = &signing.Store{Path: filepath.Join(t.TempDir(), "signing.json")}
	if r, m := upload(t, ts.URL, mainKey, "a.gcode", "G28\n", map[string]string{"print": "true"}); r.StatusCode != http.StatusCreated || len(p.printed) != 1 {
		t.Fatalf("upload %d %v", r.StatusCode, m)
	}
	if _, m := do(t, "GET", ts.URL+"/api/plugin/printdeck", mainKey, nil, ""); m["signed_only"] != false {
		t.Errorf("signed_only %v", m["signed_only"])
	}
}

// "Get a key" with a public key: Allow adds the key to the trusted keys.
func TestAppKeysTrustThePublicKey(t *testing.T) {
	s, ts, _ := newTest(t)
	s.Signing = &signing.Store{Path: filepath.Join(t.TempDir(), "signing.json")}
	id := signing.KeyID(sigKey(3).Public().(ed25519.PublicKey))
	asked := make(chan func(bool), 1)
	s.Ask = func(q, yes, no string, answer func(bool)) {
		if q != "Allow zslice to use the Printdeck, and trust its prints (key "+id+")?" {
			t.Errorf("question %q", q)
		}
		asked <- answer
	}
	if r, _ := do(t, "POST", ts.URL+"/plugin/appkeys/request", "", strings.NewReader(`{"app":"zslice","public_key":"ed25519 AAAA"}`), "application/json"); r.StatusCode != http.StatusBadRequest {
		t.Errorf("a bad key: %d", r.StatusCode)
	}
	_, m := do(t, "POST", ts.URL+"/plugin/appkeys/request", "", strings.NewReader(`{"app":"zslice","public_key":"`+pubText(sigKey(3))+`"}`), "application/json")
	if len(s.Signing.Keys()) != 0 {
		t.Fatal("a key before the answer")
	}
	(<-asked)(true)
	r, m := do(t, "GET", ts.URL+"/plugin/appkeys/request/"+m["app_token"].(string), "", nil, "")
	if r.StatusCode != 200 || m["trusted_key"] != id {
		t.Fatalf("poll %d %v", r.StatusCode, m)
	}
	if k := s.Signing.Keys(); len(k) != 1 || k[0].ID != id || k[0].App != "zslice" {
		t.Errorf("keys %+v", k)
	}
	// Deny adds no key.
	s.Ask = func(q, yes, no string, answer func(bool)) { answer(false) }
	do(t, "POST", ts.URL+"/plugin/appkeys/request", "", strings.NewReader(`{"app":"x","public_key":"`+pubText(sigKey(4))+`"}`), "application/json")
	if len(s.Signing.Keys()) != 1 {
		t.Errorf("deny added a key: %+v", s.Signing.Keys())
	}
}
