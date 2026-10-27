// Package printdeck_sdcard reads the SD card of the printer through the printer board. On the TAZ
// Workhorse, the card is in the slot of the display, on the SPI bus of the board. The deck lists
// the files of the card, and copies a file to the G-code folder to print it. It is a Printdeck
// module, with no Klipper source. It reads the card in SPI mode with the spi_transfer
// command of the board, as scripts/spi_flash of Klipper does. It does not write to the card.
//
//	[printdeck_sdcard]
//	cs_pin: PB0
//	#spi_speed: 400000
//	#spi_bus:
//
// Endpoints:
//
//	printdeck/sdcard/list {"path": "parts"}: {"files": [{"name", "size", "dir", "modified"}]}
//	printdeck/sdcard/copy {"path": "parts/clip.gcode", "name": "clip.gcode"}: starts the copy
//	printdeck/sdcard/cancel: stops the copy
//
// The status has the copy: "id" (the number of the last copy that started; the copy endpoint
// returns it), "copying" (the path, or ""), "copied" and "size" in bytes, "done" (the name of the
// last file that was copied) and "error".
//
//	Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package printdeck_sdcard

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"printdeck/internal/fatfs"
	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/bus"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/webhooks"
)

// chunk is the most bytes of one spi_transfer: the response of the board fits in one message.
const chunk = 48

// copyBlocks is the count of blocks that one turn of the copy reads before it lets the other
// timers run.
const copyBlocks = 8

// SDCard is the [printdeck_sdcard] section.
type SDCard struct {
	printer *host.Printer
	reactor *reactor.Reactor
	spi     *bus.MCUSPI // the card, with its chip select
	clocks  *bus.MCUSPI // the same bus with no chip select, for the clocks before the first command
	vsd     func() gcodeDir

	blockAddr bool // an SDHC or SDXC card: the address of a read is the block number

	mu     sync.Mutex
	status map[string]any
	timer  *reactor.Timer
	cancel bool
	copies int // the count of copies that started
}

// gcodeDir is the virtual_sdcard: its folder, and whether it prints.
type gcodeDir interface {
	Dirname() string
	IsActive() bool
}

func load(config *configfile.Wrapper) any {
	printer := host.Of(config)
	s := &SDCard{printer: printer, reactor: printer.Reactor(),
		status: map[string]any{"id": 0, "copying": "", "copied": 0, "size": 0, "done": "", "error": ""}}
	s.spi = bus.MCUSPIFromConfig(config, 0, "cs_pin", 400000, "", false)
	busName, _ := config.GetOptional("spi_bus", configfile.Default(nil))
	speed := config.GetInt("spi_speed", configfile.Default(400000), configfile.Min(100000))
	s.clocks = bus.NewMCUSPI(s.spi.MCU(), busName, "", 0, speed, nil, false)
	s.vsd = func() gcodeDir {
		v, _ := printer.TryLookupObject("virtual_sdcard").(gcodeDir)
		return v
	}
	wh := webhooks.Of(printer)
	wh.RegisterEndpoint("printdeck/sdcard/list", s.handleList)
	wh.RegisterEndpoint("printdeck/sdcard/copy", s.handleCopy)
	wh.RegisterEndpoint("printdeck/sdcard/cancel", s.handleCancel)
	return s
}

func init() { host.RegisterModule("printdeck_sdcard", load, nil) }

// Status is the state of the copy.
func (s *SDCard) Status(eventtime float64) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]any, len(s.status))
	for k, v := range s.status {
		out[k] = v
	}
	return out
}

func (s *SDCard) set(kv ...any) {
	s.mu.Lock()
	for i := 0; i+1 < len(kv); i += 2 {
		s.status[kv[i].(string)] = kv[i+1]
	}
	s.mu.Unlock()
}

// The SD protocol in SPI mode.

// transfer sends data to the card and returns the bytes that the card sent back.
func (s *SDCard) transfer(data []byte) []byte {
	var out []byte
	for len(data) > 0 {
		n := min(chunk, len(data))
		out = append(out, s.spi.SPITransfer(data[:n], 0, 0).Bytes("response")...)
		data = data[n:]
	}
	return out
}

var errNoCard = errors.New("no SD card in the printer")

// command sends command cmd with arg. It returns the R1 byte of the reply, and all the bytes that
// came after it: at least extra bytes (4 for R3 and R7).
func (s *SDCard) command(cmd byte, arg uint32, extra int) (byte, []byte) {
	crc := byte(0x01)
	switch cmd {
	case 0:
		crc = 0x95
	case 8:
		crc = 0x87
	}
	msg := []byte{0x40 | cmd, byte(arg >> 24), byte(arg >> 16), byte(arg >> 8), byte(arg), crc}
	resp := s.transfer(append(msg, bytesFF(10+extra)...))[len(msg):]
	for i, b := range resp {
		if b&0x80 == 0 {
			rest := resp[i+1:]
			if len(rest) < extra {
				rest = append(rest, s.transfer(bytesFF(extra-len(rest)))...)
			}
			return b, rest
		}
	}
	return 0xff, nil
}

func bytesFF(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = 0xff
	}
	return b
}

// start puts the card in SPI mode and gets it ready.
func (s *SDCard) start() error {
	// At least 74 clocks with the chip select off.
	s.clocks.SPITransfer(bytesFF(10), 0, 0)
	ok := false
	for i := 0; i < 10 && !ok; i++ {
		r, _ := s.command(0, 0, 0)
		ok = r == 0x01
	}
	if !ok {
		return errNoCard
	}
	r, r7 := s.command(8, 0x1aa, 4)
	v2 := r&0x04 == 0
	if v2 && (len(r7) < 4 || r7[3] != 0xaa) {
		return errors.New("the SD card returned no echo for the voltage check (CMD8)")
	}
	var hcs uint32
	if v2 {
		hcs = 1 << 30
	}
	deadline := s.reactor.Monotonic() + 2.
	for {
		s.command(55, 0, 0)
		r, _ = s.command(41, hcs, 0)
		if r == 0 {
			break
		}
		if r&0xfe != 0 || s.reactor.Monotonic() > deadline {
			return errors.New("the SD card is not ready after 2 seconds")
		}
		s.reactor.Pause(s.reactor.Monotonic() + 0.02)
	}
	s.blockAddr = false
	if v2 {
		if r, ocr := s.command(58, 0, 4); r == 0 && len(ocr) >= 4 {
			s.blockAddr = ocr[0]&0x40 != 0
		}
	}
	if !s.blockAddr {
		s.command(16, fatfs.BlockSize, 0)
	}
	return nil
}

// ReadBlock reads block lba of the card.
func (s *SDCard) ReadBlock(lba uint32, b []byte) error {
	addr := lba
	if !s.blockAddr {
		addr = lba * fatfs.BlockSize
	}
	r, data := s.command(17, addr, 0)
	if r != 0 {
		return fmt.Errorf("the SD card returned error %#02x for the read of block %d", r, lba)
	}
	// The data token 0xfe, then the block and its CRC.
	for tries := 0; ; tries++ {
		if i := indexToken(data); i >= 0 {
			data = data[i+1:]
			break
		}
		if tries > 200 {
			return fmt.Errorf("the SD card sent no data for block %d", lba)
		}
		data = s.transfer(bytesFF(chunk))
	}
	if len(data) < fatfs.BlockSize+2 {
		data = append(data, s.transfer(bytesFF(fatfs.BlockSize+2-len(data)))...)
	}
	copy(b, data[:fatfs.BlockSize])
	return nil
}

// indexToken returns the place of the data token in b, or -1.
func indexToken(b []byte) int {
	for i, c := range b {
		if c == 0xfe {
			return i
		}
	}
	return -1
}

// open starts the card and opens its file system.
func (s *SDCard) open() *fatfs.FS {
	if err := s.start(); err != nil {
		panic(gcode.Errorf("%s", err))
	}
	fs, err := fatfs.Open(s)
	if err != nil {
		panic(gcode.Errorf("%s", err))
	}
	return fs
}

func (s *SDCard) copying() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.timer != nil
}

// handleList returns the files and the folders of a folder of the card.
func (s *SDCard) handleList(req *webhooks.WebRequest) {
	if v := s.vsd(); v != nil && v.IsActive() {
		panic(gcode.Errorf("not during a print"))
	}
	if s.copying() {
		panic(gcode.Errorf("the deck copies a file from the SD card"))
	}
	path, _ := req.GetStr("path", "").(string)
	list, err := s.open().ReadDir(path)
	if err != nil {
		panic(gcode.Errorf("%s", err))
	}
	files := []map[string]any{}
	for _, e := range list {
		if strings.HasPrefix(e.Name, ".") {
			continue
		}
		var mod int64
		if !e.Modified.IsZero() {
			mod = e.Modified.Unix()
		}
		files = append(files, map[string]any{"name": e.Name, "size": e.Size, "dir": e.Dir, "modified": mod})
	}
	req.Send(map[string]any{"path": path, "files": files})
}

// handleCopy starts the copy of a file of the card to the G-code folder, as name.
func (s *SDCard) handleCopy(req *webhooks.WebRequest) {
	path, _ := req.GetStr("path").(string)
	name, _ := req.GetStr("name").(string)
	vsd := s.vsd()
	if vsd == nil {
		panic(gcode.Errorf("the printer has no [virtual_sdcard]"))
	}
	if vsd.IsActive() {
		panic(gcode.Errorf("not during a print"))
	}
	dir := vsd.Dirname()
	if name == "" || filepath.Base(name) != name || strings.HasPrefix(name, ".") {
		panic(gcode.Errorf("not a valid file name: %q", name))
	}
	if s.copying() {
		panic(gcode.Errorf("the deck copies a file from the SD card"))
	}
	f, err := s.open().Open(path)
	if err != nil {
		panic(gcode.Errorf("%s", err))
	}
	dest := filepath.Join(dir, name)
	tmp, err := os.CreateTemp(dir, ".sdcard-*")
	if err != nil {
		panic(gcode.Errorf("%s", err))
	}
	w := bufio.NewWriterSize(tmp, 64<<10)
	s.copies++
	id := s.copies
	s.set("id", id, "copying", path, "copied", 0, "size", f.Size, "error", "")
	var copied int64
	buf := make([]byte, fatfs.BlockSize)
	finish := func(err error) float64 {
		if err == nil {
			err = w.Flush()
		}
		if e := tmp.Close(); err == nil {
			err = e
		}
		if err == nil {
			err = os.Rename(tmp.Name(), dest)
		}
		if err != nil {
			os.Remove(tmp.Name())
			s.set("error", err.Error())
		} else {
			s.set("done", name)
		}
		s.mu.Lock()
		s.status["copying"], s.timer, s.cancel = "", nil, false
		s.mu.Unlock()
		return reactor.Never
	}
	s.mu.Lock()
	s.cancel = false
	s.timer = s.reactor.RegisterTimer(func(eventtime float64) float64 {
		for i := 0; i < copyBlocks; i++ {
			s.mu.Lock()
			stop := s.cancel
			s.mu.Unlock()
			if stop {
				return finish(errors.New("the copy was stopped"))
			}
			n, err := f.Read(buf)
			if err == io.EOF {
				return finish(nil)
			}
			if err == nil {
				_, err = w.Write(buf[:n])
			}
			if err != nil {
				return finish(err)
			}
			copied += int64(n)
			s.set("copied", copied)
		}
		return reactor.Now
	}, reactor.Now)
	s.mu.Unlock()
	req.Send(map[string]any{"id": id, "name": name, "size": f.Size})
}

// handleCancel stops the copy. The part of the file that was copied is deleted.
func (s *SDCard) handleCancel(req *webhooks.WebRequest) {
	s.mu.Lock()
	s.cancel = s.timer != nil
	s.mu.Unlock()
}
