package spotify

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"html"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Login with a phone. The screen shows a QR code for LinkBase/link. That page sends the phone to Spotify,
// and Spotify sends it back to LinkBase/callback, which this server answers. Spotify allows only https
// for such an address, so the server has a self-signed certificate. The phone asks once to accept it.

// LinkURL is the address for the QR code, or "" if the login with a phone is off.
func (c *Client) LinkURL() string {
	if c.cfg.LinkBase == "" || c.cfg.ClientID == "" {
		return ""
	}
	return c.cfg.LinkBase + "/link"
}

type session struct{ state, verifier string }

// ServeLink answers the login requests until ctx ends.
func (c *Client) ServeLink(ctx context.Context, notify func()) error {
	base, err := url.Parse(c.cfg.LinkBase)
	if err != nil || c.cfg.ClientID == "" {
		return fmt.Errorf("no link address")
	}
	cert, err := loadOrMakeCert(filepath.Join(c.cfg.StateDir, "link-cert.pem"), filepath.Join(c.cfg.StateDir, "link-key.pem"))
	if err != nil {
		return err
	}
	redirect := c.cfg.LinkBase + "/callback"
	var (
		mu  sync.Mutex
		cur session
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/link", func(w http.ResponseWriter, r *http.Request) {
		raw := make([]byte, 16)
		rand.Read(raw)
		state := base64.RawURLEncoding.EncodeToString(raw)
		link, verifier := c.AuthURL(redirect, state)
		mu.Lock()
		cur = session{state, verifier}
		mu.Unlock()
		http.Redirect(w, r, link, http.StatusFound)
	})
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		q := r.URL.Query()
		msg := "Spotify is linked. You can close this page."
		switch {
		case q.Get("error") != "":
			msg = "Spotify refused: " + q.Get("error")
		case cur.state == "" || q.Get("state") != cur.state:
			msg = "This login is not valid any more. Scan the code on the screen again."
		default:
			if err := c.Exchange(q.Get("code"), cur.verifier, redirect); err != nil {
				msg = "The login failed: " + err.Error()
			} else {
				cur = session{}
				notify()
			}
		}
		fmt.Fprintf(w, `<!doctype html><meta name=viewport content="width=device-width"><body style="font:22px sans-serif;padding:2em"><h2>Speakerdeck</h2><p>%s</p>`, html.EscapeString(msg))
	})
	srv := &http.Server{
		Addr: ":" + base.Port(), Handler: mux,
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
	}
	go func() { <-ctx.Done(); srv.Close() }()
	return srv.ListenAndServeTLS("", "")
}

// loadOrMakeCert keeps one certificate for the speaker, so the phone does not ask again.
func loadOrMakeCert(certFile, keyFile string) (tls.Certificate, error) {
	if c, err := tls.LoadX509KeyPair(certFile, keyFile); err == nil {
		// Make a new one a month before it ends.
		if leaf, err := x509.ParseCertificate(c.Certificate[0]); err == nil && time.Until(leaf.NotAfter) > 30*24*time.Hour {
			return c, nil
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	host, _ := os.Hostname()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now().Add(-time.Hour),
		// Phones refuse certificates that are valid for more than about 13 months.
		NotAfter:              time.Now().AddDate(0, 0, 397),
		BasicConstraintsValid: true,
		IsCA:                  true, // self-signed: the phone can also be told to trust it
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{host, host + ".local", "localhost"},
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && !strings.HasPrefix(ipn.IP.String(), "169.254") {
				tmpl.IPAddresses = append(tmpl.IPAddresses, ipn.IP)
			}
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	kb, _ := x509.MarshalECPrivateKey(key)
	os.MkdirAll(filepath.Dir(certFile), 0o700)
	os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600)
	return tls.LoadX509KeyPair(certFile, keyFile)
}
