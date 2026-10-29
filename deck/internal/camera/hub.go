package camera

import (
	"context"
	"errors"
	"image"
	"log"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Hub runs the cameras of the settings, and gives their frames to the viewers. Each camera (Cam)
// has a capture of its own. A capture runs only while a viewer watches its camera: a stream
// (Watch), or a recent call of Snapshot or Peek. It stops linger after the last viewer. One
// capture feeds all viewers of its camera: each viewer gets the newest frame, so a slow viewer
// skips frames and the capture never waits for it.
//
// The methods of the Hub with no camera (Peek, Snapshot, Status and the others) are those of the
// main camera (RoleMain).
type Hub struct {
	// OnSettings gets the new settings, to save them, or is nil.
	OnSettings func(Settings)
	// OnCalibration gets the calibrations of all cameras after a change, to save them. It can be
	// nil.
	OnCalibration func([]*Calibration)

	mu       sync.Mutex
	settings Settings
	cams     map[string]*Cam
	none     *Cam            // the camera of a role with no usable camera
	ctx      context.Context // the context of Run, or nil before Run
	seq      uint64          // the number of the last frame of all cameras

	devices []Device
	scanned time.Time
	capture captureFunc
	scan    func() []Device
	saveMu  sync.Mutex // one save at a time

	stepSave *time.Timer // the save after a change of a focus or a PTZ position (saveSoon), or nil

	// ptzs are the PTZ controls of the cameras with a zoom control, by ID. They stay after the
	// capture stops. follow are the positions of Follow, by ID: they replace the position of the
	// settings and are not saved.
	ptzs   map[string]PTZ
	follow map[string]PTZPos
	// byPart are the cameras with a position of FollowPart, and userMoved the cameras with a
	// change of the user (SetPTZ) during the follow.
	byPart, userMoved map[string]bool

	// The views (views.go): the calibration of each camera by ID, the places of the printer, and
	// the last format of each device.
	cals    map[string]*Calibration
	calGen  int // goes up at each change of a calibration
	scene   Scene
	formats map[string]Format

	// The focus on the part (focus.go). lens are the lens positions that replace the focus of the
	// settings, by ID: the positions of a search, and the result of a search of a print. They are
	// not saved. runs are the searches that run or wait for the next park, and focusCtls the last
	// focus control of each camera with one. printing is true during a print: focusDone has the
	// height of the print at the last search of each camera, and userFocused the cameras with a
	// focus change of the user (SetFocus).
	lens        map[string]int
	runs        map[string]*focusRun
	focusCtls   map[string]Focus
	printing    bool
	focusDone   map[string]float64
	userFocused map[string]bool
}

// Cam is one camera of the hub. It is a Source.
type Cam struct {
	h  *Hub
	id string // the ID of the camera, or "" for the camera of a role with no usable camera

	// The fields under h.mu.
	viewers   int
	wantUntil time.Time
	wake      chan struct{}
	latest    *Frame
	changed   chan struct{} // closed at each new frame
	status    Status
	lastLog   string // the last error in the log, so that an error is in the log once
	running   bool   // run has started

	// The crop of the toolhead view, with the time and the number of its frame.
	thCrop [4]float64
	thAt   time.Time
	thSeq  uint64
	thSize image.Point

	// focusNow wakes the capture to set the focus now (refocus). focusSeq is the number of the
	// last frame of the hub at the last change of the focus, and settle the settle time of the
	// lens in frames, or 0 before the first search.
	focusNow chan struct{}
	focusSeq uint64
	settle   int
}

// Source is a camera for the viewers: a Cam, or a role of the hub (Hub.Role).
type Source interface {
	// ID returns the ID of the camera now, or "".
	ID() string
	// On reports that the camera is on, and that the deck finds it.
	On() bool
	Status() Status
	// Peek returns the newest frame, or nil, and keeps the camera on for linger.
	Peek() *Frame
	// Next waits for a frame after the frame with the number after, and returns it.
	Next(ctx context.Context, after uint64) (*Frame, error)
	// Watch counts a viewer that watches the frames until stop.
	Watch() (stop func())
	// Snapshot returns the newest frame, and starts the camera for it when it is not on.
	Snapshot(ctx context.Context) (*Frame, error)
	// View returns the view of a frame at the width w, and the name of the view that it shows.
	View(f *Frame, view string, w int) (*Frame, string)
	// Views returns the views that show a part of the picture now, with full first.
	Views() []string
	// Calibration returns the calibration, or nil, and reports that it is valid for the camera.
	Calibration() (*Calibration, bool)
	// SetCalibration saves a calibration, or removes it with nil.
	SetCalibration(*Calibration)
}

// Status is the state of a camera, for the screen.
type Status struct {
	// State is "off" (the setting is off), "none" (no camera), "idle" (no viewer), "starting",
	// "on", "nosignal" (no picture, or a blank picture: an HDMI capture with no signal), or
	// "error".
	State  string
	Device Device
	// Name is the name of the camera on the screens (Info.Name).
	Name   string
	Format Format
	// Focus is the focus control of a camera that is on, or nil for a camera with no focus
	// control.
	Focus *Focus
	// PTZ is the PTZ control of a camera that is on, or nil for a camera with no zoom control.
	PTZ *PTZ
	// Searching reports that a focus search on the part runs (Hub.FocusOnPart, Hub.FocusPrint).
	Searching bool
	Err       string
}

// captureFunc runs the capture of a device until ctx ends or the device fails.
type captureFunc func(ctx context.Context, d Device, l link) error

// link connects a capture to its camera.
type link struct {
	// settings returns the settings of the camera now. The capture reads the turn once, and the
	// focus a few times a second.
	settings func() CamSettings
	// format gets the format once it is set.
	format func(Format)
	// focus gets the focus control after each change, for a camera with a focus control.
	focus func(Focus)
	// focusNow gets a value when the capture must set the focus of the settings before its next
	// frame. It can be nil.
	focusNow <-chan struct{}
	// ptz gets the PTZ control after each change, for a camera with a zoom control. It can be nil.
	ptz func(PTZ)
	// emit gets each frame.
	emit func(*Frame)
}

// reportPTZ gives the PTZ control to l.ptz when it is set.
func (l link) reportPTZ(p PTZ) {
	if l.ptz != nil {
		l.ptz(p)
	}
}

// linger is how long a camera runs after its last viewer.
var linger = 8 * time.Second

// scanAge is how long the list of the devices is kept.
const scanAge = 5 * time.Second

// NewHub returns a hub with the settings s. Call Run to start it.
func NewHub(s Settings) *Hub {
	scan, capture := withTestCamera(listDevices, capture)
	h := &Hub{settings: s.clone(), cams: map[string]*Cam{}, capture: capture, scan: scan,
		cals: map[string]*Calibration{}}
	h.none = h.newCam("")
	return h
}

func (h *Hub) newCam(id string) *Cam {
	return &Cam{h: h, id: id, wake: make(chan struct{}, 1), changed: make(chan struct{}), focusNow: make(chan struct{}, 1)}
}

// Settings returns the settings.
func (h *Hub) Settings() Settings {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.settings.clone()
}

// focusSaveAfter is the time from the last change of a focus to the save of the settings.
var focusSaveAfter = 3 * time.Second

// SetFocus sets the focus of the camera id: fixed at the lens position pos, or auto. The capture
// applies it in a fraction of a second. The save of the settings comes focusSaveAfter after the
// last change, so a series of steps writes the file once. It ends a focus search of the camera,
// and during a print it stops the searches of the print for the camera.
func (h *Hub) SetFocus(id string, fixed bool, pos int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if run := h.runs[id]; run != nil {
		if run.cancel != nil {
			run.cancel()
		}
		delete(h.runs, id)
	}
	delete(h.lens, id)
	if h.printing {
		if h.userFocused == nil {
			h.userFocused = map[string]bool{}
		}
		h.userFocused[id] = true
	}
	s := h.settings.clone()
	cs := s.Camera(id)
	cs.FixedFocus, cs.Focus = fixed, pos
	s.SetCamera(cs)
	h.settings = s
	h.saveSoonLocked()
}

// saveSoonLocked saves the settings focusSaveAfter after the last call.
func (h *Hub) saveSoonLocked() {
	if h.stepSave == nil {
		h.stepSave = time.AfterFunc(focusSaveAfter, h.saveSettings)
	} else {
		h.stepSave.Reset(focusSaveAfter)
	}
}

// SetPTZ sets the PTZ position of the camera id, or the wide position with nil. The capture
// applies it in a fraction of a second, and the save of the settings comes as for SetFocus. It
// ends a Follow of the camera.
func (h *Hub) SetPTZ(id string, pos *PTZPos) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.settings.clone()
	cs := s.Camera(id)
	if pos != nil {
		p := *pos
		pos = &p
	}
	cs.PTZ = pos
	s.SetCamera(cs)
	h.settings = s
	if h.byPart[id] {
		delete(h.byPart, id)
		if h.userMoved == nil {
			h.userMoved = map[string]bool{}
		}
		h.userMoved[id] = true
	}
	delete(h.follow, id)
	h.calGen++
	h.saveSoonLocked()
}

// SetPTZMap sets the PTZ map of the camera id, or removes it with nil, and saves the settings.
func (h *Hub) SetPTZMap(id string, m *PTZMap) {
	h.mu.Lock()
	s := h.settings.clone()
	cs := s.Camera(id)
	cs.PTZMap = m
	s.SetCamera(cs)
	h.mu.Unlock()
	h.SetSettings(s)
	h.mu.Lock()
	h.calGen++
	h.mu.Unlock()
}

// Follow sets a PTZ position of the camera id that replaces the position of its settings until
// Follow with nil. The settings do not change.
func (h *Hub) Follow(id string, pos *PTZPos) {
	h.mu.Lock()
	defer h.mu.Unlock()
	old, had := h.follow[id]
	switch {
	case pos == nil && had:
		delete(h.follow, id)
	case pos != nil && (!had || old != *pos):
		if h.follow == nil {
			h.follow = map[string]PTZPos{}
		}
		h.follow[id] = *pos
	default:
		return
	}
	h.calGen++
}

// camSettingsLocked returns the settings of the camera id, with the position of Follow and the
// lens position of a focus search.
func (h *Hub) camSettingsLocked(id string) CamSettings {
	cs := h.settings.Camera(id)
	if p, ok := h.follow[id]; ok {
		q := p
		cs.PTZ = &q
	}
	if p, ok := h.lens[id]; ok {
		cs.FixedFocus, cs.Focus = true, p
	}
	return cs
}

// PTZ returns the PTZ control of the camera id: the last that its capture reported, with the
// position of the settings (or of Follow) in its ranges. It is false for a camera with no zoom
// control, and before the first capture of the camera.
func (h *Hub) PTZ(id string) (PTZ, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ptzLocked(id)
}

func (h *Hub) ptzLocked(id string) (PTZ, bool) {
	p, ok := h.ptzs[id]
	if !ok {
		return PTZ{}, false
	}
	cs := h.camSettingsLocked(id)
	p.Pos = p.Target(cs.PTZ)
	p.Mapped = cs.PTZMap.ValidAt(cs.Rotate)
	return p, true
}

// saveSettings gives the settings to OnSettings.
func (h *Hub) saveSettings() {
	h.mu.Lock()
	s, save := h.settings.clone(), h.OnSettings
	h.mu.Unlock()
	if save != nil {
		h.saveMu.Lock()
		save(s)
		h.saveMu.Unlock()
	}
}

// SetSettings changes the settings. A camera with a new turn, or that goes off, starts again.
func (h *Hub) SetSettings(s Settings) {
	h.mu.Lock()
	s = s.clone()
	for i, c := range s.Cameras {
		for _, d := range h.devices {
			if d.ID == c.ID {
				s.Cameras[i].Model = d.Name
			}
		}
	}
	h.settings = s.clone()
	save := h.OnSettings
	cams := h.camsLocked()
	h.mu.Unlock()
	for _, c := range cams {
		c.signal()
	}
	if save != nil {
		h.saveMu.Lock()
		save(s.clone())
		h.saveMu.Unlock()
	}
}

func (h *Hub) camsLocked() []*Cam {
	out := []*Cam{h.none}
	for _, c := range h.cams {
		out = append(out, c)
	}
	return out
}

// Devices returns the cameras that the deck finds, in the order of their IDs.
func (h *Hub) Devices() []Device {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.devicesLocked()
}

func (h *Hub) devicesLocked() []Device {
	if time.Since(h.scanned) > scanAge {
		h.devices = h.scan()
		sort.Slice(h.devices, func(i, j int) bool { return h.devices[i].ID < h.devices[j].ID })
		h.scanned = time.Now()
		if h.normalizeLocked() {
			s, cals, save, saveCal := h.settings.clone(), h.calsLocked(), h.OnSettings, h.OnCalibration
			go func() {
				h.saveMu.Lock()
				defer h.saveMu.Unlock()
				if save != nil {
					save(s)
				}
				if saveCal != nil {
					saveCal(cals)
				}
			}()
		}
	}
	return h.devices
}

// normalizeLocked changes the IDs of the settings and of the calibrations to the IDs of the
// devices: a path or an alias becomes the ID. The camera "" of an older deck becomes the camera
// of the calibration, or the first camera, and the main camera. It reports a change.
func (h *Hub) normalizeLocked() bool {
	if len(h.devices) == 0 {
		return false
	}
	canon := func(id string) string {
		for _, d := range h.devices {
			if d.Is(id) {
				return d.ID
			}
		}
		return id
	}
	old := h.settings.clone()
	var cams []CamSettings
	seen := map[string]bool{}
	for _, c := range h.settings.Cameras {
		if c.ID == "" {
			// Auto of an older deck: the camera of the calibration, or the first camera in the
			// order of the paths, as the older deck chose it. It stays the main camera.
			first := h.devices[0]
			for _, d := range h.devices {
				if d.Path < first.Path {
					first = d
				}
			}
			c.ID = first.ID
			for id := range h.cals {
				if n := canon(id); h.foundLocked(n) {
					c.ID = n
					break
				}
			}
			if h.settings.Main == "" {
				h.settings.Main = c.ID
			}
		}
		c.ID = canon(c.ID)
		if !seen[c.ID] {
			seen[c.ID] = true
			cams = append(cams, c)
		}
	}
	h.settings.Cameras = cams
	for _, role := range Roles {
		if id := h.settings.Role(role); id != "" {
			h.settings.SetRole(role, canon(id))
		}
	}
	calChanged := false
	for id, c := range h.cals {
		if n := canon(id); n != id {
			delete(h.cals, id)
			c.Device = n
			h.cals[n] = c
			calChanged = true
		}
	}
	return calChanged || !reflect.DeepEqual(old, h.settings)
}

// foundLocked reports that the deck finds the device id.
func (h *Hub) foundLocked(id string) bool {
	for _, d := range h.devices {
		if d.ID == id {
			return true
		}
	}
	return false
}

// Cameras returns the cameras: the cameras of the settings, and the other cameras that the deck
// finds. The first camera that is on is the default camera of the roles.
func (h *Hub) Cameras() []Info {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.listLocked()
}

func (h *Hub) listLocked() []Info {
	return List(h.settings, h.devicesLocked())
}

// infoLocked returns the camera id in the list.
func (h *Hub) infoLocked(id string) (Info, bool) {
	for _, in := range h.listLocked() {
		if in.ID == id {
			return in, true
		}
	}
	return Info{}, false
}

// roleLocked returns the ID of the camera of a role now, or "".
func (h *Hub) roleLocked(role string) string {
	for _, in := range h.listLocked() {
		if in.HasRole(role) {
			return in.ID
		}
	}
	return ""
}

// camLocked returns the camera id, and starts its capture loop when the hub runs.
func (h *Hub) camLocked(id string) *Cam {
	if id == "" {
		return h.none
	}
	c := h.cams[id]
	if c == nil {
		c = h.newCam(id)
		h.cams[id] = c
	}
	if h.ctx != nil && !c.running {
		c.running = true
		go c.run(h.ctx)
	}
	return c
}

// Source returns the camera of a selector: "" or a role (RoleMain, RoleLapse, RolePicture) for
// the camera of the role, the number of a camera in the list (1, 2), or the ID, the path or the
// alias of a camera. The camera of a role follows the role. It reports false for a selector
// that is not a camera.
func (h *Hub) Source(sel string) (Source, bool) {
	switch sel {
	case "":
		return h.Role(RoleMain), true
	case RoleMain, RoleLapse, RolePicture:
		return h.Role(sel), true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	list := h.listLocked()
	if n, err := strconv.Atoi(sel); err == nil {
		if n < 1 || n > len(list) {
			return nil, false
		}
		return h.camLocked(list[n-1].ID), true
	}
	for _, in := range list {
		if in.ID == sel || (in.Found && in.Device.Is(sel)) {
			return h.camLocked(in.ID), true
		}
	}
	return nil, false
}

// Cam returns the camera with the ID id.
func (h *Hub) Cam(id string) *Cam {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.camLocked(id)
}

// Role returns the camera of a role. It finds the camera of the role at each call, so it follows a
// change of the settings.
func (h *Hub) Role(role string) Source { return roleSource{h, role} }

// roleCam returns the camera of a role now.
func (h *Hub) roleCam(role string) *Cam {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.camLocked(h.roleLocked(role))
}

// roleSource is the camera of a role (Hub.Role).
type roleSource struct {
	h    *Hub
	role string
}

func (r roleSource) cam() *Cam                         { return r.h.roleCam(r.role) }
func (r roleSource) ID() string                        { return r.cam().ID() }
func (r roleSource) On() bool                          { return r.cam().On() }
func (r roleSource) Status() Status                    { return r.cam().Status() }
func (r roleSource) Peek() *Frame                      { return r.cam().Peek() }
func (r roleSource) Watch() func()                     { return r.cam().Watch() }
func (r roleSource) Views() []string                   { return r.cam().Views() }
func (r roleSource) Calibration() (*Calibration, bool) { return r.cam().Calibration() }
func (r roleSource) SetCalibration(c *Calibration)     { r.cam().SetCalibration(c) }
func (r roleSource) Snapshot(ctx context.Context) (*Frame, error) {
	return r.cam().Snapshot(ctx)
}
func (r roleSource) Next(ctx context.Context, after uint64) (*Frame, error) {
	return r.cam().Next(ctx, after)
}

// View returns the view of the frame f with the camera of the frame. The role can change between
// the frame and the view.
func (r roleSource) View(f *Frame, view string, w int) (*Frame, string) {
	if f != nil && f.cam != "" {
		return r.h.Cam(f.cam).View(f, view, w)
	}
	return r.cam().View(f, view, w)
}

// The main camera.

// On reports that the deck has a main camera: a camera that is on, and that the deck finds.
func (h *Hub) On() bool { return h.Role(RoleMain).On() }

// Status returns the state of the main camera.
func (h *Hub) Status() Status { return h.Role(RoleMain).Status() }

// Peek returns the newest frame of the main camera. See Cam.Peek.
func (h *Hub) Peek() *Frame { return h.Role(RoleMain).Peek() }

// Watch counts a viewer of the main camera. See Cam.Watch.
func (h *Hub) Watch() (stop func()) { return h.Role(RoleMain).Watch() }

// Snapshot returns a frame of the main camera. See Cam.Snapshot.
func (h *Hub) Snapshot(ctx context.Context) (*Frame, error) { return h.Role(RoleMain).Snapshot(ctx) }

// Next returns a new frame of the main camera. See Cam.Next.
func (h *Hub) Next(ctx context.Context, after uint64) (*Frame, error) {
	return h.Role(RoleMain).Next(ctx, after)
}

// ID returns the ID of the camera.
func (c *Cam) ID() string { return c.id }

// signal wakes the capture loop of the camera.
func (c *Cam) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// deviceLocked returns the device and the settings of the camera, and reports that the camera is
// on and that the deck finds it.
func (c *Cam) deviceLocked() (Device, CamSettings, bool) {
	cs := c.h.settings.Camera(c.id)
	if c.id == "" || cs.Off {
		return Device{}, cs, false
	}
	for _, d := range c.h.devicesLocked() {
		if d.ID == c.id {
			return d, cs, true
		}
	}
	return Device{}, cs, false
}

// On reports that the camera is on, and that the deck finds it.
func (c *Cam) On() bool {
	c.h.mu.Lock()
	defer c.h.mu.Unlock()
	_, _, ok := c.deviceLocked()
	return ok
}

// Status returns the state of the camera. The camera of a role with no usable camera is "off"
// when the deck finds a camera, and "none" when it finds no camera.
func (c *Cam) Status() Status {
	h := c.h
	h.mu.Lock()
	defer h.mu.Unlock()
	d, cs, ok := c.deviceLocked()
	in, _ := h.infoLocked(c.id)
	switch {
	case c.id == "" && len(h.devicesLocked()) > 0:
		return Status{State: "off"}
	case c.id == "":
		return Status{State: "none"}
	case cs.Off:
		return Status{State: "off", Name: in.Name}
	case !ok:
		return Status{State: "none", Name: in.Name}
	case c.status.State == "":
		return Status{State: "idle", Device: d, Name: in.Name}
	}
	s := c.status
	s.Name = in.Name
	s.Searching = h.runs[c.id] != nil && h.runs[c.id].cancel != nil
	if s.PTZ != nil {
		p := *s.PTZ
		p.Mapped = cs.PTZMap.ValidAt(cs.Rotate)
		s.PTZ = &p
	}
	return s
}

// wantLocked keeps the camera on for linger.
func (c *Cam) wantLocked() {
	if until := time.Now().Add(linger); until.After(c.wantUntil) {
		c.wantUntil = until
	}
}

func (c *Cam) wanted() bool {
	c.h.mu.Lock()
	defer c.h.mu.Unlock()
	return c.id != "" && !c.h.settings.Camera(c.id).Off && (c.viewers > 0 || time.Now().Before(c.wantUntil))
}

// Watch counts a viewer that watches the frames (a stream) until stop.
func (c *Cam) Watch() (stop func()) {
	c.h.mu.Lock()
	c.viewers++
	c.h.mu.Unlock()
	c.signal()
	var once sync.Once
	return func() {
		once.Do(func() {
			c.h.mu.Lock()
			c.viewers--
			c.wantLocked()
			c.h.mu.Unlock()
		})
	}
}

// Peek returns the newest frame, or nil, and keeps the camera on for linger. The deck UI calls it
// at each frame of the camera page.
func (c *Cam) Peek() *Frame {
	c.h.mu.Lock()
	c.wantLocked()
	f := c.latest
	c.h.mu.Unlock()
	c.signal()
	return f
}

// errNoSignal is the error of a camera that sends no frames: an HDMI capture with no signal, or
// a camera that is broken.
var errNoSignal = errors.New("no signal: the camera sends no pictures")

// ErrNoCamera is the error of a snapshot with no camera.
var ErrNoCamera = errors.New("camera: no camera")

// ErrNoFrame is the error of a camera that sends no frame in time.
var ErrNoFrame = errors.New("camera: no picture from the camera")

// Snapshot returns the newest frame. When the camera is not on, it starts the camera and waits
// for its first frame, for at most 5 seconds.
func (c *Cam) Snapshot(ctx context.Context) (*Frame, error) {
	if !c.On() {
		return nil, ErrNoCamera
	}
	if f := c.Peek(); f != nil {
		return f, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	f, err := c.Next(ctx, 0)
	if err != nil {
		return nil, ErrNoFrame
	}
	return f, nil
}

// Next waits for a frame after the frame with the number after, and returns it. The numbers of
// the frames of all cameras go up together, so a number of another camera is a valid after.
func (c *Cam) Next(ctx context.Context, after uint64) (*Frame, error) {
	for {
		c.h.mu.Lock()
		f, ch := c.latest, c.changed
		c.h.mu.Unlock()
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
func (c *Cam) publish(f *Frame) {
	c.h.mu.Lock()
	c.h.seq++
	f.Seq = c.h.seq
	f.cam = c.id
	c.latest = f
	close(c.changed)
	c.changed = make(chan struct{})
	c.status.State = "on"
	if f.blank {
		c.status.State = "nosignal"
	}
	c.h.mu.Unlock()
}

// Run starts the capture loops of the cameras, and waits until ctx ends. A capture runs only
// while viewers watch its camera.
func (h *Hub) Run(ctx context.Context) {
	h.mu.Lock()
	h.ctx = ctx
	for _, c := range h.cams {
		if !c.running {
			c.running = true
			go c.run(ctx)
		}
	}
	h.mu.Unlock()
	<-ctx.Done()
}

// run runs the capture of the camera while viewers watch, until ctx ends.
func (c *Cam) run(ctx context.Context) {
	h := c.h
	for ctx.Err() == nil {
		if !c.wanted() {
			c.wait(ctx, time.Minute)
			continue
		}
		h.mu.Lock()
		d, cs, ok := c.deviceLocked()
		if ok && c.status.State != "nosignal" {
			// A camera with no signal keeps that state while it starts again.
			c.status = Status{State: "starting", Device: d}
		}
		h.mu.Unlock()
		if !ok {
			c.wait(ctx, 3*time.Second)
			continue
		}
		cctx, cancel := context.WithCancel(ctx)
		go c.stopWhenIdle(cctx, cancel, cs)
		err := h.capture(cctx, d, link{
			settings: func() CamSettings {
				h.mu.Lock()
				defer h.mu.Unlock()
				return h.camSettingsLocked(c.id)
			},
			format: func(f Format) {
				h.mu.Lock()
				c.status.Format = f
				if h.formats == nil {
					h.formats = map[string]Format{}
				}
				h.formats[d.ID] = f
				h.mu.Unlock()
				c.logOnce(d.Name + " (" + d.Path + "): " + f.String())
			},
			focus: func(f Focus) {
				h.mu.Lock()
				c.status.Focus = &f
				c.focusSeq = h.seq
				if h.focusCtls == nil {
					h.focusCtls = map[string]Focus{}
				}
				h.focusCtls[d.ID] = f
				h.mu.Unlock()
			},
			focusNow: c.focusNow,
			ptz: func(p PTZ) {
				h.mu.Lock()
				c.status.PTZ = &p
				if h.ptzs == nil {
					h.ptzs = map[string]PTZ{}
				}
				old, had := h.ptzs[d.ID]
				h.ptzs[d.ID] = p
				if !had || old.Pos != p.Pos {
					h.calGen++
				}
				h.mu.Unlock()
			},
			emit: c.publish,
		})
		stopped := cctx.Err() != nil
		cancel()
		h.mu.Lock()
		c.latest = nil
		switch {
		case errors.Is(err, errNoSignal) && !stopped:
			c.status = Status{State: "nosignal", Device: d, Format: c.status.Format, Err: err.Error()}
		case err != nil && !stopped:
			c.status = Status{State: "error", Device: d, Err: err.Error()}
		default:
			c.status = Status{}
		}
		h.mu.Unlock()
		if err != nil && !stopped {
			c.logOnce(d.Name + ": " + err.Error())
			c.wait(ctx, 3*time.Second)
		}
	}
}

// stopWhenIdle cancels the capture when no viewer has watched for linger, or when the camera goes
// off or gets a new turn.
func (c *Cam) stopWhenIdle(ctx context.Context, cancel func(), start CamSettings) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-c.wake:
		}
		c.h.mu.Lock()
		cs := c.h.settings.Camera(c.id)
		c.h.mu.Unlock()
		if cs.Off != start.Off || cs.Rotate != start.Rotate || !c.wanted() {
			cancel()
			return
		}
	}
}

// wait waits for a signal, for at most d.
func (c *Cam) wait(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-c.wake:
	case <-t.C:
	}
}

// logOnce logs a line of the camera when it is not the last line that it logged.
func (c *Cam) logOnce(line string) {
	c.h.mu.Lock()
	same := line == c.lastLog
	c.lastLog = line
	c.h.mu.Unlock()
	if !same {
		log.Printf("camera: %s", line)
	}
}
