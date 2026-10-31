package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"printdeck/internal/cloud"
	"printdeck/internal/ui"
)

// maxPlateFile is the largest file of a plate that the deck downloads.
const maxPlateFile = 400 << 20

// watchPlates puts the approved plates that the owner sends to the printer of the deck in its
// queue: each minute it reads them, downloads each file into the folder of the G-code files
// (builds/), checks its size and its SHA-256, adds it to the queue, and tells the service. The
// queue never starts a print.
func (p *projectLink) watchPlates(ctx context.Context, app *ui.App, gcodeDir string) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		id, printer := p.Identity(), p.printerID()
		if id == nil || printer == "" || app.Queue == nil {
			continue
		}
		rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		plates, err := p.Client.SentPlates(rctx, id, printer)
		cancel()
		if err != nil {
			continue
		}
		for _, pl := range plates {
			path, err := downloadPlate(ctx, gcodeDir, pl)
			if err != nil {
				log.Printf("plates: %s of %s: %v", pl.File, pl.ProjectName, err)
				continue
			}
			if err := app.EnqueueAs(path, "build", pl.Name, pl.ProjectName); err != nil {
				log.Printf("plates: the queue: %v", err)
				continue
			}
			tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err = p.Client.TakePlate(tctx, id, printer, cloud.PlateTaken{Project: pl.Project, Build: pl.Build, Plate: pl.Plate})
			cancel()
			if err != nil {
				log.Printf("plates: taken: %v", err)
			}
			log.Printf("plates: %s of %s is in the queue", path, pl.ProjectName)
		}
	}
}

// downloadPlate saves the file of the plate as builds/<build>-plate-<n>.gcode in the folder of the
// G-code files, and returns its path in the store. The size and the SHA-256 must match.
func downloadPlate(ctx context.Context, dir string, pl cloud.SentPlate) (string, error) {
	rel := fmt.Sprintf("builds/%s-plate-%d%s", pl.Build, pl.Plate+1, filepath.Ext(pl.File))
	if filepath.Ext(pl.File) == "" {
		rel += ".gcode"
	}
	dst := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	rctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, pl.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the download: %s", resp.Status)
	}
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxPlateFile+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
	case n > maxPlateFile:
		err = fmt.Errorf("the file is over %d MB", maxPlateFile>>20)
	case pl.Size > 0 && n != pl.Size:
		err = fmt.Errorf("the file has %d bytes, not %d", n, pl.Size)
	case pl.SHA256 != "" && hex.EncodeToString(h.Sum(nil)) != pl.SHA256:
		err = fmt.Errorf("the SHA-256 of the file does not match")
	}
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	return rel, os.Rename(tmp, dst)
}
