package camera

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// CamSettings are the settings of one camera.
type CamSettings struct {
	// ID is the ID of the camera (Device.ID). "" is the first camera that the deck finds: the
	// setting of an older deck with Auto and a turn. The hub changes it to the ID of the camera.
	ID string `json:"id"`
	// Off turns the camera off: the viewers and the roles do not use it.
	Off bool `json:"off,omitempty"`
	// Name is the name of the camera on the screens, or "" for the name of the device.
	Name string `json:"name,omitempty"`
	// Rotate is 0, or 180 for a camera that is mounted upside down.
	Rotate int `json:"rotate,omitempty"`
	// Model is the name of the device, for the screens while the deck does not find the camera.
	// The hub sets it.
	Model string `json:"model,omitempty"`
	// FixedFocus turns off the autofocus: the lens stays at the position Focus. A camera with no
	// focus control ignores both.
	FixedFocus bool `json:"fixedFocus,omitempty"`
	Focus      int  `json:"focus,omitempty"`
}

// Settings are the camera settings of the deck.
type Settings struct {
	// Cameras are the cameras with settings. A camera that is not in the list is on, with the
	// name of its device and no turn.
	Cameras []CamSettings `json:"cameras,omitempty"`
	// AllOff turns off each camera that is not in Cameras: the setting Off of an older deck.
	AllOff bool `json:"allOff,omitempty"`
	// Main is the ID of the main camera: the webcam of the API with no camera parameter, for Cura
	// and the OctoPrint clients. "" is the first camera that is on.
	Main string `json:"main,omitempty"`
	// Lapse is the ID of the camera of the time lapse, and Picture the ID of the camera of the
	// first layer picture and of the pictures of the notifications. "" is the main camera.
	Lapse   string `json:"lapse,omitempty"`
	Picture string `json:"picture,omitempty"`
}

// The roles of the cameras: the values of Role, and of the camera parameter of the API.
const (
	RoleMain    = "main"
	RoleLapse   = "lapse"
	RolePicture = "picture"
)

// Roles are the roles, in the order of the screen.
var Roles = []string{RoleMain, RoleLapse, RolePicture}

// Off is the first line of the camera file of an older deck with the camera off.
const Off = "off"

// Camera returns the settings of the camera id: its entry in Cameras, or the settings of a
// camera with no entry.
func (s Settings) Camera(id string) CamSettings {
	for _, c := range s.Cameras {
		if c.ID == id {
			return c
		}
	}
	return CamSettings{ID: id, Off: s.AllOff}
}

// SetCamera adds or changes the entry of the camera c.ID.
func (s *Settings) SetCamera(c CamSettings) {
	out := make([]CamSettings, 0, len(s.Cameras)+1)
	found := false
	for _, old := range s.Cameras {
		if old.ID == c.ID {
			old, found = c, true
		}
		out = append(out, old)
	}
	if !found {
		out = append(out, c)
	}
	s.Cameras = out
}

// Role returns the ID of the camera of a role, or "" for the default camera of the role.
func (s Settings) Role(role string) string {
	switch role {
	case RoleMain:
		return s.Main
	case RoleLapse:
		return s.Lapse
	case RolePicture:
		return s.Picture
	}
	return ""
}

// SetRole sets the camera of a role: an ID, or "" for the default camera.
func (s *Settings) SetRole(role, id string) {
	switch role {
	case RoleMain:
		s.Main = id
	case RoleLapse:
		s.Lapse = id
	case RolePicture:
		s.Picture = id
	}
}

// PinRoles sets each role to the ID of the camera that has the role in list (Info.Roles). After
// it, a change of the settings moves no other role: a role with "" follows the main camera, and a
// pinned role stays on its camera.
func (s *Settings) PinRoles(list []Info) {
	for _, in := range list {
		for _, r := range in.Roles {
			s.SetRole(r, in.ID)
		}
	}
}

// GiveRole gives a role to the camera id. Each other role stays on its camera in list.
func (s *Settings) GiveRole(list []Info, role, id string) {
	s.PinRoles(list)
	s.SetRole(role, id)
}

// SetOff turns the camera id off, or on again. A camera that goes off gives its roles to the
// main camera, and the main role to the first other camera that is on. Each other role stays on
// its camera. SetOff returns the roles that move and the ID of the camera that gets them. With no
// other camera on, the roles stay on the camera id, and to is "".
func (s *Settings) SetOff(list []Info, id string, off bool) (moved []string, to string) {
	s.PinRoles(list)
	cs := s.Camera(id)
	cs.Off = off
	s.SetCamera(cs)
	if !off {
		return nil, ""
	}
	var mine []string
	for _, in := range list {
		switch {
		case in.ID == id:
			mine = in.Roles
		case in.Usable() && to == "":
			to = in.ID
		}
		if in.ID != id && in.HasRole(RoleMain) {
			to = in.ID
		}
	}
	if to == "" {
		return nil, ""
	}
	for _, r := range mine {
		s.SetRole(r, to)
	}
	return mine, to
}

// clone returns a copy of s that shares no slice with s.
func (s Settings) clone() Settings {
	s.Cameras = append([]CamSettings(nil), s.Cameras...)
	return s
}

// ParseSettings reads the settings from the camera file of the deck: JSON, or the two lines of
// an older deck. The two lines are the device ("auto", "off" or an ID) and the turn. The old
// device becomes the main camera, with its turn. Auto becomes the camera "", which the hub
// changes to the camera that the older deck used (normalizeLocked).
func ParseSettings(b []byte) Settings {
	text := strings.TrimSpace(string(b))
	if strings.HasPrefix(text, "{") {
		var s Settings
		if err := json.Unmarshal(b, &s); err == nil {
			for i := range s.Cameras {
				if s.Cameras[i].Rotate != 180 {
					s.Cameras[i].Rotate = 0
				}
			}
			return s
		}
		return Settings{}
	}
	lines := strings.Split(text, "\n")
	rotate := 0
	if len(lines) > 1 {
		if r, err := strconv.Atoi(strings.TrimSpace(lines[1])); err == nil && r == 180 {
			rotate = 180
		}
	}
	var s Settings
	switch d := strings.TrimSpace(lines[0]); d {
	case "", "auto":
		s.Cameras = []CamSettings{{Rotate: rotate}}
	case Off:
		s.AllOff = true
	default:
		s.Cameras = []CamSettings{{ID: d, Rotate: rotate}}
		s.Main = d
	}
	return s
}

// Bytes is the text of the settings for the camera file.
func (s Settings) Bytes() []byte {
	b, _ := json.MarshalIndent(s, "", "  ")
	return append(b, '\n')
}

// Info is a camera in the list of the hub (Hub.Cameras).
type Info struct {
	// ID is the ID of the camera (Device.ID).
	ID string
	// Number is the place of the camera in the list, from 1.
	Number int
	// Name is the name on the screens: the name of the setting, or the name of the device. Two
	// cameras with the same device name get a number after the name, for example "B0561 2".
	Name string
	// Device is the device. A camera that the deck does not find has only the ID and the name of
	// its model (CamSettings.Model).
	Device Device
	// Found reports that the deck finds the camera now.
	Found  bool
	Off    bool
	Rotate int
	// Roles are the roles of the camera now (RoleMain, RoleLapse, RolePicture).
	Roles []string
}

// Usable reports that the camera is on and the deck finds it.
func (i Info) Usable() bool { return i.Found && !i.Off }

// HasRole reports that the camera has the role now.
func (i Info) HasRole(role string) bool {
	for _, r := range i.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// LinkName is the name of a camera from its ID, for a camera that the deck does not find now:
// the link name with no prefix and no suffix, for example "Logitech C270" from
// /dev/v4l/by-id/usb-Logitech_C270-video-index0.
func LinkName(id string) string {
	name := strings.TrimPrefix(filepath.Base(id), "usb-")
	if i := strings.LastIndex(name, "-video-index"); i > 0 {
		name = name[:i]
	}
	return strings.ReplaceAll(name, "_", " ")
}

// List makes the list of the cameras: the devices in the order of their IDs, and then the cameras
// of the settings that the deck does not find, so that their settings stay on the screen.
func List(s Settings, devs []Device) []Info {
	var out []Info
	used := map[string]bool{}
	for _, d := range devs {
		if used[d.ID] {
			continue
		}
		used[d.ID] = true
		c := s.Camera(d.ID)
		for _, e := range s.Cameras {
			if d.Is(e.ID) {
				c = e
				used[e.ID] = true
				break
			}
		}
		out = append(out, Info{ID: d.ID, Device: d, Found: true, Off: c.Off, Rotate: c.Rotate, Name: c.Name})
	}
	for _, c := range s.Cameras {
		if c.ID != "" && !used[c.ID] {
			used[c.ID] = true
			out = append(out, Info{ID: c.ID, Off: c.Off, Rotate: c.Rotate, Name: c.Name, Device: Device{ID: c.ID, Name: c.Model}})
		}
	}
	// The names of the devices, with a number for the names that two cameras have.
	count, seen := map[string]int{}, map[string]int{}
	for _, in := range out {
		count[deviceName(in)]++
	}
	for i := range out {
		in := &out[i]
		in.Number = i + 1
		dn := deviceName(*in)
		seen[dn]++
		if in.Name == "" {
			in.Name = dn
			if count[dn] > 1 {
				in.Name = fmt.Sprintf("%s %d", dn, seen[dn])
			}
		}
	}
	// The roles. A camera of a role that is off or not found gives its role to the main camera.
	mi := pick(out, s.Main)
	if mi >= 0 {
		out[mi].Roles = append(out[mi].Roles, RoleMain)
	}
	for _, role := range []string{RoleLapse, RolePicture} {
		i := usable(out, s.Role(role))
		if i < 0 {
			i = mi
		}
		if i >= 0 {
			out[i].Roles = append(out[i].Roles, role)
		}
	}
	return out
}

// deviceName is the name of the device of a camera, or the name of its link when the deck does
// not find it.
func deviceName(in Info) string {
	if in.Device.Name != "" {
		return in.Device.Name
	}
	return LinkName(in.ID)
}

// usable returns the index of the usable camera id in the list, or -1.
func usable(list []Info, id string) int {
	for i, in := range list {
		if id != "" && in.ID == id && in.Usable() {
			return i
		}
	}
	return -1
}

// pick returns the index of the usable camera id in the list, or of the first usable camera when
// id is "" or not usable, or -1.
func pick(list []Info, id string) int {
	if i := usable(list, id); i >= 0 {
		return i
	}
	for i, in := range list {
		if in.Usable() {
			return i
		}
	}
	return -1
}

// assignIDs sets the ID of each device: its by-id link, or its path with no link. Two cameras of
// the same model with the same serial number (or none) share one by-id link, so udev makes the
// link for one of them only. Then the cameras of that name get their by-path links as IDs, and the
// by-id link becomes the alias of each.
func assignIDs(devs []Device, byID, byPath map[string]string) []Device {
	shared := map[string]bool{} // the names of the cameras that share a by-id link
	for _, d := range devs {
		if _, ok := byID[d.Path]; !ok {
			for _, o := range devs {
				if _, ok := byID[o.Path]; ok && o.Name == d.Name {
					shared[d.Name] = true
				}
			}
		}
	}
	alias := map[string]string{} // the by-id link of each shared name
	for _, d := range devs {
		if l, ok := byID[d.Path]; ok && shared[d.Name] {
			alias[d.Name] = l
		}
	}
	out := make([]Device, len(devs))
	for i, d := range devs {
		d.ID = d.Path
		switch l, ok := byPath[d.Path]; {
		case shared[d.Name] && ok:
			d.ID, d.Alias = l, alias[d.Name]
		default:
			if l, ok := byID[d.Path]; ok {
				d.ID = l
			}
		}
		out[i] = d
	}
	return out
}
