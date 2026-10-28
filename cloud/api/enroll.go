package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"printdeck/internal/cloud"
)

// The enrollment of a deck: the owner runs `pwf enroll-code`, which writes the hash of a random
// code to codes/, and types the code on the deck. The deck sends the code and its public key. The
// code works once.

// CodeTTL is how long an enrollment code works.
const CodeTTL = time.Hour

// codeAlphabet is the alphabet of the codes: no 0, O, 1, I or L, which look alike on a screen.
const codeAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// Code is the file of a code, codes/<sha256 of the code>.json.
type Code struct {
	Kind    string    `json:"kind"` // "deck"
	Expires time.Time `json:"expires"`
}

// normalCode is a code as the deck sends it, with no dashes or spaces, in upper case.
func normalCode(c string) string {
	c = strings.ToUpper(c)
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, c)
}

func codeKey(code string) string {
	sum := sha256.Sum256([]byte(normalCode(code)))
	return "codes/" + hex.EncodeToString(sum[:]) + ".json"
}

// NewCode writes a new code of the kind to the store, and returns the code as three groups of
// four characters (about 59 bits).
func NewCode(ctx context.Context, st Store, kind string, now time.Time) (string, error) {
	b := make([]byte, 12)
	rand.Read(b)
	var sb strings.Builder
	for i, v := range b {
		if i > 0 && i%4 == 0 {
			sb.WriteByte('-')
		}
		sb.WriteByte(codeAlphabet[int(v)%len(codeAlphabet)])
	}
	code := sb.String()
	data, _ := json.Marshal(Code{Kind: kind, Expires: now.Add(CodeTTL).UTC()})
	if _, err := st.Put(ctx, codeKey(code), data, PutOptions{ContentType: "application/json", IfNoneMatch: true}); err != nil {
		return "", err
	}
	return code, nil
}

// useCode checks a code of the kind and deletes it. It returns false for a wrong or expired code.
func (s *Server) useCode(ctx context.Context, code, kind string) (bool, error) {
	n := normalCode(code)
	if len(n) != 12 {
		return false, nil
	}
	key := codeKey(n)
	b, _, err := s.Store.Get(ctx, key)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := s.Store.Delete(ctx, key); err != nil {
		return false, err
	}
	var c Code
	if json.Unmarshal(b, &c) != nil || c.Kind != kind || s.now().After(c.Expires) {
		return false, nil
	}
	return true, nil
}

// enroll serves POST /api/v1/decks.
func (s *Server) enroll(ctx context.Context, body []byte) (any, error) {
	var in cloud.EnrollRequest
	if json.Unmarshal(body, &in) != nil {
		return nil, fail(http.StatusBadRequest, "bad JSON")
	}
	pub, err := base64.StdEncoding.DecodeString(in.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, fail(http.StatusBadRequest, "bad public key")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || utf8.RuneCountInString(name) > 64 {
		return nil, fail(http.StatusBadRequest, "the name of the deck must have 1 to 64 characters")
	}
	ok, err := s.useCode(ctx, in.Code, "deck")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fail(http.StatusUnauthorized, "the code is wrong, used or expired: make a new code with pwf enroll-code")
	}
	d := Deck{ID: newID(10), Name: name, PublicKey: pub, Enrolled: s.now().UTC()}
	data, _ := json.Marshal(d)
	if _, err := s.Store.Put(ctx, deckKey(d.ID), data, PutOptions{ContentType: "application/json", IfNoneMatch: true}); err != nil {
		return nil, err
	}
	return cloud.EnrollReply{Deck: d.ID}, nil
}

// Decks returns the enrolled decks, for pwf.
func Decks(ctx context.Context, st Store) ([]Deck, error) {
	keys, err := st.List(ctx, "decks/")
	if err != nil {
		return nil, err
	}
	var out []Deck
	for _, k := range keys {
		b, _, err := st.Get(ctx, k)
		if err != nil {
			continue
		}
		var d Deck
		if json.Unmarshal(b, &d) == nil {
			out = append(out, d)
		}
	}
	return out, nil
}

// RemoveDeck removes the deck id, for pwf.
func RemoveDeck(ctx context.Context, st Store, id string) error {
	return st.Delete(ctx, deckKey(id))
}
