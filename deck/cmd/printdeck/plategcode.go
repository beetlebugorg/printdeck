package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"printdeck/internal/bambu"
)

// plateGcode copies the G-code of the first plate of the sliced 3MF at path to a file in the
// temporary folder, and returns that file. The copy has the size and the time of the 3MF in its
// name, so a new 3MF under the same name gets a new copy. The folder keeps one copy.
func plateGcode(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(os.TempDir(), "printdeck-3mf")
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s %d %d", path, fi.Size(), fi.ModTime().UnixNano())))
	out := filepath.Join(dir, hex.EncodeToString(sum[:8])+".gcode")
	if _, err := os.Stat(out); err == nil {
		return out, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	plates, err := bambu.Plates(f, fi.Size())
	if err != nil || len(plates) == 0 {
		return "", fmt.Errorf("no sliced plate: %v", err)
	}
	zr, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return "", err
	}
	rc, err := zr.Open(plates[0].GcodePath())
	if err != nil {
		return "", err
	}
	defer rc.Close()
	os.RemoveAll(dir) // the copy of the file before
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, "plate-*")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(tmp, io.LimitReader(rc, 1<<30)); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return out, os.Rename(tmp.Name(), out)
}
