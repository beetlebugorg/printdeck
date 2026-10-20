// Package touch reads a touch controller (the AR1100) from evdev.
package touch

import (
	"context"
	"encoding/binary"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type Kind int

const (
	Down Kind = iota
	Move
	Up
)

// Event has coordinates in the range 0..1 of the panel.
type Event struct {
	Kind Kind
	X, Y float64
}

type Options struct {
	InvertX, InvertY, Swap bool
	Rotate                 int // rotation of the UI on the panel: 0, 90 or 270
	// Log writes each sample of a touch to the log: the raw panel values and the position in the
	// UI (0..1). It is for the measurement of the calibration.
	Log bool
}

const (
	evKey, evAbs, evSyn = 1, 3, 0
	absX, absY          = 0, 1
	btnTouch            = 0x14a
)

// find returns the event device of the touch controller.
func find() string {
	paths, _ := filepath.Glob("/sys/class/input/event*")
	best := ""
	for _, p := range paths {
		nameB, _ := os.ReadFile(p + "/device/name")
		name := strings.ToLower(strings.TrimSpace(string(nameB)))
		if strings.HasPrefix(name, "vc4") || strings.Contains(name, "pwr_button") {
			continue
		}
		absB, _ := os.ReadFile(p + "/device/capabilities/abs")
		var abs uint64
		for _, f := range strings.Fields(string(absB)) {
			// The bitmask is printed as space separated hex words, most significant first.
			var w uint64
			for _, c := range f {
				w = w<<4 | uint64(strings.IndexRune("0123456789abcdef", c))
			}
			abs = abs<<(4*uint(len(f))) | w
		}
		if abs&3 != 3 {
			continue
		}
		dev := "/dev/input/" + filepath.Base(p)
		if strings.Contains(name, "ar1100") || strings.Contains(name, "microchip") || strings.Contains(name, "touch") {
			return dev
		}
		if best == "" {
			best = dev
		}
	}
	return best
}

func absRange(fd uintptr, axis uintptr) (min, max int32) {
	var info [6]int32
	req := uintptr(2)<<30 | uintptr(unsafe.Sizeof(info))<<16 | uintptr('E')<<8 | (0x40 + axis)
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(&info[0]))); e != 0 {
		return 0, 4095
	}
	return info[1], info[2]
}

// Run sends events until ctx ends. It waits for the controller and reconnects after unplugging.
func Run(ctx context.Context, o Options, out chan<- Event) {
	for ctx.Err() == nil {
		dev := find()
		if dev == "" {
			sleep(ctx, 2*time.Second)
			continue
		}
		read(ctx, dev, o, out)
		sleep(ctx, time.Second)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// debounce is how long the finger must be off the panel before the touch is over. The resistive panel
// loses contact for a few milliseconds, many times, during a swipe. Without this, each of them is a tap.
// (Measured: gaps of 4 to 25 ms inside a touch, and at least 485 ms between separate taps.)
const debounce = 80 * time.Millisecond

type rawEvent struct {
	typ, code uint16
	val       int32
}

func read(ctx context.Context, dev string, o Options, out chan<- Event) {
	f, err := os.Open(dev)
	if err != nil {
		return
	}
	defer f.Close()
	go func() { <-ctx.Done(); f.Close() }()

	minX, maxX := absRange(f.Fd(), absX)
	minY, maxY := absRange(f.Fd(), absY)
	if o.Log {
		log.Printf("touch %s: x range %d..%d, y range %d..%d", dev, minX, maxX, minY, maxY)
	}

	raw := make(chan rawEvent, 64)
	go func() {
		defer close(raw)
		buf := make([]byte, 24) // struct input_event on 64-bit
		for {
			if _, err := readFull(f, buf); err != nil {
				return
			}
			raw <- rawEvent{binary.LittleEndian.Uint16(buf[16:]), binary.LittleEndian.Uint16(buf[18:]), int32(binary.LittleEndian.Uint32(buf[20:]))}
		}
	}()

	process(ctx, raw, o, [4]int32{minX, maxX, minY, maxY}, out)
}

// process turns the raw events of the panel into Down, Move, and Up events. rng is the range of the axes:
// minX, maxX, minY, maxY.
func process(ctx context.Context, raw <-chan rawEvent, o Options, rng [4]int32, out chan<- Event) {
	minX, maxX, minY, maxY := rng[0], rng[1], rng[2], rng[3]
	var x, y float64
	var rawX, rawY int32
	touching, moved, pendingDown := false, false, false
	logSample := func(kind string, p Event) {
		if o.Log {
			log.Printf("touch %s raw=%d,%d ui=%.4f,%.4f", kind, rawX, rawY, p.X, p.Y)
		}
	}
	var release *time.Timer // running while the finger is off the panel, but the touch is not over yet
	var releaseC <-chan time.Time
	for {
		select {
		case e, ok := <-raw:
			if !ok {
				return
			}
			switch {
			case e.typ == evAbs && e.code == absX:
				x, moved, rawX = float64(e.val-minX)/float64(max(1, maxX-minX)), true, e.val
			case e.typ == evAbs && e.code == absY:
				y, moved, rawY = float64(e.val-minY)/float64(max(1, maxY-minY)), true, e.val
			case e.typ == evKey && e.code == btnTouch:
				if e.val == 1 {
					if releaseC != nil { // the finger came back: it is the same touch
						release.Stop()
						release, releaseC = nil, nil
					} else if !touching {
						// The position arrives before the SYN event, so Down is sent there.
						touching, moved, pendingDown = true, true, true
					}
				} else if touching && releaseC == nil {
					release = time.NewTimer(debounce)
					releaseC = release.C
				}
			case e.typ == evSyn && e.code == 0 && moved && touching:
				p := adj(x, y, o)
				if pendingDown {
					pendingDown = false
					logSample("down", p)
					out <- Event{Down, p.X, p.Y}
				} else {
					logSample("move", p)
					select {
					case out <- Event{Move, p.X, p.Y}:
					default: // drop move events when the UI is busy
					}
				}
				moved = false
			}
		case <-releaseC:
			touching, release, releaseC = false, nil, nil
			p := adj(x, y, o)
			logSample("up", p)
			out <- Event{Up, p.X, p.Y}
		case <-ctx.Done():
			return
		}
	}
}

func readFull(f *os.File, b []byte) (int, error) {
	n := 0
	for n < len(b) {
		m, err := f.Read(b[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func adj(x, y float64, o Options) Event {
	if o.Swap {
		x, y = y, x
	}
	if o.InvertX {
		x = 1 - x
	}
	if o.InvertY {
		y = 1 - y
	}
	// The raw position is in panel (landscape) orientation. Turn it to the UI orientation.
	switch o.Rotate {
	case 90:
		x, y = y, 1-x
	case 270:
		x, y = 1-y, x
	}
	return Event{X: max(0, min(1, x)), Y: max(0, min(1, y))}
}
