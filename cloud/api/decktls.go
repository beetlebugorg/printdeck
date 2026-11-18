package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"printdeck/internal/cloud"
)

// The HTTPS names of the decks (deck/internal/cloud/address.go). Each deck gets the name
// <host>.d.<Domain>, with a random host. A signed call of the deck sets the A record of the name
// to the LAN address of the deck. A second call sends a CSR for the name: a job gets the
// certificate from Let's Encrypt with ACME DNS-01, and the deck reads it with a third call. The
// private key of the certificate stays on the deck.

// DNS changes the records of the names of the decks in the Route 53 zone of the service
// (route53.go).
type DNS interface {
	// Set writes the record of the name and the type with the values. With wait, Set returns
	// when all the name servers of the zone have the change.
	Set(ctx context.Context, name, typ string, values []string, ttl int64, wait bool) error
	// Remove deletes the record of the name and the type. A record that does not exist is no
	// error.
	Remove(ctx context.Context, name, typ string) error
}

// Issuer gets a certificate for the name with the CSR (DER), and returns the chain in PEM
// (acme.go).
type Issuer interface {
	Issue(ctx context.Context, name string, csr []byte) ([]byte, error)
}

const (
	// deckRecordTTL is the TTL of the A record of a deck, in seconds: a new address of the deck
	// is in the caches of the browsers after about a minute.
	deckRecordTTL = 60
	// addressGap is the shortest time between two changes of the A record of a deck.
	addressGap = 30 * time.Second
	// renewBefore is the time before the end of a certificate when the deck gets a new one. A
	// request for a certificate with more time left gets the certificate that the service has.
	renewBefore = 30 * 24 * time.Hour
	// maxCerts is the most certificates of one deck in certWindow, under the limits of Let's
	// Encrypt (50 for each registered domain in a week, 5 for the same name in a week).
	maxCerts   = 3
	certWindow = 7 * 24 * time.Hour
	// certRetry is the time after a request or a failure before a new request starts a new job.
	certRetry = 10 * time.Minute
	// certJobTime is the most time of a certificate job in the jobs function.
	certJobTime = 4 * time.Minute
)

// certRecord is the file of the certificate of a deck, certs/<deck>.json.
type certRecord struct {
	Status    string      `json:"status"`
	Name      string      `json:"name"`
	CSR       []byte      `json:"csr"`
	Key       string      `json:"key"` // the SHA-256 of the public key of the CSR
	Requested time.Time   `json:"requested"`
	Chain     string      `json:"chain,omitempty"`
	ChainKey  string      `json:"chain_key,omitempty"` // the SHA-256 of the public key of Chain
	NotAfter  time.Time   `json:"not_after,omitzero"`
	Error     string      `json:"error,omitempty"`
	Issued    []time.Time `json:"issued,omitempty"` // the times of the certificates in certWindow
}

func certKey(deck string) string { return "certs/" + deck + ".json" }

// deckName returns the HTTPS name of the deck d, and makes its host at the first use.
func (s *Server) deckName(ctx context.Context, d *Deck) (string, error) {
	if s.Domain == "" || s.DNS == nil {
		return "", fail(http.StatusServiceUnavailable, "this service gives no HTTPS names to the decks")
	}
	if d.Host == "" {
		d.Host = newID(8)
		if err := s.saveDeck(ctx, d); err != nil {
			return "", err
		}
	}
	return d.Host + ".d." + s.Domain, nil
}

func (s *Server) saveDeck(ctx context.Context, d *Deck) error {
	data, _ := json.Marshal(d)
	_, err := s.Store.Put(ctx, deckKey(d.ID), data, PutOptions{ContentType: "application/json"})
	return err
}

// privateIPv4 returns the address a when it is a private IPv4 address (RFC 1918). The zone then
// cannot point a name of a deck to a host on the internet.
func privateIPv4(a string) (string, bool) {
	ip := net.ParseIP(strings.TrimSpace(a)).To4()
	if ip == nil || !ip.IsPrivate() {
		return "", false
	}
	return ip.String(), true
}

// setAddress serves POST /api/v1/decks/self/address.
func (s *Server) setAddress(ctx context.Context, r *http.Request, d *Deck, body []byte) (any, error) {
	var in cloud.AddressRequest
	if json.Unmarshal(body, &in) != nil {
		return nil, fail(http.StatusBadRequest, "bad JSON")
	}
	ip, ok := privateIPv4(in.IPv4)
	if !ok {
		return nil, fail(http.StatusBadRequest, "the address must be a private IPv4 address (10/8, 172.16/12 or 192.168/16)")
	}
	// The time of the signature must be newer than the time of the last address: a replay of an
	// old request cannot set an old address.
	sg, _ := cloud.ReadSigned(r.Header)
	if sg.Time.Unix() <= d.AddressAt {
		return nil, fail(http.StatusConflict, "the service has a newer address of the deck")
	}
	name, err := s.deckName(ctx, d)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if ip != d.IPv4 {
		if now.Sub(d.AddressSet) < addressGap {
			return nil, fail(http.StatusTooManyRequests, "the address changed less than 30 s ago: try again later")
		}
		if err := s.DNS.Set(ctx, name, "A", []string{ip}, deckRecordTTL, false); err != nil {
			return nil, err
		}
		d.IPv4, d.AddressSet = ip, now.UTC()
	}
	d.AddressAt = sg.Time.Unix()
	if err := s.saveDeck(ctx, d); err != nil {
		return nil, err
	}
	return cloud.AddressReply{Name: name}, nil
}

// checkCSR parses the CSR (base64 of DER) and checks it: its signature, exactly the one name,
// and a P-256 or P-384 key, or an RSA key of 2048 bits or more. It returns the DER and the
// SHA-256 of the public key.
func checkCSR(b64, name string) ([]byte, string, error) {
	der, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(der) > 8<<10 {
		return nil, "", errors.New("the CSR is not base64 of DER")
	}
	csr, err := x509.ParseCertificateRequest(der)
	if err != nil {
		return nil, "", errors.New("the CSR is not valid")
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, "", errors.New("the signature of the CSR is not valid")
	}
	if len(csr.DNSNames) != 1 || !strings.EqualFold(csr.DNSNames[0], name) || len(csr.IPAddresses) > 0 ||
		len(csr.EmailAddresses) > 0 || len(csr.URIs) > 0 || (csr.Subject.CommonName != "" && !strings.EqualFold(csr.Subject.CommonName, name)) {
		return nil, "", errors.New("the CSR must have only the name " + name)
	}
	switch k := csr.PublicKey.(type) {
	case *ecdsa.PublicKey:
		if k.Curve != elliptic.P256() && k.Curve != elliptic.P384() {
			return nil, "", errors.New("the key of the CSR must be P-256, P-384, or RSA of 2048 bits or more")
		}
	case *rsa.PublicKey:
		if k.N.BitLen() < 2048 {
			return nil, "", errors.New("the key of the CSR must be P-256, P-384, or RSA of 2048 bits or more")
		}
	default:
		return nil, "", errors.New("the key of the CSR must be P-256, P-384, or RSA of 2048 bits or more")
	}
	sum := sha256.Sum256(csr.RawSubjectPublicKeyInfo)
	return der, hex.EncodeToString(sum[:]), nil
}

func (s *Server) loadCert(ctx context.Context, deck string) (*certRecord, string, error) {
	b, etag, err := s.Store.Get(ctx, certKey(deck))
	if errors.Is(err, ErrNotFound) {
		return &certRecord{}, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	var rec certRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		return nil, "", err
	}
	return &rec, etag, nil
}

func (s *Server) saveCert(ctx context.Context, deck string, rec *certRecord, etag string) error {
	data, _ := json.Marshal(rec)
	opt := PutOptions{ContentType: "application/json", IfMatch: etag, IfNoneMatch: etag == ""}
	_, err := s.Store.Put(ctx, certKey(deck), data, opt)
	if errors.Is(err, ErrConflict) {
		return fail(http.StatusConflict, "the certificate of the deck changed: try again")
	}
	return err
}

func (rec *certRecord) reply() cloud.CertReply {
	out := cloud.CertReply{Name: rec.Name, Status: rec.Status, Error: rec.Error}
	if rec.Status == cloud.CertReady {
		out.Chain, out.NotAfter = rec.Chain, rec.NotAfter
	}
	return out
}

// requestCert serves POST /api/v1/decks/self/cert.
func (s *Server) requestCert(ctx context.Context, d *Deck, body []byte) (any, error) {
	if s.Issuer == nil {
		return nil, fail(http.StatusServiceUnavailable, "this service gives no certificates to the decks")
	}
	var in cloud.CertRequest
	if json.Unmarshal(body, &in) != nil {
		return nil, fail(http.StatusBadRequest, "bad JSON")
	}
	name, err := s.deckName(ctx, d)
	if err != nil {
		return nil, err
	}
	der, key, err := checkCSR(in.CSR, name)
	if err != nil {
		return nil, fail(http.StatusBadRequest, err.Error())
	}
	rec, etag, err := s.loadCert(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	switch {
	case rec.Status == cloud.CertReady && rec.Name == name && rec.ChainKey == key && rec.NotAfter.Sub(now) > renewBefore:
		return rec.reply(), nil // the certificate that the service has
	case rec.Status != cloud.CertReady && rec.Name == name && rec.Key == key && now.Sub(rec.Requested) < certRetry:
		return rec.reply(), nil // the job runs, or failed a short time ago
	}
	var recent []time.Time
	for _, t := range rec.Issued {
		if now.Sub(t) < certWindow {
			recent = append(recent, t)
		}
	}
	if len(recent) >= maxCerts {
		return nil, fail(http.StatusTooManyRequests, "the deck got 3 certificates in 7 days: try again later")
	}
	rec.Issued = recent
	rec.Status, rec.Name, rec.CSR, rec.Key, rec.Requested, rec.Error = cloud.CertPending, name, der, key, now.UTC(), ""
	if err := s.saveCert(ctx, d.ID, rec, etag); err != nil {
		return nil, err
	}
	if !s.later(ctx, Job{Kind: "cert", Deck: d.ID}) {
		jctx, cancel := context.WithTimeout(ctx, inlineJobTime)
		defer cancel()
		s.IssueCert(jctx, d.ID)
		if rec, _, err = s.loadCert(ctx, d.ID); err != nil {
			return nil, err
		}
	}
	return rec.reply(), nil
}

// getCert serves GET /api/v1/decks/self/cert.
func (s *Server) getCert(ctx context.Context, d *Deck) (any, error) {
	rec, etag, err := s.loadCert(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	if etag == "" {
		return nil, fail(http.StatusNotFound, "the deck has asked for no certificate")
	}
	return rec.reply(), nil
}

// IssueCert runs the certificate job of the deck: it gets the certificate of a pending request.
func (s *Server) IssueCert(ctx context.Context, deck string) {
	rec, etag, err := s.loadCert(ctx, deck)
	if err != nil || rec.Status != cloud.CertPending || s.Issuer == nil {
		return
	}
	chain, err := s.Issuer.Issue(ctx, rec.Name, rec.CSR)
	var leaf *x509.Certificate
	if err == nil {
		leaf, err = firstCert(chain)
	}
	// Read the record again: the deck can send a new CSR during the job.
	now, csr := s.now(), rec.CSR
	rec, etag, lerr := s.loadCert(context.WithoutCancel(ctx), deck)
	if lerr != nil || rec.Status != cloud.CertPending || string(rec.CSR) != string(csr) {
		return
	}
	if err != nil {
		log.Printf("cert %s: %v", deck, err)
		rec.Status, rec.Error = cloud.CertFailed, "Let's Encrypt gave no certificate: "+shortError(err)
	} else {
		sum := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
		rec.Status, rec.Chain, rec.ChainKey, rec.NotAfter = cloud.CertReady, string(chain), hex.EncodeToString(sum[:]), leaf.NotAfter.UTC()
		rec.Issued = append(rec.Issued, now.UTC())
	}
	if err := s.saveCert(context.WithoutCancel(ctx), deck, rec, etag); err != nil {
		log.Printf("cert %s: %v", deck, err)
	}
}

// firstCert returns the first certificate of a PEM chain.
func firstCert(chain []byte) (*x509.Certificate, error) {
	b, _ := pem.Decode(chain)
	if b == nil || b.Type != "CERTIFICATE" {
		return nil, errors.New("the chain has no certificate")
	}
	return x509.ParseCertificate(b.Bytes)
}

// shortError returns the first line of the error, with at most 200 characters.
func shortError(err error) string {
	m, _, _ := strings.Cut(err.Error(), "\n")
	if len(m) > 200 {
		m = m[:200]
	}
	return m
}

// forgetDeckName deletes the A record and the certificate file of the deck d.
func forgetDeckName(ctx context.Context, st Store, dns DNS, domain string, d *Deck) error {
	if d.Host != "" && dns != nil && domain != "" {
		if err := dns.Remove(ctx, d.Host+".d."+domain, "A"); err != nil {
			return err
		}
	}
	err := st.Delete(ctx, certKey(d.ID))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}
