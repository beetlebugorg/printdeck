package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"printdeck/internal/cloud"
)

// fakeDNS keeps the records in a map.
type fakeDNS struct {
	mu   sync.Mutex
	recs map[string][]string // "name TYPE" -> values
}

func (f *fakeDNS) Set(_ context.Context, name, typ string, values []string, _ int64, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recs == nil {
		f.recs = map[string][]string{}
	}
	f.recs[name+" "+typ] = values
	return nil
}

func (f *fakeDNS) Remove(_ context.Context, name, typ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.recs, name+" "+typ)
	return nil
}

func (f *fakeDNS) get(key string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.recs[key]
}

// fakeIssuer signs the CSR with a test CA, or fails with err.
type fakeIssuer struct {
	mu    sync.Mutex
	calls int
	err   error
	ca    *x509.Certificate
	key   *ecdsa.PrivateKey
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test CA"}, IsCA: true,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour * 24 * 365)}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(der)
	return &fakeIssuer{ca: ca, key: key}
}

func (f *fakeIssuer) Issue(_ context.Context, name string, csrDER []byte) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		return nil, err
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(int64(f.calls + 1)), DNSNames: []string{name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(90 * 24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tpl, f.ca, csr.PublicKey, f.key)
	if err != nil {
		return nil, err
	}
	return append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.ca.Raw})...), nil
}

func (f *fakeIssuer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// testCSR returns a CSR (base64 of DER) for the names, with a new P-256 key.
func testCSR(t *testing.T, names ...string) string {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{DNSNames: names}, key)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(der)
}

func tlsServer(t *testing.T) (*MemStore, *Server, *fakeDNS, *fakeIssuer, *cloud.Identity) {
	st, s, url := testServer(t)
	dns, iss := &fakeDNS{}, newFakeIssuer(t)
	s.Domain, s.DNS, s.Issuer = "printdeck.example.org", dns, iss
	var c cloud.Client
	id, err := c.Enroll(context.Background(), url, mustCode(t, st), "Workhorse")
	if err != nil {
		t.Fatal(err)
	}
	return st, s, dns, iss, id
}

// The address of a deck: a private address sets the A record of its random name. A public
// address, a replay, and a change within 30 s fail.
func TestDeckAddress(t *testing.T) {
	_, s, dns, _, id := tlsServer(t)
	ctx := context.Background()
	now := time.Now()
	s.Now = func() time.Time { return now }
	clock := now
	c := cloud.Client{Now: func() time.Time { clock = clock.Add(time.Second); return clock }}
	out, err := c.SetAddress(ctx, id, "192.168.77.135")
	if err != nil {
		t.Fatal(err)
	}
	host, rest, _ := strings.Cut(out.Name, ".")
	if rest != "d.printdeck.example.org" || !validID(host) {
		t.Fatalf("name %q", out.Name)
	}
	if got := dns.get(out.Name + " A"); len(got) != 1 || got[0] != "192.168.77.135" {
		t.Fatalf("A record %v", got)
	}
	for _, ip := range []string{"8.8.8.8", "127.0.0.1", "169.254.1.1", "fd00::1", "", "192.168.1"} {
		if _, err := c.SetAddress(ctx, id, ip); status(err) != 400 {
			t.Errorf("%q: %v", ip, err)
		}
	}
	// The same address again is no change of the record.
	if again, err := c.SetAddress(ctx, id, "192.168.77.135"); err != nil || again.Name != out.Name {
		t.Fatalf("again: %+v %v", again, err)
	}
	if _, err := c.SetAddress(ctx, id, "10.0.0.5"); status(err) != 429 {
		t.Fatalf("a change within 30 s: %v", err)
	}
	now = now.Add(time.Minute)
	if _, err := c.SetAddress(ctx, id, "10.0.0.5"); err != nil {
		t.Fatal(err)
	}
	if got := dns.get(out.Name + " A"); got[0] != "10.0.0.5" {
		t.Fatalf("A record %v", got)
	}
	// A request signed at an older time cannot set an old address.
	last := clock
	old := cloud.Client{Now: func() time.Time { return last.Add(-5 * time.Second) }}
	now = now.Add(time.Minute)
	if _, err := old.SetAddress(ctx, id, "192.168.77.135"); status(err) != 409 {
		t.Fatalf("old request: %v", err)
	}
}

// A certificate: the CSR must name the deck; the job gets the chain; a second request with the
// same key gets the same chain; at most 3 certificates in 7 days.
func TestDeckCert(t *testing.T) {
	_, s, _, iss, id := tlsServer(t)
	ctx := context.Background()
	var c cloud.Client
	addr, err := c.SetAddress(ctx, id, "192.168.77.135")
	if err != nil {
		t.Fatal(err)
	}
	name := addr.Name
	if _, err := c.Cert(ctx, id); status(err) != 404 {
		t.Fatalf("no request: %v", err)
	}
	bad := []string{
		testCSR(t, "other.d.printdeck.example.org"),
		testCSR(t, name, "other.d.printdeck.example.org"),
		testCSR(t),
		"bm90IGEgY3Ny",
	}
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 1024)
	der, _ := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{DNSNames: []string{name}}, rsaKey)
	bad = append(bad, base64.StdEncoding.EncodeToString(der))
	for i, csr := range bad {
		if _, err := c.RequestCert(ctx, id, csr); status(err) != 400 {
			t.Errorf("bad CSR %d: %v", i, err)
		}
	}
	csr := testCSR(t, name)
	out, err := c.RequestCert(ctx, id, csr)
	if err != nil || out.Status != cloud.CertReady || out.Name != name || !strings.Contains(out.Chain, "BEGIN CERTIFICATE") {
		t.Fatalf("request: %+v %v", out, err)
	}
	leaf, err := firstCert([]byte(out.Chain))
	if err != nil || leaf.DNSNames[0] != name {
		t.Fatalf("leaf: %v", err)
	}
	if got, err := c.Cert(ctx, id); err != nil || got.Chain != out.Chain {
		t.Fatalf("get: %+v %v", got, err)
	}
	if again, err := c.RequestCert(ctx, id, csr); err != nil || again.Chain != out.Chain || iss.count() != 1 {
		t.Fatalf("same key: %+v %v, %d issues", again, err, iss.count())
	}
	for i := 2; i <= 3; i++ {
		if out, err := c.RequestCert(ctx, id, testCSR(t, name)); err != nil || out.Status != cloud.CertReady || iss.count() != i {
			t.Fatalf("new key %d: %+v %v", i, out, err)
		}
	}
	if _, err := c.RequestCert(ctx, id, testCSR(t, name)); status(err) != 429 {
		t.Fatalf("4th certificate: %v", err)
	}
	later := func() time.Time { return time.Now().Add(8 * 24 * time.Hour) }
	s.Now = later
	c8 := cloud.Client{Now: later}
	if out, err := c8.RequestCert(ctx, id, testCSR(t, name)); err != nil || out.Status != cloud.CertReady || iss.count() != 4 {
		t.Fatalf("after 7 days: %+v %v", out, err)
	}
}

// A failed job: the deck gets the error, and the same CSR starts no new job for 10 minutes.
func TestDeckCertFails(t *testing.T) {
	_, _, _, iss, id := tlsServer(t)
	ctx := context.Background()
	var c cloud.Client
	addr, _ := c.SetAddress(ctx, id, "10.1.2.3")
	iss.err = errors.New("rate limited\nmore text")
	csr := testCSR(t, addr.Name)
	out, err := c.RequestCert(ctx, id, csr)
	if err != nil || out.Status != cloud.CertFailed || !strings.Contains(out.Error, "rate limited") || strings.Contains(out.Error, "more") {
		t.Fatalf("failed: %+v %v", out, err)
	}
	if out, err := c.RequestCert(ctx, id, csr); err != nil || out.Status != cloud.CertFailed || iss.count() != 1 {
		t.Fatalf("retry: %+v %v %d", out, err, iss.count())
	}
}

// A deck that disconnects loses its A record and its certificate.
func TestDeckForget(t *testing.T) {
	st, _, dns, _, id := tlsServer(t)
	ctx := context.Background()
	var c cloud.Client
	addr, _ := c.SetAddress(ctx, id, "10.1.2.3")
	if _, err := c.RequestCert(ctx, id, testCSR(t, addr.Name)); err != nil {
		t.Fatal(err)
	}
	if err := c.Disconnect(ctx, id); err != nil {
		t.Fatal(err)
	}
	if dns.get(addr.Name+" A") != nil {
		t.Error("the A record stays")
	}
	if _, _, err := st.Get(ctx, certKey(id.Deck)); !errors.Is(err, ErrNotFound) {
		t.Errorf("the certificate stays: %v", err)
	}
}

// A service with no DNS gives no names.
func TestDeckAddressOff(t *testing.T) {
	st, _, url := testServer(t)
	var c cloud.Client
	id, err := c.Enroll(context.Background(), url, mustCode(t, st), "Mini")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetAddress(context.Background(), id, "10.1.2.3"); status(err) != http.StatusServiceUnavailable {
		t.Fatalf("off: %v", err)
	}
}
