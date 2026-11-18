// Package tlscert gets and keeps the HTTPS certificate of the deck. The cloud service of the
// deck gives it a name, <host>.d.<domain of the service>, with the LAN address of the deck in its
// A record. The deck makes its TLS key, and sends a CSR for the name. The service gets the
// certificate from Let's Encrypt (cloud/api/decktls.go). The key never leaves the deck.
//
// Then a web page on HTTPS, for example the zslice web app, can call the deck with no
// mixed-content block. A deck with no cloud service serves only HTTP.
package tlscert

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"printdeck/internal/cloud"
)

const (
	// renewBefore is the time before the end of the certificate when the deck gets a new one.
	renewBefore = 30 * 24 * time.Hour
	// addressAgain is the time after which the deck sends the same address again: the service
	// then knows that the deck is on.
	addressAgain = 6 * time.Hour
	// checkEvery is the time between two checks of the address and the certificate.
	checkEvery = 5 * time.Minute
	// pollFor is the most time of the checks of a pending certificate.
	pollFor = 6 * time.Minute
	// retryAfter is the time after a failure before the next request of a certificate.
	retryAfter = 30 * time.Minute
)

// pollEvery is the time between two checks of a pending certificate. Tests change it.
var pollEvery = 15 * time.Second

// Identity gives the identity of the deck at its cloud service, or nil for a deck with no
// service (cloud.Link).
type Identity interface {
	Identity() *cloud.Identity
}

// Manager keeps the key, the name, and the certificate of the deck, in Dir.
type Manager struct {
	Dir    string
	Link   Identity
	Client *cloud.Client
	// LocalIP returns the LAN address of the deck, or nil for the address of the route to the
	// internet.
	LocalIP func() (string, error)
	// Roots are the trusted roots of the certificate, or nil for the roots of the system.
	Roots *x509.CertPool
	// Now is the clock, or nil for time.Now.
	Now func() time.Time

	mu      sync.Mutex
	loaded  bool
	key     *ecdsa.PrivateKey
	cert    *tls.Certificate
	leaf    *x509.Certificate
	name    string
	sentIP  string
	sentAt  time.Time
	failAt  time.Time
	problem string
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Manager) keyPath() string  { return filepath.Join(m.Dir, "key.pem") }
func (m *Manager) certPath() string { return filepath.Join(m.Dir, "cert.pem") }

// load reads the key and the certificate from Dir, once.
func (m *Manager) load() {
	if m.loaded {
		return
	}
	m.loaded = true
	if b, err := os.ReadFile(m.keyPath()); err == nil {
		if blk, _ := pem.Decode(b); blk != nil {
			if k, err := x509.ParseECPrivateKey(blk.Bytes); err == nil {
				m.key = k
			}
		}
	}
	if m.key == nil {
		return
	}
	if b, err := os.ReadFile(m.certPath()); err == nil {
		if c, leaf, err := m.check(b, "", m.key); err == nil {
			m.cert, m.leaf = c, leaf
			m.name = leaf.DNSNames[0]
		} else {
			log.Printf("tlscert: the saved certificate: %v", err)
		}
	}
}

// GetCertificate gives the certificate to the TLS server (tls.Config.GetCertificate).
func (m *Manager) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.load()
	if m.cert == nil {
		return nil, errors.New("the deck has no certificate")
	}
	return m.cert, nil
}

// URL returns the HTTPS address of the deck, or "" when the deck has no valid certificate or no
// cloud service.
func (m *Manager) URL() string {
	if m.Link.Identity() == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.load()
	if m.leaf == nil || m.now().After(m.leaf.NotAfter) {
		return ""
	}
	return "https://" + m.name
}

// Problem returns the last error, for the system page, or "".
func (m *Manager) Problem() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.problem
}

func (m *Manager) setProblem(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err == nil {
		m.problem = ""
		return
	}
	m.problem = err.Error()
	log.Printf("tlscert: %v", err)
}

// Run checks the address and the certificate at the start, and then every 5 minutes.
func (m *Manager) Run(ctx context.Context) {
	for {
		m.Step(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(checkEvery):
		}
	}
}

// Step sends the address when it changed, and gets a new certificate when the deck needs one.
func (m *Manager) Step(ctx context.Context) {
	id := m.Link.Identity()
	if id == nil {
		return
	}
	m.mu.Lock()
	m.load()
	m.mu.Unlock()
	ip, err := m.localIP()
	if err != nil {
		m.setProblem(err)
		return
	}
	now := m.now()
	m.mu.Lock()
	send := ip != m.sentIP || now.Sub(m.sentAt) > addressAgain || m.name == ""
	m.mu.Unlock()
	if send {
		out, err := m.Client.SetAddress(ctx, id, ip)
		if err != nil {
			m.setProblem(fmt.Errorf("the address: %w", err))
			return
		}
		m.mu.Lock()
		m.sentIP, m.sentAt = ip, now
		m.name = out.Name
		m.mu.Unlock()
	}
	m.mu.Lock()
	need := m.leaf == nil || m.leaf.NotAfter.Sub(now) < renewBefore || m.leaf.DNSNames[0] != m.name
	wait := now.Sub(m.failAt) < retryAfter
	m.mu.Unlock()
	if !need {
		m.setProblem(nil)
		return
	}
	if wait {
		return
	}
	if err := m.renew(ctx, id); err != nil {
		m.mu.Lock()
		m.failAt = now
		m.mu.Unlock()
		m.setProblem(fmt.Errorf("the certificate: %w", err))
		return
	}
	m.setProblem(nil)
}

// renew makes a new key, sends a CSR for the name, waits for the certificate, checks it, and
// saves the key and the certificate. The old key and certificate stay in use until then.
func (m *Manager) renew(ctx context.Context, id *cloud.Identity) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	m.mu.Lock()
	name := m.name
	m.mu.Unlock()
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{DNSNames: []string{name}}, key)
	if err != nil {
		return err
	}
	out, err := m.Client.RequestCert(ctx, id, base64.StdEncoding.EncodeToString(der))
	for end := m.now().Add(pollFor); err == nil && out.Status == cloud.CertPending && m.now().Before(end); {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollEvery):
		}
		out, err = m.Client.Cert(ctx, id)
	}
	switch {
	case err != nil:
		return err
	case out.Status == cloud.CertFailed:
		return errors.New(out.Error)
	case out.Status != cloud.CertReady:
		return errors.New("the service has no certificate yet")
	}
	c, leaf, err := m.check([]byte(out.Chain), name, key)
	if err != nil {
		return err
	}
	// The key first: after a stop between the two writes, the saved certificate does not match
	// the key, and the next start gets a new certificate.
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	if err := writeFile(m.keyPath(), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return err
	}
	if err := writeFile(m.certPath(), []byte(out.Chain), 0o644); err != nil {
		return err
	}
	m.mu.Lock()
	m.key, m.cert, m.leaf = key, c, leaf
	m.mu.Unlock()
	log.Printf("tlscert: a new certificate for %s, to %s", name, leaf.NotAfter.Format(time.DateOnly))
	return nil
}

// check parses the chain, and checks that its certificate is for the name (or any one name for
// ""), has the key, and has a valid chain to the roots.
func (m *Manager) check(chain []byte, name string, key *ecdsa.PrivateKey) (*tls.Certificate, *x509.Certificate, error) {
	var c tls.Certificate
	for rest := chain; ; {
		var blk *pem.Block
		blk, rest = pem.Decode(rest)
		if blk == nil {
			break
		}
		if blk.Type == "CERTIFICATE" {
			c.Certificate = append(c.Certificate, blk.Bytes)
		}
	}
	if len(c.Certificate) == 0 {
		return nil, nil, errors.New("the chain has no certificate")
	}
	leaf, err := x509.ParseCertificate(c.Certificate[0])
	if err != nil {
		return nil, nil, err
	}
	if len(leaf.DNSNames) != 1 || (name != "" && leaf.DNSNames[0] != name) {
		return nil, nil, fmt.Errorf("the certificate is not for %s", name)
	}
	pub, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok || key == nil || !pub.Equal(&key.PublicKey) {
		return nil, nil, errors.New("the certificate does not have the key of the deck")
	}
	inter := x509.NewCertPool()
	for _, d := range c.Certificate[1:] {
		if ic, err := x509.ParseCertificate(d); err == nil {
			inter.AddCert(ic)
		}
	}
	if _, err := leaf.Verify(x509.VerifyOptions{DNSName: leaf.DNSNames[0], Roots: m.Roots, Intermediates: inter, CurrentTime: m.now()}); err != nil {
		return nil, nil, err
	}
	c.PrivateKey, c.Leaf = key, leaf
	return &c, leaf, nil
}

func (m *Manager) localIP() (string, error) {
	if m.LocalIP != nil {
		return m.LocalIP()
	}
	return RouteIPv4()
}

// RouteIPv4 returns the private IPv4 address of the route of the deck to the internet. It sends
// no packet: a UDP "connection" only picks the route.
func RouteIPv4() (string, error) {
	c, err := net.Dial("udp4", "192.0.2.1:9")
	if err != nil {
		return "", fmt.Errorf("no network: %w", err)
	}
	defer c.Close()
	ip := c.LocalAddr().(*net.UDPAddr).IP.To4()
	if ip == nil || !ip.IsPrivate() {
		return "", fmt.Errorf("the address %v of the deck is not a private address", c.LocalAddr())
	}
	return ip.String(), nil
}

// writeFile writes the file through a temporary file and a rename, with the mode.
func writeFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
