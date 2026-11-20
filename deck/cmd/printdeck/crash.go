package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
)

// crashDir is the folder of the crash files of the deck program, or "".
var crashDir string

// keepCrashes writes the output of a crash of the deck program (a panic, or a fault in the C code
// of zslice, with the stacks) to crash.log in dir. At the start, the crash.log of the run before
// becomes last-crash.log, when it has a crash in it.
func keepCrashes(dir string) {
	cur, last := filepath.Join(dir, "crash.log"), filepath.Join(dir, "last-crash.log")
	if fi, err := os.Stat(cur); err == nil && fi.Size() > 0 {
		if err := os.Rename(cur, last); err != nil {
			log.Printf("crash log: %v", err)
		} else {
			log.Printf("crash log: the deck program crashed in the run before; see %s", last)
		}
	}
	f, err := os.OpenFile(cur, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		log.Printf("crash log: %v", err)
		return
	}
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		log.Printf("crash log: %v", err)
	}
	f.Close() // SetCrashOutput keeps its own copy of the file
	crashDir = dir
}

// serveCrash sends the output of the last crash of the deck program, or 404.
func serveCrash(w http.ResponseWriter, r *http.Request) {
	if crashDir == "" {
		http.NotFound(w, r)
		return
	}
	b, err := os.ReadFile(filepath.Join(crashDir, "last-crash.log"))
	if err != nil {
		http.Error(w, "No crash of the deck program", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(b)
}
