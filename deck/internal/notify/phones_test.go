package notify

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testDevice = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// The app and the deck agree on the same key, and the app decrypts the message of the deck.
func TestPairAndSeal(t *testing.T) {
	p := &Phones{Path: filepath.Join(t.TempDir(), "push.json")}
	app, _ := ecdh.X25519().GenerateKey(rand.Reader)
	ph, deckPub, err := p.Pair(PairRequest{Name: "Test phone", Device: testDevice, Env: "sandbox", SendToken: "tok",
		Relay: "https://relay.example/", PublicKey: base64.StdEncoding.EncodeToString(app.PublicKey().Bytes())}, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	// The side of the app.
	raw, _ := base64.StdEncoding.DecodeString(deckPub)
	pub, _ := ecdh.X25519().NewPublicKey(raw)
	shared, _ := app.ECDH(pub)
	dev, _ := hex.DecodeString(testDevice)
	key, _ := hkdf.Key(sha256.New, shared, dev, keyInfo, 32)
	if !bytes.Equal(key, ph.Key) {
		t.Fatal("the keys differ")
	}
	payload, _ := Seal(ph.Key, Message{Title: "Print done", Body: "a.gcode · 1h 42m", Tags: "white_check_mark"}, time.Unix(1792790000, 0))
	got, err := open(key, payload)
	if err != nil || got.Title != "Print done" || got.Body != "a.gcode · 1h 42m" || got.At != 1792790000 {
		t.Fatalf("%+v %v", got, err)
	}
	// The file has the phone, and only its owner can read it.
	fi, err := os.Stat(p.Path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", fi.Mode(), err)
	}
	if l := (&Phones{Path: p.Path}).List(); len(l) != 1 || l[0].Relay != "https://relay.example" {
		t.Fatalf("%+v", l)
	}
	// A new pairing of the same phone makes a new key, with the same ID.
	ph2, _, _ := p.Pair(PairRequest{Name: "Test phone", Device: testDevice, Env: "sandbox", SendToken: "tok",
		Relay: "https://relay.example", PublicKey: base64.StdEncoding.EncodeToString(app.PublicKey().Bytes())}, time.Unix(2, 0))
	if l := p.List(); len(l) != 1 || ph2.ID != ph.ID || bytes.Equal(ph2.Key, ph.Key) {
		t.Errorf("repair: %d phones, same id %v", len(l), ph2.ID == ph.ID)
	}
}

func TestPairRefusals(t *testing.T) {
	p := &Phones{Path: filepath.Join(t.TempDir(), "push.json")}
	app, _ := ecdh.X25519().GenerateKey(rand.Reader)
	ok := PairRequest{Device: testDevice, Env: "sandbox", SendToken: "tok", Relay: "https://r.example",
		PublicKey: base64.StdEncoding.EncodeToString(app.PublicKey().Bytes())}
	for name, change := range map[string]func(*PairRequest){
		"env":    func(r *PairRequest) { r.Env = "test" },
		"device": func(r *PairRequest) { r.Device = "xyz" },
		"http":   func(r *PairRequest) { r.Relay = "http://r.example" },
		"key":    func(r *PairRequest) { r.PublicKey = "AAAA" },
		"token":  func(r *PairRequest) { r.SendToken = "" },
	} {
		r := ok
		change(&r)
		if _, _, err := p.Pair(r, time.Now()); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestSendPhone(t *testing.T) {
	var got map[string]any
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &got)
		if r.URL.Path != "/push" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	key := make([]byte, 32)
	ph := Phone{Device: testDevice, Env: "sandbox", SendToken: "tok", Relay: srv.URL, Key: key}
	if err := SendPhone(context.Background(), ph, Message{Title: "Print paused", Priority: 4}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got["device"] != testDevice || got["send_token"] != "tok" || got["priority"] != float64(4) {
		t.Errorf("%v", got)
	}
	if m, err := open(key, got["payload"].(string)); err != nil || m.Title != "Print paused" {
		t.Errorf("payload %+v %v", m, err)
	}
	status = http.StatusGone
	if err := SendPhone(context.Background(), ph, Message{Title: "x"}, time.Now()); err != ErrGone {
		t.Errorf("410: %v", err)
	}
}
