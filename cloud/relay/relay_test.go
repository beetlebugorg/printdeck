package relay

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
)

const device = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// testRelay is a relay with a new EC key, and a fake APNs server that keeps the last request.
func testRelay(t *testing.T, status int) (*Relay, *ecdsa.PrivateKey, *http.Request, *[]byte) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	p8 := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	var last http.Request
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		last = *req
		body, _ = io.ReadAll(req.Body)
		w.WriteHeader(status)
		if status != http.StatusOK {
			w.Write([]byte(`{"reason":"Unregistered"}`))
		}
	}))
	t.Cleanup(srv.Close)
	r := NewRelay(Secret{KeyID: "KEY123", TeamID: "TEAM456", P8: p8, RelaySecret: "relay-secret"}, "org.beetlebug.printdeck")
	r.Hosts = map[string]string{"production": srv.URL, "sandbox": srv.URL}
	return r, key, &last, &body
}

// register gets the send token as the app does: a request with no code, then the code of the
// background push.
func register(t *testing.T, r *Relay, last *http.Request, body *[]byte) string {
	t.Helper()
	tok, err := r.Register(RegisterRequest{Device: device, Env: "sandbox"})
	if err != nil || tok != "" {
		t.Fatalf("first request: %q %v", tok, err)
	}
	if last.Header.Get("apns-push-type") != "background" || last.Header.Get("apns-priority") != "5" {
		t.Errorf("challenge headers %v", last.Header)
	}
	var p struct {
		Aps map[string]any `json:"aps"`
		C   string         `json:"c"`
	}
	json.Unmarshal(*body, &p)
	if p.Aps["content-available"] != float64(1) || p.Aps["alert"] != nil || len(p.C) != 16 {
		t.Fatalf("challenge payload %s", *body)
	}
	tok, err = r.Register(RegisterRequest{Device: device, Env: "sandbox", Code: p.C})
	if err != nil || tok != r.SendToken("sandbox", device) {
		t.Fatalf("second request: %q %v", tok, err)
	}
	return tok
}

func TestRegisterAndPush(t *testing.T) {
	r, key, last, body := testRelay(t, http.StatusOK)
	tok := register(t, r, last, body)
	if err := r.Push(PushRequest{Device: device, Env: "sandbox", SendToken: tok, Payload: "AAECAw=="}); err != nil {
		t.Fatal(err)
	}
	if last.URL.Path != "/3/device/"+device || last.Header.Get("apns-topic") != "org.beetlebug.printdeck" || last.Header.Get("apns-push-type") != "alert" {
		t.Errorf("request %s %v", last.URL.Path, last.Header)
	}
	jwt := strings.TrimPrefix(last.Header.Get("authorization"), "bearer ")
	if !verifyES256(&key.PublicKey, jwt) {
		t.Error("the JWT does not verify with the key")
	}
	var p map[string]any
	json.Unmarshal(*body, &p)
	if p["e"] != "AAECAw==" || p["aps"].(map[string]any)["mutable-content"] != float64(1) {
		t.Errorf("payload %s", *body)
	}
}

func TestPushRefusals(t *testing.T) {
	r, _, _, _ := testRelay(t, http.StatusOK)
	tok := r.SendToken("sandbox", device)
	for name, req := range map[string]PushRequest{
		"wrong token":     {Device: device, Env: "sandbox", SendToken: "x" + tok, Payload: "AAEC"},
		"other env":       {Device: device, Env: "production", SendToken: tok, Payload: "AAEC"},
		"not hex":         {Device: "zz" + device[2:], Env: "sandbox", SendToken: tok, Payload: "AAEC"},
		"no payload":      {Device: device, Env: "sandbox", SendToken: tok},
		"too large":       {Device: device, Env: "sandbox", SendToken: tok, Payload: strings.Repeat("A", maxPayload+4)},
		"payload not b64": {Device: device, Env: "sandbox", SendToken: tok, Payload: "!!!"},
	} {
		if err := r.Push(req); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// A code is good for its window and the window after it, for that device and environment only.
func TestRegisterCode(t *testing.T) {
	r, _, _, _ := testRelay(t, http.StatusOK)
	now := time.Unix(1_800_000_000, 0)
	r.Now = func() time.Time { return now }
	code := r.Code("sandbox", device, now.Unix()/codeWindow)
	other := "fedcba9876543210" + device[16:]
	for name, req := range map[string]RegisterRequest{
		"wrong code":   {Device: device, Env: "sandbox", Code: "x" + code[1:]},
		"other device": {Device: other, Env: "sandbox", Code: code},
		"other env":    {Device: device, Env: "production", Code: code},
	} {
		if _, err := r.Register(req); err == nil || err.(*Error).Status != http.StatusForbidden {
			t.Errorf("%s: %v", name, err)
		}
	}
	now = now.Add(codeWindow * time.Second)
	if _, err := r.Register(RegisterRequest{Device: device, Env: "sandbox", Code: code}); err != nil {
		t.Errorf("the window after: %v", err)
	}
	now = now.Add(codeWindow * time.Second)
	if _, err := r.Register(RegisterRequest{Device: device, Env: "sandbox", Code: code}); err == nil {
		t.Error("two windows after: no error")
	}
}

// A device that removed the app gives 410, so the deck can stop sending to it.
func TestPushGone(t *testing.T) {
	r, _, _, _ := testRelay(t, http.StatusGone)
	err := r.Push(PushRequest{Device: device, Env: "sandbox", SendToken: r.SendToken("sandbox", device), Payload: "AAEC"})
	if e, ok := err.(*Error); !ok || e.Status != http.StatusGone {
		t.Fatalf("%v", err)
	}
}

// The provider token is used again for 50 minutes, and then made again.
func TestTokenReuse(t *testing.T) {
	r, _, _, _ := testRelay(t, http.StatusOK)
	now := time.Unix(1_800_000_000, 0)
	r.Now = func() time.Time { return now }
	a, _ := r.token()
	now = now.Add(49 * time.Minute)
	b, _ := r.token()
	now = now.Add(2 * time.Minute)
	c, _ := r.token()
	if a != b || b == c {
		t.Errorf("reuse %v, new %v", a == b, b != c)
	}
}

func TestHandle(t *testing.T) {
	r, _, _, _ := testRelay(t, http.StatusOK)
	req := events.LambdaFunctionURLRequest{RawPath: "/register", Body: `{"device":"` + device + `","env":"sandbox"}`}
	req.RequestContext.HTTP.Method = "POST"
	resp := Handle(r, req)
	if resp.StatusCode != http.StatusAccepted || strings.Contains(resp.Body, "send_token") {
		t.Fatalf("no code: %d %s", resp.StatusCode, resp.Body)
	}
	code := r.Code("sandbox", device, r.Now().Unix()/codeWindow)
	req.Body = `{"device":"` + device + `","env":"sandbox","code":"` + code + `"}`
	resp = Handle(r, req)
	if resp.StatusCode != 200 || !strings.Contains(resp.Body, r.SendToken("sandbox", device)) {
		t.Fatalf("%d %s", resp.StatusCode, resp.Body)
	}
	req.RequestContext.HTTP.Method = "GET"
	if resp := Handle(r, req); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET: %d", resp.StatusCode)
	}
}
