package system

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	keys, unsigned := updateKeys, allowUnsigned
	defer func() { updateKeys, allowUnsigned = keys, unsigned }()
	updateKeys = []string{base64.StdEncoding.EncodeToString(pub)}
	allowUnsigned = func() bool { return false }

	dir := t.TempDir()
	sums := []byte("abc  rootfs.erofs\n")
	os.WriteFile(filepath.Join(dir, "SHA256SUMS"), sums, 0o644)
	if err := checkSignature(dir); err == nil {
		t.Error("a bundle with no signature passed")
	}
	os.WriteFile(filepath.Join(dir, "SHA256SUMS.sig"), ed25519.Sign(priv, sums), 0o644)
	if err := checkSignature(dir); err != nil {
		t.Errorf("a signed bundle failed: %v", err)
	}
	// A changed SHA256SUMS: the signature does not match.
	os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte("abd  rootfs.erofs\n"), 0o644)
	if err := checkSignature(dir); err == nil {
		t.Error("a bundle with a changed SHA256SUMS passed")
	}
	// The debug image takes it.
	allowUnsigned = func() bool { return true }
	if err := checkSignature(dir); err != nil {
		t.Errorf("the debug image refused it: %v", err)
	}
}
