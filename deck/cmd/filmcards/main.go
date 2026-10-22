// Command filmcards draws the title cards of the film of the desktop app (desktop/film/) as the
// dots of the printer display, in the colors of the start screen of the deck: the word PRINTDECK
// in the 8 x 16 font of Klipper, and the other words in the 5 x 8 font.
//
//	go run ./cmd/filmcards CARDS.json OUT
//
// It writes OUT/N.png for card N of CARDS.json, and OUT/N-sub.png for its second line, each with
// a blank version (N-blank.png, N-sub-blank.png): the display with no dot lit, for the film to
// light the dots row by row.
package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"

	"printdeck/internal/lcd"
)

type card struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
	Sub  string `json:"sub"`
}

var (
	gap = color.RGBA{0x1f, 0x4b, 0xd1, 255}
	off = color.RGBA{0x2a, 0x58, 0xe0, 255}
	on  = color.RGBA{0xf2, 0xf6, 0xff, 255}
)

func write(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
}

// line is one line of text in a margin of m dark dots at each side, as the edge of a display.
func line(text, font string, scale, m int) image.Image {
	img := lcd.DotLines([]string{text}, len(text), font, scale, gap, off, on)
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx()+2*m*scale, b.Dy()+2*m*scale))
	draw.Draw(out, out.Bounds(), &image.Uniform{gap}, image.Point{}, draw.Src)
	for y := 0; y < out.Bounds().Dy(); y += scale {
		for x := 0; x < out.Bounds().Dx(); x += scale {
			draw.Draw(out, image.Rect(x, y, x+scale-1, y+scale-1), &image.Uniform{off}, image.Point{}, draw.Src)
		}
	}
	draw.Draw(out, b.Add(image.Pt(m*scale, m*scale)), img, image.Point{}, draw.Src)
	return out
}

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: filmcards CARDS.json OUT")
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	var cards []card
	if err := json.Unmarshal(data, &cards); err != nil {
		log.Fatal(err)
	}
	out := os.Args[2]
	if err := os.MkdirAll(out, 0o755); err != nil {
		log.Fatal(err)
	}
	both := func(name, text, font string, scale, m int) {
		write(filepath.Join(out, name+".png"), line(text, font, scale, m))
		write(filepath.Join(out, name+"-blank.png"), line(strings.Repeat(" ", len(text)), font, scale, m))
	}
	for i, c := range cards {
		if c.Kind == "card" {
			both(fmt.Sprint(i), c.Text, "5x8", 7, 4)
			continue
		}
		both(fmt.Sprint(i), c.Text, "", 11, 6)
		if c.Sub != "" {
			both(fmt.Sprintf("%d-sub", i), c.Sub, "5x8", 6, 3)
		}
	}
}
