package ui

import (
	"errors"
	"image"
	"path"
	"path/filepath"
	"strings"
	"time"

	"printdeck/internal/board"
	"printdeck/internal/gcodemeta"
	"printdeck/internal/printer"
	"printdeck/internal/timelapse"
	"printdeck/internal/wifi"
)

// Demo sources give the UI fixed data, to look at it without a printer.

type DemoPrinter struct {
	State_ printer.State
	Img    image.Image
	View   int // layer of the scrubber, -1 for live
	// LayerTimes_ is the seconds of each layer of the print, from the slicer.
	LayerTimes_ []float64
	Boards_     []board.Info // the boards of the printer setup
	Machine_    printer.Machine
	History_    []printer.Record
	Console_    []printer.ConsoleLine
	Meta_       gcodemeta.Meta  // the slicer data of every file
	Spools_     *printer.Spools // the rolls of filament, or nil
	// SDCard_ is the SD card of the printer: the list of each folder ("" is the top). It shows
	// when Machine_ has printdeck_sdcard.
	SDCard_ map[string][]printer.SDFile
}

func (d *DemoPrinter) HasSDCard() bool { return d.Machine_.Has("printdeck_sdcard") }
func (d *DemoPrinter) SDFiles(dir string) ([]printer.SDFile, error) {
	if d.SDCard_ == nil {
		return nil, errors.New("no SD card in the printer")
	}
	return d.SDCard_[dir], nil
}
func (d *DemoPrinter) CopySDFile(p string, size int64) (string, int, error) {
	return path.Base(p), 0, nil
}
func (d *DemoPrinter) CancelSDCopy() {}

func (d *DemoPrinter) State() printer.State {
	st := d.State_
	if st.SpeedPct == 0 { // Klipper reports 100 % when no M220 or M221 ran
		st.SpeedPct, st.FlowPct = 100, 100
	}
	return st
}
func (d *DemoPrinter) Files() []printer.File {
	return []printer.File{{Path: "benchy.gcode", Name: "benchy.gcode"}, {Path: "case_body.gcode", Name: "case_body.gcode"}}
}
func (d *DemoPrinter) Preview() image.Image  { return d.Img }
func (d *DemoPrinter) LayerTimes() []float64 { return d.LayerTimes_ }
func (d *DemoPrinter) RecordPicture(printer.Record) (image.Image, bool) {
	return d.Img, false
}
func (d *DemoPrinter) FilePicture(printer.File, string) image.Image { return nil }
func (d *DemoPrinter) SpoolStore() *printer.Spools                  { return d.Spools_ }
func (d *DemoPrinter) LoadFiles()                                   {}
func (d *DemoPrinter) Pause()                                       {}
func (d *DemoPrinter) Resume()                                      {}
func (d *DemoPrinter) Cancel()                                      {}
func (d *DemoPrinter) Connect()                                     {}
func (d *DemoPrinter) Heat(int, int)                                {}
func (d *DemoPrinter) PrintFile(string)                             {}

func (d *DemoPrinter) SetViewLayer(l int) { d.View = l }
func (d *DemoPrinter) ViewLayer() int     { return d.View }

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
func (d *DemoPrinter) Boards() ([]board.Info, bool)     { return d.Boards_, false }
func (d *DemoPrinter) Machine() printer.Machine         { return d.Machine_ }
func (d *DemoPrinter) Rates() printer.Rates             { return printer.ComputeRates(d.History_) }
func (d *DemoPrinter) FileMeta(string) gcodemeta.Meta   { return d.Meta_ }
func (d *DemoPrinter) DeleteFile(string) error          { return nil }
func (d *DemoPrinter) History() []printer.Record        { return d.History_ }
func (d *DemoPrinter) ProbeBoards()                     {}

// SetNote sets the note of the record of the demo history with the path and the end of r.
func (d *DemoPrinter) SetNote(r printer.Record, note string) bool {
	for i := range d.History_ {
		if d.History_[i].Path == r.Path && d.History_[i].End.Equal(r.End) {
			d.History_[i].Note = note
			return true
		}
	}
	return false
}

// SetProject sets the project of the record of the demo history with the path and the end of r.
func (d *DemoPrinter) SetProject(r printer.Record, project string) bool {
	for i := range d.History_ {
		if d.History_[i].Path == r.Path && d.History_[i].End.Equal(r.End) {
			d.History_[i].Project = project
			return true
		}
	}
	return false
}
func (d *DemoPrinter) RestartKlipper() error                        { return nil }
func (d *DemoPrinter) FlashBoard(string, board.FlashProgress) error { return nil }
func (d *DemoPrinter) RunAction(string, ...string) error            { return nil }
func (d *DemoPrinter) PrintPATower(int, int, string) error          { return nil }
func (d *DemoPrinter) PrintCalibTowers(int, int, string) error      { return nil }
func (d *DemoPrinter) SetPressureAdvance(float64) error             { return nil }
func (d *DemoPrinter) UseConfig(string, string) error               { return nil }
func (d *DemoPrinter) Console() []printer.ConsoleLine               { return d.Console_ }
func (d *DemoPrinter) SendConsole(line string) {
	d.Console_ = append(d.Console_, printer.ConsoleLine{Kind: "in", Text: line})
}

// DemoTimeLapse is a time lapse with fixed settings, and videos in the folder Dir; for the demo.
type DemoTimeLapse struct {
	S   timelapse.Settings
	In  timelapse.Info
	Dir string
}

func (d *DemoTimeLapse) Settings() timelapse.Settings     { return d.S }
func (d *DemoTimeLapse) SetSettings(s timelapse.Settings) { d.S = s }
func (d *DemoTimeLapse) Info() timelapse.Info             { return d.In }
func (d *DemoTimeLapse) VideoPath(name string) (string, bool) {
	for _, v := range d.In.Videos {
		if v.Name == name {
			return filepath.Join(d.Dir, name), true
		}
	}
	return "", false
}

// DemoPlayer opens the player of a video over the pages, paused at frame; for the pictures of the
// manual.
func (a *App) DemoPlayer(name, title string, frame int) {
	a.openPlayer(name, title)
	a.player.autoplay, a.player.pos = false, frame
}

// UseSim sets the name of the simulated printer in the state of the demo.
func (d *DemoPrinter) UseSim(id string) error {
	d.State_.Simulated = ""
	for _, m := range printer.SimModels() {
		if m.ID == id {
			d.State_.Simulated = m.Name
		}
	}
	return nil
}

// DemoPickColor opens the palette of the color of a roll, with the color hex lit.
func (a *App) DemoPickColor(hex string) { a.pickColor(hex, func(string) {}) }

// ResetScreens closes every screen, dialog and page; for the pictures of the manual.
func (a *App) ResetScreens() {
	a.setup, a.pages, a.swap, a.tune, a.keypad, a.confirm, a.consoleKB, a.flash, a.form, a.colors = nil, nil, nil, false, nil, nil, nil, nil, nil, nil
	a.camKB = nil
	a.doneSeen = ""
	a.sd = sdView{}
}

// DemoAway shows the screen that is open now as it is away from the print (Menu on the screen of
// the print), with the print bar; for the demo.
func (a *App) DemoAway() {
	st := a.Printer.State()
	a.away, a.awayFile, a.touchAt = true, st.FilePath, a.Now()
	// The print runs already: it is no new print for followPrint.
	a.followed, a.wasBusy, a.wasPaused = true, st.Reachable && st.Busy(), st.Paused
}

// DemoScroll scrolls the list of the page that is open to px from its top; for the pictures of
// the manual.
func (a *App) DemoScroll(px int) { *a.pageScroll() = px }

// OpenPrintScreen opens a screen by name; for the demo.
func (a *App) OpenPrintScreen(name string) {
	a.pages, a.tune, a.swap, a.keypad, a.form, a.away = nil, false, nil, nil, nil, false
	switch name {
	case "control", "move":
		a.pages = []page{pageMove}
	case "files":
		a.pages = []page{pageFiles}
	case "sd-card":
		// The top folder of the card, read at once.
		a.pages = []page{pageFiles}
		if sp, ok := a.sdPrinter(); ok {
			files, err := sp.SDFiles("")
			a.sd = sdView{on: true, loaded: err == nil, files: files}
		}
	case "filament":
		a.pages = []page{pageFilament}
	case "maintenance":
		a.pages = []page{pageMachine, pageMaintenance}
	case "machine":
		a.pages = []page{pageMachine}
	case "machine-restart":
		a.pages = []page{pageMachine}
		a.askRestart()
	case "macros":
		a.pages = []page{pageMachine, pageMacros}
	case "history":
		a.pages = []page{pageMachine, pageStats, pageHistory}
	case "rolls":
		a.pages = []page{pageFilament, pageSpools}
	case "roll":
		if s := a.Printer.SpoolStore(); s != nil {
			if sp, ok := s.In(); ok {
				a.spoolID = sp.ID
			}
		}
		a.pages = []page{pageFilament, pageSpools, pageSpool}
	case "new-roll":
		a.openRollForm(nil)
		a.form.name, a.form.scale, a.form.reel, a.form.price = "Galaxy Black", 1180, 180, 22
	case "record":
		a.record = a.Printer.History()[0]
		a.pages = []page{pageMachine, pageStats, pageHistory, pageRecord}
	case "stats":
		a.pages = []page{pageMachine, pageStats}
	case "settings":
		a.pages = []page{pageMachine, pageSettings}
	case "monitor":
		a.pages = []page{pageMachine, pageMonitor}
	case "camera":
		a.pages = []page{pageMachine, pageSettings, pageCamera}
		a.camFocus, a.camPTZ = false, false
	case "camera-focus":
		a.pages = []page{pageMachine, pageSettings, pageCamera}
		a.camFocus, a.camPTZ = true, false
	case "camera-ptz":
		a.pages = []page{pageMachine, pageSettings, pageCamera}
		a.camFocus, a.camPTZ = false, true
	case "camera-all":
		a.pages = []page{pageMachine, pageSettings, pageCamera}
		a.camID = camAll
	case "cameras":
		a.pages = []page{pageMachine, pageSettings, pageCameras}
	case "calibrate", "calibrate-tower", "calibrate-fit":
		a.demoCalibration(name)
	case "camera-settings":
		// The second camera, with the name of the manual.
		if list := a.Camera.Cameras(); len(list) > 1 {
			a.camEdit = list[1].ID
		}
		a.pages = []page{pageMachine, pageSettings, pageCameras, pageCameraEdit}
	case "simulator":
		a.pages = []page{pageMachine, pageSimulator}
	case "configs":
		a.configBoard = board.Info{Device: "/dev/serial/by-id/usb-UltiMachine_RAMBo_64033353730351918201-if00", Name: "UltiMachine RAMBo", Klipper: true, MCU: "atmega2560"}
		a.pages = []page{pageConfigs}
	case "action":
		if d, ok := a.Printer.(*DemoPrinter); ok {
			d.State_.Action, d.State_.ActionStart = "Bed tilt", a.Now().Add(-42*time.Second)
		}
	case "flash":
		a.flash = &flashState{step: "Writing", done: 41, total: 100}
	case "update":
		a.pages = []page{pageMachine, pageUpdate}
	case "console":
		a.pages = []page{pageMachine, pageConsole}
	case "starting":
		if d, ok := a.Printer.(*DemoPrinter); ok {
			d.State_ = printer.State{Err: "Printer is not ready", Starting: true, ErrDetail: "Printer is not ready\nThe klippy host software is attempting to connect."}
		}
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
// pictures: "wifi", "password", "user", or "done". "" closes it.
func (a *App) OpenSetupScreen(name string) {
	if name == "" {
		a.setup = nil
		return
	}
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

// DemoUpdater is an updater with a fixed state; for the demo and the manual.
type DemoUpdater struct{ S UpdateStatus }

func (d *DemoUpdater) Status() UpdateStatus { return d.S }
func (d *DemoUpdater) Check()               {}
func (d *DemoUpdater) Install()             {}
func (d *DemoUpdater) SetChannel(beta bool) { d.S.Beta = beta }
func (d *DemoUpdater) RestartNow()          {}
func (d *DemoUpdater) SwitchDeck()          {}

// Parts returns the places of the keys and the named parts of the last frame.
func (a *App) Parts() []image.Rectangle {
	out := make([]image.Rectangle, len(a.names))
	for i, f := range a.names {
		out[i] = f.r
	}
	return out
}

// KeyRect returns the place of the key or the named part (region) of the last frame: the last
// one with the same label, or else the first whose label starts with it. A part of a screen is
// drawn after the status display, so the part of the screen wins over a part of the display with
// the same name. The manual places its callouts with it.
func (a *App) KeyRect(label string) (image.Rectangle, bool) {
	want := lcdText(label)
	for i := len(a.names) - 1; i >= 0; i-- {
		if f := a.names[i]; f.label == want {
			return f.r, true
		}
	}
	for _, f := range a.names {
		if strings.HasPrefix(f.label, want) {
			return f.r, true
		}
	}
	return image.Rectangle{}, false
}
