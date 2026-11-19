package printer

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"printdeck/internal/bambu"
	"printdeck/internal/board"
	"printdeck/internal/gcodemeta"
)

// Bambu is the backend of a Bambu Lab printer on the network (for example the X2D): the deck reads
// the state from the MQTT reports of package bambu, and keeps the prints in its history. The
// printer keeps its own firmware and its own screen.
//
// Pause, Resume and Cancel send the commands of the printer. The other controls return
// ErrNotSupported, or ErrNotTested until their test on a print.
type Bambu struct {
	Client *bambu.Client
	Hist   *History
	Name   string // the name of the printer, for the screens
	// SavePin keeps the certificate of the printer after the first connection (Setting.Pin).
	// It can be nil.
	SavePin func(pin string)

	mu          sync.Mutex
	st          State
	last        bambu.Report
	has         bool      // last is a report
	path        string    // the key of the print now in the history ("" with no print)
	name        string    // the name of the print now
	system      bool      // the print now is a job of the printer itself (a calibration)
	started     time.Time // the start of the print now
	nextProject string
	camera      string // the RTSPS address of the camera
}

// bambuRetry is the wait before a new connection after a lost link.
var bambuRetry = 10 * time.Second

// bambuCancelled is the print_error of a print that the user stopped.
const bambuCancelled = 0x0300400C

// NewBambu returns the backend of the printer at addr with the serial number and the access code.
// pin is the SHA-256 of the certificate of the printer, or "" to learn it.
func NewBambu(addr, serial, code, pin, name string, hist *History) *Bambu {
	return &Bambu{Client: &bambu.Client{Addr: addr, Serial: serial, Code: code, Pin: pin}, Hist: hist, Name: name,
		st: State{Text: "Connecting", Activity: "Connecting to the printer"}}
}

// Run follows the printer until ctx ends. notify runs after each change of the state.
func (b *Bambu) Run(ctx context.Context, notify func()) {
	for {
		err := b.Client.Run(ctx, func(r bambu.Report) {
			b.update(r, time.Now())
			if notify != nil {
				notify()
			}
		})
		if ctx.Err() != nil {
			return
		}
		b.offline(err)
		if notify != nil {
			notify()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(bambuRetry):
		}
	}
}

// offline shows the error of the link.
func (b *Bambu) offline(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := &b.st
	st.Reachable, st.Connected, st.Starting = false, false, false
	st.Text, st.Activity = "Offline", "The printer does not answer"
	switch {
	case errors.Is(err, bambu.ErrRefused):
		st.Err = "The printer refused the access code"
	case errors.Is(err, bambu.ErrPin):
		st.Err = "The certificate of the printer changed. Add the printer again if you replaced it."
	default:
		st.Err = "No answer from the printer at " + b.Client.Addr
	}
	log.Printf("bambu: %s: %v", b.Client.Addr, err)
}

// update sets the state from the report r at now, and adds a print to the history at its end.
func (b *Bambu) update(r bambu.Report, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.has && b.SavePin != nil && !b.Client.HasPin() {
		if pin := b.Client.Seen(); pin != "" {
			b.Client.SetPin(pin)
			go b.SavePin(pin)
		}
	}
	was := b.last
	b.last, b.has = r, true
	b.camera = b.Client.CameraURL(r)
	st := &b.st
	st.Reachable, st.Connected, st.Err, st.Starting = true, true, "", false
	st.Tool, st.ToolTarget = float64(r.Nozzle), float64(r.NozzleTarget)
	st.Bed, st.BedTarget = float64(r.Bed), float64(r.BedTarget)
	st.Fan, st.HasFan = r.FanPercent(), true
	st.SpeedPct = r.SpeedPct.Int()
	st.Message = hmsText(r.HMS)

	if r.Busy() {
		// A new print: no print before, or another task.
		if b.path == "" || r.TaskID != was.TaskID && r.TaskID != "" {
			b.started = now
			b.path = "bambu-" + strconv.FormatInt(now.Unix(), 10)
			b.name, b.system = r.Name(), r.PrintType == "system"
			if b.nextProject != "" && b.Hist != nil {
				b.Hist.SetNextProject(b.path, b.nextProject)
				b.nextProject = ""
			}
		}
		st.Printing, st.Paused = true, r.Paused()
		st.Preparing = r.State == "PREPARE" || r.State == "SLICING"
		st.FilePath, st.File, st.Title = b.path, b.name, b.name
		st.Completion = float64(r.Percent)
		st.TimeLeft, st.HasTimeLeft = r.TimeLeft(), r.Remaining > 0
		st.Layer, st.Layers = r.Layer.Int(), r.Layers.Int()
		st.Elapsed = now.Sub(b.started)
		st.PrintDuration = st.Elapsed
		st.Text = "Printing"
		if st.Paused {
			st.Text = "Paused"
		}
		st.Activity = r.StageText()
		if st.Activity == "" {
			st.Activity = st.Text
		}
		st.Finished = ""
		return
	}
	if b.path != "" {
		result := "complete"
		switch {
		case r.State == "FAILED" && int(r.Error) == bambuCancelled:
			result = "cancelled"
		case r.State == "FAILED":
			result = "error"
			st.FinishedError = fmt.Sprintf("print_error %08X", int(r.Error))
		case r.State != "FINISH":
			result = "cancelled" // the printer went to IDLE with no end
		}
		st.Finished, st.FinishedPath, st.FinishedFile, st.FinishedTitle = result, b.path, b.name, b.name
		st.FinishedTime = now.Sub(b.started)
		// A job of the printer itself (a calibration) is no print of the user.
		if b.Hist != nil && !b.system {
			b.Hist.add(*st, now, nil, 0, 0, nil)
		}
		b.path = ""
	}
	st.Printing, st.Paused, st.Preparing = false, false, false
	st.FilePath, st.File, st.Title = "", "", ""
	st.Completion, st.Elapsed, st.PrintDuration, st.HasTimeLeft = 0, 0, 0, false
	st.Layer, st.Layers = 0, 0
	st.Text, st.Activity = "Ready", "Ready"
}

// hmsText is the text of the faults of the printer, or "".
func hmsText(h []bambu.HMS) string {
	if len(h) == 0 {
		return ""
	}
	codes := make([]string, len(h))
	for i, e := range h {
		codes[i] = e.String()
	}
	return "Printer message " + strings.Join(codes, ", ")
}

// State returns the state of the printer.
func (b *Bambu) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.st
}

// Report returns the last report of the printer. ok is false before the first report.
func (b *Bambu) Report() (bambu.Report, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.last, b.has
}

// Camera returns the RTSPS address of the camera with no login, and with the login. The address
// is the usual one before the first report.
func (b *Bambu) Camera() (shown, login string) {
	b.mu.Lock()
	shown = b.camera
	b.mu.Unlock()
	if shown == "" {
		shown = b.Client.CameraURL(bambu.Report{})
	}
	u, err := url.Parse(shown)
	if err != nil {
		return shown, shown
	}
	u.User = url.UserPassword(bambu.User, b.Client.Code)
	return shown, u.String()
}

// Machine returns what the printer has: an FDM printer with its own controls.
func (b *Bambu) Machine() Machine {
	board := "Bambu Lab"
	if model, fw := b.Client.Version(); model != "" {
		board = strings.TrimSpace(model + " " + fw)
	}
	return Machine{Process: ProcessFDM, Board: board, OwnControls: true}
}

// History and projects, as the ChiTu backend.

func (b *Bambu) History() []Record {
	if b.Hist == nil {
		return nil
	}
	return b.Hist.Records()
}

func (b *Bambu) Rates() Rates {
	if b.Hist == nil {
		return ComputeRates(nil)
	}
	return b.Hist.Rates()
}

func (b *Bambu) SetNote(r Record, note string) bool {
	return b.Hist != nil && b.Hist.SetNote(r.Path, r.End, note)
}

func (b *Bambu) SetProject(r Record, project string) bool {
	return b.Hist != nil && b.Hist.SetProject(r.Path, r.End, project)
}

// SetNextProject sets the project of the print now, or of the next print when no print runs.
func (b *Bambu) SetNextProject(project string) bool {
	st := b.State()
	if b.Hist == nil {
		return false
	}
	if !st.Printing || st.FilePath == "" {
		b.mu.Lock()
		b.nextProject = project
		b.mu.Unlock()
		return true
	}
	b.Hist.SetNextProject(st.FilePath, project)
	return true
}

// SetFileProject sets the project of the next print.
func (b *Bambu) SetFileProject(_, project string) {
	b.mu.Lock()
	b.nextProject = project
	b.mu.Unlock()
}

// NextProject returns the project of the print now, or of the next print.
func (b *Bambu) NextProject() string {
	st := b.State()
	if b.Hist == nil {
		return ""
	}
	if !st.Printing {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.nextProject
	}
	return b.Hist.NextProject(st.FilePath)
}

// Pause, Resume and Cancel send the commands of the printer. An error shows in the panel.
func (b *Bambu) Pause()  { b.command("Pause", b.Client.Pause) }
func (b *Bambu) Resume() { b.command("Resume", b.Client.Resume) }
func (b *Bambu) Cancel() { b.command("Stop", b.Client.Stop) }

// command runs the command f, logs it, and shows an error in the panel.
func (b *Bambu) command(name string, f func() error) {
	err := f()
	log.Printf("bambu: %s: %v", name, err)
	if err != nil {
		b.mu.Lock()
		b.st.Message = name + " did not go to the printer: " + err.Error()
		b.mu.Unlock()
	}
}

// The files, the pictures and the layers: the deck reads no file of the printer yet.

func (b *Bambu) Files() []File                                { return nil }
func (b *Bambu) LoadFiles()                                   {}
func (b *Bambu) Preview() image.Image                         { return nil }
func (b *Bambu) LayerTimes() []float64                        { return nil }
func (b *Bambu) SetViewLayer(int)                             {}
func (b *Bambu) ViewLayer() int                               { return -1 }
func (b *Bambu) FileMeta(string) gcodemeta.Meta               { return gcodemeta.Meta{} }
func (b *Bambu) SpoolStore() *Spools                          { return nil }
func (b *Bambu) Connect()                                     {}
func (b *Bambu) Console() []ConsoleLine                       { return nil }
func (b *Bambu) SendConsole(string)                           {}
func (b *Bambu) RecordPicture(Record) (image.Image, bool)     { return nil, false }
func (b *Bambu) FilePicture(File, string) image.Image         { return nil }
func (b *Bambu) DeleteFile(string) error                      { return ErrNotSupported }
func (b *Bambu) PrintFile(string)                             {}
func (b *Bambu) Boards() ([]board.Info, bool)                 { return nil, false }
func (b *Bambu) ProbeBoards()                                 {}
func (b *Bambu) FlashBoard(string, board.FlashProgress) error { return ErrNotSupported }
func (b *Bambu) UseConfig(string, string) error               { return ErrNotSupported }
func (b *Bambu) UseSim(string) error                          { return ErrNotSupported }
func (b *Bambu) RestartKlipper() error                        { return ErrNotSupported }
func (b *Bambu) FirmwareRestart() error                       { return ErrNotSupported }
func (b *Bambu) RunAction(string, ...string) error            { return ErrNotSupported }
func (b *Bambu) PrintPATower(int, int, string) error          { return ErrNotSupported }
func (b *Bambu) PrintCalibTowers(int, int, string) error      { return ErrNotSupported }
func (b *Bambu) SetPressureAdvance(float64) error             { return ErrNotSupported }
func (b *Bambu) Heat(int, int)                                {}
func (b *Bambu) SetToolTemp(int) error                        { return ErrNotSupported }
func (b *Bambu) SetBedTemp(int) error                         { return ErrNotSupported }
func (b *Bambu) Extrude(float64, int) error                   { return ErrNotSupported }
func (b *Bambu) Fan(int) error                                { return ErrNotSupported }
func (b *Bambu) SpeedFactor(int) error                        { return ErrNotSupported }
func (b *Bambu) FlowFactor(int) error                         { return ErrNotSupported }
func (b *Bambu) BabyStep(float64) error                       { return ErrNotSupported }
func (b *Bambu) SaveZOffset() (bool, error)                   { return false, ErrNotSupported }
func (b *Bambu) Gcode(...string) error                        { return ErrNotTested }
func (b *Bambu) Home(...string) error                         { return ErrNotTested }
func (b *Bambu) Jog(float64, float64, float64, int) error     { return ErrNotTested }
func (b *Bambu) MotorsOff() error                             { return ErrNotTested }
func (b *Bambu) EmergencyStop() error                         { return ErrNotTested }

// The calibrations of the deck run on a Klipper printer only.

func (b *Bambu) PrintRingTower(int, int, string, bool) error { return ErrNotSupported }
func (b *Bambu) SetMaxAccel(float64) error                   { return ErrNotSupported }
func (b *Bambu) PrintShakeTest(string) error                 { return ErrNotSupported }
func (b *Bambu) PrintRetractTower(int, int, string) error    { return ErrNotSupported }
func (b *Bambu) PrintPadTower(int, int, string, bool) error  { return ErrNotSupported }
func (b *Bambu) PadChecks() ([][3]float64, error)            { return nil, ErrNotSupported }
func (b *Bambu) PrintProbePad(int, int, string) error        { return ErrNotSupported }
func (b *Bambu) FoundProbeOffset() (float64, float64, bool, error) {
	return 0, 0, false, ErrNotSupported
}
func (b *Bambu) HeatMaps() []BedMesh                            { return nil }
func (b *Bambu) HeatSweep(int) error                            { return ErrNotSupported }
func (b *Bambu) DriftTest(int, int) error                       { return ErrNotSupported }
func (b *Bambu) DriftReadings() ([][2]float64, error)           { return nil, ErrNotSupported }
func (b *Bambu) FindProbeOffsetOnPad() error                    { return ErrNotSupported }
func (b *Bambu) SetProbeXYOffset(float64, float64) error        { return ErrNotSupported }
func (b *Bambu) SetRetraction(float64) error                    { return ErrNotSupported }
func (b *Bambu) SetBacklash(float64, float64) error             { return ErrNotSupported }
func (b *Bambu) CubeResult() (float64, float64, float64, error) { return 0, 0, 0, ErrNotSupported }
func (b *Bambu) PrintSweepTower(int, int, string, bool) error   { return ErrNotSupported }
func (b *Bambu) PrintPairTowers(int, int, string, [2]float64, [2]float64) error {
	return ErrNotSupported
}
func (b *Bambu) SetInputShaper(float64, float64) error { return ErrNotSupported }
