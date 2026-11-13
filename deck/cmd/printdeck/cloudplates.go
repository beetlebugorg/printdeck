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
	"printdeck/internal/gcode"
	"printdeck/internal/printer"
	"printdeck/internal/ui"
)

// maxPlateFile is the largest file of a plate that the deck downloads.
const maxPlateFile = 400 << 20

// watchPlates puts the approved plates that the owner sends to the printer of the deck in its
// queue: each minute it reads them, downloads each file into the folder of the G-code files
// (builds/), checks its size and its SHA-256, adds it to the queue, and tells the service. The
// queue never starts a print.
func (p *projectLink) watchPlates(ctx context.Context, app *ui.App, place func(context.Context, cloud.SentPlate) (string, error)) {
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
		p.applyRemoves(ctx, app.Queue)
		rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		plates, err := p.Client.SentPlates(rctx, id, printer)
		cancel()
		if err != nil {
			log.Printf("plates: %v", err)
			continue
		}
		for _, pl := range plates {
			path, err := place(ctx, pl)
			if err != nil {
				log.Printf("plates: %s of %s: %v", pl.File, pl.ProjectName, err)
				continue
			}
			if err := p.bindProject(id, pl.ProjectName, pl.Project); err != nil {
				log.Printf("plates: the project of %s: %v", pl.File, err)
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

// plateName is the name of the file of a plate on the deck: <build>-plate-<n> with the extension
// of the file.
func plateName(pl cloud.SentPlate) string {
	ext := filepath.Ext(pl.File)
	if ext == "" {
		ext = ".gcode"
	}
	return fmt.Sprintf("%s-plate-%d%s", pl.Build, pl.Plate+1, ext)
}

// gcodePlate saves the file of an FDM plate as builds/<name> in the folder of the G-code files,
// and returns its path in the store.
func gcodePlate(dir string) func(context.Context, cloud.SentPlate) (string, error) {
	return func(ctx context.Context, pl cloud.SentPlate) (string, error) {
		rel := "builds/" + plateName(pl)
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		if err := downloadTo(ctx, dst, pl); err != nil {
			return "", err
		}
		if gcode.IsFile(dst) {
			if err := gcode.AddObjects(dst); err != nil {
				return "", fmt.Errorf("the objects of %s: %w", rel, err)
			}
		}
		return rel, nil
	}
}

// resinPlate saves the file of a resin plate (a .cbddlp of ChiTuBox) in the copies of the deck,
// and sends it to the board in the background: after the print, when the board prints. The
// queue gets the name on the board. It never starts a print: a press on the deck does.
func resinPlate(c *printer.ChiTu) func(context.Context, cloud.SentPlate) (string, error) {
	return func(ctx context.Context, pl cloud.SentPlate) (string, error) {
		name := plateName(pl)
		if c.UploadDir == "" {
			return "", fmt.Errorf("the deck has no folder for resin files")
		}
		if err := os.MkdirAll(c.UploadDir, 0o755); err != nil {
			return "", err
		}
		tmp := filepath.Join(c.UploadDir, ".plate-"+name)
		if err := downloadTo(ctx, tmp, pl); err != nil {
			return "", err
		}
		dst, err := c.AddFile(name, tmp)
		if err != nil {
			return "", err
		}
		go func() {
			if err := c.SendFile(dst); err != nil {
				log.Printf("plates: %s to the printer: %v", name, err)
			}
		}()
		return name, nil
	}
}

// downloadTo saves the file of the plate at dst. The size and the SHA-256 must match the build.
func downloadTo(ctx context.Context, dst string, pl cloud.SentPlate) error {
	rctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, pl.URL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the download: %s", resp.Status)
	}
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
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
		return err
	}
	return os.Rename(tmp, dst)
}
