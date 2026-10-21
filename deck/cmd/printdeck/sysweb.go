package main

import (
	_ "embed"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"printdeck/internal/remote"
	"printdeck/internal/system"
)

// The system page of the web server: the state of the system, its logs, and the upload of an
// update. The release image has no SSH, so this page is the way to look at a running Printdeck
// and to update it. The login of the remote screen protects it.

//go:embed system.html
var systemHTML string

var systemPage = template.Must(template.New("system").Parse(systemHTML))

// systemServices are the services on the system page.
var systemServices = []string{"klipper.service", "printdeck-deck.service", "wpa_supplicant@wlan0.service",
	"systemd-networkd.service", "systemd-resolved.service", "systemd-timesyncd.service"}

// maxBundle is the largest upload. A bundle is about 40 MB.
const maxBundle = 200 << 20

type systemView struct {
	Version, Variant, Slot, Uptime, Memory string
	APIKey                                 string // the key of the OctoPrint-compatible API, for slicers
	Services                               []struct{ Name, State string }
	Journal, Klippy                        string
	Message                                string
	Failed                                 bool
}

func registerSystemPage(mux *http.ServeMux, rs *remote.Server, pr printerBackend, klippyLog, apiKey string) {
	mux.HandleFunc("GET /system", rs.Auth(func(w http.ResponseWriter, r *http.Request) {
		renderSystem(w, klippyLog, apiKey, "", false)
	}))
	mux.HandleFunc("POST /system/update", rs.Auth(func(w http.ResponseWriter, r *http.Request) {
		msg, failed := installUpload(w, r, pr)
		renderSystem(w, klippyLog, apiKey, msg, failed)
	}))
}

func renderSystem(w http.ResponseWriter, klippyLog, apiKey, msg string, failed bool) {
	v := systemView{Version: osVersion(), Variant: osRelease("PRINTDECK_VARIANT"), APIKey: apiKey, Message: msg, Failed: failed}
	if s, err := system.Slot(); err == nil {
		v.Slot = strings.ToUpper(s)
	}
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			if secs, err := strconv.ParseFloat(f[0], 64); err == nil {
				v.Uptime = (time.Duration(secs) * time.Second).String()
			}
		}
	}
	v.Memory = memoryUse()
	for _, s := range systemServices {
		out, _ := exec.Command("systemctl", "is-active", s).Output()
		v.Services = append(v.Services, struct{ Name, State string }{strings.TrimSuffix(s, ".service"), strings.TrimSpace(string(out))})
	}
	if out, err := exec.Command("journalctl", "-b", "-n", "300", "--no-pager", "-o", "short-precise").CombinedOutput(); err == nil {
		v.Journal = string(out)
	} else {
		v.Journal = fmt.Sprintf("journalctl: %v\n%s", err, out)
	}
	v.Klippy = tail(klippyLog, 64<<10)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := systemPage.Execute(w, v); err != nil {
		log.Printf("system page: %v", err)
	}
}

// installUpload saves an uploaded update bundle, and asks the system helper to install it. The
// helper writes the other slot, answers, and restarts the Printdeck.
func installUpload(w http.ResponseWriter, r *http.Request, pr printerBackend) (string, bool) {
	if pr != nil && pr.State().Busy() {
		return "A print runs. Install the update after the print.", true
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBundle)
	f, _, err := r.FormFile("bundle")
	if err != nil {
		return "No bundle: " + err.Error(), true
	}
	defer f.Close()
	dir := filepath.Dir(system.UpdateBundle)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err.Error(), true
	}
	tmp := system.UpdateBundle + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err.Error(), true
	}
	_, err = io.Copy(out, f)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return "The upload failed: " + err.Error(), true
	}
	if err := os.Rename(tmp, system.UpdateBundle); err != nil {
		return err.Error(), true
	}
	log.Printf("system page: installing an uploaded update")
	if err := system.AskHelper(system.HelperRequest{Action: "update"}); err != nil {
		os.Remove(system.UpdateBundle)
		return "The update failed: " + err.Error(), true
	}
	return "The update is written. The Printdeck restarts into it now. Load this page again in a minute.", false
}

// memoryUse is the memory in use and the total memory, from /proc/meminfo.
func memoryUse() string {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return ""
	}
	kb := map[string]int{}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 {
			n, _ := strconv.Atoi(f[1])
			kb[strings.TrimSuffix(f[0], ":")] = n
		}
	}
	total, avail := kb["MemTotal"], kb["MemAvailable"]
	return fmt.Sprintf("%d MB of %d MB", (total-avail)/1024, total/1024)
}

// tail returns the end of a file, at most n bytes, from the start of a line.
func tail(path string, n int64) string {
	f, err := os.Open(path)
	if err != nil {
		return err.Error()
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err.Error()
	}
	off := max(0, fi.Size()-n)
	f.Seek(off, io.SeekStart)
	b, _ := io.ReadAll(f)
	if off > 0 {
		if i := strings.IndexByte(string(b), '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	return string(b)
}

// osRelease returns a value of /usr/lib/os-release, or "".
func osRelease(key string) string {
	b, err := os.ReadFile("/usr/lib/os-release")
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(l, key+"="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}
