package octoapi

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// A request at the root of a deck gives a deck key: it works on the API of each printer, and
// it can be removed. A key of one printer stays a key of that printer.
func TestDeckKeys(t *testing.T) {
	root, ts, _ := newTest(t)
	other, ts2, _ := newTest(t)
	dk := NewDeckKeys(filepath.Join(t.TempDir(), "deck-apikeys.json"))
	root.DeckKeys, root.DeckWide = dk, true
	other.DeckKeys = dk
	root.Ask = func(q, yes, no string, answer func(bool)) {
		if q != "Allow zslice (127.0.0.1) to use this Printdeck and its printers?" {
			t.Errorf("question %q", q)
		}
		answer(true)
	}
	_, m := do(t, "POST", ts.URL+"/plugin/appkeys/request", "", strings.NewReader(`{"app":"zslice"}`), "application/json")
	_, m = do(t, "GET", ts.URL+"/plugin/appkeys/request/"+m["app_token"].(string), "", nil, "")
	key, _ := m["api_key"].(string)
	if key == "" || m["scope"] != "deck" {
		t.Fatalf("answer %v", m)
	}
	for _, u := range []string{ts.URL, ts2.URL} {
		if r, _ := do(t, "GET", u+"/api/job", key, nil, ""); r.StatusCode != 200 {
			t.Errorf("deck key on %s: %d", u, r.StatusCode)
		}
	}
	req, _ := http.NewRequest("GET", ts2.URL+"/api/job", nil)
	req.Header.Set("X-Api-Key", key)
	if other.OwnKey(req) || !other.ValidKey(req) || !dk.ValidRequest(req) {
		t.Errorf("own %v valid %v deck %v", other.OwnKey(req), other.ValidKey(req), dk.ValidRequest(req))
	}
	// A key of the other printer only: its own request gives scope "printer".
	other.Ask = func(q, yes, no string, answer func(bool)) {
		if q != "Allow Cura (127.0.0.1) to use the Printdeck?" {
			t.Errorf("printer question %q", q)
		}
		answer(true)
	}
	_, m = do(t, "POST", ts2.URL+"/plugin/appkeys/request", "", strings.NewReader(`{"app":"Cura"}`), "application/json")
	_, m = do(t, "GET", ts2.URL+"/plugin/appkeys/request/"+m["app_token"].(string), "", nil, "")
	pkey, _ := m["api_key"].(string)
	if m["scope"] != "printer" {
		t.Errorf("printer answer %v", m)
	}
	if r, _ := do(t, "GET", ts.URL+"/api/job", pkey, nil, ""); r.StatusCode != http.StatusForbidden {
		t.Errorf("a key of one printer on another: %d", r.StatusCode)
	}
	// The list names the apps, never the keys, and a removed key stops working.
	list := dk.List()
	if len(list) != 1 || list[0].App != "zslice" || len(list[0].ID) != 8 || strings.Contains(key, list[0].ID) {
		t.Fatalf("list %+v", list)
	}
	if ks := other.Keys(); len(ks) != 1 || ks[0].App != "Cura" {
		t.Errorf("printer keys %+v", ks)
	}
	if !dk.Remove(list[0].ID) {
		t.Fatal("remove")
	}
	if r, _ := do(t, "GET", ts2.URL+"/api/job", key, nil, ""); r.StatusCode != http.StatusForbidden {
		t.Errorf("a removed deck key: %d", r.StatusCode)
	}
	if NewDeckKeys(dk.ks.file).ks.valid(key) {
		t.Error("the removal is not saved")
	}
}
