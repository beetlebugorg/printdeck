package ui

import (
	"fmt"
	"image"
	"image/color"
	"time"

	"printdeck/internal/board"
	"printdeck/internal/eq"
	"printdeck/internal/gcodemeta"
	"printdeck/internal/printer"
	"printdeck/internal/spotify"
	"printdeck/internal/wifi"
)

// Demo sources give the UI fixed data, to look at it without a Spotify account or a printer.

type DemoMusic struct{ State_ spotify.State }

func (d *DemoMusic) State() spotify.State { return d.State_ }

// demoCover is the cover of DemoMusic. The real client also keeps one image for each track.
var demoCover = func() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 400, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 400; x++ {
			img.Set(x, y, color.RGBA{uint8(40 + x/3), uint8(80 + y/5), uint8(200 - x/4), 255})
		}
	}
	return img
}()

func (d *DemoMusic) Cover() image.Image { return demoCover }
func (d *DemoMusic) Toggle()            {}
func (d *DemoMusic) Next()              {}
func (d *DemoMusic) Previous()          {}
func (d *DemoMusic) Seek(time.Duration) {}
func (d *DemoMusic) SetShuffle(bool)    {}
func (d *DemoMusic) CycleRepeat()       {}

type DemoPrinter struct {
	State_   printer.State
	Img      image.Image
	View     int          // layer of the scrubber, -1 for live
	Boards_  []board.Info // the boards of the printer setup
	Machine_ printer.Machine
	History_ []printer.Record
	Console_ []printer.ConsoleLine
	Meta_    gcodemeta.Meta // the slicer data of every file
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

func (d *DemoPrinter) Gcode(...string) error                        { return nil }
func (d *DemoPrinter) Home(...string) error                         { return nil }
func (d *DemoPrinter) Jog(x, y, z float64, s int) error             { return nil }
func (d *DemoPrinter) SetToolTemp(int) error                        { return nil }
func (d *DemoPrinter) SetBedTemp(int) error                         { return nil }
func (d *DemoPrinter) Extrude(float64, int) error                   { return nil }
func (d *DemoPrinter) Fan(int) error                                { return nil }
func (d *DemoPrinter) MotorsOff() error                             { return nil }
func (d *DemoPrinter) SpeedFactor(int) error                        { return nil }
func (d *DemoPrinter) FlowFactor(int) error                         { return nil }
func (d *DemoPrinter) BabyStep(float64) error                       { return nil }
func (d *DemoPrinter) EmergencyStop() error                         { return nil }
func (d *DemoPrinter) FirmwareRestart() error                       { return nil }
func (d *DemoPrinter) Boards() ([]board.Info, bool)                 { return d.Boards_, false }
func (d *DemoPrinter) Machine() printer.Machine                     { return d.Machine_ }
func (d *DemoPrinter) FileMeta(string) gcodemeta.Meta               { return d.Meta_ }
func (d *DemoPrinter) DeleteFile(string) error                      { return nil }
func (d *DemoPrinter) History() []printer.Record                    { return d.History_ }
func (d *DemoPrinter) ProbeBoards()                                 {}
func (d *DemoPrinter) RestartKlipper() error                        { return nil }
func (d *DemoPrinter) FlashBoard(string, board.FlashProgress) error { return nil }
func (d *DemoPrinter) RunAction(string, ...string) error            { return nil }
func (d *DemoPrinter) UseConfig(string, string) error               { return nil }
func (d *DemoPrinter) Console() []printer.ConsoleLine               { return d.Console_ }
func (d *DemoPrinter) SendConsole(line string) {
	d.Console_ = append(d.Console_, printer.ConsoleLine{Kind: "in", Text: line})
}

// ResetScreens closes every screen, dialog and page; for the pictures of the manual.
func (a *App) ResetScreens() {
	a.setup, a.pages, a.swap, a.tune, a.keypad, a.confirm, a.consoleKB, a.flash = nil, nil, nil, false, nil, nil, nil, nil
	a.picker, a.tracks, a.eqOpen, a.doneSeen = nil, nil, false, ""
}

// OpenPrintScreen opens a screen of the Print tab by name; for the demo.
func (a *App) OpenPrintScreen(name string) {
	a.tab, a.pages, a.tune, a.swap, a.keypad = TabPrint, nil, false, nil, nil
	switch name {
	case "control", "move":
		a.pages = []page{pageMove}
	case "files":
		a.pages = []page{pageFiles}
	case "filament":
		a.pages = []page{pageFilament}
	case "maintenance":
		a.pages = []page{pageMaintenance}
	case "machine":
		a.pages = []page{pageMachine}
	case "macros":
		a.pages = []page{pageMachine, pageMacros}
	case "history":
		a.pages = []page{pageMachine, pageHistory}
	case "stats":
		a.pages = []page{pageMachine, pageStats}
	case "configs":
		a.configBoard = board.Info{Device: "/dev/serial/by-id/usb-UltiMachine_RAMBo_64033353730351918201-if00", Name: "UltiMachine RAMBo", Klipper: true, MCU: "atmega2560"}
		a.pages = []page{pageConfigs}
	case "action":
		if d, ok := a.Printer.(*DemoPrinter); ok {
			d.State_.Action, d.State_.ActionStart = "Bed tilt", a.Now().Add(-42*time.Second)
		}
	case "flash":
		a.flash = &flashState{step: "Writing", done: 41, total: 100}
	case "console":
		a.pages = []page{pageMachine, pageConsole}
	case "setup", "error":
		if d, ok := a.Printer.(*DemoPrinter); ok {
			if name == "setup" {
				d.State_ = printer.State{Err: "Klipper is not ready", NeedsSetup: true}
				d.Boards_ = []board.Info{{Name: "UltiMachine ultimachine.com RAMBo", Klipper: true, MCU: "atmega2560",
					Version: "v0.13.0-777-g7bc4d0946-prind-20260930_185514-10f1cdde21e6"}}
			} else {
				d.State_ = printer.State{Err: "Error in printer.cfg", ErrDetail: "Option 'rotation_distance' in section 'stepper_x' must be specified\n\nOnce the underlying issue is corrected, use the \"RESTART\" command to reload the config and restart the host software.\nPrinter is halted"}
			}
			a.setupProbed = true
		}
	case "file":
		a.file = a.Printer.Files()[0]
		a.pages = []page{pageFiles, pageFile}
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
