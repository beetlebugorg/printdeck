package signing

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A file that zslice signed (src/gcode_sign.zig) with the key of the seed of 32 bytes of 7.
const (
	zsliceFile = "G28\nG1 X10 Y10 F3000\n" +
		"; zslice-signature: ed25519 fe812c12f3ab4ce6 NpWBONZsbyx0M1Cv+gX7xxjDAS+DEGDv7a6uUsFzf4HWjjaj9qeaHXY7OhF9I8evNBAhk9VM4pSV/pWVTjlsDw==\n"
	zsliceKey = "ed25519 6kpsY+KcUgq+9VB7Ey7F+ZVHdq6+vnuSQh7qaRRG0iw="
)

func testKey(b byte) ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{b}, ed25519.SeedSize))
}

// sign signs body as zslice does.
func sign(key ed25519.PrivateKey, body string) string {
	h := sha256.Sum256([]byte(body))
	sig := ed25519.Sign(key, h[:])
	return body + LinePrefix + "ed25519 " + KeyID(key.Public().(ed25519.PublicKey)) + " " + base64.StdEncoding.EncodeToString(sig) + "\n"
}

func check(t *testing.T, file string, trusted ...ed25519.PublicKey) Verdict {
	t.Helper()
	v, err := Check(strings.NewReader(file), int64(len(file)), trusted)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestZsliceFile(t *testing.T) {
	pub, err := ParsePublic(zsliceKey)
	if err != nil {
		t.Fatal(err)
	}
	if KeyID(pub) != "fe812c12f3ab4ce6" {
		t.Errorf("key id %s", KeyID(pub))
	}
	if !bytes.Equal(pub, testKey(7).Public().(ed25519.PublicKey)) {
		t.Error("the public key of the seed")
	}
	if got := sign(testKey(7), "G28\nG1 X10 Y10 F3000\n"); got != zsliceFile {
		t.Errorf("the signature of Go and zslice differ:\n%s", got)
	}
	if v := check(t, zsliceFile, pub); v.Result != OK || v.KeyID != "fe812c12f3ab4ce6" {
		t.Errorf("%+v", v)
	}
}

func TestGoodSignature(t *testing.T) {
	k := testKey(1)
	f := sign(k, strings.Repeat("G1 X10 Y10 E0.5\n", 5000))
	if v := check(t, f, testKey(2).Public().(ed25519.PublicKey), k.Public().(ed25519.PublicKey)); v.Result != OK {
		t.Errorf("%+v", v)
	}
	// No newline after the signature line, and a CRLF.
	if v := check(t, strings.TrimSuffix(f, "\n"), k.Public().(ed25519.PublicKey)); v.Result != OK {
		t.Errorf("no newline: %+v", v)
	}
	if v := check(t, strings.TrimSuffix(f, "\n")+"\r\n", k.Public().(ed25519.PublicKey)); v.Result != OK {
		t.Errorf("CRLF: %+v", v)
	}
}

func TestChangedByte(t *testing.T) {
	k := testKey(1)
	body := "G28\nM104 S200\nG1 X10\n"
	f := []byte(sign(k, body))
	for i := range len(body) {
		f[i] ^= 1
		if v := check(t, string(f), k.Public().(ed25519.PublicKey)); v.Result == OK {
			t.Errorf("byte %d changed: %+v", i, v)
		}
		f[i] ^= 1
	}
	if v := check(t, string(f), k.Public().(ed25519.PublicKey)); v.Result != OK {
		t.Errorf("%+v", v)
	}
}

func TestUnknownKey(t *testing.T) {
	f := sign(testKey(1), "G28\n")
	v := check(t, f, testKey(2).Public().(ed25519.PublicKey))
	if v.Result != UnknownKey || v.KeyID != KeyID(testKey(1).Public().(ed25519.PublicKey)) {
		t.Errorf("%+v", v)
	}
	if !strings.HasPrefix(v.Message(), "Rejected: not signed by a trusted slicer (key ") || !strings.Contains(v.Message(), v.KeyID) {
		t.Errorf("message %q", v.Message())
	}
	if v := check(t, f); v.Result != UnknownKey {
		t.Errorf("no keys: %+v", v)
	}
}

func TestUnsigned(t *testing.T) {
	k := testKey(1).Public().(ed25519.PublicKey)
	for _, f := range []string{"", "G28\n", "G28\nG1 X1", sign(testKey(1), "G28\n") + "G1 X5\n"} {
		if v := check(t, f, k); v.Result != Unsigned || v.KeyID != "" {
			t.Errorf("%q: %+v", f, v)
		}
	}
	if v := check(t, "G28\n; zslice-signature: ed25519 0011 AAAA\n", k); v.Result != Bad {
		t.Errorf("malformed: %+v", v)
	}
}

func TestStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "signing.json")
	s := &Store{Path: path}
	gcode := filepath.Join(dir, "a.gcode")
	os.WriteFile(gcode, []byte("G28\n"), 0o644)
	if s.SignedOnly() || s.Allowed(gcode) != "" {
		t.Fatal("off by default: each file goes in")
	}
	if _, err := s.Add("zslice", "ed25519 AAAA", time.Now()); err == nil {
		t.Error("a bad key")
	}
	k, err := s.Add("zslice", zsliceKey, time.Now())
	if err != nil || k.ID != "fe812c12f3ab4ce6" {
		t.Fatal(k, err)
	}
	if _, err := s.Add("zslice 2", zsliceKey, time.Now()); err != nil || len(s.Keys()) != 1 || s.Keys()[0].App != "zslice 2" {
		t.Errorf("the same key twice: %v %+v", err, s.Keys())
	}
	if err := s.SetSignedOnly(true); err != nil {
		t.Fatal(err)
	}
	if msg := s.Allowed(gcode); msg != "Rejected: not signed by a trusted slicer (the file has no signature)" {
		t.Errorf("unsigned: %q", msg)
	}
	os.WriteFile(gcode, []byte(zsliceFile), 0o644)
	if msg := s.Allowed(gcode); msg != "" {
		t.Errorf("signed: %q", msg)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode())
	}
	// A new store reads the file.
	s2 := &Store{Path: path}
	if !s2.SignedOnly() || len(s2.Keys()) != 1 {
		t.Errorf("reload: %v %+v", s2.SignedOnly(), s2.Keys())
	}
	if err := s2.Remove(k.ID); err != nil || len(s2.Keys()) != 0 {
		t.Error(err)
	}
	if msg := s2.Allowed(gcode); msg != "Rejected: not signed by a trusted slicer (key fe812c12f3ab4ce6)" {
		t.Errorf("removed key: %q", msg)
	}
	if s2.Remove(k.ID) == nil {
		t.Error("remove twice")
	}
}
