// Package fb draws a canvas to the Linux framebuffer, rotated for the portrait display.
package fb

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"printdeck/internal/gfx"
)

type FB struct {
	mem                []byte
	w, h, stride       int
	bpp                int
	rot                int // 90 or 270: rotation from the logical portrait canvas to the panel
	tty                *os.File
	LogicalW, LogicalH int
}

// Open maps the framebuffer. rot is 90 or 270 for a portrait mounted landscape panel, 0 for none.
func Open(dev string, rot int) (*FB, error) {
	name := strings.TrimPrefix(dev, "/dev/")
	sys := "/sys/class/graphics/" + name + "/"
	size, err := os.ReadFile(sys + "virtual_size")
	if err != nil {
		return nil, err
	}
	var w, h int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(size)), "%d,%d", &w, &h); err != nil {
		return nil, err
	}
	read := func(n string) int {
		b, _ := os.ReadFile(sys + n)
		v, _ := strconv.Atoi(strings.TrimSpace(string(b)))
		return v
	}
	bpp, stride := read("bits_per_pixel"), read("stride")
	if bpp != 16 && bpp != 32 {
		return nil, fmt.Errorf("unsupported framebuffer depth %d", bpp)
	}
	f, err := os.OpenFile(dev, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	mem, err := syscall.Mmap(int(f.Fd()), 0, stride*h, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return nil, err
	}
	d := &FB{mem: mem, w: w, h: h, stride: stride, bpp: bpp, rot: rot}
	if rot == 0 {
		d.LogicalW, d.LogicalH = w, h
	} else {
		d.LogicalW, d.LogicalH = h, w
	}
	// Stop the text console from drawing over the application.
	if t, err := os.OpenFile("/dev/tty1", os.O_RDWR, 0); err == nil {
		d.tty = t
		// Show tty1 (VT_ACTIVATE, VT_WAITACTIVE), so a desktop on another console does not cover the application.
		syscall.Syscall(syscall.SYS_IOCTL, t.Fd(), 0x5606, 1)
		syscall.Syscall(syscall.SYS_IOCTL, t.Fd(), 0x5607, 1)
		syscall.Syscall(syscall.SYS_IOCTL, t.Fd(), 0x4B3A, 1) // KDSETMODE, KD_GRAPHICS
	}
	return d, nil
}

func (d *FB) Close() {
	if d.tty != nil {
		syscall.Syscall(syscall.SYS_IOCTL, d.tty.Fd(), 0x4B3A, 0) // KD_TEXT
		d.tty.Close()
	}
	syscall.Munmap(d.mem)
}

// Present converts the canvas to the panel format. When the framebuffer has a different size than the
// canvas, for example after the display selected a smaller mode, it scales the canvas to fit.
func (d *FB) Present(c *gfx.Canvas) {
	lw, lh := d.LogicalW, d.LogicalH
	cw, ch := c.Bounds().Dx(), c.Bounds().Dy()
	bytes := d.bpp / 8
	sx := make([]int, lw) // the canvas column for each framebuffer column
	for x := range sx {
		sx[x] = x * cw / lw
	}
	for y := 0; y < lh; y++ {
		sy := y * ch / lh
		row := c.Pix[sy*c.Stride : sy*c.Stride+cw*4]
		for x := 0; x < lw; x++ {
			src := row[sx[x]*4:]
			var X, Y int
			switch d.rot {
			case 90:
				X, Y = lh-1-y, x
			case 270:
				X, Y = y, lw-1-x
			default:
				X, Y = x, y
			}
			o := Y*d.stride + X*bytes
			r, g, b := src[0], src[1], src[2]
			if d.bpp == 16 {
				v := uint16(r>>3)<<11 | uint16(g>>2)<<5 | uint16(b>>3)
				d.mem[o], d.mem[o+1] = byte(v), byte(v>>8)
			} else {
				d.mem[o], d.mem[o+1], d.mem[o+2], d.mem[o+3] = b, g, r, 0xff
			}
		}
	}
}

// PanelSize is the native size, used to map touch coordinates.
func (d *FB) PanelSize() (int, int) { return d.w, d.h }
