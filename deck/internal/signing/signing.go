// Package signing checks the signatures of G-code files, and keeps the setting "Signed prints
// only" and the public keys of the trusted slicers.
//
// zslice signs each G-code file. The last line of a signed file is a comment:
//
//	; zslice-signature: ed25519 <key id> <base64 signature>
//
// The signature is Ed25519 (RFC 8032) over the SHA-256 hash of all bytes before that line. The
// key id is the first 8 bytes of the SHA-256 hash of the 32 bytes of the public key, in lowercase
// hex. A public key text is "ed25519 <base64 of the 32 bytes>". The format is the format of
// src/gcode_sign.zig in zslice.
//
// A slicer gets trust when it pairs: the app sends its public key with the App Keys request, and
// the "Allow" press on the touch screen adds the key.
package signing

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// LinePrefix is the start of the signature line.
const LinePrefix = "; zslice-signature: "

// tailSize is the most bytes at the end of a file that hold the signature line and its newline.
const tailSize = 512

// Result is the result of a check.
type Result int

const (
	OK         Result = iota // a good signature of a trusted key
	Unsigned                 // no signature line
	UnknownKey               // the key is not trusted
	Bad                      // a malformed line, or a file that changed after the signature
)

// Verdict is the result of a check and the key id of the signature line ("" with no line).
type Verdict struct {
	Result Result
	KeyID  string
}

// Rejected is the start of the message for a file that fails the check.
const Rejected = "Rejected: not signed by a trusted slicer"

// Message is the reason for a rejection, for the screen and the API: "" for OK.
func (v Verdict) Message() string {
	switch v.Result {
	case OK:
		return ""
	case Unsigned:
		return Rejected + " (the file has no signature)"
	case UnknownKey:
		return Rejected + " (key " + v.KeyID + ")"
	}
	if v.KeyID == "" {
		return Rejected + " (the signature does not read)"
	}
	return Rejected + " (the file changed after the signature, key " + v.KeyID + ")"
}

// KeyID is the key id of a public key.
func KeyID(pub ed25519.PublicKey) string {
	h := sha256.Sum256(pub)
	return hex.EncodeToString(h[:8])
}

// ParsePublic reads a public key text: "ed25519 <base64>".
func ParsePublic(text string) (ed25519.PublicKey, error) {
	alg, b, ok := strings.Cut(strings.TrimSpace(text), " ")
	if !ok || alg != "ed25519" {
		return nil, errors.New("not an ed25519 public key")
	}
	k, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b))
	if err != nil || len(k) != ed25519.PublicKeySize {
		return nil, errors.New("the public key does not read")
	}
	return ed25519.PublicKey(k), nil
}

// PublicText is the text of a public key.
func PublicText(pub ed25519.PublicKey) string {
	return "ed25519 " + base64.StdEncoding.EncodeToString(pub)
}

// line is a parsed signature line.
type line struct {
	keyID string
	sig   []byte
}

func parseLine(s string) (line, bool) {
	f := strings.Fields(strings.TrimPrefix(s, LinePrefix))
	if len(f) != 3 || f[0] != "ed25519" || len(f[1]) != 16 {
		return line{}, false
	}
	id := strings.ToLower(f[1])
	if _, err := hex.DecodeString(id); err != nil {
		return line{}, false
	}
	sig, err := base64.StdEncoding.DecodeString(f[2])
	if err != nil || len(sig) != ed25519.SignatureSize {
		return line{}, false
	}
	return line{keyID: id, sig: sig}, true
}

// split finds the signature line in the tail of a file that starts at offset off: the last line,
// before at most one newline ("\n" or "\r\n"). It returns the offset of the start of the line in
// the file, and the line, or -1 with no signature line.
func split(tail []byte, off int64) (int64, string) {
	end := len(tail)
	if end > 0 && tail[end-1] == '\n' {
		end--
	}
	if end > 0 && tail[end-1] == '\r' {
		end--
	}
	i := bytes.LastIndexByte(tail[:end], '\n')
	if i < 0 && off > 0 {
		return -1, "" // the line is longer than the tail
	}
	s := string(tail[i+1 : end])
	if !strings.HasPrefix(s, LinePrefix) {
		return -1, ""
	}
	return off + int64(i+1), s
}

// Check checks the signature of the file f of size bytes against the trusted keys.
func Check(f io.ReaderAt, size int64, trusted []ed25519.PublicKey) (Verdict, error) {
	off := max(0, size-tailSize)
	tail := make([]byte, size-off)
	if _, err := f.ReadAt(tail, off); err != nil && !errors.Is(err, io.EOF) {
		return Verdict{}, err
	}
	start, s := split(tail, off)
	if start < 0 {
		return Verdict{Result: Unsigned}, nil
	}
	ln, ok := parseLine(s)
	if !ok {
		return Verdict{Result: Bad}, nil
	}
	v := Verdict{Result: UnknownKey, KeyID: ln.keyID}
	i := slices.IndexFunc(trusted, func(k ed25519.PublicKey) bool { return KeyID(k) == ln.keyID })
	if i < 0 {
		return v, nil
	}
	h := sha256.New()
	if _, err := io.Copy(h, io.NewSectionReader(f, 0, start)); err != nil {
		return Verdict{}, err
	}
	if !ed25519.Verify(trusted[i], h.Sum(nil), ln.sig) {
		v.Result = Bad
		return v, nil
	}
	v.Result = OK
	return v, nil
}

// CheckFile checks the signature of the file at path.
func CheckFile(path string, trusted []ed25519.PublicKey) (Verdict, error) {
	f, err := os.Open(path)
	if err != nil {
		return Verdict{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return Verdict{}, err
	}
	return Check(f, fi.Size(), trusted)
}

// Key is a trusted public key.
type Key struct {
	ID     string    `json:"id"`
	Public string    `json:"public"` // the public key text
	App    string    `json:"app"`    // the name of the app at pairing, for example "zslice"
	Added  time.Time `json:"added"`
}

// Store keeps the setting and the trusted keys in a JSON file that only root can read.
type Store struct {
	Path string

	mu         sync.Mutex
	loaded     bool
	signedOnly bool
	keys       []Key
}

type storeFile struct {
	SignedOnly bool  `json:"signed_only"`
	Keys       []Key `json:"keys"`
}

// load reads the file once. s.mu must be locked.
func (s *Store) load() {
	if s.loaded {
		return
	}
	s.loaded = true
	b, err := os.ReadFile(s.Path)
	if err != nil {
		return
	}
	var f storeFile
	if json.Unmarshal(b, &f) == nil {
		s.signedOnly, s.keys = f.SignedOnly, f.Keys
	}
}

// save writes the file. s.mu must be locked.
func (s *Store) save() error {
	if s.Path == "" {
		return nil
	}
	b, err := json.MarshalIndent(storeFile{SignedOnly: s.signedOnly, Keys: s.keys}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	tmp := s.Path + ".new"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

// SignedOnly tells if the deck accepts only files with a good signature of a trusted key.
func (s *Store) SignedOnly() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	return s.signedOnly
}

// SetSignedOnly turns "Signed prints only" on or off.
func (s *Store) SetSignedOnly(on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	s.signedOnly = on
	return s.save()
}

// Keys returns the trusted keys, the oldest first.
func (s *Store) Keys() []Key {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	return slices.Clone(s.keys)
}

// Add trusts the public key text of the app. A key that is there already keeps its place and
// gets the new app name.
func (s *Store) Add(app, public string, now time.Time) (Key, error) {
	pub, err := ParsePublic(public)
	if err != nil {
		return Key{}, err
	}
	k := Key{ID: KeyID(pub), Public: PublicText(pub), App: app, Added: now.UTC()}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if i := slices.IndexFunc(s.keys, func(o Key) bool { return o.ID == k.ID }); i >= 0 {
		s.keys[i].App = app
		return s.keys[i], s.save()
	}
	s.keys = append(s.keys, k)
	return k, s.save()
}

// Remove stops the trust of the key with the id.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	n := len(s.keys)
	s.keys = slices.DeleteFunc(s.keys, func(k Key) bool { return k.ID == id })
	if len(s.keys) == n {
		return fmt.Errorf("no trusted key %s", id)
	}
	return s.save()
}

// trusted returns the public keys. s.mu must be locked.
func (s *Store) trusted() []ed25519.PublicKey {
	var out []ed25519.PublicKey
	for _, k := range s.keys {
		if pub, err := ParsePublic(k.Public); err == nil {
			out = append(out, pub)
		}
	}
	return out
}

// Allowed checks the file at path when "Signed prints only" is on. It returns "" when the deck
// can queue the file, or else the reason for the rejection.
func (s *Store) Allowed(path string) string {
	s.mu.Lock()
	s.load()
	on, trusted := s.signedOnly, s.trusted()
	s.mu.Unlock()
	if !on {
		return ""
	}
	v, err := CheckFile(path, trusted)
	if err != nil {
		return Rejected + " (the file does not read)"
	}
	return v.Message()
}
