package main

import (
	"bytes"
	"context"
	"image"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"printdeck/internal/config"
	"printdeck/internal/gcodestore"
	"printdeck/internal/gfx"
	"printdeck/internal/notify"
	"printdeck/internal/octoapi"
	"printdeck/internal/printer"
	"printdeck/internal/remote"
	"printdeck/internal/touch"
	"printdeck/internal/ui"
)

// startWeb starts the web server of the Printdeck on addr, and returns the remote screen (or nil).
// The server has two parts:
//
//   - the remote screen at /, with the password of the Printdeck user (when the system has one)
//   - the OctoPrint-compatible API for slicers at /api, /plugin/appkeys and /downloads (with the
//     Klipper backend)
//   - the state of the simulated printer at /sim, for the 3D window of the desktop app (with an
//     API key)
func startWeb(cfg config.Config, host *setupHost, pr printerBackend, app *ui.App) *remote.Server {
	if cfg.RemoteAddr == "" {
		return nil
	}
	mux := http.NewServeMux()
	api := newAPI(cfg, pr, app, mux)
	var rs *remote.Server
	if host != nil {
		rs = newRemote(app, api, func() string {
			_, pass := host.Login()
			return pass
		}, nil)
		rs.Source = sourceURL(osVersion())
		mux.Handle("/", rs.Handler())
		registerSystemPage(mux, rs, pr, app.Metrics, cfg.KlippyLog, cfg.OctoPrintKey)
		registerRollPages(mux, rs, pr, app)
		// The 3D window of the desktop app shows the simulated printer of the deck, with an API key.
		if k, ok := pr.(*printer.Klippy); ok && api != nil {
			registerSim(mux, k, api.ValidKey)
		}
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

// newAPI registers the OctoPrint-compatible API for slicers on mux, and returns it. It returns nil
// for a printer that is not a Klipper host.
func newAPI(cfg config.Config, pr printerBackend, app *ui.App, mux *http.ServeMux) *octoapi.Server {
	k, ok := pr.(*printer.Klippy)
	if !ok {
		return nil
	}
	api := octoapi.New(k, &gcodestore.Store{Dir: cfg.GcodeDir}, cfg.OctoPrintKey, filepath.Join(cfg.StateDir, "apikeys.json"))
	api.Version, api.Ask = osVersion(), app.Ask
	api.Spools = k.Spools
	api.Diameter = func() float64 { return k.Machine().Diameter() }
	materialFile := filepath.Join(cfg.StateDir, "material")
	api.Material = func() string {
		b, _ := os.ReadFile(materialFile)
		return strings.TrimSpace(string(b))
	}
	api.Frame = cameraFrame(app)
	api.StartPrint = app.ConfirmFilePrint
	api.Status = func() any { return app.StatusScreen() }
	api.Model = k.View.ModelPicture
	// The phones of the Printdeck app for push messages; a new phone gets a test message.
	api.Phones = phonesIn(cfg.StateDir)
	api.OnPaired = func(ph notify.Phone) {
		go func() {
			m := notify.Message{Title: "Printdeck", Body: "This phone gets the messages of your prints.", Tags: "printer"}
			if err := notify.SendPhone(context.Background(), ph, m, time.Now()); err != nil {
				log.Printf("notify: phone %s: the test message: %v", ph.ID, err)
			}
		}()
	}
	api.Register(mux)
	return api
}

// newRemote returns the remote screen of app. password is the password of the login. The screen
// also accepts the API keys of api (for the Printdeck tab of Cura) and the keys that key accepts.
func newRemote(app *ui.App, api *octoapi.Server, password func() string, key func(*http.Request) bool) *remote.Server {
	rs := remote.New()
	rs.Password = password
	rs.NewSession = uiSession(app)
	rs.Icon = ui.Icon()
	rs.APIKey = func(r *http.Request) bool {
		return (key != nil && key(r)) || (api != nil && api.ValidKey(r))
	}
	return rs
}

// uiSession returns the function that starts a session of the UI for one browser: an App on the
// same printer, with the screen shape of the browser, that records its frames. A frame that is the
// same as the last one is not sent.
func uiSession(app *ui.App) func(ctx context.Context, size image.Point, frame func([]byte)) (image.Point, remote.Input) {
	return func(ctx context.Context, size image.Point, frame func([]byte)) (image.Point, remote.Input) {
		s := app.Session(size.X, size.Y)
		s.Record = true
		events := make(chan touch.Event, 64)
		var last []byte
		go s.Run(ctx, events, func(c *gfx.Canvas) {
			if b := c.Recording().JSON(); !bytes.Equal(b, last) {
				last = b
				frame(b)
			}
		})
		scr := s.Screen()
		return image.Pt(scr.W, scr.H), remote.Input{Touch: func(e touch.Event) {
			select {
			case events <- e:
			default: // the session is busy; drop the touch, as the touch screen does
			}
		}, Key: s.Key}
	}
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

// cameraFrame returns a function that draws the screen of the deck, as on a display that is
// mounted wide, for the webcam of the slicers. The frames have their own screens, like a browser.
func cameraFrame(app *ui.App) func() image.Image {
	var mu sync.Mutex
	var s *ui.App
	var cv *gfx.Canvas
	return func() image.Image {
		mu.Lock()
		defer mu.Unlock()
		if s == nil {
			s = app.Session(800, 480)
			cv = gfx.New(800, 480)
		}
		s.Draw(cv)
		return image.Image(cloneRGBA(cv.RGBA))
	}
}

// cloneRGBA copies an image, so that the next frame does not change it.
func cloneRGBA(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(src.Rect)
	copy(dst.Pix, src.Pix)
	return dst
}
