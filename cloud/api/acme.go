package api

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"net/http"

	"golang.org/x/crypto/acme"
)

// The directories of Let's Encrypt. pwf deploy -acme-staging uses the staging directory, for
// tests: its certificates have no trust in the browsers.
const (
	LetsEncrypt        = "https://acme-v02.api.letsencrypt.org/directory"
	LetsEncryptStaging = "https://acme-staging-v02.api.letsencrypt.org/directory"
)

// acmeKeyFile is the key of the ACME account of the service in the bucket. The account has no
// email address.
const acmeKeyFile = "keys/acme-account"

// ACMEIssuer gets the certificates of the decks from an ACME CA with the DNS-01 challenge: it
// writes the TXT record _acme-challenge.<name> with DNS, and deletes it after the check.
type ACMEIssuer struct {
	Directory string
	Store     Store
	DNS       DNS
	// HTTP is the client of the ACME requests, or nil for the default client.
	HTTP *http.Client
}

// accountKey returns the key of the ACME account: from the bucket, or a new key at the first
// use.
func (a *ACMEIssuer) accountKey(ctx context.Context) (crypto.Signer, error) {
	b, _, err := a.Store.Get(ctx, acmeKeyFile)
	if errors.Is(err, ErrNotFound) {
		k, kerr := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if kerr != nil {
			return nil, kerr
		}
		der, _ := x509.MarshalPKCS8PrivateKey(k)
		b = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
		_, err = a.Store.Put(ctx, acmeKeyFile, b, PutOptions{ContentType: "application/octet-stream", IfNoneMatch: true})
		if errors.Is(err, ErrConflict) {
			b, _, err = a.Store.Get(ctx, acmeKeyFile) // another run made it first
		}
	}
	if err != nil {
		return nil, err
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return nil, errors.New("the ACME account key is not PEM")
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, err
	}
	s, ok := k.(crypto.Signer)
	if !ok {
		return nil, errors.New("the ACME account key is not a signer")
	}
	return s, nil
}

func (a *ACMEIssuer) Issue(ctx context.Context, name string, csr []byte) ([]byte, error) {
	key, err := a.accountKey(ctx)
	if err != nil {
		return nil, err
	}
	c := &acme.Client{Key: key, DirectoryURL: a.Directory, UserAgent: "printdeck-cloud", HTTPClient: a.HTTP}
	if _, err := c.Register(ctx, &acme.Account{}, acme.AcceptTOS); err != nil && !errors.Is(err, acme.ErrAccountAlreadyExists) {
		return nil, fmt.Errorf("register: %w", err)
	}
	order, err := c.AuthorizeOrder(ctx, acme.DomainIDs(name))
	if err != nil {
		return nil, fmt.Errorf("order: %w", err)
	}
	for _, u := range order.AuthzURLs {
		z, err := c.GetAuthorization(ctx, u)
		if err != nil {
			return nil, fmt.Errorf("authorization: %w", err)
		}
		if z.Status == acme.StatusValid {
			continue
		}
		var ch *acme.Challenge
		for _, x := range z.Challenges {
			if x.Type == "dns-01" {
				ch = x
			}
		}
		if ch == nil {
			return nil, errors.New("the CA offers no dns-01 challenge")
		}
		val, err := c.DNS01ChallengeRecord(ch.Token)
		if err != nil {
			return nil, err
		}
		rec := "_acme-challenge." + z.Identifier.Value
		if err := a.DNS.Set(ctx, rec, "TXT", []string{`"` + val + `"`}, 60, true); err != nil {
			return nil, fmt.Errorf("TXT record: %w", err)
		}
		defer func() {
			if err := a.DNS.Remove(context.WithoutCancel(ctx), rec, "TXT"); err != nil {
				log.Printf("acme: remove %s: %v", rec, err)
			}
		}()
		if _, err := c.Accept(ctx, ch); err != nil {
			return nil, fmt.Errorf("accept: %w", err)
		}
		if _, err := c.WaitAuthorization(ctx, z.URI); err != nil {
			return nil, fmt.Errorf("check: %w", err)
		}
	}
	if order, err = c.WaitOrder(ctx, order.URI); err != nil {
		return nil, fmt.Errorf("order: %w", err)
	}
	ders, _, err := c.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return nil, fmt.Errorf("finalize: %w", err)
	}
	var out []byte
	for _, d := range ders {
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: d})...)
	}
	return out, nil
}
