package system

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/user"
	"strconv"
	"sync"
	"time"
)

// The helper does the few actions of the deck that need root: set the Wi-Fi network, restart
// Klipper, install an update, and make and restore a backup. The deck runs as the user deck. systemd listens on the socket
// (printdeck-helper.socket), which only the group deck can open, and starts the helper at the first
// request. The helper stops after a time with no requests.

// Paths of the helper. Tests change them.
var (
	HelperSocket = "/run/printdeck-helper.sock"
	// UpdateBundle is the only file that the update action installs. The deck saves an uploaded
	// bundle here.
	UpdateBundle = "/var/lib/printdeck/update/printdeck-update.tar.xz"
)

// helperIdle is how long the helper waits for a request before it stops.
const helperIdle = 30 * time.Second

// HelperRequest is one request to the helper.
type HelperRequest struct {
	Action   string `json:"action"` // "wifi", "restart-klipper", "stop-klipper", "update", "backup" or "restore"
	SSID     string `json:"ssid,omitempty"`
	Password string `json:"password,omitempty"` // the Wi-Fi password, or the backup password
	Country  string `json:"country,omitempty"`
	Gcode    bool   `json:"gcode,omitempty"` // backup: with the G-code files
}

// helperResponse is the answer of the helper. Before the answer, an update sends lines with Step,
// for its progress.
type helperResponse struct {
	Error string `json:"error,omitempty"`
	Step  string `json:"step,omitempty"`
	Done  int64  `json:"done,omitempty"`
	Total int64  `json:"total,omitempty"`
}

// helperActions does the actions. progress sends the progress of an update. Tests replace it.
var helperActions = func(r HelperRequest, progress Progress) (after func(), err error) {
	switch r.Action {
	case "wifi":
		return nil, SetWifi(r.SSID, r.Password, r.Country)
	case "restart-klipper":
		return nil, run("systemctl", "restart", "klipper.service")
	case "stop-klipper":
		return nil, run("systemctl", "stop", "klipper.service")
	case "backup":
		return nil, MakeBackup(r.Password, r.Gcode, progress)
	case "restore":
		if err := RestoreBackup(r.Password, progress); err != nil {
			return nil, err
		}
		return restartAfterRestore, nil
	case "update":
		slot, err := Update(UpdateBundle, progress)
		os.Remove(UpdateBundle)
		if err != nil {
			return nil, err
		}
		log.Printf("helper: slot %s is written, restarting", slot)
		// Restart after the answer is sent, and after the deck sent its page to the browser.
		return func() {
			time.Sleep(3 * time.Second)
			if err := RebootTryboot(); err != nil {
				log.Printf("helper: %v", err)
			}
		}, nil
	}
	return nil, fmt.Errorf("unknown action %q", r.Action)
}

// Helper serves the requests on the socket from systemd (LISTEN_FDS), or on HelperSocket, until it
// has no request for helperIdle.
func Helper() error {
	ln, err := helperListener()
	if err != nil {
		return err
	}
	allowed := map[uint32]bool{0: true}
	if u, err := user.Lookup("deck"); err == nil {
		if uid, err := strconv.Atoi(u.Uid); err == nil {
			allowed[uint32(uid)] = true
		}
	}
	return serveHelper(ln.(*net.UnixListener), allowed, helperIdle)
}

func helperListener() (net.Listener, error) {
	if os.Getenv("LISTEN_FDS") == "1" && os.Getenv("LISTEN_PID") == strconv.Itoa(os.Getpid()) {
		return net.FileListener(os.NewFile(3, "printdeck-helper.sock"))
	}
	os.Remove(HelperSocket)
	return net.Listen("unix", HelperSocket)
}

// serveHelper answers one request on each connection. One action runs at a time.
func serveHelper(ln *net.UnixListener, allowed map[uint32]bool, idle time.Duration) error {
	var mu sync.Mutex
	for {
		ln.SetDeadline(time.Now().Add(idle))
		c, err := ln.AcceptUnix()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				mu.Lock() // wait for an action that runs
				return nil
			}
			return err
		}
		go func() {
			defer c.Close()
			// Read the request before the check, so that the caller always gets the answer.
			var r HelperRequest
			if err := json.NewDecoder(bufio.NewReader(io.LimitReader(c, 4096))).Decode(&r); err != nil {
				json.NewEncoder(c).Encode(helperResponse{Error: "bad request"})
				return
			}
			if uid, err := peerUID(c); err != nil || !allowed[uid] {
				json.NewEncoder(c).Encode(helperResponse{Error: "not allowed"})
				return
			}
			// The progress of an update: a line when the step or the percent changes.
			enc := json.NewEncoder(c)
			lastStep, lastPct := "", int64(-1)
			progress := func(step string, done, total int64) {
				pct := int64(0)
				if total > 0 {
					pct = done * 100 / total
				}
				if step == lastStep && pct == lastPct {
					return
				}
				lastStep, lastPct = step, pct
				enc.Encode(helperResponse{Step: step, Done: done, Total: total})
			}
			mu.Lock()
			after, err := helperActions(r, progress)
			mu.Unlock()
			resp := helperResponse{}
			if err != nil {
				resp.Error = err.Error()
			}
			json.NewEncoder(c).Encode(resp)
			if after != nil {
				after()
			}
		}()
	}
}

// AskHelper sends a request to the helper and waits for its answer. An update can take a minute.
// A backup with many G-code files can take longer: each line of progress gives 5 more minutes.
func AskHelper(r HelperRequest) error { return AskHelperProgress(r, nil) }

// AskHelperProgress is AskHelper, and gives the progress of an update to progress.
func AskHelperProgress(r HelperRequest, progress Progress) error {
	c, err := net.DialTimeout("unix", HelperSocket, 5*time.Second)
	if err != nil {
		return fmt.Errorf("the system helper does not answer: %w", err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Minute))
	if err := json.NewEncoder(c).Encode(r); err != nil {
		return err
	}
	dec := json.NewDecoder(c)
	var resp helperResponse
	for {
		resp = helperResponse{}
		if err := dec.Decode(&resp); err != nil {
			return fmt.Errorf("the system helper did not answer: %w", err)
		}
		if resp.Step == "" {
			break
		}
		c.SetDeadline(time.Now().Add(5 * time.Minute))
		if progress != nil {
			progress(resp.Step, resp.Done, resp.Total)
		}
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}
