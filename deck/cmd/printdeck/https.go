package main

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"printdeck/internal/cloud"
	"printdeck/internal/tlscert"
)

// deckTLS keeps the HTTPS certificate of the deck (tlscert), or is nil with no TLS_ADDR. The
// system page and /slicer/printer.json show its address.
var deckTLS *tlscert.Manager

// startTLSCert starts the loop of the HTTPS name and certificate of the deck, when cfg has a TLS
// address. A deck with no cloud service gets no certificate, and serves HTTP only.
func startTLSCert(ctx context.Context, stateDir, tlsAddr string, link *cloud.Link) {
	if tlsAddr == "" || link == nil {
		return
	}
	deckTLS = &tlscert.Manager{Dir: filepath.Join(stateDir, "tls"), Link: link, Client: &cloud.Client{}}
	go deckTLS.Run(ctx)
}

// httpsURL returns the HTTPS address of the deck, or "".
func httpsURL() string {
	if deckTLS == nil {
		return ""
	}
	return deckTLS.URL()
}

// serveHTTPS serves h on addr with the certificate of the deck. The server starts at once: a
// connection before the first certificate fails its handshake. HTTP stays on, with no redirect:
// a router that blocks DNS names with LAN addresses (DNS rebinding protection) blocks the HTTPS
// name, and then HTTP is the only way to the deck.
func serveHTTPS(addr string, h http.Handler) {
	if deckTLS == nil {
		return
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("https server is off: %v", err)
		return
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second,
		ErrorLog: log.New(discardHandshakes{}, "", 0)}
	tln := tls.NewListener(ln, &tls.Config{GetCertificate: deckTLS.GetCertificate, MinVersion: tls.VersionTLS12})
	go func() {
		log.Printf("https server on %s", addr)
		if err := srv.Serve(tln); err != nil {
			log.Printf("https server is off: %v", err)
		}
	}()
}

// discardHandshakes drops the log lines of the HTTPS server: each connection before the first
// certificate, and each scan of the port, logs a failed handshake.
type discardHandshakes struct{}

func (discardHandshakes) Write(p []byte) (int, error) { return len(p), nil }
