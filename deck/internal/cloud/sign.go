// Package cloud is the link of the deck to its cloud service (cloud/ in the repository): the share
// links and Parts with Friends. The deck has no cloud key. It has an Ed25519 key of its own, and
// signs each request to the API of the service with it. The API stores the public key at the
// enrollment of the deck. The types and the signature here are shared by the deck and the API.
package cloud

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"time"
)

// The headers of a signed request.
const (
	HeaderDeck      = "X-Printdeck-Deck"
	HeaderTime      = "X-Printdeck-Time"
	HeaderRequest   = "X-Printdeck-Request"
	HeaderSignature = "X-Printdeck-Signature"
)

// MaxSkew is the largest difference between the time of a signed request and the clock of the
// API.
const MaxSkew = 5 * time.Minute

// Errors of Verify.
var (
	ErrUnsigned = errors.New("the request has no signature")
	ErrClock    = errors.New("the time of the request is more than 5 minutes from the clock of the service: set the clock of the deck")
	ErrBadSign  = errors.New("the signature of the request is not valid")
)

// signedText is the text that a deck signs: a version, the method, the path with its query, the
// time, the request ID and the SHA-256 of the body, one on each line.
func signedText(method, path string, t int64, reqID string, body []byte) []byte {
	sum := sha256.Sum256(body)
	s := "printdeck-v1\n" + method + "\n" + path + "\n" + strconv.FormatInt(t, 10) + "\n" + reqID + "\n" +
		hex.EncodeToString(sum[:])
	return []byte(s)
}

// Sign adds the headers of the signature to h, for a request of the deck deckID with the method,
// the path (with the query, as in URL.RequestURI) and the body, at the time now.
func Sign(h http.Header, key ed25519.PrivateKey, deckID, method, path string, body []byte, now time.Time) {
	id := make([]byte, 12)
	rand.Read(id)
	reqID := base64.RawURLEncoding.EncodeToString(id)
	t := now.Unix()
	sig := ed25519.Sign(key, signedText(method, path, t, reqID, body))
	h.Set(HeaderDeck, deckID)
	h.Set(HeaderTime, strconv.FormatInt(t, 10))
	h.Set(HeaderRequest, reqID)
	h.Set(HeaderSignature, base64.RawURLEncoding.EncodeToString(sig))
}

// Signed is the signature part of a request: the deck, the request ID, and the time.
type Signed struct {
	Deck    string
	Request string
	Time    time.Time
}

// ReadSigned returns the deck and the request ID of the headers h, with no check of the
// signature.
func ReadSigned(h http.Header) (Signed, error) {
	deck, ts, req, sig := h.Get(HeaderDeck), h.Get(HeaderTime), h.Get(HeaderRequest), h.Get(HeaderSignature)
	if deck == "" || ts == "" || req == "" || sig == "" {
		return Signed{}, ErrUnsigned
	}
	t, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return Signed{}, ErrBadSign
	}
	return Signed{Deck: deck, Request: req, Time: time.Unix(t, 0)}, nil
}

// Verify checks the signature of a request with the public key of its deck, and the time of the
// request against now.
func Verify(pub ed25519.PublicKey, h http.Header, method, path string, body []byte, now time.Time) error {
	s, err := ReadSigned(h)
	if err != nil {
		return err
	}
	if d := now.Sub(s.Time); d > MaxSkew || d < -MaxSkew {
		return ErrClock
	}
	sig, err := base64.RawURLEncoding.DecodeString(h.Get(HeaderSignature))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return ErrBadSign
	}
	if !ed25519.Verify(pub, signedText(method, path, s.Time.Unix(), s.Request, body), sig) {
		return ErrBadSign
	}
	return nil
}
