package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"

	"printdeck/internal/gfx"
	"printdeck/internal/ui"
)

// The upgrade booklet for the LulzBot TAZ Workhorse: the manual for a wide display, with the
// guides of the mount before it (lulzbot/), and an advertisement on the inside of the cover.
//
//	printdeck manual DIR lulzbot
//
// The data and the line-art images of the mount come from lulzbot/guide/, which
// lulzbot/make_pdf.py writes.

// lulzGuideDir is the folder of the mount data, from the folder of the deck.
const lulzGuideDir = "../lulzbot/guide"

// lulzGuide is lulzbot/guide/guide.json.
type lulzGuide struct {
	Totals struct {
		Grams, Metres, Parts int
		Time, Cost           string
		PricePerKg           string
	}
	Prints   []lulzPrint
	Test     lulzPrint
	RollNut  lulzPrint // the printed roll-in nut: an option until you have the metal T-nuts
	Settings [][2]string
	Hardware []struct {
		Count       int
		Item, Where string
	}
	Electronics [][2]any
	Tools       []string
	Checks      []string
	Steps       []struct {
		Image, Title, Note string
		Need, Items        []string
	}
	Cables [][3]string
	// The corners of the screen in lulzbot-printer.png: top left, top right, bottom right,
	// bottom left.
	Screen [4][2]float64
}

type lulzPrint struct {
	File, Name, Colour, Swatch, Time, Cost, Size, Notes, Image string
	Count, Grams                                               int
	Metres                                                     float64
}

// adPage is the advertisement: an advertisement for the upgrade in the style of a computer
// magazine of the 1980s.
type adPage struct {
	Kicker, Burst string
	Headline      []string // the lines of the headline
	Image         string
	Body          []string
	Checks        []string
	Coupon        string
	Boxes         []string
	Mail          string
	Fine          string
}

// lulzEdition reads the mount data, copies its images into dir/img, draws the advertisement
// picture, and returns the edition of the booklet.
func lulzEdition(dir string, a *ui.App, w, h int) bookEdition {
	b, err := os.ReadFile(filepath.Join(lulzGuideDir, "guide.json"))
	if err != nil {
		log.Fatal(err)
	}
	var g lulzGuide
	if err := json.Unmarshal(b, &g); err != nil {
		log.Fatal(err)
	}
	imgs, _ := filepath.Glob(filepath.Join(lulzGuideDir, "img", "*.png"))
	for _, f := range imgs {
		data, err := os.ReadFile(f)
		if err != nil {
			log.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, "img", filepath.Base(f)), data, 0o644)
	}

	// The printer of the advertisement shows the idle screen of the deck.
	a.ResetScreens()
	a.OpenPrintScreen("")
	scr := gfx.New(w, h)
	a.Draw(scr)
	writePNG(filepath.Join(dir, "img", "lulzbot-ad.png"), onScreen(filepath.Join(lulzGuideDir, "img", "lulzbot-printer.png"), scr.RGBA, g.Screen))

	return bookEdition{Kind: "UPGRADE KIT", For: "For the LulzBot TAZ Workhorse", Code: "PD-TAZ-WH-1",
		Guides: lulzGuides(g), Ad: lulzAd(g)}
}

// onScreen draws the picture of a screen into the four corners of a screen in a picture of a
// printer (top left, top right, bottom right, bottom left), with the perspective of the corners.
// Each pixel is the mean of 4 x 4 samples.
func onScreen(path string, shot *image.RGBA, q [4][2]float64) *image.RGBA {
	f, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		log.Fatal(err)
	}
	out := image.NewRGBA(src.Bounds())
	for y := out.Rect.Min.Y; y < out.Rect.Max.Y; y++ {
		for x := out.Rect.Min.X; x < out.Rect.Max.X; x++ {
			out.Set(x, y, src.At(x, y))
		}
	}
	// The homography from the unit square to the corners (Heckbert), and its inverse.
	m := squareToQuad(q)
	inv := invert3(m)
	minX, minY, maxX, maxY := q[0][0], q[0][1], q[0][0], q[0][1]
	for _, p := range q {
		minX, minY, maxX, maxY = math.Min(minX, p[0]), math.Min(minY, p[1]), math.Max(maxX, p[0]), math.Max(maxY, p[1])
	}
	sw, sh := float64(shot.Rect.Dx()), float64(shot.Rect.Dy())
	const n = 4
	for y := int(minY); y <= int(maxY); y++ {
		for x := int(minX); x <= int(maxX); x++ {
			var r, g, b, k float64
			for sy := 0; sy < n; sy++ {
				for sx := 0; sx < n; sx++ {
					px, py := float64(x)+(float64(sx)+0.5)/n, float64(y)+(float64(sy)+0.5)/n
					d := inv[6]*px + inv[7]*py + inv[8]
					u, v := (inv[0]*px+inv[1]*py+inv[2])/d, (inv[3]*px+inv[4]*py+inv[5])/d
					if u < 0 || u >= 1 || v < 0 || v >= 1 {
						continue
					}
					c := shot.RGBAAt(int(u*sw), int(v*sh))
					r, g, b, k = r+float64(c.R), g+float64(c.G), b+float64(c.B), k+1
				}
			}
			if k == 0 {
				continue
			}
			// The edge pixels mix with the picture of the printer.
			f := k / (n * n)
			o := out.RGBAAt(x, y)
			mix := func(s, d float64) uint8 { return uint8(math.Round(s/k*f + d*(1-f))) }
			out.SetRGBA(x, y, color.RGBA{mix(r, float64(o.R)), mix(g, float64(o.G)), mix(b, float64(o.B)), 255})
		}
	}
	return out
}

// squareToQuad is the projective map from the unit square to the quad q, as a 3 x 3 matrix in
// rows: x' = (m0 u + m1 v + m2) / (m6 u + m7 v + m8), y' = (m3 u + m4 v + m5) / (...).
func squareToQuad(q [4][2]float64) [9]float64 {
	x0, y0, x1, y1, x2, y2, x3, y3 := q[0][0], q[0][1], q[1][0], q[1][1], q[2][0], q[2][1], q[3][0], q[3][1]
	dx1, dx2, dx3 := x1-x2, x3-x2, x0-x1+x2-x3
	dy1, dy2, dy3 := y1-y2, y3-y2, y0-y1+y2-y3
	den := dx1*dy2 - dx2*dy1
	g := (dx3*dy2 - dx2*dy3) / den
	h := (dx1*dy3 - dx3*dy1) / den
	return [9]float64{x1 - x0 + g*x1, x3 - x0 + h*x3, x0, y1 - y0 + g*y1, y3 - y0 + h*y3, y0, g, h, 1}
}

// invert3 is the inverse of a 3 x 3 matrix in rows.
func invert3(m [9]float64) [9]float64 {
	a, b, c, d, e, f, g, h, i := m[0], m[1], m[2], m[3], m[4], m[5], m[6], m[7], m[8]
	det := a*(e*i-f*h) - b*(d*i-f*g) + c*(d*h-e*g)
	return [9]float64{(e*i - f*h) / det, (c*h - b*i) / det, (b*f - c*e) / det,
		(f*g - d*i) / det, (a*i - c*g) / det, (c*d - a*f) / det,
		(d*h - e*g) / det, (b*g - a*h) / det, (a*e - b*d) / det}
}

// The chapters of the upgrade.
const (
	chUpgrade = "Upgrade the Workhorse"
	chBuild   = "Build the mount"
)

// lulzGuides are the guides of the upgrade, before the guides of the manual: what you build, the
// print plan, the hardware, the checks on the printer, the drive, the steps, and the cables.
func lulzGuides(g lulzGuide) []bookGuide {
	t := g.Totals
	para := func(b *bookGuide, s string) { b.add(bookBlock{Kind: "para", Text: s}, lines(len(s), 82)*bookLine+0.1) }
	list := func(b *bookGuide, kind string, items []string) {
		h := 0.1
		for _, s := range items {
			h += lines(len(s), 76)*bookLine + 0.05
		}
		b.add(bookBlock{Kind: kind, Items: items}, h)
	}
	note := func(b *bookGuide, s string) { b.add(bookBlock{Kind: "note", Text: s}, lines(len(s), 78)*0.17+0.18) }
	sub := func(b *bookGuide, s string) { b.add(bookBlock{Kind: "sub", Text: s}, 0.4) }

	what := bookGuide{Chapter: chUpgrade, Title: "What you build"}
	what.add(bookBlock{Kind: "title", Text: what.Title}, bookTitleH)
	para(&what, "This kit puts the Printdeck on the front left of your TAZ Workhorse. The 7 inch touch screen is in a "+
		"wide head below the TAZ Workhorse logo, and the Raspberry Pi 5 is in a pod on the left side of the "+
		"electronics chassis. The head leans back 15 degrees, so you see the screen from the front of the printer.")
	para(&what, "You print the parts on the Workhorse itself. Two printed clips hold on the front rail with metal "+
		"T-nuts, and two screws of the side cover hold the plate. You drill no hole in the printer.")
	what.add(bookBlock{Kind: "image", Images: []string{"lulzbot-hero", "lulzbot-side"}, ImageH: 3.1}, 3.25)
	what.add(bookBlock{Kind: "chips", Chips: []string{
		fmt.Sprintf("%d printed parts", t.Parts), fmt.Sprintf("%d g of PLA", t.Grams), t.Time + " of printing",
		"No supports", "No holes in the printer"}}, 0.45)
	para(&what, "The deck screens in this booklet are the wide screens: the display is mounted wide in the head.")

	plan := bookGuide{Chapter: chUpgrade, Title: "Print the parts"}
	plan.add(bookBlock{Kind: "title", Text: plan.Title}, bookTitleH)
	para(&plan, "The STL files are in lulzbot/stl/, in their print orientation. Load them as they are. The numbers come "+
		"from slices with the Cura profiles of the mount: LulzBot TAZ Workhorse, SE tool head, 0.5 mm nozzle, 2.85 mm PLA.")
	plan.add(bookBlock{Kind: "parts", Parts: g.Prints}, 0.35+float64(len(g.Prints))*0.47+0.35)
	note(&plan, fmt.Sprintf("Total: %d g of PLA (%d m), %s of printing, %s at %s a kilogram. The fit test of the "+
		"screen opening (%s, %d g, %s) is optional.", t.Grams, t.Metres, t.Time, t.Cost, t.PricePerKg, g.Test.File, g.Test.Grams, g.Test.Time))
	r := g.RollNut
	note(&plan, fmt.Sprintf("Until you have the metal T-nuts, you can print %d roll-in nuts (%s, %d g, %s) and press an "+
		"M3 hex nut into each one. They roll into the slot at any point on the rail, the same as the metal T-nuts.",
		r.Count, r.File, r.Grams, r.Time))
	note(&plan, "Print the fit test first, and check the display in it: the display pocket has 0.3 mm of clearance at each "+
		"side. Then print the head and the back panel: steps 2 to 4 need them. Then print the plate, the clips, the pod "+
		"and the grille.")
	sub(&plan, "Settings")
	for _, r := range g.Settings {
		plan.add(bookBlock{Kind: "table", Rows: [][2]string{r}}, lines(len(r[1]), 44)*0.17+0.12)
	}

	need := bookGuide{Chapter: chUpgrade, Title: "What you need"}
	need.add(bookBlock{Kind: "title", Text: need.Title}, bookTitleH)
	sub(&need, "Hardware")
	var hw [][]string
	h := 0.3
	for _, x := range g.Hardware {
		hw = append(hw, []string{fmt.Sprintf("%d ×", x.Count), x.Item, x.Where})
		h += lines(len(x.Where), 40)*0.16 + 0.12
	}
	need.add(bookBlock{Kind: "grid", Head: []string{"", "Item", "Where"}, Cells: hw}, h)
	para(&need, "All the screws are M3 socket cap screws, for a 2.5 mm hex key. A shorter or a longer heat-set insert fits, up to the length in brackets.")
	sub(&need, "Electronics and cables")
	var el []string
	for _, e := range g.Electronics {
		el = append(el, fmt.Sprintf("%v × %v", e[0], e[1]))
	}
	list(&need, "list", el)
	sub(&need, "Tools")
	list(&need, "list", append(g.Tools, "One strip of 2 mm foam tape, between the panel and the display board"))

	check := bookGuide{Chapter: chUpgrade, Title: "Before you start"}
	check.add(bookBlock{Kind: "title", Text: check.Title}, bookTitleH)
	check.add(bookBlock{Kind: "image", Images: []string{"lulzbot-exploded"}, ImageH: 2.6}, 2.75)
	para(&check, "The mount uses two screws of the printer and the front rail. The upper pair of screws of the side cover "+
		"(PP-FP0164) holds the plate. Two printed clips on the front slot of the front left rail hold the right end of the "+
		"head: a metal T-nut rolls into the slot behind the lips, and an M3 × 8 screw pulls it up. Two screws through a flange of the plate hold up the head, and the pod slides onto two hooks of the plate.")
	sub(&check, "Check on your printer")
	list(&check, "olist", g.Checks)

	drive := bookGuide{Chapter: chUpgrade, Title: "Prepare the drive"}
	drive.add(bookBlock{Kind: "title", Text: drive.Title}, bookTitleH)
	para(&drive, "The Printdeck starts from the 2.5 inch drive. Write the Printdeck image to it before you build the mount: "+
		"the drive goes into the head in step 3.")
	list(&drive, "olist", []string{
		"Build the image on a computer with Docker: run os/build.sh in the Printdeck repository. It writes os/out/printdeck.img.xz.",
		"Connect the drive to the computer with its USB adapter. Write printdeck.img.xz to the drive with Raspberry Pi Imager (Use custom) or balenaEtcher.",
		"Open the PRINTDECK partition of the drive, and open printdeck.txt in a text editor.",
		"Set display_rotate=180. The display is upside down in the head, so the deck turns its picture. A width larger than the height gives the wide layout.",
		"Fill in the name and the password of your Wi-Fi network, if you want to skip the Wi-Fi setup. The deck reads the file at each start, and then clears the password in it.",
	})
	note(&drive, "If the picture is upside down at the first start, set display_rotate=0.")

	build := bookGuide{Chapter: chBuild, Title: "Build the mount"}
	build.add(bookBlock{Kind: "title", Text: build.Title}, bookTitleH)
	para(&build, "Each step names the parts and the hardware that it needs. The parts from an earlier step have the number of that step.")
	for i, s := range g.Steps {
		// The picture at the left, 2.1 inches high at most; the actions and the note at the right.
		text := 0.0
		for _, it := range s.Items {
			text += lines(len(it), 50)*bookLine + 0.08
		}
		if s.Note != "" {
			text += lines(len(s.Note), 54)*0.17 + 0.18
		}
		h := 0.45 + 0.3 + math.Max(2.1, text) + 0.12
		build.add(bookBlock{Kind: "step", N: i + 1, Text: s.Title, Chips: s.Need, Images: []string{"lulzbot-" + s.Image},
			Items: s.Items, Notes: nonEmpty(s.Note)}, h)
	}

	conn := bookGuide{Chapter: chBuild, Title: "Connect and start"}
	conn.add(bookBlock{Kind: "title", Text: conn.Title}, bookTitleH)
	conn.add(bookBlock{Kind: "image", Images: []string{"lulzbot-connect"}, ImageH: 2.6}, 2.75)
	var cab [][]string
	for _, c := range g.Cables {
		cab = append(cab, c[:])
	}
	conn.add(bookBlock{Kind: "grid", Head: []string{"Cable", "Length", "Route"}, Cells: cab}, 0.3+float64(len(cab))*0.3)
	list(&conn, "olist", []string{
		"Connect the power cable to the Raspberry Pi 27 W USB-C power supply behind the printer.",
		"Connect the printer cable to the USB socket of the printer board.",
		"Turn on the printer and the power supply. The deck starts from the drive and opens the setup. The guides after this page show the setup.",
	})
	sub(&conn, "To open the mount")
	list(&conn, "olist", []string{
		"Lift the grille out at the fingernail notch, and disconnect the fan cable. Remove the two screws under the pod. Move the pod down and out at 45 degrees, off the hooks, then 4 mm to the rear and out.",
		"Disconnect the cables from the Pi. Remove the two flange screws from inside the pod.",
		"Lift the head 30 mm and pull it off the clip keys. The display and the drive come off with it.",
		"To get to the display or the drive, remove the four screws from the rear of the back panel.",
	})
	return []bookGuide{what, plan, need, check, drive, build, conn}
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

// lulzAd is the advertisement on the inside of the cover.
func lulzAd(g lulzGuide) *adPage {
	return &adPage{
		Kicker:   "Introducing Printdeck for the TAZ Workhorse",
		Headline: []string{"Your Workhorse called.", "It wants a dashboard."},
		Burst:    "Now with touch!",
		Image:    "lulzbot-ad",
		Body: []string{
			"Let’s be honest. Your TAZ Workhorse works fine. It prints. It has a knob. Nobody asked you to change a thing.",
			"That is exactly why you should.",
			"Printdeck puts a seven-inch touch screen on the front of the printer, in a case that you print on the very printer you upgrade. The printer builds its own upgrade. If that is not the future, we do not know what is.",
			"Inside is Klipper, ported to Go, in one program. It plans every move with the same look-ahead as the printer, so the time left is the real time left. Not a guess. Not a mood.",
			"It updates itself. It flashes the firmware of the printer for you. It even takes over the old display of the printer and puts the same menus on it, so the knob gets a promotion too.",
			"No drilling. No nuts in the rail. Two cover screws and two printed clips. If you can turn a hex key, you are qualified.",
		},
		Checks: []string{
			"7-inch color touch screen, mounted wide",
			"The Klipper host, in Go, in one program",
			"Time left from the motion planner",
			"Signed updates that install themselves",
			fmt.Sprintf("%d g of PLA, %s, no supports", g.Totals.Grams, g.Totals.Time),
		},
		Coupon: "YES! Upgrade my Workhorse. I have no good reason.",
		Boxes:  []string{"Because I can", "Because I can", "Both"},
		Mail:   fmt.Sprintf("Mail to: your own printer. Allow %s for delivery.", g.Totals.Time),
		Fine: "Printdeck is free software (AGPL-3.0). Printer not included. LulzBot and TAZ Workhorse are names of their " +
			"owner. Printdeck is not made by LulzBot and is not connected with LulzBot. The time left is exact for the " +
			"moves of the file; the heating and your pauses are your own business.",
	}
}
