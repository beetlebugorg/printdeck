package cloud

import (
	"context"
	"net/http"
	"time"
)

// The HTTPS address of a deck: the service gives each deck a name of its own,
// <host>.d.<domain of the service>. The host is random: the name goes into the public logs of the
// certificates, so it has no name of a person, a printer or a place. The A record of the name is
// the LAN address of the deck, and the deck serves HTTPS with a certificate for the name. The
// private key of the certificate stays on the deck: the deck sends a CSR, and the service gets
// the certificate from Let's Encrypt (ACME DNS-01 in its Route 53 zone).

// AddressRequest is the body of POST /api/v1/decks/self/address: the LAN address of the deck.
// The service accepts only private addresses.
type AddressRequest struct {
	IPv4 string `json:"ipv4"`
}

// AddressReply is the reply to POST /api/v1/decks/self/address and the name of the deck.
type AddressReply struct {
	Name string `json:"name"` // for example k3x9q2m7wfa4c.d.printdeck.beetlebug.org
}

// CertRequest is the body of POST /api/v1/decks/self/cert: a CSR (DER, base64) for the name of
// the deck.
type CertRequest struct {
	CSR string `json:"csr"`
}

// The states of a certificate.
const (
	CertPending = "pending" // the service gets the certificate
	CertReady   = "ready"
	CertFailed  = "failed"
)

// CertReply is the reply to POST and GET /api/v1/decks/self/cert.
type CertReply struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	// Chain is the certificate and its intermediates, in PEM, when Status is ready.
	Chain    string    `json:"chain,omitempty"`
	NotAfter time.Time `json:"not_after,omitzero"`
	Error    string    `json:"error,omitempty"`
}

// SetAddress sends the LAN address of the deck, and returns the name of the deck.
func (c *Client) SetAddress(ctx context.Context, id *Identity, ipv4 string) (AddressReply, error) {
	var out AddressReply
	err := c.Do(ctx, id, http.MethodPost, "/api/v1/decks/self/address", AddressRequest{IPv4: ipv4}, &out)
	return out, err
}

// RequestCert sends a CSR for the name of the deck.
func (c *Client) RequestCert(ctx context.Context, id *Identity, csrB64 string) (CertReply, error) {
	var out CertReply
	err := c.Do(ctx, id, http.MethodPost, "/api/v1/decks/self/cert", CertRequest{CSR: csrB64}, &out)
	return out, err
}

// Cert returns the state of the certificate of the deck.
func (c *Client) Cert(ctx context.Context, id *Identity) (CertReply, error) {
	var out CertReply
	err := c.Do(ctx, id, http.MethodGet, "/api/v1/decks/self/cert", nil, &out)
	return out, err
}
