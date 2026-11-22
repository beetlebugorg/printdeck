package printer

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"printdeck/internal/bambu"
	"printdeck/internal/board"
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
	// SavePin keeps the SHA-256 of the last certificate of the printer (Setting.Pin). It can be
	// nil.
	SavePin func(pin string)
	// Spools are the rolls of the printer, or nil. TrayPath keeps the roll of each tray of the AMS
	// ("" keeps them in memory).
	Spools   *Spools
	TrayPath string
	// HMS keeps the texts of the messages of the printer, or is nil for the texts of the program.
	HMS *bambu.HMSFile
	// HeatLog records the heat-ups and the cool-downs from the reports, or is nil.
	HeatLog *HeatLog
	// Store is the folder of the 3MF files of the printer, or nil.
	Store Store
	// OnTrayQuestion runs when a tray has a new spool with no RFID tag: the screens ask which roll
	// it is (TrayQuestion). It can be nil.
	OnTrayQuestion func()

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
	cur         bambuPrint
	change      changeWatch    // the filament change now (bambuchange.go)
	counting    bool           // countUse runs for the print that ended: the rolls keep their live grams
	useWG       sync.WaitGroup // the loadUse of the prints, for the tests
	savedPin    string         // the pin of printer.json
	homed       string         // the axes that the deck homed, or "xyz" during a print
	console     []ConsoleLine  // the lines of the Console page

	sent   bambuSent // the last file that the deck started, under mu
	notify func()    // the notify of Run, for the progress of an upload

	filesMu  sync.Mutex
	files3MF map[string]bambuFile // the 3MF files that the deck read, by path

	trayWG   sync.WaitGroup // the handling of the spool changes (spoolsChanged), for the tests
	trayMu   sync.Mutex
	trays    map[int]string    // the roll of each tray, by slot (trayLinks)
	pending  []int             // the trays with a new spool that the screens ask about
	trayPrev map[int]traySpool // the spools of the last report, under mu
	// VideoDir is the folder of the time lapse videos that the deck copies from the card of the
	// printer (bambulapse.go), or "" for none.
	VideoDir  string
	lapsePath string    // the file of the last time lapse that the printer reported
	endedPath string    // the print that ended last, for its time lapse
	endedAt   time.Time // its end
	startForm int       // the index in bambu.StartURLs of the address of the last start that worked
	viewLayer int       // the layer of the slider of the view of the print, or -1 to follow the print
	prepNow   int       // the step now of the list of the start steps, for a step that is not in the list

	liveViews string // the camera fields that the log showed last (logLiveViews)
}

// bambuRetry is the wait before a new connection after a lost link.
var bambuRetry = 10 * time.Second

// bambuCancelled is the print_error of a print that the user stopped.
const bambuCancelled = 0x0300400C

// NewBambu returns the backend of the printer at addr with the serial number and the access code.
// pin is the SHA-256 of the certificate of the printer, or "" to learn it.
func NewBambu(addr, serial, code, pin, name string, hist *History) *Bambu {
	return &Bambu{Client: &bambu.Client{Addr: addr, Serial: serial, Code: code, Pin: pin}, Hist: hist, Name: name, savedPin: pin,
		st: State{Text: "Connecting", Activity: "Connecting to the printer"}}
}

// Run follows the printer until ctx ends. notify runs after each change of the state.
func (b *Bambu) Run(ctx context.Context, notify func()) {
	b.notify = notify
	if b.HMS != nil {
		go func() {
			hctx, cancel := context.WithTimeout(ctx, time.Minute)
			defer cancel()
			if err := b.HMS.Refresh(hctx, b.Client.Serial); err != nil {
				log.Printf("bambu: the texts of the printer messages: %v", err)
			}
		}()
	}
	for {
		err := b.Client.Run(ctx, func(r bambu.Report) {
			b.update(r, time.Now())
			b.checkTraySpools()
			b.logLiveViews()
			b.refreshLive()
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
	st.ErrDetail = "" // the messages of the printer are from before: the deck has no link now
	st.ErrFinal = true
	switch {
	case errors.Is(err, bambu.ErrRefused):
		st.Err = "The printer refused the access code"
	case errors.Is(err, bambu.ErrCert):
		st.Err = "The printer at " + b.Client.Addr + " is not the Bambu Lab printer with this serial number"
	default:
		st.Err, st.ErrFinal = "No answer from the printer at "+b.Client.Addr, false
	}
	log.Printf("bambu: %s: %v", b.Client.Addr, err)
}

// update sets the state from the report r at now, and adds a print to the history at its end.
func (b *Bambu) update(r bambu.Report, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// The last certificate of the printer, for printer.json: Check takes a new certificate of the
	// same printer, for example after a firmware update.
	if pin := b.Client.Seen(); pin != "" && pin != b.savedPin && b.SavePin != nil {
		b.savedPin = pin
		go b.SavePin(pin)
	}
	was := b.last
	b.last, b.has = r, true
	b.camera = b.Client.CameraURL(r)
	b.watchTrays(r)
	defer b.watchLapse(r, now) // after the end of a print in this report
	st := &b.st
	st.Reachable, st.Connected, st.Err, st.Starting, st.ErrFinal = true, true, "", false, false
	st.Tool, st.ToolTarget = float64(r.Nozzle), float64(r.NozzleTarget)
	st.HasTool2, st.Tool2, st.Tool2Target, st.ToolNow = false, 0, 0, 0
	if ex := r.Extruders(); len(ex) >= 2 {
		st.HasTool2 = true
		for _, e := range ex {
			if e.ID == mainExtruder {
				st.Tool, st.ToolTarget = e.Temp, e.Target
			} else {
				st.Tool2, st.Tool2Target = e.Temp, e.Target
			}
		}
		if id := r.ActiveExtruder(); id >= 0 && id != mainExtruder {
			st.ToolNow = 1
		}
	}
	st.Bed, st.BedTarget = float64(r.Bed), float64(r.BedTarget)
	st.Fan, st.HasFan = r.FanPercent(), true
	st.HasChamber = r.HasChamber()
	st.Chamber, st.ChamberTarget = r.ChamberHeat()
	b.HeatLog.Observe(*st, now)
	st.SpeedPct = r.SpeedPct.Int()
	st.Message, st.ErrDetail = b.hmsLines(r.HMS)
	st.NoPosition = true
	if v, ok := b.volume(); ok {
		st.Axis = [2][3]float64{{0, 0, 0}, v}
	}
	if r.Busy() {
		b.homed = "xyz" // a print homes each axis at its start
	}
	st.Homed = b.homed

	if r.Busy() {
		// A new print: no print before, or another task.
		if b.path == "" || r.TaskID != was.TaskID && r.TaskID != "" {
			b.started = now.Add(-r.Elapsed())
			b.path = "bambu-" + strconv.FormatInt(now.Unix(), 10)
			// A print of a file that the deck sent: the file in the store of the deck, so the
			// screens show its model and its picture.
			if b.sent.path != "" && (path.Base(r.File) == b.sent.name || r.Subtask+".gcode.3mf" == b.sent.name) {
				b.path = b.sent.path
			}
			b.name, b.system = r.Name(), r.PrintType == "system"
			b.cur = bambuPrint{}
			b.change = changeWatch{}
			b.viewLayer = -1
			if b.nextProject != "" && b.Hist != nil {
				b.Hist.SetNextProject(b.path, b.nextProject)
				b.nextProject = ""
			}
		}
		b.track(r)
		b.watchChange(r, now)
		st.Printing, st.Paused = true, r.Paused()
		st.Preparing = r.Preparing()
		st.Prep, st.PrepN, b.prepNow = bambuPrep(r, b.prepNow)
		st.FilePath, st.File, st.Title = b.path, b.name, b.name
		st.Completion = float64(r.Percent)
		st.TimeLeft, st.HasTimeLeft = r.TimeLeft(), r.Remaining > 0
		st.Layer, st.Layers = r.Layer.Int(), r.Layers.Int()
		if !st.HasTimeLeft {
			// The printer reports no time before the first layer (and with a file that has no
			// M73 R): the time of the slicer of a file that the deck sent, less its done layers.
			if total, ok := b.predicted(b.path); ok {
				done := 0.0
				if st.Layers > 0 {
					done = float64(max(0, st.Layer-1)) / float64(st.Layers)
				}
				st.TimeLeft, st.HasTimeLeft = time.Duration(float64(total)*(1-done)).Round(time.Minute), true
				if st.Completion == 0 && st.Layer > 1 {
					st.Completion = done * 100
				}
			}
		}
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
		if st.Preparing && st.Activity == st.Text {
			st.Activity = "Preparing"
		}
		// A step of the printer during the print, for example a change of the filament: the
		// panel shows it, after the messages of the printer.
		if n := r.Stage.Int(); !st.Preparing && n > 0 && st.Message == "" && r.StageText() != "" {
			st.Message = r.StageText()
		}
		st.ReleaseLeft, st.HasReleaseLeft = 0, false
		if c, ok := b.HeatLog.Report().CoolOf(HeatBed); ok && st.HasTimeLeft {
			st.ReleaseLeft, st.HasReleaseLeft = st.TimeLeft+time.Duration(c.Median*float64(time.Second)), true
		}
		st.Finished = ""
		return
	}
	st.ReleaseLeft, st.HasReleaseLeft = 0, false
	b.change = changeWatch{} // a print that ends during a change gives no sample
	if b.path != "" {
		b.HeatLog.PrintEnded(now)
		result := "complete"
		switch {
		case r.State == "FAILED" && int(r.Error) == bambuCancelled:
			result = "cancelled"
		case r.State == "FAILED":
			result = "error"
			st.FinishedError = fmt.Sprintf("Error %08X", int(r.Error))
			if int(r.Error) == 0 {
				// The printer stopped the print with no error code: its first message, or a
				// text that says so.
				st.FinishedError = "The printer gave no reason"
				if first, all := b.hmsLines(r.HMS); first != "" {
					st.FinishedError = first
				} else if all != "" {
					st.FinishedError = strings.SplitN(all, "\n", 2)[0]
				}
			} else if text := b.hms().ErrorText(int64(r.Error)); text != "" {
				st.FinishedError = text + " (" + fmt.Sprintf("%08X", int(r.Error)) + ")"
			}
		case r.State != "FINISH":
			result = "cancelled" // the printer went to IDLE with no end
		}
		st.Finished, st.FinishedPath, st.FinishedFile, st.FinishedTitle = result, b.path, b.name, b.name
		st.FinishedTime = now.Sub(b.started)
		// A job of the printer itself (a calibration) is no print of the user.
		if b.Hist != nil && !b.system {
			b.Hist.add(*st, now, nil, 0, 0, nil)
		}
		if !b.system {
			b.counting = true
			go b.countUse(b.cur, result == "complete")
			b.endedPath, b.endedAt = b.path, now
		}
		b.path = ""
	}
	st.Printing, st.Paused, st.Preparing = false, false, false
	st.FilePath, st.File, st.Title = "", "", ""
	st.Completion, st.Elapsed, st.PrintDuration, st.HasTimeLeft = 0, 0, 0, false
	st.Layer, st.Layers = 0, 0
	st.Text, st.Activity = "Ready", "Ready"
}

// hmsLines returns the messages of the printer (its HMS codes) for the panel: the first message,
// with the count of the others, and each message with its code for the detail.
func (b *Bambu) hmsLines(h []bambu.HMS) (first, all string) {
	if len(h) == 0 {
		return "", ""
	}
	var lines []string
	shown := 0
	for _, e := range h {
		if b.hms().Silent(e) {
			// Bambu Lab gives no text for the code: the list shows it, the panel does not.
			lines = append(lines, "No text from Bambu Lab ("+e.String()+")")
			continue
		}
		text, _ := b.hms().Text(e)
		lines = append(lines, text+" ("+e.String()+")")
		if shown == 0 {
			first = text
		}
		shown++
	}
	if shown > 1 {
		first += fmt.Sprintf(" · %d more", shown-1)
	}
	return first, strings.Join(lines, "\n")
}

// hms returns the texts of the codes of the printer, or nil for the texts of the program.
func (b *Bambu) hms() *bambu.HMSTexts {
	if b.HMS == nil {
		return nil
	}
	return b.HMS.Texts()
}

// HMSMessages returns each message of the printer with its code, for the screens.
func (b *Bambu) HMSMessages() []string {
	r, _ := b.Report()
	_, all := b.hmsLines(r.HMS)
	if all == "" {
		return nil
	}
	return strings.Split(all, "\n")
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

// HeatTimes returns the heat-ups and the cool-downs of the printer.
func (b *Bambu) HeatTimes() HeatReport { return b.HeatLog.Report() }

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

// SpoolStore returns the rolls of the printer, or nil.
func (b *Bambu) SpoolStore() *Spools { return b.Spools }

// predicted returns the print time that the slicer predicts for the first plate of the 3MF p of
// the store, or false for a file that the deck does not have (a print of the printer).
func (b *Bambu) predicted(p string) (time.Duration, bool) {
	if b.Store == nil || p == "" || strings.HasPrefix(p, "bambu-") {
		return 0, false
	}
	f, err := b.threeMF(p)
	if err != nil || len(f.plates) == 0 || f.plates[0].Seconds <= 0 {
		return 0, false
	}
	return time.Duration(f.plates[0].Seconds * float64(time.Second)), true
}

// bambuPrep returns the steps of the start of the print of r (stg), with the step now (stg_cur),
// for the screens: at most len(State.Prep) steps, with the step now in them. A step that the deck
// does not know is out of the list.
func bambuPrep(r bambu.Report, prev int) (out [8]PrepStep, n, now int) {
	if !r.Preparing() {
		return out, 0, -1
	}
	cur := r.Stage.Int()
	var steps []PrepStep
	now = -1
	for _, v := range r.Stages {
		id := v.Int()
		name, kind, ok := bambu.StepName(id)
		if !ok {
			continue
		}
		if id == cur {
			now = len(steps)
		}
		steps = append(steps, PrepStep{Kind: kind, Name: name})
	}
	if len(steps) == 0 {
		return out, 0, -1
	}
	switch {
	case now >= 0:
	case prev >= 0 && prev < len(steps):
		// A step that is not in the list (a wait between two steps): the list stays, with the
		// step before as the step now.
		now = prev
	default:
		now = 0
	}
	for i := range steps {
		steps[i].Done, steps[i].Now = i < now, i == now
	}
	// A window of the steps with the step now in it, and one done step before it.
	start := max(0, min(now-1, len(steps)-len(out)))
	n = copy(out[:], steps[start:])
	return out, n, now
}

// Pause, Resume and Cancel send the commands of the printer. An error shows in the panel.
func (b *Bambu) Pause()  { b.command("Pause", b.Client.Pause) }
func (b *Bambu) Resume() { b.command("Resume", b.Client.Resume) }
func (b *Bambu) Cancel() { b.command("Stop", b.Client.Stop) }

// command runs the command f, logs it, and shows an error in the panel.
func (b *Bambu) command(name string, f func() error) error {
	err := f()
	log.Printf("bambu: %s: %v", name, err)
	if err != nil {
		b.mu.Lock()
		b.st.Message = name + " did not go to the printer: " + err.Error()
		b.mu.Unlock()
	}
	return err
}

// The files, the pictures and the layers: the deck reads no file of the printer yet.

func (b *Bambu) LayerTimes() []float64 { return nil }

// Preview returns the picture of the plate of the print: the picture in the 3MF of a print that
// the deck sent, or nil.
func (b *Bambu) Preview() image.Image {
	st := b.State()
	if !st.Busy() || b.Store == nil || st.FilePath == "" {
		return nil
	}
	return b.FilePicture(File{Path: st.FilePath}, "")
}

// SetViewLayer shows the layer n (from 1) in the view of the print; -1 follows the print.
func (b *Bambu) SetViewLayer(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.viewLayer = n
}

// ViewLayer returns the layer of the view of the print: the layer of the slider, or the layer
// that the printer prints. The printer reports no position in the file, so the view follows the
// layer.
func (b *Bambu) ViewLayer() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.viewLayer > 0 {
		return b.viewLayer
	}
	if b.st.Layer > 0 {
		return b.st.Layer
	}
	return -1
}
func (b *Bambu) Connect()                                     {}
func (b *Bambu) RecordPicture(Record) (image.Image, bool)     { return nil, false }
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
func (b *Bambu) FlowFactor(int) error                         { return ErrNotSupported }
func (b *Bambu) BabyStep(float64) error                       { return ErrNotSupported }
func (b *Bambu) SaveZOffset() (bool, error)                   { return false, ErrNotSupported }

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

// NozzleInfo is a nozzle of a printer for the slicers: its ID in the reports, its role ("main"
// with the AMS, "aux" with the external spool), its diameter in mm and its type code as the
// printer reports it (for example "HS01").
type NozzleInfo struct {
	ID       int
	Role     string
	Diameter float64
	Type     string
}

// Nozzles returns the nozzles of the last report, or none.
func (b *Bambu) Nozzles() []NozzleInfo {
	r, ok := b.Report()
	if !ok {
		return nil
	}
	var out []NozzleInfo
	two := len(r.Device.Nozzle.Info) > 1
	for _, n := range r.Device.Nozzle.Info {
		role := "main"
		if two && n.ID != mainExtruder {
			role = "aux"
		}
		out = append(out, NozzleInfo{ID: n.ID, Role: role, Diameter: n.Diameter, Type: n.Type})
	}
	return out
}

// PlateID returns the code of the build plate that the printer found on the heatbed (for example
// "P0101"), or "" when it found none.
func (b *Bambu) PlateID() string {
	r, _ := b.Report()
	return r.Device.Plate.CurID
}
