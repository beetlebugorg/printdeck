package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// challDNS is the DNS of pebble-challtestsrv: its management API sets the TXT records that
// Pebble reads.
type challDNS struct{ url string }

func (c challDNS) post(path string, v any) error {
	b, _ := json.Marshal(v)
	resp, err := http.Post(c.url+path, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c challDNS) Set(_ context.Context, name, typ string, values []string, _ int64, _ bool) error {
	return c.post("/set-txt", map[string]string{"host": name + ".", "value": strings.Trim(values[0], `"`)})
}

func (c challDNS) Remove(_ context.Context, name, typ string) error {
	return c.post("/clear-txt", map[string]string{"host": name + "."})
}

// TestACMEPebble gets a certificate from Pebble, the ACME test server of Let's Encrypt, with
// DNS-01. It runs only with PEBBLE (the directory URL), PEBBLE_CA (the CA file of its HTTPS) and
// CHALLTEST (the management URL of pebble-challtestsrv).
func TestACMEPebble(t *testing.T) {
	dir, caFile, chall := os.Getenv("PEBBLE"), os.Getenv("PEBBLE_CA"), os.Getenv("CHALLTEST")
	if dir == "" {
		t.Skip("no PEBBLE")
	}
	ca, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca)
	hc := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	st := NewMemStore()
	iss := &ACMEIssuer{Directory: dir, Store: st, DNS: challDNS{chall}, HTTP: hc}
	name := "k3x9q2m7wfa4c.d.printdeck.example.org"
	der, _ := base64.StdEncoding.DecodeString(testCSR(t, name))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	chain, err := iss.Issue(ctx, name, der)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := firstCert(chain)
	if err != nil || len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != name {
		t.Fatalf("leaf: %v %v", leaf, err)
	}
	if strings.Count(string(chain), "BEGIN CERTIFICATE") < 2 {
		t.Fatal("no intermediate in the chain")
	}
	// A second certificate uses the same account.
	if _, err := iss.Issue(ctx, name, der); err != nil {
		t.Fatalf("second: %v", err)
	}
	t.Logf("certificate for %s to %s", name, leaf.NotAfter)
}
