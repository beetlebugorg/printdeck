package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/printer"
)

// The printer.cfg of the Klipper host, for the editor of the system page and for scripts:
//
//	GET /system/printer.cfg           the file, as text
//	GET /system/printer.cfg?backup=1  the file before the last PUT
//	PUT /system/printer.cfg           the new file, as the body
//
// A PUT checks the new file, writes the old file to the backup (printer.cfg.bak in the state
// folder of the deck: the deck cannot make files in the config folder of Klipper), writes the
// new file, and restarts Klipper. It refuses a file with a parse error or with no [printer]
// section and no include, and it refuses during a print. The login of the remote screen or an API key of the
// OctoPrint-compatible API gives access.

// maxPrinterCfg is the largest printer.cfg of a PUT.
const maxPrinterCfg = 1 << 20

// cfgPrinter is the part of the Klipper backend that the endpoint uses.
type cfgPrinter interface {
	State() printer.State
	RestartKlipper() error
}

func registerPrinterCfg(mux *http.ServeMux, path, backup string, pr cfgPrinter, allow func(*http.Request) bool) {
	mux.HandleFunc("GET /system/printer.cfg", func(w http.ResponseWriter, r *http.Request) {
		if !allow(r) {
			http.Error(w, "Log in, or send an API key in X-Api-Key.", http.StatusUnauthorized)
			return
		}
		p := path
		if r.URL.Query().Get("backup") == "1" {
			p = backup
		}
		b, err := os.ReadFile(p)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(b)
	})
	mux.HandleFunc("PUT /system/printer.cfg", func(w http.ResponseWriter, r *http.Request) {
		if !allow(r) {
			http.Error(w, "Log in, or send an API key in X-Api-Key.", http.StatusUnauthorized)
			return
		}
		b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPrinterCfg))
		if err != nil {
			http.Error(w, "The file is too large.", http.StatusRequestEntityTooLarge)
			return
		}
		if pr.State().Busy() {
			http.Error(w, "A print runs. Change printer.cfg after the print.", http.StatusConflict)
			return
		}
		b = bytes.ReplaceAll(b, []byte("\r"), nil)
		if err := checkPrinterCfg(b); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		old, err := os.ReadFile(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := replaceFile(backup, old); err != nil {
			http.Error(w, "The backup failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := replaceFile(path, b); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp := map[string]any{"saved": true, "backup": "printer.cfg.bak", "restarted": true}
		if err := pr.RestartKlipper(); err != nil {
			resp["restarted"], resp["error"] = false, "The restart of Klipper failed: "+err.Error()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
}

// replaceFile writes data to a new file in the folder of path, and renames it over path. The rename
// needs only write access to the folder: Klipper (the user printer) can own the old file, for example
// after SAVE_CONFIG, and the deck (in the group printer) can still replace it. The new file is 0664,
// so that Klipper can replace it too.
func replaceFile(path string, data []byte) error {
	dir, name := filepath.Split(path)
	f, err := os.CreateTemp(dir, "."+name+"-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Chmod(0o664)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// checkPrinterCfg returns an error for a file that the config parser of Klipper cannot read, or
// that has no [printer] section and no include.
func checkPrinterCfg(b []byte) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("the file cannot be read: %v", p)
		}
	}()
	fc := configfile.NewFileConfig()
	if err := fc.Read(string(b), "printer.cfg"); err != nil {
		return err
	}
	for _, s := range fc.Sections() {
		if s == "printer" || strings.HasPrefix(s, "include ") {
			return nil
		}
	}
	return errors.New("the file has no [printer] section and no [include]")
}
