package api

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The cookies of a project: their policy names the files of the project only, the public key
// checks their signature, and their path is the folder of the project.
func TestFileCookies(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &FileSigner{Domain: "printdeck.example", KeyID: "K123", Key: key}
	cs, err := f.Cookies("abcdefgh", time.Unix(2000000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range cs {
		got[c.Name] = c.Value
		if c.Path != "/x/abcdefgh/" || !c.Secure || !c.HttpOnly {
			t.Errorf("cookie %+v", c)
		}
	}
	dec := func(s string) []byte {
		b, err := base64.StdEncoding.DecodeString(strings.NewReplacer("-", "+", "_", "=", "~", "/").Replace(s))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	policy := dec(got["CloudFront-Policy"])
	if !strings.Contains(string(policy), `"Resource":"https://printdeck.example/x/abcdefgh/*"`) || !strings.Contains(string(policy), "2000000000") {
		t.Errorf("policy %s", policy)
	}
	sum := sha1.Sum(policy)
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA1, sum[:], dec(got["CloudFront-Signature"])); err != nil {
		t.Errorf("signature: %v", err)
	}
	if got["CloudFront-Key-Pair-Id"] != "K123" {
		t.Errorf("key id %q", got["CloudFront-Key-Pair-Id"])
	}

	// A reply that links to a file of a project sets the cookies of that project.
	s := &Server{Files: f}
	ctx, jar := withFileJar(context.Background())
	noteFile(ctx, "x/abcdefgh/prints/p1/model.jpg")
	noteFile(ctx, "s/share/model.jpg")
	w := httptest.NewRecorder()
	s.setFileCookies(w, jar)
	if n := len(w.Result().Cookies()); n != 3 {
		t.Errorf("%d cookies, want 3", n)
	}
}
