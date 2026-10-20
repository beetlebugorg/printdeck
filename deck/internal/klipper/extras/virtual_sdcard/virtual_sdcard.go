// Package virtual_sdcard prints the G-code files of a host directory, as an sdcard of the
// printer.
//
// Ported to Go from klippy/extras/virtual_sdcard.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2018-2024  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package virtual_sdcard

import (
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/aio_executor"
	"printdeck/internal/klipper/extras/gcode_macro"
	"printdeck/internal/klipper/extras/print_stats"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
)

var validGcodeExts = []string{"gcode", "g", "gco"}

const defaultErrorGcode = `
{% if 'heaters' in printer %}
   TURN_OFF_HEATERS
{% endif %}
`

// File is a file of the sdcard directory.
type File struct {
	Name string
	Size int64
}

// sdFile is an open file. The executor does each read and seek.
type sdFile struct {
	name     string
	f        *os.File
	executor *aio_executor.Executor
	// pending are the bytes of an incomplete UTF-8 character at the end of the last read.
	pending []byte
}

func (s *sdFile) seek(pos int64) error {
	_, err := s.executor.Submit(func() (any, error) { return s.f.Seek(pos, io.SeekStart) })
	s.pending = nil
	return err
}

// read reads the next text of the file. Invalid UTF-8 is an error, as Python's text decoder.
func (s *sdFile) read(n int) (string, error) {
	v, err := s.executor.Submit(func() (any, error) {
		buf := make([]byte, n)
		k, err := io.ReadFull(s.f, buf)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			err = nil
		}
		return buf[:k], err
	})
	if err != nil {
		return "", err
	}
	data := append(s.pending, v.([]byte)...)
	s.pending = nil
	if len(v.([]byte)) > 0 {
		// Keep an incomplete character for the next read
		for i := 1; i <= 3 && i <= len(data); i++ {
			if utf8.RuneStart(data[len(data)-i]) {
				if !utf8.FullRune(data[len(data)-i:]) {
					s.pending = append([]byte(nil), data[len(data)-i:]...)
					data = data[:len(data)-i]
				}
				break
			}
		}
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("UnicodeDecodeError: 'utf-8' codec can't decode the file data")
	}
	return string(data), nil
}

func (s *sdFile) close() {
	s.executor.Submit(func() (any, error) { return nil, s.f.Close() })
}

// VirtualSD is the virtual_sdcard object.
type VirtualSD struct {
	printer          *host.Printer
	sdcardDirname    string
	currentFile      *sdFile
	filePosition     int64
	fileSize         int64
	printStats       *print_stats.PrintStats
	reactor          *reactor.Reactor
	mustPauseWork    bool
	cmdFromSD        bool
	nextFilePosition int64
	workTimer        *reactor.Timer
	executor         *aio_executor.Executor
	onErrorGcode     *gcode_macro.TemplateWrapper
	gcode            *gcode.Dispatch
}

// expandUser is Python's os.path.expanduser.
func expandUser(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + p[1:]
		}
	}
	return p
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	v := &VirtualSD{printer: printer}
	// sdcard state
	sd := config.Get("path")
	v.sdcardDirname = filepath.Clean(expandUser(sd))
	// Print Stat Tracking
	v.printStats = print_stats.Load(config)
	// Work timer
	v.reactor = printer.Reactor()
	// Error handling
	gm := gcode_macro.Load(config)
	aio := aio_executor.Load(config)
	v.executor = aio.AllocateExecutor("virtual_sdcard")
	def := defaultErrorGcode
	v.onErrorGcode = gm.LoadTemplate(config, "on_error_gcode", &def)
	// Register commands
	v.gcode = gcode.Of(printer)
	for _, c := range []struct {
		name string
		fn   gcode.Handler
	}{{"M20", v.cmdM20}, {"M21", v.cmdM21}, {"M23", v.cmdM23}, {"M24", v.cmdM24}, {"M25", v.cmdM25},
		{"M26", v.cmdM26}, {"M27", v.cmdM27}} {
		v.gcode.RegisterCommand(c.name, c.fn, false, "")
	}
	for _, name := range []string{"M28", "M29", "M30"} {
		v.gcode.RegisterCommand(name, v.cmdError, false, "")
	}
	v.gcode.RegisterCommand("SDCARD_RESET_FILE", v.cmdSdcardResetFile, false,
		"Clears a loaded SD File. Stops the print if necessary")
	v.gcode.RegisterCommand("SDCARD_PRINT_FILE", v.cmdSdcardPrintFile, false,
		"Loads a SD file and starts the print.  May include files in subdirectories.")
	printer.RegisterEventHandler("klippy:analyze_shutdown", func(args ...any) { v.handleAnalyzeShutdown() })
	printer.RegisterEventResultHandler("gcode:debuginput_exit", func(...any) any { return v.handleDebuginputExit() })
	return v
}

func init() { host.RegisterModule("virtual_sdcard", load, nil) }

func (v *VirtualSD) handleAnalyzeShutdown() {
	if v.workTimer == nil {
		return
	}
	filePosition := v.filePosition
	currentFile := v.currentFile
	v.mustPauseWork = true
	logDebugData := func(eventtime float64) any {
		readpos := max(filePosition-1024, 0)
		readcount := filePosition - readpos
		var data []byte
		_, err := currentFile.executor.Submit(func() (any, error) {
			if _, err := currentFile.f.Seek(readpos, io.SeekStart); err != nil {
				return nil, err
			}
			buf := make([]byte, readcount+128)
			n, err := io.ReadFull(currentFile.f, buf)
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				err = nil
			}
			data = buf[:n]
			return nil, err
		})
		if err != nil {
			log.Printf("virtual_sdcard shutdown read: %v", err)
			return nil
		}
		k := min(int(readcount), len(data))
		log.Printf("Virtual sdcard (%d): %q\nUpcoming (%d): %q", readpos, data[:k], filePosition, data[k:])
		return nil
	}
	v.reactor.RegisterCallback(logDebugData, reactor.Now)
}

func (v *VirtualSD) handleDebuginputExit() bool {
	// When in batch debugging mode, wait until sdcard idle before exiting
	return v.workTimer == nil
}

// Stats is the statistics of the print.
func (v *VirtualSD) Stats(eventtime float64) (bool, string) {
	if v.workTimer == nil {
		return false, ""
	}
	return true, fmt.Sprintf("sd_pos=%d", v.filePosition)
}

func hasValidExt(name string) bool {
	ext := name[strings.LastIndex(name, ".")+1:]
	for _, e := range validGcodeExts {
		if ext == e {
			return true
		}
	}
	return false
}

// FileList returns the G-code files. With checkSubdirs, it includes the files of the
// subdirectories, with their paths.
func (v *VirtualSD) FileList(checkSubdirs bool) []File {
	var flist []File
	if checkSubdirs {
		walk(v.sdcardDirname, func(fullPath string, info fs.FileInfo) {
			if !hasValidExt(info.Name()) {
				return
			}
			flist = append(flist, File{fullPath[len(v.sdcardDirname)+1:], info.Size()})
		})
	} else {
		dname := v.sdcardDirname
		entries, err := os.ReadDir(dname)
		if err != nil {
			log.Printf("virtual_sdcard get_file_list: %v", err)
			panic(gcode.Errorf("Unable to get file list"))
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			// os.path.isfile follows the links
			info, err := os.Stat(filepath.Join(dname, e.Name()))
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			flist = append(flist, File{e.Name(), info.Size()})
		}
	}
	sort.SliceStable(flist, func(i, j int) bool { return strings.ToLower(flist[i].Name) < strings.ToLower(flist[j].Name) })
	return flist
}

// walk calls fn for each file of a directory tree, as Python's os.walk with followlinks.
func walk(dir string, fn func(fullPath string, info fs.FileInfo)) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var dirs []string
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		info, err := os.Stat(full)
		if err == nil && info.IsDir() {
			dirs = append(dirs, full)
			continue
		}
		if err != nil {
			// A broken link is a file of os.walk, and os.path.getsize raises an error.
			panic(fmt.Errorf("FileNotFoundError: %v", err))
		}
		fn(full, info)
	}
	for _, d := range dirs {
		walk(d, fn)
	}
}

// FileNames are the names of FileList(false), for the menu.
func (v *VirtualSD) FileNames() []string {
	var out []string
	for _, f := range v.FileList(false) {
		out = append(out, f.Name)
	}
	return out
}

// Status is the state of the sdcard.
func (v *VirtualSD) Status(eventtime float64) map[string]any {
	return map[string]any{
		"file_path":     v.FilePath(),
		"progress":      v.Progress(),
		"is_active":     v.IsActive(),
		"file_position": v.filePosition,
		"file_size":     v.fileSize,
	}
}

// FilePath is the path of the current file, or nil.
func (v *VirtualSD) FilePath() any {
	if v.currentFile != nil {
		return v.currentFile.name
	}
	return nil
}

// Progress is the part of the file that is done.
func (v *VirtualSD) Progress() float64 {
	if v.fileSize != 0 {
		return float64(v.filePosition) / float64(v.fileSize)
	}
	return 0.
}

// IsActive tells if a print is in progress.
func (v *VirtualSD) IsActive() bool { return v.workTimer != nil }

// DoPause pauses the print, and waits for the work timer to stop.
func (v *VirtualSD) DoPause() {
	if v.workTimer != nil {
		v.mustPauseWork = true
		for v.workTimer != nil && !v.cmdFromSD {
			v.reactor.Pause(v.reactor.Monotonic() + .001)
		}
	}
}

// DoResume starts or resumes the print.
func (v *VirtualSD) DoResume() {
	if v.workTimer != nil {
		panic(gcode.Errorf("SD busy"))
	}
	v.mustPauseWork = false
	v.workTimer = v.reactor.RegisterTimer(v.workHandler, reactor.Now)
}

// DoCancel stops the print and closes the file.
func (v *VirtualSD) DoCancel() {
	if v.currentFile != nil {
		v.DoPause()
		v.currentFile.close()
		v.currentFile = nil
		v.printStats.NoteCancel()
	}
	v.filePosition, v.fileSize = 0, 0
}

// G-Code commands

func (v *VirtualSD) cmdError(gcmd *gcode.Command) { panic(gcmd.Error("SD write not supported")) }

func (v *VirtualSD) resetFile() {
	if v.currentFile != nil {
		v.DoPause()
		v.currentFile.close()
		v.currentFile = nil
	}
	v.filePosition, v.fileSize = 0, 0
	v.printStats.Reset()
	v.printer.SendEvent("virtual_sdcard:reset_file")
}

func (v *VirtualSD) cmdSdcardResetFile(gcmd *gcode.Command) {
	if v.cmdFromSD {
		panic(gcmd.Error("SDCARD_RESET_FILE cannot be run from the sdcard"))
	}
	v.resetFile()
}

func (v *VirtualSD) cmdSdcardPrintFile(gcmd *gcode.Command) {
	if v.workTimer != nil {
		panic(gcmd.Error("SD busy"))
	}
	v.resetFile()
	filename := gcmd.GetString("FILENAME")
	if filename == "" {
		panic(fmt.Errorf("IndexError: string index out of range"))
	}
	if filename[0] == '/' {
		filename = filename[1:]
	}
	v.loadFile(gcmd, filename, true)
	v.DoResume()
}

func (v *VirtualSD) cmdM20(gcmd *gcode.Command) {
	// List SD card
	files := v.FileList(false)
	gcmd.RespondRaw("Begin file list")
	for _, f := range files {
		gcmd.RespondRaw(fmt.Sprintf("%s %d", f.Name, f.Size))
	}
	gcmd.RespondRaw("End file list")
}

func (v *VirtualSD) cmdM21(gcmd *gcode.Command) {
	// Initialize SD card
	gcmd.RespondRaw("SD card ok")
}

func (v *VirtualSD) cmdM23(gcmd *gcode.Command) {
	// Select SD file
	if v.workTimer != nil {
		panic(gcmd.Error("SD busy"))
	}
	v.resetFile()
	filename := strings.TrimSpace(gcmd.RawParams())
	filename = strings.TrimPrefix(filename, "/")
	v.loadFile(gcmd, filename, false)
}

func (v *VirtualSD) loadFile(gcmd *gcode.Command, filename string, checkSubdirs bool) {
	files := v.FileList(checkSubdirs)
	filesByLower := map[string]string{}
	found := false
	for _, f := range files {
		filesByLower[strings.ToLower(f.Name)] = f.Name
		found = found || f.Name == filename
	}
	fname := filename
	if !found {
		var ok bool
		if fname, ok = filesByLower[strings.ToLower(fname)]; !ok {
			log.Printf("virtual_sdcard file open: KeyError: %q", strings.ToLower(filename))
			panic(gcmd.Error("Unable to open file"))
		}
	}
	fname = filepath.Join(v.sdcardDirname, fname)
	var fsize int64
	r, err := v.executor.Submit(func() (any, error) {
		f, err := os.Open(fname)
		if err != nil {
			return nil, err
		}
		size, err := f.Seek(0, io.SeekEnd)
		if err == nil {
			fsize = size
			_, err = f.Seek(0, io.SeekStart)
		}
		if err != nil {
			f.Close()
			return nil, err
		}
		return f, nil
	})
	if err != nil {
		log.Printf("virtual_sdcard file open: %v", err)
		panic(gcmd.Error("Unable to open file"))
	}
	gcmd.RespondRaw(fmt.Sprintf("File opened:%s Size:%d", filename, fsize))
	gcmd.RespondRaw("File selected")
	v.currentFile = &sdFile{name: fname, f: r.(*os.File), executor: v.executor}
	v.filePosition = 0
	v.fileSize = fsize
	v.printStats.SetCurrentFile(filename)
}

func (v *VirtualSD) cmdM24(gcmd *gcode.Command) {
	// Start/resume SD print
	v.DoResume()
}

func (v *VirtualSD) cmdM25(gcmd *gcode.Command) {
	// Pause SD print
	v.DoPause()
}

func (v *VirtualSD) cmdM26(gcmd *gcode.Command) {
	// Set SD position
	if v.workTimer != nil {
		panic(gcmd.Error("SD busy"))
	}
	pos := gcmd.GetInt("S", configfile.Min(0))
	v.filePosition = int64(pos)
}

func (v *VirtualSD) cmdM27(gcmd *gcode.Command) {
	// Report SD print status
	if v.currentFile == nil {
		gcmd.RespondRaw("Not SD printing.")
		return
	}
	gcmd.RespondRaw(fmt.Sprintf("SD printing byte %d/%d", v.filePosition, v.fileSize))
}

// FilePosition is the file position of the next command.
func (v *VirtualSD) FilePosition() int64 { return v.nextFilePosition }

// SetFilePosition sets the file position of the next command.
func (v *VirtualSD) SetFilePosition(pos int64) { v.nextFilePosition = pos }

// IsCmdFromSD tells if the current command is from the sdcard.
func (v *VirtualSD) IsCmdFromSD() bool { return v.cmdFromSD }

// runScript runs a line of the file. It returns the message of a command error, and false for
// another error.
func (v *VirtualSD) runScript(line string) (errorMessage *string, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			if e, isCmd := r.(*gcode.CommandError); isCmd {
				msg := e.Error()
				errorMessage, ok = &msg, true
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("virtual_sdcard on_error: %v", r)
						}
					}()
					v.gcode.RunScript(v.onErrorGcode.Render(nil))
				}()
				return
			}
			log.Printf("virtual_sdcard dispatch: %v", r)
			errorMessage, ok = nil, false
		}
	}()
	v.gcode.RunScript(line)
	return nil, true
}

// Background work timer
func (v *VirtualSD) workHandler(eventtime float64) float64 {
	log.Printf("Starting SD card print (position %d)", v.filePosition)
	v.reactor.UnregisterTimer(v.workTimer)
	if err := v.currentFile.seek(v.filePosition); err != nil {
		log.Printf("virtual_sdcard seek: %v", err)
		v.workTimer = nil
		return reactor.Never
	}
	v.printStats.NoteStart()
	gcodeMutex := v.gcode.Mutex()
	partialInput := ""
	var lines []string
	var errorMessage *string
	for !v.mustPauseWork {
		if len(lines) == 0 {
			// Read more data
			data, err := v.currentFile.read(8192)
			if err != nil {
				log.Printf("virtual_sdcard read: %v", err)
				break
			}
			if data == "" {
				// End of file
				v.currentFile.close()
				v.currentFile = nil
				log.Print("Finished SD card print")
				v.gcode.RespondRaw("Done printing file")
				break
			}
			lines = strings.Split(data, "\n")
			lines[0] = partialInput + lines[0]
			partialInput = lines[len(lines)-1]
			lines = lines[:len(lines)-1]
			for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
				lines[i], lines[j] = lines[j], lines[i]
			}
			v.reactor.Pause(reactor.Now)
			continue
		}
		// Pause if any other request is pending in the gcode class
		if gcodeMutex.Test() {
			v.reactor.Pause(v.reactor.Monotonic() + 0.050)
			continue
		}
		// Dispatch command
		v.cmdFromSD = true
		line := lines[len(lines)-1]
		lines = lines[:len(lines)-1]
		nextFilePosition := v.filePosition + int64(len(line)) + 1
		v.nextFilePosition = nextFilePosition
		msg, ok := v.runScript(line)
		if msg != nil {
			errorMessage = msg
			break
		}
		if !ok {
			break
		}
		v.cmdFromSD = false
		v.filePosition = v.nextFilePosition
		// Do we need to skip around?
		if v.nextFilePosition != nextFilePosition {
			if err := v.currentFile.seek(v.filePosition); err != nil {
				log.Printf("virtual_sdcard seek: %v", err)
				v.workTimer = nil
				return reactor.Never
			}
			lines = nil
			partialInput = ""
		}
	}
	log.Printf("Exiting SD card print (position %d)", v.filePosition)
	v.workTimer = nil
	v.cmdFromSD = false
	if errorMessage != nil {
		v.printStats.NoteError(*errorMessage)
	} else if v.currentFile != nil {
		v.printStats.NotePause()
	} else {
		v.printStats.NoteComplete()
	}
	return reactor.Never
}
