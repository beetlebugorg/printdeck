package tlscert

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"printdeck/internal/cloud"
)

// fakeService is the cloud service of the test: it gives the name, and signs the CSR with a test
// CA after one pending reply.
type fakeService struct {
	t        *testing.T
	ca       *x509.Certificate
	caKey    *ecdsa.PrivateKey
	name     string
	mu       sync.Mutex
	ips      []string
	requests int
	pending  bool
	chain    string
	days     int        // the life of the certificates
	wrongKey bool       // sign another key
	reply    func() int // a status for the next request, or 0
}

func newFakeService(t *testing.T) *fakeService {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test CA"}, IsCA: true,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0)}
	der, _ := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	ca, _ := x509.ParseCertificate(der)
	return &fakeService{t: t, ca: ca, caKey: key, name: "abcdefgh.d.printdeck.example.org", days: 90}
}

func (f *fakeService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get(cloud.HeaderSignature) == "" {
		http.Error(w, "unsigned", 401)
		return
	}
	body, _ := io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/api/v1/decks/self/address":
		var in cloud.AddressRequest
		json.Unmarshal(body, &in)
		f.ips = append(f.ips, in.IPv4)
		json.NewEncoder(w).Encode(cloud.AddressReply{Name: f.name})
	case r.URL.Path == "/api/v1/decks/self/cert" && r.Method == http.MethodPost:
		var in cloud.CertRequest
		json.Unmarshal(body, &in)
		der, _ := base64.StdEncoding.DecodeString(in.CSR)
		csr, err := x509.ParseCertificateRequest(der)
		if err != nil || csr.DNSNames[0] != f.name {
			http.Error(w, `{"error":"bad CSR"}`, 400)
			return
		}
		f.requests++
		pub := csr.PublicKey
		if f.wrongKey {
			k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			pub = &k.PublicKey
		}
		tpl := &x509.Certificate{SerialNumber: big.NewInt(int64(f.requests + 1)), DNSNames: []string{f.name},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Duration(f.days) * 24 * time.Hour),
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		cert, _ := x509.CreateCertificate(rand.Reader, tpl, f.ca, pub, f.caKey)
		f.chain = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert}))
		f.pending = true
		json.NewEncoder(w).Encode(cloud.CertReply{Name: f.name, Status: cloud.CertPending})
	case r.URL.Path == "/api/v1/decks/self/cert":
		if f.pending {
			f.pending = false // pending once more, then ready
			json.NewEncoder(w).Encode(cloud.CertReply{Name: f.name, Status: cloud.CertPending})
			return
		}
		json.NewEncoder(w).Encode(cloud.CertReply{Name: f.name, Status: cloud.CertReady, Chain: f.chain})
	default:
		http.NotFound(w, r)
	}
}

type fixedLink struct{ id *cloud.Identity }

func (l fixedLink) Identity() *cloud.Identity { return l.id }

func setup(t *testing.T) (*fakeService, *Manager) {
	pollEvery = 10 * time.Millisecond
	f := newFakeService(t)
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	id := &cloud.Identity{Domain: srv.URL, Deck: "deckdeckdeck", Key: key.Seed()}
	roots := x509.NewCertPool()
	roots.AddCert(f.ca)
	ip := "192.168.77.135"
	m := &Manager{Dir: filepath.Join(t.TempDir(), "tls"), Link: fixedLink{id}, Client: &cloud.Client{}, Roots: roots,
		LocalIP: func() (string, error) { return ip, nil }}
	return f, m
}

// A deck with a cloud service sends its address, gets a certificate, and serves HTTPS with it.
// A new start reads the saved key and certificate, and asks for no new one.
func TestGetAndServe(t *testing.T) {
	f, m := setup(t)
	if m.URL() != "" {
		t.Fatal("a URL before the certificate")
	}
	m.Step(context.Background())
	if p := m.Problem(); p != "" {
		t.Fatal(p)
	}
	if m.URL() != "https://"+f.name || f.requests != 1 || len(f.ips) != 1 || f.ips[0] != "192.168.77.135" {
		t.Fatalf("URL %q, %d requests, ips %v", m.URL(), f.requests, f.ips)
	}
	if st, err := os.Stat(filepath.Join(m.Dir, "key.pem")); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("key file: %v %v", st, err)
	}
	// A TLS server with the certificate: a client that trusts the test CA connects to the name.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	tln := tls.NewListener(ln, &tls.Config{GetCertificate: m.GetCertificate, MinVersion: tls.VersionTLS12})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "deck") })}
	go srv.Serve(tln)
	defer srv.Close()
	roots := x509.NewCertPool()
	roots.AddCert(f.ca)
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: f.name},
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", ln.Addr().String())
		},
	}}
	resp, err := client.Get("https://" + f.name + "/")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "deck" {
		t.Fatalf("reply %q", b)
	}
	// A new start: the saved files, no new request.
	m2 := &Manager{Dir: m.Dir, Link: m.Link, Client: m.Client, Roots: m.Roots, LocalIP: m.LocalIP}
	if m2.URL() != m.URL() {
		t.Fatalf("after a start: %q", m2.URL())
	}
	m2.Step(context.Background())
	if f.requests != 1 {
		t.Fatalf("%d requests after a start", f.requests)
	}
}

// A certificate with less than 30 days left gets a new one. A changed address goes to the
// service.
func TestRenewAndAddress(t *testing.T) {
	f, m := setup(t)
	f.days = 20
	m.Step(context.Background())
	if f.requests != 1 || m.URL() == "" {
		t.Fatalf("first: %d %q %s", f.requests, m.URL(), m.Problem())
	}
	f.days = 90
	m.mu.Lock()
	m.failAt = time.Time{}
	m.mu.Unlock()
	m.Step(context.Background())
	if f.requests != 2 || m.leaf.NotAfter.Sub(time.Now()) < 80*24*time.Hour {
		t.Fatalf("renew: %d requests, to %v", f.requests, m.leaf.NotAfter)
	}
	m.LocalIP = func() (string, error) { return "10.0.0.9", nil }
	m.Step(context.Background())
	if len(f.ips) != 2 || f.ips[1] != "10.0.0.9" || f.requests != 2 {
		t.Fatalf("ips %v, %d requests", f.ips, f.requests)
	}
}

// A certificate for another key is not used, and the next request waits 30 minutes.
func TestWrongKey(t *testing.T) {
	f, m := setup(t)
	f.wrongKey = true
	m.Step(context.Background())
	if m.URL() != "" || !strings.Contains(m.Problem(), "key of the deck") {
		t.Fatalf("URL %q, problem %q", m.URL(), m.Problem())
	}
	m.Step(context.Background())
	if f.requests != 1 {
		t.Fatalf("%d requests: no wait after a failure", f.requests)
	}
	if _, err := m.GetCertificate(nil); err == nil {
		t.Fatal("a certificate for TLS")
	}
}

// A deck with no cloud service sends no request.
func TestNoService(t *testing.T) {
	f, m := setup(t)
	m.Link = fixedLink{}
	m.Step(context.Background())
	if len(f.ips) != 0 || m.URL() != "" {
		t.Fatal("a request with no service")
	}
}
