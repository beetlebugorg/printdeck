package main

import (
	_ "embed"
	"encoding/json"
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
	"sync"
	"time"

	"printdeck/internal/camera"
	"printdeck/internal/hostmetrics"
	"printdeck/internal/remote"
	"printdeck/internal/system"
	"printdeck/internal/timelapse"
)

// The system page of the web server: the state of the system, its logs, the upload of an update,
// and the backup and restore of the data. The release image has no SSH, so this page is the way to look at a running Printdeck
// and to update it. The login of the remote screen protects it.

//go:embed system.html
var systemHTML string

var systemPage = template.Must(template.New("system").Parse(systemHTML))

// systemServices are the services on the system page.
var systemServices = []string{"klipper.service", "printdeck-deck.service", "wpa_supplicant@wlan0.service",
	"systemd-networkd.service", "systemd-resolved.service", "systemd-timesyncd.service"}

// maxBundle is the largest upload. A bundle is about 40 MB.
const maxBundle = 200 << 20

// maxRestore is the largest backup to restore. A backup with G-code files can be large.
const maxRestore = 16 << 30

type systemView struct {
	Version, Variant, Slot, Uptime, Memory string
	Metrics                                string // the CPU and the disk writes of the last minute
	APIKey                                 string // the key of the OctoPrint-compatible API, for slicers
	Source                                 string // the source code of this version
	Services                               []struct{ Name, State string }
	Journal, Klippy                        string
	Title, Message                         string // a message at the top: UPDATE, BACKUP or RESTORE
	Failed                                 bool
	TimeLapse                              bool // the deck has the time lapse
	Videos                                 []struct{ Name, Size, Time string }
	FirstLayer                             string // the file and the time of the picture of the first layer, or ""
}

// sysMetrics are the samples of the host for the system page, or nil.
var sysMetrics *hostmetrics.Recorder

// sysTimelapse is the time lapse for the system page, or nil.
var sysTimelapse *timelapse.Runner

func registerSystemPage(mux *http.ServeMux, rs *remote.Server, pr printerBackend, metrics *hostmetrics.Recorder, klippyLog, apiKey string) {
	sysMetrics = metrics
	mux.HandleFunc("GET /system/metrics", rs.Auth(func(w http.ResponseWriter, r *http.Request) {
		writeMetricsJSON(w, r, metrics)
	}))
	mux.HandleFunc("GET /system", rs.Auth(func(w http.ResponseWriter, r *http.Request) {
		renderSystem(w, klippyLog, apiKey, "", false)
	}))
	mux.HandleFunc("POST /system/update", rs.Auth(func(w http.ResponseWriter, r *http.Request) {
		// The script of the page installs in the background, and reads the progress.
		if r.Header.Get("X-Printdeck-Progress") == "1" {
			msg, failed := saveUpload(w, r, pr)
			if !failed {
				installing.Lock()
				installing.installState = installState{Step: system.StepUnpack, Running: true}
				installing.Unlock()
				go installSaved()
			}
			writeInstallJSON(w, map[string]any{"error": map[bool]string{true: msg}[failed]})
			return
		}
		msg, failed := installUpload(w, r, pr)
		renderSystem(w, klippyLog, apiKey, msg, failed)
	}))
	mux.HandleFunc("POST /system/backup", rs.Auth(func(w http.ResponseWriter, r *http.Request) {
		if msg := sendBackup(w, r); msg != "" {
			renderMessage(w, klippyLog, apiKey, "BACKUP", msg, true)
		}
	}))
	mux.HandleFunc("POST /system/restore", rs.Auth(func(w http.ResponseWriter, r *http.Request) {
		msg, failed := restoreUpload(w, r, pr)
		renderMessage(w, klippyLog, apiKey, "RESTORE", msg, failed)
	}))
	registerTimelapse(mux, rs)
	mux.HandleFunc("GET /system/update/progress", rs.Auth(func(w http.ResponseWriter, r *http.Request) {
		installing.Lock()
		st := installing.installState
		installing.Unlock()
		writeInstallJSON(w, map[string]any{"version": osVersion(), "step": st.Step, "done": st.Done, "total": st.Total,
			"error": st.Err, "running": st.Running, "finished": st.Finished})
	}))
}

// installState is the progress of an update that the system page installs.
type installState struct {
	Step        string
	Done, Total int64
	Err         string
	Running     bool
	Finished    bool // the slot is written, and the Printdeck restarts
}

var installing struct {
	sync.Mutex
	installState
}

// installSaved installs the saved bundle with the helper, and keeps its progress for the page.
func installSaved() {
	log.Printf("system page: installing an uploaded update")
	err := system.AskHelperProgress(system.HelperRequest{Action: "update"}, func(step string, done, total int64) {
		installing.Lock()
		installing.Step, installing.Done, installing.Total = step, done, total
		installing.Unlock()
	})
	installing.Lock()
	defer installing.Unlock()
	installing.Running = false
	if err != nil {
		os.Remove(system.UpdateBundle)
		installing.Err = "The update failed: " + err.Error()
		return
	}
	installing.Step, installing.Finished = "Restarting", true
}

func writeInstallJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func renderSystem(w http.ResponseWriter, klippyLog, apiKey, msg string, failed bool) {
	renderMessage(w, klippyLog, apiKey, "UPDATE", msg, failed)
}

func renderMessage(w http.ResponseWriter, klippyLog, apiKey, title, msg string, failed bool) {
	v := systemView{Version: osVersion(), Variant: osRelease("PRINTDECK_VARIANT"), APIKey: apiKey, Title: title, Message: msg,
		Failed: failed, Source: sourceURL(osVersion())}
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
	v.Metrics = metricsLine(sysMetrics, time.Now())
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
	if r := sysTimelapse; r != nil {
		v.TimeLapse = true
		in := r.Info()
		for _, vid := range in.Videos {
			v.Videos = append(v.Videos, struct{ Name, Size, Time string }{vid.Name,
				fmt.Sprintf("%.1f MB", float64(vid.Size)/(1<<20)), vid.Time.Format("2006-01-02 15:04")})
		}
		if in.First != nil {
			v.FirstLayer = in.FirstAt.Format("2006-01-02 15:04")
			if in.FirstFile != "" {
				v.FirstLayer = filepath.Base(in.FirstFile) + " · " + v.FirstLayer
			}
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := systemPage.Execute(w, v); err != nil {
		log.Printf("system page: %v", err)
	}
}

// installUpload saves an uploaded update bundle, and asks the system helper to install it. The
// helper writes the other slot, answers, and restarts the Printdeck.
func installUpload(w http.ResponseWriter, r *http.Request, pr printerBackend) (string, bool) {
	if msg, failed := saveUpload(w, r, pr); failed {
		return msg, true
	}
	log.Printf("system page: installing an uploaded update")
	if err := system.AskHelper(system.HelperRequest{Action: "update"}); err != nil {
		os.Remove(system.UpdateBundle)
		return "The update failed: " + err.Error(), true
	}
	return "The update is written. The Printdeck restarts into it now. Load this page again in a minute.", false
}

// saveUpload saves an uploaded update bundle as system.UpdateBundle. It fails during a print, or
// while an update installs.
func saveUpload(w http.ResponseWriter, r *http.Request, pr printerBackend) (string, bool) {
	installing.Lock()
	running := installing.Running
	installing.Unlock()
	if running {
		return "An update installs now.", true
	}
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
	return "", false
}

// sendBackup asks the helper for a backup with the password of the form, and sends the file to the
// browser. It returns the message of a failure, or "" after the download.
func sendBackup(w http.ResponseWriter, r *http.Request) string {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	pass := r.FormValue("password")
	switch {
	case len(pass) < 8:
		return "The backup password needs 8 characters or more."
	case pass != r.FormValue("password2"):
		return "The two passwords are not the same."
	}
	gcode := r.FormValue("gcode") == "1"
	log.Printf("system page: making a backup (G-code files: %v)", gcode)
	if err := system.AskHelper(system.HelperRequest{Action: "backup", Password: pass, Gcode: gcode}); err != nil {
		return "The backup failed: " + err.Error()
	}
	f, err := os.Open(system.BackupFile())
	if err != nil {
		return "The backup failed: " + err.Error()
	}
	defer os.Remove(system.BackupFile())
	defer f.Close()
	host, _ := os.Hostname()
	name := fmt.Sprintf("printdeck-backup-%s-%s.pdbk", host, time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	if fi, err := f.Stat(); err == nil {
		w.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
	}
	io.Copy(w, f)
	return ""
}

// restoreUpload saves an uploaded backup as system.RestoreFile, and asks the helper to restore it
// with the password of the form. The upload goes to the file as it arrives: a large backup does not
// fill the memory. It fails during a print, and while an update installs.
func restoreUpload(w http.ResponseWriter, r *http.Request, pr printerBackend) (string, bool) {
	installing.Lock()
	running := installing.Running
	installing.Unlock()
	if running {
		return "An update installs now.", true
	}
	if pr != nil && pr.State().Busy() {
		return "A print runs. Restore the backup after the print.", true
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRestore)
	mr, err := r.MultipartReader()
	if err != nil {
		return "No backup file: " + err.Error(), true
	}
	file := system.RestoreFile()
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err.Error(), true
	}
	defer os.Remove(file) // the helper removes it too
	var pass string
	saved := false
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "The upload failed: " + err.Error(), true
		}
		switch part.FormName() {
		case "password":
			b, _ := io.ReadAll(io.LimitReader(part, 1024))
			pass = string(b)
		case "backup":
			if err := saveFile(file, part); err != nil {
				return "The upload failed: " + err.Error(), true
			}
			saved = true
		}
		part.Close()
	}
	if !saved {
		return "Choose the backup file.", true
	}
	if pass == "" {
		return "Type the backup password.", true
	}
	log.Printf("system page: restoring a backup")
	if err := system.AskHelper(system.HelperRequest{Action: "restore", Password: pass}); err != nil {
		return "The restore failed: " + err.Error(), true
	}
	return "The backup is restored. Klipper and the Printdeck restart now. Load this page again in a minute.", false
}

// saveFile writes r to path through a temporary file.
func saveFile(path string, r io.Reader) error {
	tmp := path + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, r)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
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

// metricsLine is the line of the system page for the samples of the host: the CPU, and the writes
// to the disk in the last minute. It is "" with no samples.
func metricsLine(m *hostmetrics.Recorder, now time.Time) string {
	if m == nil || m.Len() == 0 {
		return ""
	}
	b := m.Total(now.Add(-time.Minute), now)
	var parts []string
	if b.HasCPU {
		parts = append(parts, fmt.Sprintf("CPU %.0f%%", 100*b.CPU))
	}
	if b.HasDisk {
		parts = append(parts, fmt.Sprintf("%d writes, %d KiB to %s in %.0f s", b.Writes, b.WriteBytes/1024, m.Disk(), b.Covered.Seconds()))
	}
	return strings.Join(parts, " · ")
}

// metricsBucket is a part of the time in the JSON of /system/metrics. A value with no data is
// null.
type metricsBucket struct {
	Start      time.Time `json:"start"`
	Seconds    float64   `json:"seconds"` // the time of the samples in the bucket
	CPU        *float64  `json:"cpu"`     // the busy share, 0 to 1
	CPUMax     *float64  `json:"cpu_max"` // the largest busy share of a 2 s sample
	MemUsed    *uint64   `json:"mem_used"`
	MemTotal   *uint64   `json:"mem_total"`
	Writes     *uint64   `json:"writes"`
	WriteBytes *uint64   `json:"write_bytes"`
}

// writeMetricsJSON writes the samples of the host as JSON: the last 10 minutes in buckets of 2
// seconds, or with range=1h or range=24h, the last hour in buckets of 10 seconds or the last day
// in minutes.
func writeMetricsJSON(w http.ResponseWriter, r *http.Request, m *hostmetrics.Recorder) {
	span, n := 10*time.Minute, 300
	switch r.URL.Query().Get("range") {
	case "1h":
		span, n = time.Hour, 360
	case "24h":
		span, n = 24*time.Hour, 1440
	}
	out := struct {
		Interval float64         `json:"interval_s"`
		Bucket   float64         `json:"bucket_s"`
		Disk     string          `json:"disk"`
		Buckets  []metricsBucket `json:"buckets"`
	}{Interval: hostmetrics.FineInterval.Seconds(), Bucket: (span / time.Duration(n)).Seconds(), Buckets: []metricsBucket{}}
	if m != nil {
		out.Disk = m.Disk()
		now := time.Now()
		for _, b := range m.Window(now.Add(-span), now, n) {
			mb := metricsBucket{Start: b.Start.UTC(), Seconds: b.Covered.Seconds()}
			if b.HasCPU {
				mb.CPU, mb.CPUMax = &b.CPU, &b.CPUMax
			}
			if b.HasMem {
				mb.MemUsed, mb.MemTotal = &b.MemUsed, &b.MemTotal
			}
			if b.HasDisk {
				mb.Writes, mb.WriteBytes = &b.Writes, &b.WriteBytes
			}
			out.Buckets = append(out.Buckets, mb)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(out)
}

// registerTimelapse adds the downloads of the time lapse: a video of the folder of the videos, and
// the picture of the last first layer.
func registerTimelapse(mux *http.ServeMux, rs *remote.Server) {
	mux.HandleFunc("GET /system/videos/{name}", rs.Auth(func(w http.ResponseWriter, r *http.Request) {
		if sysTimelapse == nil {
			http.NotFound(w, r)
			return
		}
		name := r.PathValue("name")
		p, ok := sysTimelapse.VideoPath(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "video/quicktime")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		http.ServeFile(w, r, p)
	}))
	mux.HandleFunc("GET /system/firstlayer.jpg", rs.Auth(func(w http.ResponseWriter, r *http.Request) {
		var f *camera.Frame
		if sysTimelapse != nil {
			f = sysTimelapse.Info().First
		}
		if f == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(f.JPEG())
	}))
}
