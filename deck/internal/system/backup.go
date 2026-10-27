package system

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"printdeck/internal/backup"
)

// The backup of the data of the deck. The helper makes it and restores it, because the API key
// (api.env) and the Klipper config have other owners than the deck. The deck sends the file to the
// browser, and saves an uploaded file for a restore. Both files are in a fixed place, so the deck
// cannot make the helper read or write another file.

// Paths of the backup. The deck sets the Klipper folders from its config. Tests change them.
var (
	KlipperConfigDir = "/var/lib/klipper/config"
	GcodeDir         = "/var/lib/klipper/gcodes"
)

// BackupFile is the backup that the helper makes, and RestoreFile is the uploaded backup that it
// restores.
func BackupFile() string  { return filepath.Join(stateDir, "backup", "printdeck-backup.pdbk") }
func RestoreFile() string { return filepath.Join(stateDir, "backup", "restore.pdbk") }

// StepBackup and StepRestore are the steps of the progress.
const (
	StepBackup  = "Making the backup"
	StepRestore = "Restoring"
)

// backupSkip is the state that stays out of a backup: the password of this deck, the files that
// the deck makes again (the settings from printdeck.txt, the logs, the swap of the home screen),
// the folders of updates and backups, and the time lapse videos.
func backupSkip(rel string) bool {
	top, _, _ := strings.Cut(rel, "/")
	switch top {
	case "password", "settings.env", "logs", "swap-home", "update", "backup", "videos":
		return true
	}
	return false
}

// BackupSources are the folders in a backup: the state of the deck, the Klipper config, and the
// G-code files if gcode is true.
func BackupSources(gcode bool) []backup.Source {
	s := []backup.Source{
		{Prefix: "state", Dir: stateDir, Skip: backupSkip},
		{Prefix: "klipper-config", Dir: KlipperConfigDir},
	}
	if gcode {
		s = append(s, backup.Source{Prefix: "gcodes", Dir: GcodeDir})
	}
	return s
}

// backupDir makes the folder of the backup files, with the owner of the state folder (the deck).
func backupDir() (string, error) {
	dir := filepath.Dir(BackupFile())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	chownLike(dir, stateDir)
	return dir, nil
}

// chownLike gives p the owner of like. Only root can do it. A test runs as a user, and the owner
// stays.
func chownLike(p, like string) {
	fi, err := os.Stat(like)
	if err != nil {
		return
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		os.Chown(p, int(st.Uid), int(st.Gid))
	}
}

// MakeBackup writes the backup to BackupFile, encrypted with password. The deck can read the file.
func MakeBackup(password string, gcode bool, progress Progress) error {
	if len(password) < 8 {
		return errors.New("the backup password needs 8 characters or more")
	}
	dir, err := backupDir()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "backup-*.part")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	err = backup.Write(tmp, password, BackupSources(gcode), func(done, total int64) {
		if progress != nil {
			progress(StepBackup, done, total)
		}
	})
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	chownLike(tmp.Name(), stateDir)
	if err := os.Rename(tmp.Name(), BackupFile()); err != nil {
		return err
	}
	log.Printf("helper: backup made (G-code files: %v)", gcode)
	return nil
}

// RestoreBackup restores RestoreFile with password, and removes the file. The deck and Klipper
// must restart after it, to read the restored files.
func RestoreBackup(password string, progress Progress) error {
	defer os.Remove(RestoreFile())
	dir, err := backupDir()
	if err != nil {
		return err
	}
	f, err := os.Open(RestoreFile())
	if err != nil {
		return err
	}
	defer f.Close()
	// The restore reads all the sources that a backup can have. A backup without G-code files has
	// no entries in that folder.
	err = backup.Restore(f, password, BackupSources(true), dir, func(done, total int64) {
		if progress != nil {
			progress(StepRestore, done, total)
		}
	})
	if err != nil {
		return err
	}
	log.Printf("helper: backup restored")
	return nil
}

// restartAfterRestore restarts Klipper and the deck, after the deck sent its page to the browser.
func restartAfterRestore() {
	time.Sleep(3 * time.Second)
	if err := run("systemctl", "restart", "klipper.service", "printdeck-deck.service"); err != nil {
		log.Printf("helper: %v", err)
	}
}

// SetDataDirs sets the folders of the data of the deck, from its config.
func SetDataDirs(state, klipperConfig, gcodes string) {
	stateDir, KlipperConfigDir, GcodeDir = state, klipperConfig, gcodes
}
