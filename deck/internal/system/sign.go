package system

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
)

// updateKeys are the public keys of the Printdeck releases (ed25519, base64). A bundle must have a
// signature of SHA256SUMS by one of them: SHA256SUMS.sig, made by os/board/post-image.sh. The
// private key is the secret PRINTDECK_SIGNING_KEY of the GitHub repository.
var updateKeys = []string{"KI5zkeJ32/y2KV6uabQfQ/oVmog6q5Ia9kkmpB3/j6w="}

// allowUnsigned reports that this system accepts a bundle with no valid signature: the debug image,
// for development. Tests change it.
var allowUnsigned = func() bool { return Variant() == "debug" }

// checkSignature checks SHA256SUMS.sig in dir against SHA256SUMS. checkSums then checks each file
// against SHA256SUMS.
func checkSignature(dir string) error {
	sums, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return errors.New("The bundle is damaged: it has no SHA256SUMS")
	}
	sig, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS.sig"))
	if err != nil {
		if allowUnsigned() {
			return nil
		}
		return errors.New("The bundle has no signature. Install a release of Printdeck.")
	}
	for _, k := range updateKeys {
		pub, err := base64.StdEncoding.DecodeString(k)
		if err == nil && len(pub) == ed25519.PublicKeySize && ed25519.Verify(pub, sums, sig) {
			return nil
		}
	}
	if allowUnsigned() {
		return nil
	}
	return errors.New("The signature of the bundle is not valid. Install a release of Printdeck.")
}
