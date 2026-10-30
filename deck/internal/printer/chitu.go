package printer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"log"
	"math"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"printdeck/internal/board"
	"printdeck/internal/chitu"
	"printdeck/internal/gcodemeta"
	"printdeck/internal/timelapse"

	xdraw "golang.org/x/image/draw"
)

// ChiTu is the backend of a ChiTu resin printer on the network (the Peopoly Phenom): the deck reads
// the state of the board with the queries of package chitu, and keeps the prints in its history.
// The board keeps its own firmware and its own screen.
//
// The start of a print (PrintFile) sends M6030. The other controls (pause, resume, stop, upload)
// send no command until their test on a small print. The controls of a FDM printer and of Klipper
// return ErrNotSupported.
type ChiTu struct {
	Client *chitu.Client
	Hist   *History
	Name   string // the name of the printer, for the screens
	// PrintStore keeps the print now (resin-print.json), so a deck that starts again during a print
	// goes on with the same print: the same key in the history, the same project and live tile.
	// "" keeps nothing.
	PrintStore string
	// UploadDir keeps a copy of the files that ChiTuBox sent to the board through the relay of the
	// deck (SaveUpload), for their previews. "" keeps no copy.
	UploadDir string

	mu          sync.Mutex
	info        chitu.Info   // the answer to M99999, once
	files       []chitu.File // the sliced files on the card of the board (M20), by LoadFiles
	listed      bool         // the deck read the files once
	st          State
	last        chitu.Status // the last answer to M4000
	lastAt      time.Time    // the time of last
	moved       bool         // the message says that the plate moves
	misses      int          // the queries with no answer since the last answer
	path        string       // the key of the print now in the history ("" with no print)
	started     time.Time
	console     []ConsoleLine
	hold        bool                     // a ChiTuBox session talks to the board: no queries of the deck
	sending     string                   // the file that the deck sends to the board now, or ""
	waiting     []string                 // the files that wait for the end of that upload
	incoming    string                   // the last upload from ChiTuBox, before SaveUpload takes it
	startAfter  string                   // the file to print when its upload ends, or ""
	stopSend    context.CancelFunc       // stops the upload now (CancelSend)
	nextProject string                   // the project of the next print (SetNextProject)
	relisted    bool                     // the deck read the list of the card again for the print now
	layerKey    string                   // the file and the layer of layerImg
	layerImg    image.Image              // the last picture of LayerPicture
	sendMu      sync.Mutex               // held for each upload to the board, so the uploads go one at a time
	pics        map[string]image.Image   // the previews of the copies in UploadDir, by name
	headers     map[string]*chitu.Sliced // the headers of the copies in UploadDir, by name
	models      map[string]*chitu.Model  // the 3D models of the copies, by name; nil while the deck reads one
	modelPics   map[string]image.Image   // the pictures of the models, by file, for the screens
	lapse       chituLapse               // the time lapse of the print now
	plans       map[string]filePlan      // the plans of the layers of the copies, by name
	moves       float64                  // the seconds of the moves of a layer, as the print now measures them
	movesFile   string                   // the file of moves
}

// chituHoldEvery is the time between two looks at the board while an upload waits for the end of
// a print.
var chituHoldEvery = 30 * time.Second

// chituUploadsKept is the count of the copies in UploadDir.
const chituUploadsKept = 8

// ErrNotSupported is a control that this printer does not have.
var ErrNotSupported = errors.New("this printer does not have this control")

// ErrNotTested is a control of a ChiTu board that has no test on a print yet.
var ErrNotTested = errors.New("this control of the printer is not tested yet")

// The times between two queries of the state: during a print, with no print, and with no answer.
const (
	chituPrintingEvery = 5 * time.Second
	chituIdleEvery     = 10 * time.Second
	chituOfflineEvery  = 10 * time.Second
	chituMisses        = 3 // the queries with no answer before the printer is offline
)

// chituDone is the share of the file that a print with a lost end needs to count as complete. The
// board can clear its state before the deck sees the last bytes (not checked on a Phenom).
const chituDone = 0.99

// NewChiTu returns the backend of the board at addr ("192.168.76.58" or "host:port").
func NewChiTu(addr, name string, hist *History) *ChiTu {
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr += ":" + strconv.Itoa(chitu.Port)
	}
	return &ChiTu{Client: &chitu.Client{Addr: addr}, Hist: hist, Name: name,
		st: State{Text: "Connecting", Activity: "Connecting to the printer"}}
}

// Run reads the state of the board until ctx ends. notify runs after each change of the state.
func (c *ChiTu) Run(ctx context.Context, notify func()) {
	for {
		wait := c.poll(ctx, time.Now())
		if notify != nil {
			notify()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// poll sends one M4000 and updates the state. It returns the time to the next query. It asks for
// the board (M99999) until the board answers once.
func (c *ChiTu) poll(ctx context.Context, now time.Time) time.Duration {
	c.mu.Lock()
	known, hold := c.info.Name != "", c.hold || c.sending != ""
	c.mu.Unlock()
	// A query during an upload of ChiTuBox can break the upload.
	if hold {
		return chituPrintingEvery
	}
	if !known {
		qctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		if in, err := c.Client.Info(qctx); err == nil {
			c.mu.Lock()
			c.info = in
			c.mu.Unlock()
		}
		cancel()
	}
	qctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	s, err := c.Client.Status(qctx)
	cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.misses++
		if c.misses >= chituMisses {
			c.st.Reachable, c.st.Connected = false, false
			c.st.Text, c.st.Activity = "Offline", "The printer does not answer"
			c.st.Err = "No answer from the printer at " + c.Client.Addr
		}
		return chituOfflineEvery
	}
	c.misses = 0
	c.update(s, now)
	if !c.listed && !s.Printing() {
		c.listed = true
		go c.LoadFiles()
	}
	if s.Printing() {
		if c.lapse.enable {
			return time.Second // the time lapse catches the plate when it is up
		}
		return chituPrintingEvery
	}
	return chituIdleEvery
}

// update sets the state from the answer s at now, and adds a print to the history at its end. The
// caller holds mu.
func (c *ChiTu) update(s chitu.Status, now time.Time) {
	was := c.last
	c.last = s
	defer func() { c.lastAt = now }()
	st := &c.st
	st.Reachable, st.Connected, st.Err = true, true, ""
	st.Z, st.HasZ = s.Z, true
	if s.Printing() {
		if c.path == "" || s.Read < was.Read && was.Printing() {
			// A new print: the board does not give the name of its file in M4000.
			c.started = now.Add(-time.Duration(s.Seconds) * time.Second)
			c.path = "resin-" + strconv.FormatInt(c.started.Unix(), 10)
			if was.Size == 0 && c.lastAt.IsZero() {
				// The first answer since the deck started: maybe a print from before the start.
				c.resume(s)
			}
			c.savePrint(s)
			c.relisted = false
			if c.nextProject != "" && c.Hist != nil {
				c.Hist.SetNextProject(c.path, c.nextProject)
				c.nextProject = ""
			}
		}
		st.Printing, st.Paused = true, s.Flag == 1
		st.FilePath, st.File, st.Title = c.path, c.printFile(), c.printName()
		if st.File == c.path && !c.relisted {
			// No file of the list has the size of the print: the list is old, for example from
			// before a change of the drive. The next answer has the new list.
			c.relisted = true
			go c.LoadFiles()
		}
		st.FileSize, st.FilePos = s.Size, s.Read
		st.Completion = s.Progress() * 100
		// With no plan of the layers: the time left from the share of the file that the board read.
		st.TimeLeft, st.HasTimeLeft = 0, false
		if p := s.Progress(); p > 0.02 && s.Seconds > 0 {
			st.TimeLeft, st.HasTimeLeft = time.Duration(float64(s.Seconds)*(1-p)/p)*time.Second, true
		}
		// The layers from the header of the copy of the file: the board reads the layers in order,
		// so the share of the file that it read is about the share of the layers.
		st.Layer, st.Layers = 0, 0
		if h, ok := c.headers[filepath.Base(st.File)]; ok {
			if h != nil && h.Layers > 0 {
				st.Layers = int(h.Layers)
				st.Layer = max(1, min(st.Layers, int(math.Round(s.Progress()*float64(h.Layers)))))
				// The time left from the plan of the layers: the light of each layer from the file,
				// and the moves of the plate for each layer as this print measures them. The
				// print time of the slicer is often far off, and the share of the file is a poor
				// clock while the long bottom layers print.
				if pl, ok := c.plans[filepath.Base(st.File)]; ok && len(pl.light) == st.Layers {
					moves := pl.moves
					done := st.Layer - 1
					if done >= 3 && s.Seconds > 0 {
						lit := 0.0
						for _, t := range pl.light[:done] {
							lit += t
						}
						if m := (float64(s.Seconds) - lit) / float64(done); m > 0 && m < 600 {
							moves = m
						}
					}
					c.moves, c.movesFile = moves, filepath.Base(st.File)
					left := 0.0
					for _, t := range pl.light[done:] {
						left += t + moves
					}
					// The part of the layer now that already went: the time since the start of
					// the layer, from the clock of the board.
					if spent := float64(s.Seconds) - planUntil(pl, done, moves); spent > 0 && spent < pl.light[done]+moves {
						left -= spent
					}
					st.TimeLeft, st.HasTimeLeft = time.Duration(left*float64(time.Second)), true
				}
			}
		} else if st.File != c.path {
			go c.header(st.File) // the next answer has the layers
		}
		st.Elapsed = time.Duration(s.Seconds) * time.Second
		st.PrintDuration = st.Elapsed // the board gives no time of its pauses

		st.Text, st.Activity = "Printing", "Printing"
		if st.Paused {
			st.Text, st.Activity = "Paused", "Paused"
		}
		st.Finished = ""
		return
	}
	if c.path != "" {
		// The end of the print, from the answer that still shows the file: this answer when the
		// board read the whole file, else the answer before it. The print is complete when the
		// board read the file, or most of it.
		end := was
		if s.Size > 0 {
			end = s
		}
		result := "cancelled"
		if end.Size > 0 && (end.Read >= end.Size || end.Progress() >= chituDone) {
			result = "complete"
		}
		st.Finished, st.FinishedPath, st.FinishedFile = result, c.path, c.printFile()
		st.FinishedTitle = c.printName()
		st.FinishedTime = time.Duration(end.Seconds) * time.Second
		if c.Hist != nil {
			c.Hist.add(*st, now, nil, 0, end.Read, nil)
		}
		c.path = ""
		if c.PrintStore != "" {
			os.Remove(c.PrintStore)
		}
	}
	st.Printing, st.Paused = false, false
	st.FilePath, st.File, st.Title, st.FileSize, st.FilePos = "", "", "", 0, 0
	st.Completion, st.Elapsed, st.PrintDuration, st.HasTimeLeft = 0, 0, 0, false
	st.Text, st.Activity = "Ready", "Ready"
	// The board reports no print, but the plate moves: a start on the screen of the printer, or a
	// move on that screen. The panel says so, so the deck never looks idle while the plate moves.
	moving := was.Z != s.Z && c.lastAt.After(now.Add(-time.Minute))
	switch {
	case moving:
		st.Message, c.moved = fmt.Sprintf("The plate moves · Z %.1f", s.Z), true
	case c.moved:
		st.Message, c.moved = "", false
	}
}

// printName is the name of the print now for the screens: the name of the file on the card with
// the size of the file of the print, when one file has that size, else the name of the printer and
// the start. M4000 has no name of the file. The caller holds mu.
func (c *ChiTu) printName() string {
	if f := c.printFile(); f != c.path {
		return strings.TrimSuffix(strings.TrimSuffix(f, ".cbddlp"), ".ctb")
	}
	return fmt.Sprintf("%s print of %s", c.Name, c.started.Local().Format("Jan 2, 15:04"))
}

// printFile returns the name of the file of the print now on the card, from its size, or the key
// of the print when no file or two files have that size. The caller holds mu.
func (c *ChiTu) printFile() string {
	name := ""
	for _, f := range c.files {
		if f.Size == c.last.Size {
			if name != "" {
				name = ""
				break // two files of the same size
			}
			name = f.Name
		}
	}
	if name != "" {
		return name
	}
	return c.path
}

// LoadFiles reads the files on the card of the board (M20). It waits for the board.
func (c *ChiTu) LoadFiles() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	files, err := c.Client.Files(ctx)
	if err != nil {
		return
	}
	var keep []chitu.File
	for _, f := range files {
		if f.Printable() {
			keep = append(keep, f)
		}
	}
	c.mu.Lock()
	c.files = keep
	c.mu.Unlock()
}

// Files returns the sliced files on the card of the board, from the last LoadFiles. The board
// gives no date of a file.
func (c *ChiTu) Files() []File {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]File, 0, len(c.files))
	for _, f := range c.files {
		name := f.Name
		if have, ok := c.copySize(f.Name); ok && have != f.Size {
			name = fmt.Sprintf("INCOMPLETE %s (%s of %s)", f.Name, mbText(f.Size), mbText(have))
		}
		out = append(out, File{Path: f.Name, Name: name})
	}
	return out
}

// copySize returns the size of the copy of the file name in UploadDir. ok is false with no copy.
func (c *ChiTu) copySize(name string) (int64, bool) {
	if c.UploadDir == "" {
		return 0, false
	}
	fi, err := os.Stat(filepath.Join(c.UploadDir, filepath.Base(name)))
	if err != nil || !fi.Mode().IsRegular() {
		return 0, false
	}
	return fi.Size(), true
}

// mbText is a size in MB, or in KB under 1 MB.
func mbText(n int64) string {
	if n < 1e6 {
		return fmt.Sprintf("%d KB", n/1000)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/1e6)
}

// incomplete reports that the card has the file name with another size than the copy of the deck.
// The caller holds mu.
func (c *ChiTu) incomplete(name string) bool {
	for _, f := range c.files {
		if f.Name == name {
			have, ok := c.copySize(name)
			return ok && have != f.Size
		}
	}
	return false
}

// State returns the state of the printer.
func (c *ChiTu) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.st
}

// Machine returns what the printer has: a resin printer, and its board when it answered.
func (c *ChiTu) Machine() Machine {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := Machine{Process: ProcessResin}
	if c.info.Name != "" {
		m.Board = strings.TrimSpace(c.info.Name + " " + c.info.Version)
	}
	return m
}

// History and projects, as the Klipper backend.

func (c *ChiTu) History() []Record {
	if c.Hist == nil {
		return nil
	}
	return c.Hist.Records()
}

func (c *ChiTu) Rates() Rates {
	if c.Hist == nil {
		return ComputeRates(nil)
	}
	return c.Hist.Rates()
}

func (c *ChiTu) SetNote(r Record, note string) bool {
	return c.Hist != nil && c.Hist.SetNote(r.Path, r.End, note)
}

func (c *ChiTu) SetProject(r Record, project string) bool {
	return c.Hist != nil && c.Hist.SetProject(r.Path, r.End, project)
}

// SetNextProject sets the project of the print now, or of the next print when no print runs.
// The next print gets it at its start (update).
func (c *ChiTu) SetNextProject(project string) bool {
	st := c.State()
	if c.Hist == nil {
		return false
	}
	if !st.Printing || st.FilePath == "" {
		c.mu.Lock()
		c.nextProject = project
		c.mu.Unlock()
		return true
	}
	c.Hist.SetNextProject(st.FilePath, project)
	return true
}

// NextProject returns the project of the print now, or of the next print.
func (c *ChiTu) NextProject() string {
	st := c.State()
	if c.Hist == nil {
		return ""
	}
	if !st.Printing {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.nextProject
	}
	return c.Hist.NextProject(st.FilePath)
}

// Console returns the last lines of the console. The deck shows no console for this printer yet.
func (c *ChiTu) Console() []ConsoleLine {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ConsoleLine(nil), c.console...)
}

// The files, the pictures and the layers: the deck reads no sliced file of the printer yet.

func (c *ChiTu) Preview() image.Image                         { return nil }
func (c *ChiTu) SetViewLayer(int)                             {}
func (c *ChiTu) ViewLayer() int                               { return -1 }
func (c *ChiTu) FileMeta(string) gcodemeta.Meta               { return gcodemeta.Meta{} }
func (c *ChiTu) SpoolStore() *Spools                          { return nil }
func (c *ChiTu) Connect()                                     {}
func (c *ChiTu) SendConsole(string)                           {}
func (c *ChiTu) Boards() ([]board.Info, bool)                 { return nil, false }
func (c *ChiTu) ProbeBoards()                                 {}
func (c *ChiTu) FlashBoard(string, board.FlashProgress) error { return ErrNotSupported }
func (c *ChiTu) UseConfig(string, string) error               { return ErrNotSupported }
func (c *ChiTu) UseSim(string) error                          { return ErrNotSupported }
func (c *ChiTu) RestartKlipper() error                        { return ErrNotSupported }
func (c *ChiTu) FirmwareRestart() error                       { return ErrNotSupported }
func (c *ChiTu) RunAction(string, ...string) error            { return ErrNotSupported }
func (c *ChiTu) PrintPATower(int, int, string) error          { return ErrNotSupported }
func (c *ChiTu) PrintCalibTowers(int, int, string) error      { return ErrNotSupported }
func (c *ChiTu) SetPressureAdvance(float64) error             { return ErrNotSupported }
func (c *ChiTu) Heat(int, int)                                {}
func (c *ChiTu) SetToolTemp(int) error                        { return ErrNotSupported }
func (c *ChiTu) SetBedTemp(int) error                         { return ErrNotSupported }
func (c *ChiTu) Extrude(float64, int) error                   { return ErrNotSupported }
func (c *ChiTu) Fan(int) error                                { return ErrNotSupported }
func (c *ChiTu) SpeedFactor(int) error                        { return ErrNotSupported }
func (c *ChiTu) FlowFactor(int) error                         { return ErrNotSupported }
func (c *ChiTu) BabyStep(float64) error                       { return ErrNotSupported }

// The controls of phase 4: no command goes to the board before the test of each control.

func (c *ChiTu) Gcode(...string) error { return ErrNotTested }
func (c *ChiTu) MotorsOff() error      { return ErrNotTested }
func (c *ChiTu) EmergencyStop() error  { return ErrNotTested }

// PrintFile starts the print of the file on the card of the board (M6030). An error shows in the
// panel.
func (c *ChiTu) PrintFile(path string) {
	c.mu.Lock()
	bad := c.incomplete(path)
	c.mu.Unlock()
	if bad {
		// The card has a part of the file: the deck sends its copy again, and starts no print.
		c.showAnswer(path + " is incomplete on the printer. The deck sends it again: print it after that.")
		go c.SendFile(filepath.Join(c.UploadDir, filepath.Base(path)))
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := c.Client.StartPrint(ctx, path)
		log.Printf("chitu: M6030 ':%s' (Print on the deck): %v", path, err)
		c.mu.Lock()
		c.st.Message = ""
		if err != nil {
			c.st.Message = "The print did not start: " + err.Error()
		}
		c.mu.Unlock()
	}()
}

// The controls of the build plate and of the print. The commands are the ones of other programs
// for ChiTu boards; the test on the Phenom of 2026-10-30 checks them. The panel shows the answer
// of the board to the last control (State.Message), for the test.

// chituMoveWait is the wait for the answer to a move or a homing: the board can answer at the end.
const chituMoveWait = 60 * time.Second

// chituMaxDown is the longest move of the plate down in one step, in mm: a plate that goes down
// too far crushes the vat.
const chituMaxDown = 10.0

// send sends the lines to the board, one at a time, and shows the answer to the last line in the
// panel. An answer with "Error" stops.
func (c *ChiTu) send(wait time.Duration, lines ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	cl := &chitu.Client{Addr: c.Client.Addr, Timeout: wait}
	var answer string
	for _, l := range lines {
		a, err := cl.Query(ctx, l)
		if err != nil {
			c.showAnswer(l + ": " + err.Error())
			return err
		}
		answer = l + ": " + a
		if strings.Contains(a, "Error") {
			c.showAnswer(answer)
			return errors.New(a)
		}
	}
	c.showAnswer(answer)
	return nil
}

// showAnswer shows the answer of the board in the panel.
func (c *ChiTu) showAnswer(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.st.Message = s
}

// Home homes the build plate (G28 Z0). A resin printer has only Z.
func (c *ChiTu) Home(...string) error { return c.send(chituMoveWait, "G28 Z0") }

// Jog moves the build plate by z mm, up for a positive z, at speed mm/min. A move down is at most
// chituMaxDown.
func (c *ChiTu) Jog(_, _, z float64, speed int) error {
	if z < -chituMaxDown {
		return fmt.Errorf("the plate moves down %.0f mm at most in one step", chituMaxDown)
	}
	// The Phenom moves Z at 6 mm/s at most (M8013 I6 of its settings): 300 mm/min stays under it.
	speed = min(speed, 300)
	return c.send(chituMoveWait, "G91", fmt.Sprintf("G1 Z%.2f F%d", z, speed), "G90")
}

// Pause pauses the print (M25), Resume resumes it (M24), and Cancel stops it (M33).
func (c *ChiTu) Pause()  { go c.send(5*time.Second, "M25") }
func (c *ChiTu) Resume() { go c.send(5*time.Second, "M24") }
func (c *ChiTu) Cancel() { go c.send(5*time.Second, "M33") }

// RelayInfo returns the board (M99999) for the relay of ChiTuBox. ok is false before the board
// answered.
func (c *ChiTu) RelayInfo() (chitu.Info, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.info, c.info.Name != ""
}

// Hold stops the queries of the deck while a ChiTuBox session talks to the board (true), and
// starts them again (false).
func (c *ChiTu) Hold(on bool) {
	c.mu.Lock()
	c.hold = on
	c.mu.Unlock()
}

// SaveUpload takes the copy at path of a file that ChiTuBox sent to the deck, keeps it in
// UploadDir, and sends it to the card of the board. It keeps the last chituUploadsKept copies.
// The panel shows the progress.
func (c *ChiTu) SaveUpload(name, path string) {
	name = filepath.Base(name)
	if c.UploadDir == "" || name == "." || name == "/" || strings.HasPrefix(name, ".") {
		os.Remove(path)
		return
	}
	if err := os.MkdirAll(c.UploadDir, 0o755); err != nil {
		log.Printf("chitu: %v", err)
		os.Remove(path)
		return
	}
	dst := filepath.Join(c.UploadDir, name)
	if err := os.Rename(path, dst); err != nil {
		log.Printf("chitu: %v", err)
		os.Remove(path)
		return
	}
	c.mu.Lock()
	delete(c.pics, name)
	c.mu.Unlock()
	pruneUploads(c.UploadDir, chituUploadsKept)
	if err := c.SendFile(dst); err != nil {
		log.Printf("chitu: the upload of %s: %v", name, err)
	}
}

// SendFile sends the file at path to the card of the board, one packet at a time
// (chitu.Client.Upload), and checks its size in the list of the card.
func (c *ChiTu) SendFile(path string) error {
	name := filepath.Base(path)
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	// A second upload waits for the first.
	c.mu.Lock()
	if c.incoming == name {
		c.incoming = ""
	}
	c.waiting = append(c.waiting, name)
	c.st.Upload.Waiting = len(c.waiting)
	c.mu.Unlock()
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	c.mu.Lock()
	if i := slices.Index(c.waiting, name); i >= 0 {
		c.waiting = slices.Delete(c.waiting, i, i+1)
	}
	size := fi.Size()
	c.mu.Unlock()
	// The deck sends no file while the board prints: a write to the drive of the board can break
	// the print. A file that the board has already, with the same size, does not go again: the
	// deck keeps its copy for the preview and the layers.
	for {
		c.LoadFiles()
		c.mu.Lock()
		have := false
		for _, x := range c.files {
			have = have || x.Name == name && x.Size == size
		}
		printing := c.last.Printing()
		if have {
			now := time.Now()
			c.st.Upload = Upload{Name: name, Size: size, Done: size, Started: now, Ended: now, Result: "sent", Waiting: len(c.waiting)}
			c.mu.Unlock()
			log.Printf("chitu: the printer has %s already: the deck keeps its copy", name)
			return nil
		}
		if !printing {
			c.mu.Unlock()
			break
		}
		c.st.Upload = Upload{Name: name, Size: size, Started: time.Now(), Waiting: len(c.waiting), Held: true}
		c.mu.Unlock()
		time.Sleep(chituHoldEvery)
	}
	c.mu.Lock()
	c.sending = name
	c.st.Upload = Upload{Name: name, Size: size, Started: time.Now(), Waiting: len(c.waiting), StartAfter: c.startAfter == name}
	c.mu.Unlock()
	var result error
	defer func() {
		c.mu.Lock()
		c.sending = ""
		c.st.Upload.Result, c.st.Upload.Ended, c.st.Upload.Waiting = "sent", time.Now(), len(c.waiting)
		start := c.startAfter == name
		if start {
			c.startAfter = ""
		}
		if result != nil {
			c.st.Upload.Result, c.st.Upload.Err = "failed", result.Error()
			if start {
				c.st.Upload.Err += " · print not started"
			}
		}
		c.mu.Unlock()
		if start && result == nil {
			a := c.startNow(name)
			c.mu.Lock()
			if c.st.Upload.Name == name {
				c.st.Upload.StartAnswer = a
			}
			c.mu.Unlock()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	progress := func(done int64) {
		c.mu.Lock()
		c.st.Upload.Done, c.st.Upload.Waiting = done, len(c.waiting)
		c.mu.Unlock()
	}
	c.mu.Lock()
	c.stopSend = cancel
	c.mu.Unlock()
	// A failed upload goes once more from the start: M28 starts the file again on the card.
	for try := 0; ; try++ {
		err = c.Client.Upload(ctx, name, f, size, progress)
		if err == nil || try == 1 || ctx.Err() != nil {
			break
		}
		log.Printf("chitu: the upload of %s: %v: once more", name, err)
		time.Sleep(5 * time.Second)
	}
	c.mu.Lock()
	c.stopSend = nil
	at := c.st.Upload.Done
	c.mu.Unlock()
	if err != nil && errors.Is(err, context.Canceled) {
		// Stop sending: close the file on the card, and delete the part of it.
		c.send(10*time.Second, "M29")
		c.send(10*time.Second, "M30 "+name)
		result = fmt.Errorf("Stopped at %d%%: the deck deleted the part on the printer", at*100/max(1, size))
		return err
	}
	if err != nil {
		result = uploadError(err, at, size)
		return err
	}
	c.LoadFiles()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, x := range c.files {
		if x.Name == name {
			if x.Size != size {
				result = fmt.Errorf("the printer saved %d of %d bytes", x.Size, size)
				return result
			}
			return nil
		}
	}
	result = errors.New("the printer does not list the file")
	return result
}

// Busy returns the reason that the board can start no print now, or "".
func (c *ChiTu) Busy() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sending != "" || len(c.waiting) > 0 {
		return "The deck sends " + c.sending + " to the printer"
	}
	return ""
}

// pruneUploads removes the oldest files of dir after the first keep.
func pruneUploads(dir string, keep int) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type f struct {
		name string
		mod  time.Time
	}
	var fs []f
	for _, e := range ents {
		if i, err := e.Info(); err == nil && i.Mode().IsRegular() && !strings.HasPrefix(e.Name(), ".") {
			fs = append(fs, f{e.Name(), i.ModTime()})
		}
	}
	sort.Slice(fs, func(i, j int) bool { return fs[i].mod.After(fs[j].mod) })
	for _, x := range fs[min(keep, len(fs)):] {
		os.Remove(filepath.Join(dir, x.name))
	}
}

// picture returns the preview of the copy of the file name in UploadDir, or nil with no copy.
func (c *ChiTu) picture(name string) image.Image {
	if c.UploadDir == "" || name == "" {
		return nil
	}
	name = filepath.Base(name)
	c.mu.Lock()
	img, ok := c.pics[name]
	c.mu.Unlock()
	if ok {
		return img
	}
	data, err := os.ReadFile(filepath.Join(c.UploadDir, name))
	if err == nil {
		img, err = chitu.Preview(data, true)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("chitu: the preview of %s: %v", name, err)
	}
	c.mu.Lock()
	if c.pics == nil {
		c.pics = map[string]image.Image{}
	}
	c.pics[name] = img
	c.mu.Unlock()
	return img
}

// FilePicture returns the 3D model of a file that ChiTuBox sent through the deck, or its preview
// while the deck reads the model, or nil.
func (c *ChiTu) FilePicture(f File, _ string) image.Image {
	if m := c.model(f.Path); m != nil {
		c.mu.Lock()
		img, ok := c.modelPics[f.Path]
		c.mu.Unlock()
		if !ok {
			img = m.Draw(480, 360, 1<<30, resinColor)
			c.mu.Lock()
			if c.modelPics == nil {
				c.modelPics = map[string]image.Image{}
			}
			c.modelPics[f.Path] = img
			c.mu.Unlock()
		}
		return img
	}
	return c.picture(f.Path)
}

// RecordPicture returns the preview of the file of a print, or nil. File is the name of the file
// on the card, or the key of the print ("resin-<start>") when the deck did not know it.
func (c *ChiTu) RecordPicture(r Record) (image.Image, bool) {
	if m := c.model(r.File); m != nil {
		return m.Draw(480, 360, 1<<30, resinColor), false
	}
	return c.picture(r.File), false
}

// header returns the header of the copy of the file name in UploadDir, or nil. The caller holds
// no lock.
func (c *ChiTu) header(name string) *chitu.Sliced {
	if name == "" {
		return nil
	}
	name = filepath.Base(name)
	c.mu.Lock()
	h, ok := c.headers[name]
	c.mu.Unlock()
	if ok || c.UploadDir == "" {
		return h
	}
	var pl filePlan
	f, err := os.Open(filepath.Join(c.UploadDir, name))
	if err == nil {
		buf := make([]byte, 112)
		if _, err = io.ReadFull(f, buf); err == nil {
			h, _ = chitu.ReadSliced(buf)
		}
		if h != nil {
			pl.light, pl.moves, _ = chitu.LayerPlan(f, h)
		}
		f.Close()
	}
	c.mu.Lock()
	if c.headers == nil {
		c.headers, c.plans = map[string]*chitu.Sliced{}, map[string]filePlan{}
	}
	c.headers[name], c.plans[name] = h, pl
	c.mu.Unlock()
	return h
}

// HeaderOf returns the header of the deck's copy of the file name, or nil.
func (c *ChiTu) HeaderOf(name string) *chitu.Sliced { return c.header(name) }

// filePlan is the plan of the layers of a file: the seconds of light of each layer, and the seconds
// of the moves of the plate for each layer from the print time of the header.
type filePlan struct {
	light []float64
	moves float64
}

// times returns the seconds of each layer, with moves seconds of moves for each layer.
func (p filePlan) times(moves float64) []float64 {
	out := make([]float64, len(p.light))
	for i, t := range p.light {
		out[i] = t + moves
	}
	return out
}

// PlanOf returns the seconds of each layer of the file name, from the deck's copy, or nil. The
// moves of a layer are the moves that the print now measured, or the moves of the header.
func (c *ChiTu) PlanOf(name string) []float64 {
	if c.header(name) == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	pl, ok := c.plans[filepath.Base(name)]
	if !ok || len(pl.light) == 0 {
		return nil
	}
	moves := pl.moves
	if c.movesFile == filepath.Base(name) && c.moves > 0 {
		moves = c.moves
	}
	return pl.times(moves)
}

// LayerTimes returns the seconds of each layer of the print now, from the deck's copy of its file,
// or nil.
func (c *ChiTu) LayerTimes() []float64 {
	st := c.State()
	if !st.Printing {
		return nil
	}
	return c.PlanOf(st.File)
}

// DeleteFile deletes the file name from the card of the board (M30 <name>), and checks the list of
// the card after it. It deletes no file during a print or an upload.
func (c *ChiTu) DeleteFile(name string) error {
	if name == "" || strings.ContainsAny(name, "\r\n") {
		return fmt.Errorf("a bad file name %q", name)
	}
	c.mu.Lock()
	busy := c.sending != "" || c.last.Printing()
	c.mu.Unlock()
	if busy {
		return errors.New("the printer is busy: delete the file after the print or the upload")
	}
	if err := c.send(10*time.Second, "M30 "+name); err != nil {
		return err
	}
	c.LoadFiles()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, f := range c.files {
		if f.Name == name {
			return errors.New("the printer still lists " + name)
		}
	}
	return nil
}

// uploadError is the short reason of a failed upload for the status panel: at of size bytes went.
func uploadError(err error, at, size int64) error {
	pct := int64(0)
	if size > 0 {
		pct = at * 100 / size
	}
	if errors.Is(err, chitu.ErrTimeout) {
		return fmt.Errorf("No answer from the printer at %d%%", pct)
	}
	if strings.Contains(err.Error(), "write dat") {
		// The answer of the Phenom when it cannot write to its card. The card of the Phenom of
		// 2026-10-30 had free space, so the cause is not known.
		return fmt.Errorf("The printer could not write to its card at %d%%", pct)
	}
	msg := strings.ReplaceAll(err.Error(), "chitu: ", "")
	return fmt.Errorf("%s (at %d%%)", msg, pct)
}

// Received keeps the progress of an upload from ChiTuBox to the deck, for the status panel.
func (c *ChiTu) Received(name string, n int64, done bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if n == 0 && !done {
		c.st.Receive = Receive{Name: name, Started: now}
	}
	if done && n >= 0 {
		c.incoming = name
	}
	c.st.Receive.Bytes, c.st.Receive.Updated, c.st.Receive.Done = n, now, done
}

// RequestStart is the start of a print that ChiTuBox asks for (M6030 through the relay), and it
// returns the answer for ChiTuBox. A file that the deck still sends to the printer, or that waits
// for it, prints when the printer has the whole file (SendFile). Another file prints now.
func (c *ChiTu) RequestStart(name string) string {
	if name == "" || strings.ContainsAny(name, "'\r\n") {
		return "Error:Bad file name"
	}
	c.mu.Lock()
	if name == c.sending || name == c.incoming || slices.Contains(c.waiting, name) {
		// No start waits for an upload: on 2026-10-30 a start that waited 12 minutes started the
		// Phenom with no build plate in place. The print starts from a new press when the
		// printer has the file.
		c.mu.Unlock()
		log.Printf("chitu: refused a start of %s during its upload", name)
		return "Error:The file is not on the printer yet. Press Print when the deck says the printer has it."
	}
	busy, bad, printing := c.sending != "", c.incomplete(name), c.last.Printing()
	c.mu.Unlock()
	if printing {
		log.Printf("chitu: refused a start of %s during a print", name)
		return "Error:The printer prints now"
	}
	if busy {
		return "Error:The deck sends a file to the printer"
	}
	if bad {
		return "Error:The file on the printer is incomplete"
	}
	return c.startNow(name)
}

// CancelStart cancels the start of a print after its upload.
func (c *ChiTu) CancelStart() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.startAfter != "" {
		log.Printf("chitu: the start of %s is cancelled", c.startAfter)
	}
	c.startAfter = ""
	c.st.Upload.StartAfter = false
}

// startNow sends M6030 for the file name, and returns and logs the answer of the board.
func (c *ChiTu) startNow(name string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	a := "ok"
	if err := c.Client.StartPrint(ctx, name); err != nil {
		a = "Error:" + err.Error()
	}
	log.Printf("chitu: M6030 ':%s' (a press in ChiTuBox): %q", name, a)
	return a
}

// CancelSend stops the upload to the printer now, and cancels its print start.
func (c *ChiTu) CancelSend() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.startAfter = ""
	c.st.Upload.StartAfter = false
	if c.stopSend != nil {
		log.Printf("chitu: the upload of %s is stopped", c.sending)
		c.stopSend()
	}
}

// LayerPicture returns the layer that the printer cures now, from the deck's copy of the file of
// the print (.cbddlp only), about 480 pixels wide, or nil.
func (c *ChiTu) LayerPicture() image.Image {
	st := c.State()
	if !st.Printing || st.Layer <= 0 || c.UploadDir == "" {
		return nil
	}
	name := filepath.Base(st.File)
	key := fmt.Sprintf("%s@%d", name, st.Layer)
	c.mu.Lock()
	if c.layerKey == key {
		img := c.layerImg
		c.mu.Unlock()
		return img
	}
	c.mu.Unlock()
	h := c.header(name)
	if h == nil {
		return nil
	}
	f, err := os.Open(filepath.Join(c.UploadDir, name))
	if err != nil {
		return nil
	}
	defer f.Close()
	img, err := chitu.LayerImage(f, h, st.Layer-1, 480)
	if err != nil {
		log.Printf("chitu: layer %d of %s: %v", st.Layer, name, err)
		img = nil
	}
	c.mu.Lock()
	c.layerKey, c.layerImg = key, nil
	if img != nil {
		c.layerImg = img
	}
	c.mu.Unlock()
	if img == nil {
		return nil
	}
	return img
}

// ResinFile returns the header of the file of the print now, or of the print that ended, from the
// deck's copy, or nil.
func (c *ChiTu) ResinFile() *chitu.Sliced {
	st := c.State()
	name := st.File
	if !st.Printing {
		name = st.FinishedFile
	}
	return c.header(name)
}

// SharePicture returns the picture of the share page: the preview of the slicer at the left, and
// the layer that cures now at the right. A print that ended has the preview only.
func (c *ChiTu) SharePicture(st State, _ Bed) image.Image {
	name := st.File
	if !st.Printing {
		name = st.FinishedFile
	}
	preview := c.picture(name)
	if m := c.model(name); m != nil {
		// The model of the layers, cured up to the layer now: it shows what prints and how far.
		done := st.Layer
		if !st.Printing {
			done = 1 << 30
		}
		preview = m.Draw(480, 300, done, resinColor)
	}
	var layer image.Image
	if st.Printing {
		layer = c.LayerPicture()
	}
	switch {
	case preview == nil && layer == nil:
		return nil
	case layer == nil:
		return preview
	case preview == nil:
		return layer
	}
	// Two cells of 480 x 300, in the colors of the page of the printer: dark around the pictures.
	const cw, ch, gap = 480, 300, 12
	out := image.NewRGBA(image.Rect(0, 0, 2*cw+gap, ch))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.RGBA{27, 28, 26, 255}), image.Point{}, draw.Src)
	put := func(cell image.Rectangle, img image.Image) {
		b := img.Bounds()
		w, h := cell.Dx(), cell.Dx()*b.Dy()/b.Dx()
		if h > cell.Dy() {
			w, h = cell.Dy()*b.Dx()/b.Dy(), cell.Dy()
		}
		x, y := cell.Min.X+(cell.Dx()-w)/2, cell.Min.Y+(cell.Dy()-h)/2
		xdraw.ApproxBiLinear.Scale(out, image.Rect(x, y, x+w, y+h), img, b, draw.Over, nil)
	}
	put(image.Rect(0, 0, cw, ch), preview)
	put(image.Rect(cw+gap, 0, 2*cw+gap, ch), layer)
	return out
}

// savedPrint is resin-print.json: the print now.
type savedPrint struct {
	Path    string    `json:"path"`
	Started time.Time `json:"started"`
	Size    int64     `json:"size"`
}

// resumeSlack is the most difference between the start of a saved print and the start from the
// answer of the board, for the same print.
const resumeSlack = 10 * time.Minute

// resume takes the key and the start of the print from before a start of the deck, when the
// board prints the same file: from resin-print.json, or from the project of the print in the
// history (a deck that saved no resin-print.json). The caller holds mu.
func (c *ChiTu) resume(s chitu.Status) {
	near := func(t time.Time) bool { d := t.Sub(c.started); return d < resumeSlack && d > -resumeSlack }
	if c.PrintStore != "" {
		var sp savedPrint
		if b, err := os.ReadFile(c.PrintStore); err == nil && json.Unmarshal(b, &sp) == nil &&
			sp.Path != "" && sp.Size == s.Size && near(sp.Started) {
			c.path, c.started = sp.Path, sp.Started
			log.Printf("chitu: the deck goes on with the print %s", c.path)
			return
		}
	}
	if c.Hist != nil {
		if p := c.Hist.NextPath(); strings.HasPrefix(p, "resin-") {
			if sec, err := strconv.ParseInt(strings.TrimPrefix(p, "resin-"), 10, 64); err == nil && near(time.Unix(sec, 0)) {
				c.path, c.started = p, time.Unix(sec, 0)
				log.Printf("chitu: the deck goes on with the print %s, from its project", c.path)
			}
		}
	}
}

// savePrint writes resin-print.json for the print now. The caller holds mu.
func (c *ChiTu) savePrint(s chitu.Status) {
	if c.PrintStore == "" {
		return
	}
	b, _ := json.Marshal(savedPrint{Path: c.path, Started: c.started, Size: s.Size})
	tmp := c.PrintStore + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, c.PrintStore)
	}
}

// planUntil returns the seconds of the first n layers of the plan pl, with moves seconds of moves
// for each layer.
func planUntil(pl filePlan, n int, moves float64) float64 {
	t := 0.0
	for _, l := range pl.light[:min(n, len(pl.light))] {
		t += l + moves
	}
	return t
}

// Model returns the brand and the model of the resin printer, for the pages of the cloud: "Peopoly
// Phenom" for a printer with the name Phenom (the setup names it so), else its name.
func (c *ChiTu) Model() string {
	if strings.Contains(strings.ToLower(c.Name), "phenom") && !strings.Contains(strings.ToLower(c.Name), "peopoly") {
		return "Peopoly " + c.Name
	}
	return c.Name
}

// resinColor is the color of the cured part in the pictures of a resin print.
var resinColor = color.RGBA{232, 118, 43, 255}

// model returns the 3D model of the deck's copy of the file name, or nil. The deck reads the model
// once in the background: the first calls return nil.
func (c *ChiTu) model(name string) *chitu.Model {
	if c.UploadDir == "" || name == "" {
		return nil
	}
	name = filepath.Base(name)
	c.mu.Lock()
	m, ok := c.models[name]
	if !ok {
		if c.models == nil {
			c.models = map[string]*chitu.Model{}
		}
		c.models[name] = nil // reading
		go c.readModel(name)
	}
	c.mu.Unlock()
	return m
}

// readModel reads the model of the copy of the file name.
func (c *ChiTu) readModel(name string) {
	h := c.header(name)
	if h == nil {
		return
	}
	f, err := os.Open(filepath.Join(c.UploadDir, name))
	if err != nil {
		return
	}
	defer f.Close()
	start := time.Now()
	m, err := chitu.ReadModel(f, h)
	if err != nil {
		log.Printf("chitu: the model of %s: %v", name, err)
		return
	}
	log.Printf("chitu: the model of %s: %d x %d x %d cells in %v", name, m.W, m.H, m.D, time.Since(start).Round(time.Millisecond))
	c.mu.Lock()
	c.models[name] = m
	c.mu.Unlock()
}

// The time lapse of a resin print (timelapse.Host): the deck asks for a frame at each new layer of
// the print, from the layer that it follows (State.Layer). The board has no module for it, and no
// park: the camera takes the frame as it is.
type chituLapse struct {
	enable       bool
	every        int
	frame, taken int
	layer        int // the layer of the last frame
	path         string
	seenLayer    int     // the layer of lowZ
	lowZ         float64 // the lowest height of the plate in the layer seenLayer
}

// chituLapseLift is the height of the plate over its lowest point in a layer for a frame of the
// time lapse: the plate is out of the vat (the Phenom lifts 8 mm).
const chituLapseLift = 3.0

// TimelapseStatus is the state of the time lapse of the print now.
func (c *ChiTu) TimelapseStatus() (timelapse.HostStatus, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	l := &c.lapse
	st := c.st
	if !st.Reachable {
		return timelapse.HostStatus{}, false
	}
	if st.FilePath != l.path {
		l.path, l.frame, l.taken, l.layer = st.FilePath, 0, 0, 0
	}
	every := max(1, l.every)
	// A frame for each new layer, when the plate is up out of the vat: at least chituLapseLift over
	// the lowest point of the plate in the layer.
	if st.Layer != l.seenLayer {
		l.seenLayer, l.lowZ = st.Layer, st.Z
	}
	l.lowZ = math.Min(l.lowZ, st.Z)
	if l.enable && st.Printing && !st.Paused && st.Layer > 0 && st.Layer >= l.layer+every && st.Z >= l.lowZ+chituLapseLift {
		l.frame++
		l.layer = st.Layer
	}
	return timelapse.HostStatus{Enable: l.enable, Every: every, Frame: l.frame, Taken: l.taken,
		Waiting: l.frame > l.taken, IsLapse: l.frame > l.taken, Layer: l.layer, Present: true}, true
}

// TimelapseTaken marks the frame n as taken.
func (c *ChiTu) TimelapseTaken(n int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lapse.taken = max(c.lapse.taken, n)
	return nil
}

// SetTimelapse keeps the settings of the time lapse. A resin printer has no park.
func (c *ChiTu) SetTimelapse(enable, _ bool, _, every int, _ float64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lapse.enable, c.lapse.every = enable, every
	return nil
}

// SetVideo gives the time lapse video name to the record of the print of path.
func (c *ChiTu) SetVideo(path, name string) {
	if c.Hist != nil {
		c.Hist.SetVideo(path, name, time.Now())
	}
}
