package system

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

// Paths of the credentials. Tests change them.
var (
	stateDir    = "/var/lib/printdeck"
	userProgram = "/usr/bin/printdeck-user"
)

// loginName is the user for SSH and sudo in the debug image.
const loginName = "printdeck"

// Credentials makes the login of this Printdeck at the first start:
//
//   - the password of the remote screen, four random words. The touch screen shows it
//     (Print > Control > Help). In the debug image, it is also the password of the user
//     "printdeck" for SSH and sudo.
//   - the key of the OctoPrint-compatible API, for slicers that take a key (OrcaSlicer,
//     PrusaSlicer). printdeck-deck.service reads it from api.env.
func Credentials() error {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(stateDir, 0o700); err != nil {
		return err
	}
	pwFile := filepath.Join(stateDir, "password")
	if b, err := os.ReadFile(pwFile); err != nil || len(b) == 0 {
		pass, err := NewPassword()
		if err != nil {
			return err
		}
		// The debug image has the program that makes the login user.
		if _, err := os.Stat(userProgram); err == nil {
			if err := run(userProgram, loginName, pass); err != nil {
				return err
			}
		}
		if err := writeFile(pwFile, []byte(pass+"\n"), 0o600); err != nil {
			return err
		}
	}
	// The deck shows the password on the Help screen. It runs as the user deck.
	if u, err := user.Lookup("deck"); err == nil {
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)
		os.Chown(stateDir, uid, gid)
		os.Chown(pwFile, uid, gid)
	}

	apiFile := filepath.Join(stateDir, "api.env")
	if b, err := os.ReadFile(apiFile); err != nil || len(strings.TrimSpace(string(b))) == 0 {
		key := make([]byte, 16)
		if _, err := rand.Read(key); err != nil {
			return err
		}
		if err := writeFile(apiFile, []byte("OCTOPRINT_API_KEY="+hex.EncodeToString(key)+"\n"), 0o600); err != nil {
			return err
		}
	}
	return nil
}
