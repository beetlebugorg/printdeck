package main

import (
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"printdeck/internal/config"
	"printdeck/internal/gcodestore"
	"printdeck/internal/octoapi"
	"printdeck/internal/printer"
	"printdeck/internal/remote"
	"printdeck/internal/touch"
	"printdeck/internal/ui"
	"printdeck/internal/webui"
)

// startWeb starts the web server of the Printdeck on addr, and returns the remote screen (or nil).
// The server has two parts:
//
//   - the remote screen at /, with the password of the Printdeck user (when the system has one)
//   - the OctoPrint-compatible API for slicers at /api, /plugin/appkeys and /downloads (with the
//     Klipper backend)
func startWeb(cfg config.Config, host *setupHost, events chan<- touch.Event, pr printerBackend, app *ui.App) *remote.Server {
	if cfg.RemoteAddr == "" {
		return nil
	}
	mux := http.NewServeMux()
	if k, ok := pr.(*printer.Klippy); ok {
		api := octoapi.New(k, &gcodestore.Store{Dir: cfg.GcodeDir}, cfg.OctoPrintKey, filepath.Join(cfg.StateDir, "apikeys.json"))
		api.Version, api.Ask = osVersion(), app.Ask
		api.Register(mux)
	}
	var rs *remote.Server
	if host != nil {
		rs = remote.New()
		rs.Source = sourceURL(osVersion())
		rs.Password = func() string {
			_, pass := host.Login()
			return pass
		}
		rs.Touch = func(e touch.Event) {
			select {
			case events <- e:
			default: // the deck is busy; drop the touch, as the touch screen does
			}
		}
		mux.Handle("/", rs.Handler())
		klippyLog := filepath.Join(filepath.Dir(filepath.Dir(cfg.KlipperConfig)), "logs", "klippy.log")
		registerSystemPage(mux, rs, pr, klippyLog)
		page := &webui.Server{Printer: pr, Volume: app.Vol, Store: &gcodestore.Store{Dir: cfg.GcodeDir},
			PreheatTool: cfg.PreheatTool, PreheatBed: cfg.PreheatBed, Auth: rs.Auth}
		if app.Music != nil {
			page.Music = app.Music
		}
		page.Register(mux)
	}
	srv := &http.Server{Addr: cfg.RemoteAddr, Handler: remote.SecurityHeaders(mux), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("web server on %s", cfg.RemoteAddr)
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("web server is off: %v", err)
		}
	}()
	return rs
}

// sourceURL is the address of the source code of a version from "git describe", for example
// "v2.2.0" (a tag), "v2.2.0-3-g1a2b3c4" or "1a2b3c4" (a commit). A version with "-dirty" has
// changes that are not in the repository, so it gets the address of the repository.
func sourceURL(version string) string {
	switch {
	case version == "" || strings.HasSuffix(version, "-dirty"):
		return remote.DefaultSource
	case describeAfterTag.MatchString(version):
		return remote.DefaultSource + "/tree/" + describeAfterTag.FindStringSubmatch(version)[1]
	case tagOrCommit.MatchString(version):
		return remote.DefaultSource + "/tree/" + version
	}
	return remote.DefaultSource
}

var (
	describeAfterTag = regexp.MustCompile(`^.+-[0-9]+-g([0-9a-f]{7,40})$`)
	tagOrCommit      = regexp.MustCompile(`^(v[0-9][0-9A-Za-z.\-]*|[0-9a-f]{7,40})$`)
)
