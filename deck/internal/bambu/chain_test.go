package bambu

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"testing"
	"time"
)

// testChain makes a root, a device CA under it, and a printer certificate under the device CA.
func testChain(t *testing.T, serial string) (roots *x509.CertPool, inter, leaf *x509.Certificate) {
	t.Helper()
	mk := func(cn string, ca bool, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) (*x509.Certificate, *ecdsa.PrivateKey) {
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		tmpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: cn},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
		if ca {
			tmpl.IsCA, tmpl.BasicConstraintsValid, tmpl.KeyUsage = true, true, x509.KeyUsageCertSign
		}
		if parent == nil {
			parent, parentKey = tmpl, key
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
		if err != nil {
			t.Fatal(err)
		}
		c, _ := x509.ParseCertificate(der)
		return c, key
	}
	root, rootKey := mk("Test CA", true, nil, nil)
	inter, interKey := mk("Test Device CA", true, root, rootKey)
	leaf, _ = mk(serial, false, inter, interKey)
	roots = x509.NewCertPool()
	roots.AddCert(root)
	return roots, inter, leaf
}

// The camera of an X2D sends only the certificate of the printer. The check finds the chain with
// the device CA of the other connections, and still refuses a certificate with no chain to a root.
func TestCheckNoIntermediate(t *testing.T) {
	roots, inter, leaf := testChain(t, "20P5AJ0001")
	c := &Client{Serial: "20P5AJ0001", Roots: roots}
	camera := &tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}}
	if err := c.Check(camera); !errors.Is(err, ErrCert) {
		t.Fatalf("the camera before a full chain: %v, want ErrCert", err)
	}
	if err := c.Check(&tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf, inter}}); err != nil {
		t.Fatalf("the full chain (MQTT): %v", err)
	}
	if err := c.Check(camera); err != nil {
		t.Fatalf("the camera after the full chain: %v", err)
	}
	// Another chain of the same CN, with no root of the check.
	_, inter2, leaf2 := testChain(t, "20P5AJ0001")
	if err := c.Check(&tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf2, inter2}}); !errors.Is(err, ErrCert) {
		t.Fatalf("a chain to another root: %v, want ErrCert", err)
	}
	if err := verifyPrinter([]*x509.Certificate{leaf}, "20P5AJ0001", roots, inter); err != nil {
		t.Fatalf("a known intermediate: %v", err)
	}
}

// The embedded device CA chains to the embedded roots.
func TestEmbeddedDeviceCA(t *testing.T) {
	if len(bblIntermediates) == 0 {
		t.Fatal("no device CA")
	}
	for _, c := range bblIntermediates {
		if _, err := c.Verify(x509.VerifyOptions{Roots: bblRoots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}); err != nil {
			t.Errorf("%s: %v", c.Subject.CommonName, err)
		}
	}
}
