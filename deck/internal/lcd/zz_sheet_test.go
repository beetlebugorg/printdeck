package lcd

import (
	"image"
	"image/draw"
	"image/png"
	"os"
	"sort"
	"testing"
)

func TestZZSheet(t *testing.T) {
	out := os.Getenv("SHEET")
	if out == "" {
		t.Skip()
	}
	var names []string
	for n := range icons {
		names = append(names, n)
	}
	sort.Strings(names)
	// List menus: 6 items per screen, as on the display.
	var imgs []*image.RGBA
	for i := 0; i < len(names); i += 6 {
		m := &Menu{Title: "Icons"}
		for _, n := range names[i:min(i+6, len(names))] {
			m.Items = append(m.Items, MenuItem{Label: n, Icon: n})
		}
		m.Selected = -1
		s := &Screen{Menu: m, Font: "5x8"}
		imgs = append(imgs, s.Image(0, 3))
	}
	// Carousel: each big icon.
	var big []string
	for n := range icons24 {
		big = append(big, n)
	}
	sort.Strings(big)
	for i, n := range big {
		items := []MenuItem{}
		for _, b := range big {
			items = append(items, MenuItem{Label: b, Icon: b})
		}
		s := &Screen{Menu: &Menu{Title: "Printdeck", Carousel: true, Items: items, Selected: i}}
		imgs = append(imgs, s.Image(0, 3))
		_ = n
	}
	w, h := imgs[0].Bounds().Dx(), imgs[0].Bounds().Dy()
	cols := 4
	rows := (len(imgs) + cols - 1) / cols
	sheet := image.NewRGBA(image.Rect(0, 0, cols*(w+8), rows*(h+8)))
	for i, im := range imgs {
		x, y := (i%cols)*(w+8), (i/cols)*(h+8)
		draw.Draw(sheet, image.Rect(x, y, x+w, y+h), im, image.Point{}, draw.Src)
	}
	f, _ := os.Create(out)
	png.Encode(f, sheet)
	f.Close()
}
