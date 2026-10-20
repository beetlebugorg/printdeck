package ui

import (
	"fmt"
	"image"
	"image/color"
	"printdeck/internal/eq"
	"time"

	"printdeck/internal/printer"
	"printdeck/internal/spotify"
	"printdeck/internal/wifi"
)

// Demo sources give the UI fixed data, to look at it without a Spotify account or a printer.

type DemoMusic struct{ State_ spotify.State }

func (d *DemoMusic) State() spotify.State { return d.State_ }
func (d *DemoMusic) Cover() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 400, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 400; x++ {
			img.Set(x, y, color.RGBA{uint8(40 + x/3), uint8(80 + y/5), uint8(200 - x/4), 255})
		}
	}
	return img
}
func (d *DemoMusic) Toggle()            {}
func (d *DemoMusic) Next()              {}
func (d *DemoMusic) Previous()          {}
func (d *DemoMusic) Seek(time.Duration) {}
func (d *DemoMusic) SetShuffle(bool)    {}
func (d *DemoMusic) CycleRepeat()       {}

type DemoPrinter struct {
	State_ printer.State
	Img    image.Image
	View   int // layer of the scrubber, -1 for live
}

func (d *DemoPrinter) State() printer.State { return d.State_ }
func (d *DemoPrinter) Files() []printer.File {
	return []printer.File{{Path: "benchy.gcode", Name: "benchy.gcode"}, {Path: "case_body.gcode", Name: "case_body.gcode"}}
}
func (d *DemoPrinter) Preview() image.Image { return d.Img }
func (d *DemoPrinter) LoadFiles()           {}
func (d *DemoPrinter) Pause()               {}
func (d *DemoPrinter) Resume()              {}
func (d *DemoPrinter) Cancel()              {}
func (d *DemoPrinter) Connect()             {}
func (d *DemoPrinter) Heat(int, int)        {}
func (d *DemoPrinter) PrintFile(string)     {}

type DemoVolume int

func (d DemoVolume) Volume() int { return int(d) }
func (d DemoVolume) Max() int    { return 100 }
func (d DemoVolume) Set(int)     {}

// SetTab selects a tab; for the demo and for tests.
func (a *App) SetTab(t Tab) { a.tab, a.picker, a.eqOpen = t, nil, false }

// OpenPicker opens the playlist picker; for the demo.
func (a *App) OpenPicker(play bool) {
	a.picker, a.tracks = &picker{}, nil
	if play {
		a.picker.mode = pickPlay
	}
}

func (d *DemoMusic) ToggleLike() error    { return nil }
func (d *DemoMusic) LoadPlaylists() error { return nil }
func (d *DemoMusic) Playlists() []spotify.Playlist {
	return []spotify.Playlist{{URI: "spotify:user:x:collection", Name: "Liked Songs", Tracks: 1103}, {URI: "spotify:playlist:dw", Name: "Discover Weekly", Recent: true}, {ID: "1", Name: "Workshop", Tracks: 212, Editable: true}, {ID: "2", Name: "Road trip", Tracks: 48, Editable: true}, {ID: "3", Name: "Lofi Beats", Tracks: 62}}
}
func (d *DemoMusic) AddToPlaylist(string) error { return nil }
func (d *DemoMusic) PlayPlaylist(string) error  { return nil }

func (d *DemoMusic) LinkURL() string { return "https://pidev.local:8888/link" }

func (d *DemoMusic) LoadTracks(uri string, offset int) ([]spotify.Track, int, error) {
	var out []spotify.Track
	for i := offset; i < min(offset+spotify.TracksPage, 1103); i++ {
		out = append(out, spotify.Track{URI: fmt.Sprintf("spotify:track:%d", i), Name: fmt.Sprintf("Song number %d", i+1), Artist: "Some Artist"})
	}
	return out, 1103, nil
}
func (d *DemoMusic) PlayTrack(string, string) error { return nil }

// OpenTracks opens the song list of a playlist, with the rows loaded; for the demo.
func (a *App) OpenTracks(scroll int) {
	a.openTracks(spotify.Playlist{URI: "spotify:user:x:collection", Name: "Liked Songs"})
	tracks, total, _ := a.Music.LoadTracks("", 0)
	a.tracks.items, a.tracks.total, a.tracks.pages[0] = make([]spotify.Track, total), total, 2
	copy(a.tracks.items, tracks)
	a.tracks.scroll = scroll
}

func (d *DemoPrinter) SetViewLayer(l int) { d.View = l }
func (d *DemoPrinter) ViewLayer() int     { return d.View }

// DemoEQ is an equalizer without CamillaDSP.
type DemoEQ struct{ G eq.Gains }

func (d *DemoEQ) Gains() eq.Gains           { return d.G }
func (d *DemoEQ) SetGain(b int, db float64) { d.G[b] = db }
func (d *DemoEQ) SetPreset(string)          {}
func (d *DemoEQ) Preset() string            { return "" }
func (d *DemoEQ) Connected() bool           { return true }

// OpenEQ opens the equalizer; for the demo.
func (a *App) OpenEQ() { a.eqOpen, a.picker, a.tracks = true, nil, nil }

func (d *DemoPrinter) Gcode(...string) error            { return nil }
func (d *DemoPrinter) Home(...string) error             { return nil }
func (d *DemoPrinter) Jog(x, y, z float64, s int) error { return nil }
func (d *DemoPrinter) SetToolTemp(int) error            { return nil }
func (d *DemoPrinter) SetBedTemp(int) error             { return nil }
func (d *DemoPrinter) Extrude(float64, int) error       { return nil }
func (d *DemoPrinter) Fan(int) error                    { return nil }
func (d *DemoPrinter) MotorsOff() error                 { return nil }
func (d *DemoPrinter) SpeedFactor(int) error            { return nil }
func (d *DemoPrinter) FlowFactor(int) error             { return nil }
func (d *DemoPrinter) BabyStep(float64) error           { return nil }
func (d *DemoPrinter) EmergencyStop() error             { return nil }
func (d *DemoPrinter) FirmwareRestart() error           { return nil }

// OpenPrintScreen opens a screen of the Print tab by name; for the demo.
func (a *App) OpenPrintScreen(name string) {
	a.tab, a.control, a.tune, a.swap, a.keypad = TabPrint, false, false, nil, nil
	switch name {
	case "control":
		a.control = true
	case "tune":
		a.tune = true
	case "swap":
		a.swap = &swapState{}
	case "swap-heat":
		a.swap = &swapState{step: swapHeat, temp: 240}
	case "keypad":
		a.editTemp("Nozzle temperature", 210, 300, toolPresets, func(int) error { return nil })
	}
}

// DemoSetup is a SetupHost with fixed networks, for the pictures of the setup screen.
type DemoSetup struct {
	Nets    []wifi.Network
	Status_ wifi.Status
}

func (d *DemoSetup) Scan() error                       { return nil }
func (d *DemoSetup) Networks() ([]wifi.Network, error) { return d.Nets, nil }
func (d *DemoSetup) Status() (wifi.Status, error)      { return d.Status_, nil }
func (d *DemoSetup) SetWifi(string, string) error      { return nil }
func (d *DemoSetup) Login() (string, string)           { return "printdeck", "stamp-ocean-cider-brick" }
func (d *DemoSetup) Done()                             {}

// OpenSetupScreen shows one step of the setup screen with the networks of a DemoSetup, for the demo
// pictures: "wifi", "password", "user", or "done".
func (a *App) OpenSetupScreen(name string) {
	d := a.Setup.(*DemoSetup)
	a.setup = &setupFlow{host: "printdeck", nets: d.Nets, status: d.Status_}
	switch name {
	case "password":
		a.setupPassword(d.Nets[0].SSID)
		a.setup.kb.value = "hunter22"
	case "done":
		a.setup.step = setupDone
	case "help":
		a.setup.step, a.setup.help = setupDone, true
	}
}
