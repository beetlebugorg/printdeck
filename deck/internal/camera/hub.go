package camera

import (
	"context"
	"errors"
	"log"
	"sort"
	"sync"
	"time"
)

// Hub runs the camera of the settings, and gives its frames to the viewers. The camera runs only
// while a viewer watches: a stream (Watch), or a recent call of Snapshot or Peek. It stops linger
// after the last viewer. One capture feeds all viewers: each viewer takes the newest frame, so a
// slow viewer skips frames and the capture never waits for it.
type Hub struct {
	// OnSettings is told the new settings, to save them, or is nil.
	OnSettings func(Settings)

	mu        sync.Mutex
	settings  Settings
	gen       int // goes up at each change of the settings
	viewers   int
	wantUntil time.Time
	wake      chan struct{}

	latest  *Frame
	seq     uint64
	changed chan struct{} // closed at each new frame

	status  Status
	devices []Device
	scanned time.Time
	lastLog string // the last error in the log, so that an error is in the log once
	capture captureFunc
	scan    func() []Device
}

// Status tells the state of the camera, for the screen.
type Status struct {
	// State is "off" (the setting is Off), "none" (no camera), "idle" (no viewer), "starting",
	// "on", or "error".
	State  string
	Device Device
	Format Format
	Err    string
}

// captureFunc runs the capture of a device until ctx ends or the device fails. It calls format
// once the format is set, and emit with each frame.
type captureFunc func(ctx context.Context, d Device, s Settings, format func(Format), emit func(*Frame)) error

// linger is how long the camera runs after the last viewer.
var linger = 8 * time.Second

// scanAge is how long the list of the devices is kept.
const scanAge = 5 * time.Second

// NewHub returns a hub with the settings s. Call Run to start it.
func NewHub(s Settings) *Hub {
	return &Hub{settings: s, wake: make(chan struct{}, 1), changed: make(chan struct{}),
		capture: capture, scan: listDevices}
}

// Settings returns the settings.
func (h *Hub) Settings() Settings {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.settings
}

// SetSettings changes the settings, and starts the camera again with them.
func (h *Hub) SetSettings(s Settings) {
	h.mu.Lock()
	h.settings = s
	h.gen++
	save := h.OnSettings
	h.mu.Unlock()
	h.signal()
	if save != nil {
		save(s)
	}
}

// Devices returns the cameras that the deck finds, in the order of their paths.
func (h *Hub) Devices() []Device {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.devicesLocked()
}

func (h *Hub) devicesLocked() []Device {
	if time.Since(h.scanned) > scanAge {
		h.devices = h.scan()
		sort.Slice(h.devices, func(i, j int) bool { return h.devices[i].Path < h.devices[j].Path })
		h.scanned = time.Now()
	}
	return h.devices
}

// device returns the camera of the settings.
func (h *Hub) deviceLocked() (Device, bool) {
	if h.settings.Device == Off {
		return Device{}, false
	}
	for _, d := range h.devicesLocked() {
		if h.settings.Device == "" || d.ID == h.settings.Device || d.Path == h.settings.Device {
			return d, true
		}
	}
	return Device{}, false
}

// On reports that the settings choose a camera, and that the deck finds it.
func (h *Hub) On() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.deviceLocked()
	return ok
}

// Status returns the state of the camera.
func (h *Hub) Status() Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.status
	switch d, ok := h.deviceLocked(); {
	case h.settings.Device == Off:
		return Status{State: "off"}
	case !ok:
		return Status{State: "none"}
	case s.State == "":
		return Status{State: "idle", Device: d}
	}
	return s
}

// signal wakes Run.
func (h *Hub) signal() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// want keeps the camera on for linger. The caller holds mu.
func (h *Hub) wantLocked() {
	if until := time.Now().Add(linger); until.After(h.wantUntil) {
		h.wantUntil = until
	}
}

func (h *Hub) wanted() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.settings.Device != Off && (h.viewers > 0 || time.Now().Before(h.wantUntil))
}

// Watch counts a viewer that watches the frames (a stream) until stop.
func (h *Hub) Watch() (stop func()) {
	h.mu.Lock()
	h.viewers++
	h.mu.Unlock()
	h.signal()
	var once sync.Once
	return func() {
		once.Do(func() {
			h.mu.Lock()
			h.viewers--
			h.wantLocked()
			h.mu.Unlock()
		})
	}
}

// Peek returns the newest frame, or nil, and keeps the camera on for linger. The deck UI calls it
// at each frame of the camera page.
func (h *Hub) Peek() *Frame {
	h.mu.Lock()
	h.wantLocked()
	f := h.latest
	h.mu.Unlock()
	h.signal()
	return f
}

// ErrNoCamera is the error of a snapshot with no camera.
var ErrNoCamera = errors.New("camera: no camera")

// ErrNoFrame is the error of a camera that sends no frame in time.
var ErrNoFrame = errors.New("camera: no picture from the camera")

// Snapshot returns the newest frame. When the camera is not on, it starts the camera and waits
// for its first frame, for at most 5 seconds.
func (h *Hub) Snapshot(ctx context.Context) (*Frame, error) {
	if !h.On() {
		return nil, ErrNoCamera
	}
	if f := h.Peek(); f != nil {
		return f, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	f, err := h.Next(ctx, 0)
	if err != nil {
		return nil, ErrNoFrame
	}
	return f, nil
}

// Next waits for a frame after the frame with the number after, and returns it.
func (h *Hub) Next(ctx context.Context, after uint64) (*Frame, error) {
	for {
		h.mu.Lock()
		f, ch := h.latest, h.changed
		h.mu.Unlock()
		if f != nil && f.Seq > after {
			return f, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ch:
		}
	}
}

// publish makes f the newest frame.
func (h *Hub) publish(f *Frame) {
	h.mu.Lock()
	h.seq++
	f.Seq = h.seq
	h.latest = f
	close(h.changed)
	h.changed = make(chan struct{})
	h.status.State = "on"
	h.mu.Unlock()
}

// Run runs the camera while viewers watch, until ctx ends.
func (h *Hub) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if !h.wanted() {
			h.wait(ctx, time.Minute)
			continue
		}
		h.mu.Lock()
		d, ok := h.deviceLocked()
		s, gen := h.settings, h.gen
		if ok {
			h.status = Status{State: "starting", Device: d}
		}
		h.mu.Unlock()
		if !ok {
			h.wait(ctx, 3*time.Second)
			continue
		}
		cctx, cancel := context.WithCancel(ctx)
		go h.stopWhenIdle(cctx, cancel, gen)
		err := h.capture(cctx, d, s, func(f Format) {
			h.mu.Lock()
			h.status.Format = f
			h.mu.Unlock()
			h.logOnce(d.Name + " (" + d.Path + "): " + f.String())
		}, h.publish)
		stopped := cctx.Err() != nil
		cancel()
		h.mu.Lock()
		h.latest = nil
		if err != nil && !stopped {
			h.status = Status{State: "error", Device: d, Err: err.Error()}
		} else {
			h.status = Status{}
		}
		h.mu.Unlock()
		if err != nil && !stopped {
			h.logOnce(d.Name + ": " + err.Error())
			h.wait(ctx, 3*time.Second)
		}
	}
}

// stopWhenIdle cancels the capture when no viewer has watched for linger, or when the settings
// change.
func (h *Hub) stopWhenIdle(ctx context.Context, cancel func(), gen int) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-h.wake:
		}
		h.mu.Lock()
		changed := h.gen != gen
		h.mu.Unlock()
		if changed || !h.wanted() {
			cancel()
			return
		}
	}
}

// wait waits for a signal, for at most d.
func (h *Hub) wait(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-h.wake:
	case <-t.C:
	}
}

// logOnce logs a line of the camera when it is not the last line that it logged.
func (h *Hub) logOnce(line string) {
	h.mu.Lock()
	same := line == h.lastLog
	h.lastLog = line
	h.mu.Unlock()
	if !same {
		log.Printf("camera: %s", line)
	}
}
