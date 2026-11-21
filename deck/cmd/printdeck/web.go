package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
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
//     Klipper backend), and the webcam at /webcam
//   - printer.cfg at /system/printer.cfg, with the login or an API key
//   - the state of the simulated printer at /sim, for the 3D window of the desktop app (with an
//     API key)
//
// The API answers CORS requests of the web pages in WEB_ORIGINS (the zslice web app): see
// remote.CORS.
// webPrinter is a printer of a deck with many printers that is not the first one, for the web
// server: its settings, its backend and its App.
type webPrinter struct {
	id  string
	cfg config.Config
	pr  printerBackend
	app *ui.App
}

func startWeb(cfg config.Config, host *setupHost, pr printerBackend, app *ui.App, up *updater, others []webPrinter) *remote.Server {
	if cfg.RemoteAddr == "" {
		return nil
	}
	mux := http.NewServeMux()
	gpuGcodeDir = cfg.GcodeDir
	api := newAPI(cfg, pr, app, mux)
	// The deck keys: an App Keys request at the root gives a key of each printer of the deck.
	deckKeys := octoapi.NewDeckKeys(filepath.Join(deckDir(cfg), "deck-apikeys.json"))
	if api != nil {
		api.DeckKeys, api.DeckWide = deckKeys, true
		api.PrinterCheck = printerCheck("")
	}
	// The API of each other printer: under /printers/<id>/, with its own API keys and the deck
	// keys. A request at /api with a key of another printer goes to that printer (keyRouter), so
	// a slicer needs only the key of its printer. A deck key stays at the root.
	var routes []keyRoute
	var otherAPIs []*octoapi.Server
	keyList := []printerKeys{{id: "", name: printerName(app), api: api}}
	for _, o := range others {
		pm := http.NewServeMux()
		if oa := newAPI(o.cfg, o.pr, o.app, pm); oa != nil {
			oa.DeckKeys = deckKeys
			oa.PrinterCheck = printerCheck(o.id)
			mux.Handle("/printers/"+o.id+"/", http.StripPrefix("/printers/"+o.id, pm))
			routes = append(routes, keyRoute{valid: oa.OwnKey, h: pm})
			otherAPIs = append(otherAPIs, oa)
			keyList = append(keyList, printerKeys{id: o.id, name: printerName(o.app), api: oa})
		}
	}
	ka := &keyAdmin{deck: deckKeys, printers: keyList}
	app.Keys = ka
	for _, o := range others {
		o.app.Keys = ka
	}
	var rs *remote.Server
	if host != nil {
		rs = newRemote(app, api, func() string {
			_, pass := host.Login()
			return pass
		}, nil)
		rs.Source = sourceURL(osVersion())
		if api != nil {
			api.Allow = rs.LoggedIn // the webcam in a browser with the login of the remote screen
			api.Login = rs.LoggedIn // the uploads of the zslice app (/slicer/)
		}
		for _, oa := range otherAPIs {
			oa.Allow, oa.Login = rs.LoggedIn, rs.LoggedIn
		}
		mux.Handle("/", rs.Handler())
		registerSystemPage(mux, rs, pr, up, app.Metrics, cfg.KlippyLog, cfg.OctoPrintKey)
		registerRollPages(mux, rs, pr, app)
		registerSlicer(mux, rs, slicerDir, deckKeys.ValidRequest)
		// The 3D window of the desktop app shows the simulated printer of the deck, with an API key.
		if k, ok := pr.(*printer.Klippy); ok && api != nil {
			registerSim(mux, k, api.ValidKey)
		}
		// printer.cfg, for the editor of the system page and for scripts with an API key.
		if k, ok := pr.(*printer.Klippy); ok && k.ConfigPath != "" {
			allow := rs.LoggedIn
			if api != nil {
				// With "Signed prints only" on, an API key reads printer.cfg, and only the login
				// writes it: a change of printer.cfg changes the limits of each print.
				allow = func(r *http.Request) bool {
					if rs.LoggedIn(r) {
						return true
					}
					return api.ValidKey(r) && (r.Method == http.MethodGet || app.Signing == nil || !app.Signing.SignedOnly())
				}
			}
			targets := map[string]cfgTarget{"": {k.ConfigPath, filepath.Join(cfg.StateDir, "printer.cfg.bak"), k}}
			for _, o := range others {
				if ko, isK := o.pr.(*printer.Klippy); isK && ko.ConfigPath != "" {
					targets[o.id] = cfgTarget{ko.ConfigPath, filepath.Join(o.cfg.StateDir, "printer.cfg.bak"), ko}
				}
			}
			registerPrinterCfg(mux, targets, allow)
		}
	}
	// The zslice web app can call the API from the browser (WEB_ORIGINS).
	h := remote.CORS(remote.SecurityHeaders(keyRouter(mux, routes)), cfg.WebOrigins)
	srv := &http.Server{Addr: cfg.RemoteAddr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("web server on %s", cfg.RemoteAddr)
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("web server is off: %v", err)
		}
	}()
	// The same server on HTTPS, with the certificate of the deck's name (https.go).
	if cfg.TLSAddr != "" {
		serveHTTPS(cfg.TLSAddr, h)
	}
	return rs
}

// newAPI registers the OctoPrint-compatible API for slicers on mux, and returns it. It returns nil
// for a printer that is not a Klipper host.
func newAPI(cfg config.Config, pr printerBackend, app *ui.App, mux *http.ServeMux) *octoapi.Server {
	if c, ok := pr.(*printer.ChiTu); ok {
		return newResinAPI(cfg, c, app, mux)
	}
	if b, ok := pr.(*printer.Bambu); ok {
		return newBambuAPI(cfg, b, app, mux)
	}
	k, ok := pr.(*printer.Klippy)
	if !ok {
		return nil
	}
	api := octoapi.New(k, &gcodestore.Store{Dir: cfg.GcodeDir}, cfg.OctoPrintKey, filepath.Join(cfg.StateDir, "apikeys.json"))
	api.Version, api.Ask = osVersion(), askOn(app)
	// The lengths of the swap of the touch screen, for the load and the unload of the apps.
	api.SwapLengths = func() (float64, float64) { return app.UnloadMM, app.LoadMM }
	api.Spools = k.Spools
	api.Diameter = func() float64 { return k.Machine().Diameter() }
	materialFile := filepath.Join(cfg.StateDir, "material")
	api.Material = func() string {
		b, _ := os.ReadFile(materialFile)
		return strings.TrimSpace(string(b))
	}
	api.Frame = cameraFrame(app)
	if cam, ok := app.Camera.(octoapi.Camera); ok {
		api.Camera = cam
	}
	if r := timelapseOf(app); r != nil {
		api.Timelapse = r
	}
	api.History = k.History
	api.Projects = func() []string {
		if cp, ok := app.Cloud.(ui.CloudProjects); ok && app.Cloud.State().Connected {
			return cp.Projects()
		}
		return nil
	}
	api.StartPrint = app.ConfirmFilePrint
	api.Signing, api.Rejected = app.Signing, app.ShowRejected
	api.Queue = app.Queue
	api.PrinterID = func() string {
		if pl, ok := app.Cloud.(*projectLink); ok {
			return pl.printerID()
		}
		return ""
	}
	api.Enqueue = func(path string) error { return app.Enqueue(path, "slicer") }
	api.Status = func() any { return app.StatusScreen() }
	api.Model = k.View.ModelPicture
	api.Estimate = k.EstimateFile
	api.Limits = k.Limits
	// The phones of the Printdeck app for push messages; a new phone gets a test message.
	api.Phones = phonesIn(deckDir(cfg))
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

// newBambuAPI registers the API for slicers and apps on the deck of a Bambu Lab printer: the upload
// of sliced 3MF files to the store of the deck, the start of a print with the question of the deck,
// the status, the controls, the camera and the push messages.
func newBambuAPI(cfg config.Config, b *printer.Bambu, app *ui.App, mux *http.ServeMux) *octoapi.Server {
	store, ok := b.Store.(*gcodestore.Store)
	if !ok {
		store = &gcodestore.Store{Dir: cfg.GcodeDir, ThreeMF: true}
	}
	api := octoapi.New(b, store, cfg.OctoPrintKey, filepath.Join(cfg.StateDir, "apikeys.json"))
	api.Version, api.Ask = osVersion(), askOn(app)
	api.Spools = b.Spools
	api.Signing, api.Rejected = app.Signing, app.ShowRejected
	api.Queue = app.Queue
	api.PrinterID = func() string {
		if pl, ok := app.Cloud.(*projectLink); ok {
			return pl.printerID()
		}
		return ""
	}
	api.Frame = cameraFrame(app)
	if cam, ok := app.Camera.(octoapi.Camera); ok {
		api.Camera = cam
	}
	api.History = b.History
	api.StartPrint = app.ConfirmFilePrint
	api.Enqueue = func(path string) error { return app.Enqueue(path, "slicer") }
	api.Status = func() any { return app.StatusScreen() }
	api.Phones = phonesIn(deckDir(cfg))
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

// deckAsk asks a question of the whole deck on the printer that the touch screen shows (the
// Switcher of a deck with many printers), or holds nil. The deck sets it after the web server
// starts, so it is atomic.
var deckAsk atomic.Pointer[func(question, yes, no string, answer func(bool))]

// askOn returns the Ask of the App Keys flow for the API of the printer of app: on the screen that
// the touch screen shows on a deck with many printers, so the user sees the question, or else on
// app.
func askOn(app *ui.App) func(question, yes, no string, answer func(bool)) {
	return func(question, yes, no string, answer func(bool)) {
		if ask := deckAsk.Load(); ask != nil {
			(*ask)(question, yes, no, answer)
			return
		}
		app.Ask(question, yes, no, answer)
	}
}

// newResinAPI registers the app API on the deck of a resin printer: the status, pause and cancel,
// the push messages and the camera. It refuses uploads, each start and each resume (Resin): the UV
// light goes on only after a press on the touch screen.
func newResinAPI(cfg config.Config, c *printer.ChiTu, app *ui.App, mux *http.ServeMux) *octoapi.Server {
	api := octoapi.New(resinAPIPrinter{c}, &gcodestore.Store{Dir: cfg.GcodeDir}, cfg.OctoPrintKey, filepath.Join(cfg.StateDir, "apikeys.json"))
	api.Resin = true
	api.Signing = app.Signing // the pairing of an app adds its key
	api.Queue = app.Queue
	api.PrinterID = func() string {
		if pl, ok := app.Cloud.(*projectLink); ok {
			return pl.printerID()
		}
		return ""
	}
	api.Version, api.Ask = osVersion(), askOn(app)
	api.Frame = cameraFrame(app)
	if cam, ok := app.Camera.(octoapi.Camera); ok {
		api.Camera = cam
	}
	api.History = c.History
	api.Status = func() any { return app.StatusScreen() }
	api.Phones = phonesIn(deckDir(cfg))
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
		// A deck with many printers: the address /?printer=<id> shows that printer.
		a := app
		if pick := deckApps; pick != nil {
			if p := pick(remote.SessionPrinter(ctx)); p != nil {
				a = p
			}
		}
		s := a.Session(size.X, size.Y)
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
// mounted wide, for the webcam of the slicers when the deck has no camera. The frames have their own screens, like a browser.
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

// keyRoute is the API of a printer that is not the first one: its key check and its handler.
type keyRoute struct {
	valid func(*http.Request) bool
	h     http.Handler
}

// keyRouter sends a request of the API at the root (/api, /plugin, /downloads) with the API key
// of another printer to the API of that printer. Each other request goes to h.
func keyRouter(h http.Handler, routes []keyRoute) http.Handler {
	if len(routes) == 0 {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/plugin/") || strings.HasPrefix(p, "/downloads/") {
			for _, rt := range routes {
				if rt.valid(r) {
					rt.h.ServeHTTP(w, r)
					return
				}
			}
		}
		h.ServeHTTP(w, r)
	})
}

// printerKeys are the API keys of one printer of the deck, for the list of the keys.
type printerKeys struct {
	id, name string
	api      *octoapi.Server // nil for a printer with no API
}

// printerName is the name of the printer of app, or "".
func printerName(app *ui.App) string {
	if app.Fleet != nil {
		return app.Fleet.Self().Name
	}
	return ""
}

// keyAdmin lists and removes the app keys of the deck for Settings, App keys (ui.KeyAdmin).
type keyAdmin struct {
	deck     *octoapi.DeckKeys
	printers []printerKeys
}

func (k *keyAdmin) Keys() []ui.AppKey {
	var out []ui.AppKey
	for _, e := range k.deck.List() {
		out = append(out, ui.AppKey{ID: "*:" + e.ID, App: e.App, Scope: "All printers"})
	}
	for _, p := range k.printers {
		if p.api == nil {
			continue
		}
		scope := p.name
		if scope == "" {
			scope = "This printer"
		}
		for _, e := range p.api.Keys() {
			out = append(out, ui.AppKey{ID: p.id + ":" + e.ID, App: e.App, Scope: scope})
		}
	}
	return out
}

func (k *keyAdmin) RemoveKey(id string) error {
	where, key, _ := strings.Cut(id, ":")
	if where == "*" { // a deck key: no printer ID has a "*"
		if k.deck.Remove(key) {
			return nil
		}
		return errors.New("no such key")
	}
	for _, p := range k.printers {
		if p.id == where && p.api != nil && p.api.RemoveKey(key) {
			return nil
		}
	}
	return errors.New("no such key")
}
