// Package gcodestore keeps the G-code files and STL models of the printer in one folder: the folder of
// Klipper's [virtual_sdcard], so Klipper reads and prints a file by its name (SDCARD_PRINT_FILE).
// The print server saves uploads here, and the deck lists and opens them.
package gcodestore

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"printdeck/internal/printer"
)

// Store is a folder of printable files.
type Store struct {
	Dir string
}

// ErrBadName is the error for a path that leaves the folder or has no G-code or STL extension.
var ErrBadName = errors.New("gcodestore: not a G-code or STL file name in the store")

// Kinds of files, by extension.
func isGcode(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".gcode", ".g", ".gco":
		return true
	}
	return false
}

func isModel(name string) bool { return strings.EqualFold(filepath.Ext(name), ".stl") }

// clean checks a path from a request, for example "parts/cap.gcode", and returns it in a clean form.
// It refuses ".." and names that start with a dot, so a path always stays inside the folder.
func clean(path string) (string, error) {
	path = strings.TrimPrefix(filepath.ToSlash(path), "/")
	for _, part := range strings.Split(path, "/") {
		if part == "" || strings.HasPrefix(part, ".") {
			return "", ErrBadName
		}
	}
	if !(isGcode(path) || isModel(path)) {
		return "", ErrBadName
	}
	return path, nil
}

// Files lists the G-code files, newest first.
func (s *Store) Files() []printer.File { return s.list(isGcode) }

// Models lists the STL models, newest first.
func (s *Store) Models() []printer.File { return s.list(isModel) }

func (s *Store) list(keep func(string) bool) []printer.File {
	var files []printer.File
	filepath.WalkDir(s.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !keep(d.Name()) || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(s.Dir, path)
		files = append(files, printer.File{Path: filepath.ToSlash(rel), Name: d.Name(), Date: info.ModTime()})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].Date.After(files[j].Date) })
	return files
}

// Open opens a file of the store.
func (s *Store) Open(path string) (io.ReadCloser, error) {
	p, err := clean(path)
	if err != nil {
		return nil, err
	}
	return os.Open(filepath.Join(s.Dir, p))
}

// Stat returns the information of a file of the store.
func (s *Store) Stat(path string) (fs.FileInfo, error) {
	p, err := clean(path)
	if err != nil {
		return nil, err
	}
	return os.Stat(filepath.Join(s.Dir, p))
}

// Save writes a file into the store, and returns its clean path. It writes to a temporary file first,
// so a broken upload never replaces a good file, and Klipper never reads a half-written file.
func (s *Store) Save(path string, r io.Reader) (string, error) {
	p, err := clean(path)
	if err != nil {
		return "", err
	}
	dst := filepath.Join(s.Dir, p)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".upload-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name()) // after the rename this does nothing
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return "", fmt.Errorf("gcodestore: save %s: %w", p, err)
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	os.Chmod(tmp.Name(), 0o644)
	return p, os.Rename(tmp.Name(), dst)
}

// FullPath returns the path in the file system of a file of the store.
func (s *Store) FullPath(path string) (string, error) {
	p, err := clean(path)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.Dir, p), nil
}

// Delete removes a file of the store.
func (s *Store) Delete(path string) error {
	p, err := clean(path)
	if err != nil {
		return err
	}
	return os.Remove(filepath.Join(s.Dir, p))
}
