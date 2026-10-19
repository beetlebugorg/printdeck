// Package eq is the equalizer of the speaker. CamillaDSP does the filtering (see docker/camilladsp);
// this package keeps the setting, saves it, and sends it to CamillaDSP through its websocket.
package eq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

// Band is one slider. Filter is the name of the filter in the CamillaDSP config.
type Band struct {
	Filter string
	Label  string // the frequency, for the screen
}

var Bands = []Band{
	{"eq_bass", "80"},
	{"eq_lowmid", "250"},
	{"eq_mid", "1k"},
	{"eq_highmid", "3.5k"},
	{"eq_treble", "10k"},
}

// MaxGain is the most that a band can boost or cut, in dB.
const MaxGain = 12.0

type Gains [5]float64

// Preset is a named setting.
type Preset struct {
	Name  string
	Gains Gains
}

var Presets = []Preset{
	{"Flat", Gains{}},
	{"Bass", Gains{6, 2, 0, 0, 0}},
	{"Loud", Gains{5, 1, 0, 1, 4}}, // more bass and treble, for low volumes
	{"Voice", Gains{-3, -1, 2, 3, 0}},
	{"Treble", Gains{0, 0, 0, 2, 5}},
}

type EQ struct {
	addr string // host:port of the CamillaDSP websocket
	path string // the saved setting

	mu        sync.Mutex
	gains     Gains
	connected bool
	kick      chan struct{}
}

func New(addr, stateDir string) *EQ {
	e := &EQ{addr: addr, path: filepath.Join(stateDir, "eq.json"), kick: make(chan struct{}, 1)}
	if b, err := os.ReadFile(e.path); err == nil {
		json.Unmarshal(b, &e.gains)
	}
	return e
}

func (e *EQ) Gains() Gains {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.gains
}

// Connected reports that CamillaDSP responds: the equalizer works.
func (e *EQ) Connected() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.connected
}

// Preset returns the name of the preset that the gains are, or "".
func (e *EQ) Preset() string {
	g := e.Gains()
	for _, p := range Presets {
		if p.Gains == g {
			return p.Name
		}
	}
	return ""
}

// SetGain sets one band, in steps of 0.5 dB.
func (e *EQ) SetGain(band int, db float64) {
	if band < 0 || band >= len(Bands) {
		return
	}
	e.mu.Lock()
	e.gains[band] = math.Round(max(-MaxGain, min(MaxGain, db))*2) / 2
	e.mu.Unlock()
	e.changed()
}

func (e *EQ) SetPreset(name string) {
	for _, p := range Presets {
		if p.Name == name {
			e.mu.Lock()
			e.gains = p.Gains
			e.mu.Unlock()
			e.changed()
		}
	}
}

func (e *EQ) changed() {
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

// Patch is the change of the CamillaDSP config for these gains. The preamp lowers everything by
// the largest boost, so that a boost cannot clip.
func Patch(g Gains) map[string]any {
	boost := 0.0
	filters := map[string]any{}
	for i, b := range Bands {
		boost = max(boost, g[i])
		filters[b.Filter] = map[string]any{"parameters": map[string]any{"gain": g[i]}}
	}
	filters["preamp"] = map[string]any{"parameters": map[string]any{"gain": -boost}}
	return map[string]any{"filters": filters}
}

// Run keeps the connection to CamillaDSP. It sends the setting when it connects (CamillaDSP starts
// flat) and after each change, and saves the setting. notify is called when something changes.
func (e *EQ) Run(ctx context.Context, notify func()) {
	for ctx.Err() == nil {
		e.session(ctx, notify)
		e.setConnected(false, notify)
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
		}
	}
}

func (e *EQ) setConnected(c bool, notify func()) {
	e.mu.Lock()
	changed := e.connected != c
	e.connected = c
	e.mu.Unlock()
	if changed {
		notify()
	}
}

func (e *EQ) session(ctx context.Context, notify func()) {
	ws, err := websocket.Dial("ws://"+e.addr, "", "http://127.0.0.1")
	if err != nil {
		return
	}
	defer ws.Close()
	go func() { <-ctx.Done(); ws.Close() }()

	send := func(msg any) error {
		if err := websocket.JSON.Send(ws, msg); err != nil {
			return err
		}
		ws.SetReadDeadline(time.Now().Add(5 * time.Second))
		var reply map[string]struct{ Result any }
		if err := websocket.JSON.Receive(ws, &reply); err != nil {
			return err
		}
		for cmd, r := range reply {
			if r.Result != "Ok" {
				log.Printf("equalizer: %s: %v", cmd, r.Result)
			}
		}
		return nil
	}
	apply := func() error {
		g := e.Gains()
		if err := send(map[string]any{"PatchConfig": Patch(g)}); err != nil {
			return err
		}
		e.save(g)
		return nil
	}

	if apply() != nil {
		return
	}
	e.setConnected(true, notify)
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		var err error
		select {
		case <-ctx.Done():
			return
		case <-e.kick:
			err = apply()
		case <-tick.C:
			err = send("GetState") // notices when CamillaDSP has gone away
		}
		if err != nil {
			return
		}
	}
}

func (e *EQ) save(g Gains) {
	b, _ := json.Marshal(g)
	os.MkdirAll(filepath.Dir(e.path), 0o700)
	tmp := e.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err == nil {
		os.Rename(tmp, e.path)
	}
}

// String is for logs.
func (g Gains) String() string { return fmt.Sprintf("%v dB", [5]float64(g)) }
