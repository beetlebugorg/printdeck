package printer

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log"
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
	// UploadDir keeps a copy of the files that ChiTuBox sent to the board through the relay of the
	// deck (SaveUpload), for their previews. "" keeps no copy.
	UploadDir string

	mu      sync.Mutex
	info    chitu.Info   // the answer to M99999, once
	files   []chitu.File // the sliced files on the card of the board (M20), by LoadFiles
	listed  bool         // the deck read the files once
	st      State
	last    chitu.Status // the last answer to M4000
	misses  int          // the queries with no answer since the last answer
	path    string       // the key of the print now in the history ("" with no print)
	started time.Time
	console []ConsoleLine
	hold    bool                   // a ChiTuBox session talks to the board: no queries of the deck
	sending string                 // the file that the deck sends to the board now, or ""
	waiting []string               // the files that wait for the end of that upload
	sendMu  sync.Mutex             // held for each upload to the board, so the uploads go one at a time
	pics    map[string]image.Image // the previews of the copies in UploadDir, by name
}

// chituUploadsKept is the count of the copies in UploadDir.
const chituUploadsKept = 8

// ErrNotSupported is a control that this printer does not have.
var ErrNotSupported = errors.New("this printer does not have this control")

// ErrNotTested is a control of a ChiTu board that has no test on a print yet.
var ErrNotTested = errors.New("this control of the printer is not tested yet")

// The times between two queries of the state: during a print, with no print, and with no answer.
const (
	chituPrintingEvery = 5 * time.Second
	chituIdleEvery     = time.Minute
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
		return chituPrintingEvery
	}
	return chituIdleEvery
}

// update sets the state from the answer s at now, and adds a print to the history at its end. The
// caller holds mu.
func (c *ChiTu) update(s chitu.Status, now time.Time) {
	was := c.last
	c.last = s
	st := &c.st
	st.Reachable, st.Connected, st.Err = true, true, ""
	st.Z, st.HasZ = s.Z, true
	if s.Printing() {
		if c.path == "" || s.Read < was.Read && was.Printing() {
			// A new print: the board does not give the name of its file in M4000.
			c.started = now.Add(-time.Duration(s.Seconds) * time.Second)
			c.path = "resin-" + strconv.FormatInt(c.started.Unix(), 10)
		}
		st.Printing, st.Paused = true, s.Flag == 1
		st.FilePath, st.File, st.Title = c.path, c.path, c.printName()
		st.FileSize, st.FilePos = s.Size, s.Read
		st.Completion = s.Progress() * 100
		st.Elapsed = time.Duration(s.Seconds) * time.Second
		st.PrintDuration = st.Elapsed // the board gives no time of its pauses
		st.TimeLeft, st.HasTimeLeft = 0, false
		if p := s.Progress(); p > 0.02 && s.Seconds > 0 {
			st.TimeLeft, st.HasTimeLeft = time.Duration(float64(s.Seconds)*(1-p)/p)*time.Second, true
		}
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
		st.Finished, st.FinishedPath, st.FinishedFile = result, c.path, c.path
		st.FinishedTitle = c.printName()
		st.FinishedTime = time.Duration(end.Seconds) * time.Second
		if c.Hist != nil {
			c.Hist.add(*st, now, nil, 0, end.Read, nil)
		}
		c.path = ""
	}
	st.Printing, st.Paused = false, false
	st.FilePath, st.File, st.Title, st.FileSize, st.FilePos = "", "", "", 0, 0
	st.Completion, st.Elapsed, st.PrintDuration, st.HasTimeLeft = 0, 0, 0, false
	st.Text, st.Activity = "Ready", "Ready"
}

// printName is the name of the print now for the screens: the name of the file on the card with
// the size of the file of the print, when one file has that size, else the name of the printer and
// the start. M4000 has no name of the file. The caller holds mu.
func (c *ChiTu) printName() string {
	name := ""
	for _, f := range c.files {
		if f.Size == c.last.Size {
			if name != "" {
				name = ""
				break // two files of the same size
			}
			name = strings.TrimSuffix(strings.TrimSuffix(f.Name, ".cbddlp"), ".ctb")
		}
	}
	if name != "" {
		return name
	}
	return fmt.Sprintf("%s print of %s", c.Name, c.started.Local().Format("Jan 2, 15:04"))
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
		out = append(out, File{Path: f.Name, Name: f.Name})
	}
	return out
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

func (c *ChiTu) SetNextProject(project string) bool {
	st := c.State()
	if c.Hist == nil || !st.Printing || st.FilePath == "" {
		return false
	}
	c.Hist.SetNextProject(st.FilePath, project)
	return true
}

func (c *ChiTu) NextProject() string {
	st := c.State()
	if c.Hist == nil || !st.Printing {
		return ""
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
func (c *ChiTu) LayerTimes() []float64                        { return nil }
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
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := c.Client.StartPrint(ctx, path)
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
	c.sending = name
	c.st.Upload = Upload{Name: name, Size: size, Started: time.Now(), Waiting: len(c.waiting)}
	c.mu.Unlock()
	var result error
	defer func() {
		c.mu.Lock()
		c.sending = ""
		c.st.Upload.Result, c.st.Upload.Ended, c.st.Upload.Waiting = "sent", time.Now(), len(c.waiting)
		if result != nil {
			c.st.Upload.Result, c.st.Upload.Err = "failed", result.Error()
		}
		c.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	err = c.Client.Upload(ctx, name, f, size, func(done int64) {
		c.mu.Lock()
		c.st.Upload.Done, c.st.Upload.Waiting = done, len(c.waiting)
		c.mu.Unlock()
	})
	if err != nil {
		result = err
		return err
	}
	c.LoadFiles()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, x := range c.files {
		if x.Name == name && x.Size != size {
			result = fmt.Errorf("the printer saved %d of %d bytes", x.Size, size)
			return result
		}
	}
	return nil
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

// FilePicture returns the preview of a file that ChiTuBox sent through the deck, or nil.
func (c *ChiTu) FilePicture(f File, _ string) image.Image { return c.picture(f.Path) }

// RecordPicture returns the preview of the file of a print, or nil.
func (c *ChiTu) RecordPicture(r Record) (image.Image, bool) { return c.picture(r.File), false }

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
