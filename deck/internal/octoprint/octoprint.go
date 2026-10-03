// Package octoprint follows and controls the local OctoPrint server through its REST API.
package octoprint

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type File struct {
	Path string
	Name string
	Date time.Time
}

type State struct {
	Reachable        bool
	Err              string
	Text             string // "Printing", "Operational", "Offline", ...
	Connected        bool   // the printer answers
	Printing         bool   // printing, pausing, or paused: a job is active
	Paused           bool
	File             string
	Completion       float64 // 0..100
	TimeLeft         time.Duration
	HasTimeLeft      bool
	Tool, ToolTarget float64
	Bed, BedTarget   float64
}

// Busy reports that a job is running or paused.
func (s State) Busy() bool { return s.Printing || s.Paused }

type Client struct {
	url, key string
	http     *http.Client

	mu    sync.Mutex
	st    State
	files []File
	poke  chan struct{}
}

func New(url, key string) *Client {
	return &Client{url: strings.TrimRight(url, "/"), key: key, http: &http.Client{Timeout: 5 * time.Second}, poke: make(chan struct{}, 1)}
}

func (c *Client) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.st
}

func (c *Client) Files() []File {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.files
}

func (c *Client) req(method, path string, body any) ([]byte, int, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.url+path, rd)
	req.Header.Set("X-Api-Key", c.key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return b, resp.StatusCode, nil
}

// Run polls OctoPrint until ctx ends.
func (c *Client) Run(ctx context.Context, notify func()) {
	for {
		c.refresh()
		notify()
		select {
		case <-ctx.Done():
			return
		case <-c.poke:
		case <-time.After(2 * time.Second):
		}
	}
}

func (c *Client) Poke() {
	select {
	case c.poke <- struct{}{}:
	default:
	}
}

func (c *Client) refresh() {
	st := State{}
	b, code, err := c.req("GET", "/api/job", nil)
	switch {
	case err != nil:
		st.Err = "OctoPrint is not reachable"
	case code == 401 || code == 403:
		st.Err = "OctoPrint refused the API key"
	case code != 200:
		st.Err = fmt.Sprintf("OctoPrint answered %d", code)
	default:
		st.Reachable = true
		var j struct {
			State string `json:"state"`
			Job   struct {
				File struct{ Name, Display string }
			} `json:"job"`
			Progress struct {
				Completion    *float64 `json:"completion"`
				PrintTimeLeft *float64 `json:"printTimeLeft"`
			} `json:"progress"`
		}
		json.Unmarshal(b, &j)
		st.Text = j.State
		st.File = j.Job.File.Display
		if st.File == "" {
			st.File = j.Job.File.Name
		}
		if j.Progress.Completion != nil {
			st.Completion = *j.Progress.Completion
		}
		if j.Progress.PrintTimeLeft != nil {
			st.TimeLeft, st.HasTimeLeft = time.Duration(*j.Progress.PrintTimeLeft)*time.Second, true
		}
		low := strings.ToLower(j.State)
		st.Paused = strings.Contains(low, "paus")
		st.Printing = strings.Contains(low, "print") || low == "starting" || low == "resuming" || low == "finishing"
		st.Connected = !strings.HasPrefix(low, "offline") && !strings.HasPrefix(low, "opening") && low != ""

		if pb, pc, perr := c.req("GET", "/api/printer?exclude=sd,state", nil); perr == nil && pc == 200 {
			var p struct {
				Temperature map[string]struct{ Actual, Target float64 } `json:"temperature"`
			}
			json.Unmarshal(pb, &p)
			st.Tool, st.ToolTarget = p.Temperature["tool0"].Actual, p.Temperature["tool0"].Target
			st.Bed, st.BedTarget = p.Temperature["bed"].Actual, p.Temperature["bed"].Target
		}
	}
	c.mu.Lock()
	c.st = st
	c.mu.Unlock()
}

// LoadFiles reads the list of files, newest first.
func (c *Client) LoadFiles() {
	b, code, err := c.req("GET", "/api/files/local?recursive=true", nil)
	if err != nil || code != 200 {
		return
	}
	var r struct {
		Files []struct {
			Name, Path, Type string
			Date             int64
			Children         []json.RawMessage
		} `json:"files"`
	}
	json.Unmarshal(b, &r)
	var files []File
	for _, f := range r.Files {
		if f.Type == "machinecode" {
			files = append(files, File{Path: f.Path, Name: f.Name, Date: time.Unix(f.Date, 0)})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Date.After(files[j].Date) })
	c.mu.Lock()
	c.files = files
	c.mu.Unlock()
}

func (c *Client) post(path string, body any) {
	go func() {
		c.req("POST", path, body)
		c.Poke()
	}()
}

func (c *Client) Pause() {
	c.post("/api/job", map[string]string{"command": "pause", "action": "pause"})
}
func (c *Client) Resume() {
	c.post("/api/job", map[string]string{"command": "pause", "action": "resume"})
}
func (c *Client) Cancel() { c.post("/api/job", map[string]string{"command": "cancel"}) }

func (c *Client) Connect() { c.post("/api/connection", map[string]string{"command": "connect"}) }

func (c *Client) Heat(tool, bed int) {
	c.post("/api/printer/tool", map[string]any{"command": "target", "targets": map[string]int{"tool0": tool}})
	c.post("/api/printer/bed", map[string]any{"command": "target", "target": bed})
}

func (c *Client) PrintFile(path string) {
	c.post("/api/files/local/"+path, map[string]any{"command": "select", "print": true})
}
