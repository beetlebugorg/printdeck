package ui

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"sort"

	xdraw "golang.org/x/image/draw"

	"printdeck/internal/gfx"
)

// LogoRotate is the rotation of the display on the Printdeck OS image (DISPLAY_ROTATE).
const LogoRotate = 270

// logoRect is the part of the start screen in the kernel logo: the plate, with a border of the
// background. The kernel fills the rest of the screen with the color of the first pixel.
var logoRect = image.Rect(16, 236, W-16, H-236)

// logoColors is the most colors that the kernel takes from the file.
const logoColors = 224

// SplashLogo returns the start screen as the TGA file that the kernel shows when the display starts
// (fullscreen_logo). The kernel does not turn the image for the rotated console, so the pixels are
// in the order of the framebuffer, as fb.Present writes them. The start screen of "printdeck
// splash" then draws the same pixels, and adds the hint and the version.
func SplashLogo(rotate int) []byte {
	c := gfx.New(W, BaseH)
	drawMu.Lock()
	setHeight(BaseH)
	DrawSplash(c, "", true)
	drawMu.Unlock()

	// The framebuffer position of a canvas pixel (fb.Present).
	at := func(x, y int) (int, int) {
		switch rotate {
		case 90:
			return H - 1 - y, x
		case 270:
			return y, W - 1 - x
		}
		return x, y
	}
	x0, y0 := at(logoRect.Min.X, logoRect.Min.Y)
	x1, y1 := at(logoRect.Max.X-1, logoRect.Max.Y-1)
	out := image.Rect(min(x0, x1), min(y0, y1), max(x0, x1)+1, max(y0, y1)+1)
	img := image.NewRGBA(out)
	for y := logoRect.Min.Y; y < logoRect.Max.Y; y++ {
		for x := logoRect.Min.X; x < logoRect.Max.X; x++ {
			X, Y := at(x, y)
			img.SetRGBA(X, Y, c.RGBAAt(x, y))
		}
	}
	return encodeTGA(quantize(img, logoColors))
}

// quantize keeps the n most frequent colors, and gives each other pixel the nearest of them. The
// start screen has few colors, and only the edges of the text and the shapes have more.
func quantize(img *image.RGBA, n int) *image.RGBA {
	count := map[color.RGBA]int{}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			count[img.RGBAAt(x, y)]++
		}
	}
	if len(count) <= n {
		return img
	}
	cols := make([]color.RGBA, 0, len(count))
	for c := range count {
		cols = append(cols, c)
	}
	sort.Slice(cols, func(i, j int) bool {
		if count[cols[i]] != count[cols[j]] {
			return count[cols[i]] > count[cols[j]]
		}
		a, b := cols[i], cols[j]
		return uint32(a.R)<<16|uint32(a.G)<<8|uint32(a.B) < uint32(b.R)<<16|uint32(b.G)<<8|uint32(b.B)
	})
	keep := cols[:n]
	near := map[color.RGBA]color.RGBA{}
	for _, c := range cols[n:] {
		best, bd := keep[0], -1
		for _, k := range keep {
			dr, dg, db := int(c.R)-int(k.R), int(c.G)-int(k.G), int(c.B)-int(k.B)
			if d := dr*dr + dg*dg + db*db; bd < 0 || d < bd {
				best, bd = k, d
			}
		}
		near[c] = best
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if k, ok := near[img.RGBAAt(x, y)]; ok {
				img.SetRGBA(x, y, k)
			}
		}
	}
	return img
}

// encodeTGA writes an uncompressed 24-bit TGA file, with the rows from the top.
func encodeTGA(img *image.RGBA) []byte {
	b := img.Bounds()
	out := make([]byte, 18, 18+b.Dx()*b.Dy()*3)
	out[2] = 2 // uncompressed true color
	binary.LittleEndian.PutUint16(out[12:], uint16(b.Dx()))
	binary.LittleEndian.PutUint16(out[14:], uint16(b.Dy()))
	out[16] = 24
	out[17] = 0x20 // the first row is the top row
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			out = append(out, c.B, c.G, c.R)
		}
	}
	return out
}

// Icon returns the icon of the web page for the home screen of a phone: the plate of the start
// screen on the night background, 512 x 512 pixels, as a PNG.
func Icon() []byte {
	c := gfx.New(W, H)
	drawMu.Lock() // DrawSplash sets the palette and uses H, which the App draws with
	setHeight(BaseH)
	DrawSplash(c, "", true)
	drawMu.Unlock()
	const size = 512
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	bgc := c.RGBAAt(0, 0)
	for i := 0; i < len(out.Pix); i += 4 {
		out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = bgc.R, bgc.G, bgc.B, 255
	}
	// The plate (box of DrawSplash), scaled to fit with a margin.
	plate := image.Rect(32, 252, W-32, 548)
	w := size - 48
	h := plate.Dy() * w / plate.Dx()
	dst := image.Rect((size-w)/2, (size-h)/2, (size+w)/2, (size+h)/2)
	xdraw.CatmullRom.Scale(out, dst, c.RGBA, plate, xdraw.Src, nil)
	var b bytes.Buffer
	png.Encode(&b, out)
	return b.Bytes()
}
