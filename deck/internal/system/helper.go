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
	"syscall"
	"time"
)

// The helper does the few actions of the deck that need root: set the Wi-Fi network, restart
// Klipper, and install an update. The deck runs as the user deck. systemd listens on the socket
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
	Action   string `json:"action"` // "wifi", "restart-klipper" or "update"
	SSID     string `json:"ssid,omitempty"`
	Password string `json:"password,omitempty"`
	Country  string `json:"country,omitempty"`
}

type helperResponse struct {
	Error string `json:"error,omitempty"`
}

// helperActions does the actions. Tests replace it.
var helperActions = func(r HelperRequest) (after func(), err error) {
	switch r.Action {
	case "wifi":
		return nil, SetWifi(r.SSID, r.Password, r.Country)
	case "restart-klipper":
		return nil, run("systemctl", "restart", "klipper.service")
	case "update":
		slot, err := Update(UpdateBundle, io.Discard)
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
			mu.Lock()
			after, err := helperActions(r)
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

// peerUID returns the user of the process at the other end of a Unix socket.
func peerUID(c *net.UnixConn) (uint32, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *syscall.Ucred
	var cerr error
	err = raw.Control(func(fd uintptr) {
		cred, cerr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	})
	if err != nil {
		return 0, err
	}
	if cerr != nil {
		return 0, cerr
	}
	return cred.Uid, nil
}

// AskHelper sends a request to the helper and waits for its answer. An update can take a minute.
func AskHelper(r HelperRequest) error {
	c, err := net.DialTimeout("unix", HelperSocket, 5*time.Second)
	if err != nil {
		return fmt.Errorf("the system helper does not answer: %w", err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Minute))
	if err := json.NewEncoder(c).Encode(r); err != nil {
		return err
	}
	var resp helperResponse
	if err := json.NewDecoder(c).Decode(&resp); err != nil {
		return fmt.Errorf("the system helper did not answer: %w", err)
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}
