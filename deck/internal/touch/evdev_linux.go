package touch

// The touch panel of the deck on Linux: an evdev device.

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
