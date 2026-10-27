package main

import (
	_ "embed"
	"fmt"
	"html/template"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"printdeck/internal/board"
	"printdeck/internal/gfx"
	"printdeck/internal/lcd"
	"printdeck/internal/notify"
	"printdeck/internal/printer"
	"printdeck/internal/ui"
	"printdeck/internal/wifi"
)

// The manual: each screen of the deck with numbered callouts, as in the booklet of an old game,
// and the text for each part. The deck draws the pictures itself, so the manual follows the UI.
// Each callout points at the part of the screen with its name (keysRect).
//
//	printdeck manual DIR [wide|lulzbot|cura|large]
//	                                      write DIR/index.html, DIR/booklet.html and the pictures in
//	                                      DIR/img. wide draws the screens of a display that is mounted
//	                                      wide. lulzbot is the wide manual with the upgrade guides for
//	                                      the TAZ Workhorse (upgrade.go). cura is the booklet of the
//	                                      Cura plugin (cura.go). large draws the tall screens with the
//	                                      large text of the Touch Display 2.

//go:embed manual.html
var manualHTML string

// keysRect is the place of the keys that a callout names: one key ("Print a file"), or a row of
// keys ("Park, Center, Motors off"). ok is false when a name is no key.
func keysRect(a *ui.App, title string) (image.Rectangle, bool) {
	var all image.Rectangle
	for _, name := range strings.Split(title, ", ") {
		r, ok := a.KeyRect(name)
		if !ok {
			return image.Rectangle{}, false
		}
		all = all.Union(r)
	}
	return all, true
}

// callout is a number on a picture: the name of the part, and what it does.
type callout struct {
	Title, Text string
}

type figure struct {
	Name     string
	Caption  string
	Setup    func(s *manualState)
	Callouts []callout
	// Wide and Tall are the callouts of the parts of one layout, after Callouts.
	Wide, Tall []callout
	// Layout is "wide" or "tall" for a figure of one layout only, or "" for both.
	Layout string
	Notes  []string // the text after the list
	Badges []badge  // the numbers in the margins, from layoutBadges
}

// badge is a number in the margin of a picture, with a line to its part.
type badge struct {
	N          int
	X, Y       int // the part
	BX, BY     int // the number, in the margin
	Right      bool
	LineStartX int // where the line leaves the number
}

// gutter is the width of the margins of a picture, in the units of the UI.
const gutter = 72

// layoutBadges puts the number of each part in the margin of one side, in the order of the
// height, at least 34 units apart. The side is the one where the line from the margin crosses
// fewer of the other parts of the screen (all), or else the nearer side. The dot of a part is on
// the edge of the part that faces its number, at the middle of its height, so the dot and the
// line do not cover the part. The picture is w x h units.
func layoutBadges(parts, all []image.Rectangle, w, h int) []badge {
	var left, right []int
	pts := make([]image.Point, len(parts))
	crossed := func(part, line image.Rectangle) int {
		n := 0
		for _, o := range all {
			if o != part && !part.In(o) && o.Overlaps(line) {
				n++
			}
		}
		return n
	}
	for i, r := range parts {
		y := r.Min.Y + r.Dy()/2
		pts[i] = image.Pt(r.Max.X, y)
		toLeft := crossed(r, image.Rect(0, y-1, r.Min.X, y+1))
		toRight := crossed(r, image.Rect(r.Max.X, y-1, w, y+1))
		if toLeft < toRight || toLeft == toRight && r.Min.X+r.Dx()/2 < w/2 {
			pts[i].X = r.Min.X
			left = append(left, i)
		} else {
			right = append(right, i)
		}
	}
	out := make([]badge, len(pts))
	place := func(idx []int, isRight bool) {
		sort.Slice(idx, func(a, b int) bool { return pts[idx[a]].Y < pts[idx[b]].Y })
		const gap, top = 34, 20
		bottom := h - 20
		ys := make([]int, len(idx))
		for k, i := range idx {
			ys[k] = max(pts[i].Y, top)
			if k > 0 {
				ys[k] = max(ys[k], ys[k-1]+gap)
			}
		}
		for k := len(ys) - 1; k >= 0; k-- { // push up from the bottom when needed
			lim := bottom
			if k < len(ys)-1 {
				lim = ys[k+1] - gap
			}
			ys[k] = min(ys[k], lim)
		}
		for k, i := range idx {
			b := badge{N: i + 1, X: pts[i].X, Y: pts[i].Y, BY: ys[k], Right: isRight}
			if isRight {
				b.BX, b.LineStartX = w+gutter/2+4, w+gutter/2+4-15
			} else {
				b.BX, b.LineStartX = -gutter/2-4, -gutter/2-4+15
			}
			out[i] = b
		}
	}
	place(left, false)
	place(right, true)
	return out
}

type section struct {
	ID, Title string
	Chapter   string // the chapter of the guide, in the contents
	Intro     []string
	// Wide and Tall are the paragraphs of one layout, after Intro.
	Wide, Tall []string
	Steps      []string // the steps of a guide, numbered
	Figures    []figure
	After      []string
	Table      [][2]string // an optional table of two columns, after the text
}

// manualState is the demo deck that the setups change.
type manualState struct {
	a          *ui.App
	p          *ui.DemoPrinter
	part, done image.Image // the model view of the print at 42 %, and when it is done
}

// The part of the model views: the body of the Printdeck pod.
const manualPart = "../lulzbot/stl/pod/fdm/pod-body_fdm.stl"

// partPreview draws the model view of manualPart printed up to frac.
func partPreview(frac float64) image.Image {
	f, err := os.Open(manualPart)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	img, err := printer.PreviewSTL(f, frac)
	if err != nil {
		log.Fatal(err)
	}
	return img
}

// The printer states of the pictures.
var (
	stIdle = printer.State{Reachable: true, Text: "Operational", Connected: true, Tool: 24, Bed: 23, Homed: "xyz",
		X: 110, Y: 110, Z: 5, Axis: [2][3]float64{{0, 0, 0}, {280, 280, 270}}, Firmware: printer.HostVersion + "-20261021_112706-printdeck"}
	stPrinting = printer.State{Reachable: true, Text: "Printing", Connected: true, Printing: true,
		File: "printdeck_body.gcode", FilePath: "printdeck_body.gcode", Activity: "Printing", Layer: 12, Layers: 27,
		HasNow: true, NowKind: "Outer wall", NowSpeed: 30, Completion: 42.4, TimeLeft: 83 * time.Minute, HasTimeLeft: true,
		Elapsed: 75 * time.Minute, FilamentUsed: 1720, Tool: 209.6, ToolTarget: 210, Bed: 60, BedTarget: 60, SpeedPct: 100, FlowPct: 100}
)

// manualParts are the texts and the pictures of the manual, by subject. manualSections puts them
// in the order of the guides.
func manualParts() []section {
	idle := func(s *manualState) {
		s.p.State_ = stIdle
		s.a.OpenPrintScreen("")
	}
	page := func(name string) func(s *manualState) {
		return func(s *manualState) {
			s.p.State_ = stIdle
			s.a.OpenPrintScreen(name)
		}
	}
	return []section{
		{ID: "start", Title: "Getting started",
			Intro: []string{
				"Printdeck is a touch screen and print server for Klipper printers. It runs Klipper. It shows the print, and it controls the printer.",
				"At the first start, the deck makes its password and opens the setup. Connect the deck to your Wi-Fi network there.",
			},
			Figures: []figure{
				{Name: "setup-wifi", Caption: "The setup: step 1",
					Setup: func(s *manualState) { s.a.OpenSetupScreen("wifi") },
					Callouts: []callout{
						{"Networks", "The Wi-Fi networks near the deck, with the signal. A lock shows a network with a password. Tap a network, then type its password."},
						{"Scan", "Looks for the networks again. The list also scans by itself."},
						{"Hidden", "For a network that does not show its name: type the name and the password."},
						{"Next", "Goes to the summary when the deck is connected."},
					}},
				{Name: "setup-done", Caption: "The setup: step 2",
					Setup: func(s *manualState) { s.a.OpenSetupScreen("done") },
					Callouts: []callout{
						{"Address", "The address of the deck on your network. Open it in a browser for the web pages."},
						{"Password", "The password of the web pages. To see it again, open Machine > Wi-Fi and help."},
						{"Finish", "Closes the setup. The deck does not open it again."},
					},
					Notes: []string{"You can also set the Wi-Fi before the first start: put the SD card in a computer, and fill in printdeck.txt on the PRINTDECK partition."}},
			}},
		{ID: "home", Title: "Print: the idle screen",
			Intro: []string{"When the printer does not print, the deck shows the idle screen. The largest key is the action that you need most: Print a file."},
			Figures: []figure{
				{Name: "home", Caption: "The idle screen", Setup: idle,
					Callouts: []callout{
						{"Nozzle", "The nozzle now, and its target under it while the heater is on. The icon is filled while the heater is on, and waves rise while it heats. Tap it to set the temperature."},
						{"Bed", "The same for the bed."},
						{"Fan", "The part cooling fan. Its blades turn at its speed."},
						{"Status", "The position of the head, the state of the printer with the loaded filament, and the clock. This is the status screen of the display of the printer: the same picture on both screens."},
						{"Print a file", "Opens the files. See Files."},
					},
					Tall: []callout{
						{"Preheat", "Heats the nozzle and the bed for the loaded filament. Its LED is lit while the heaters hold those temperatures: tap it again to turn them off."},
						{"Cool down", "Turns off both heaters."},
						{"Home all, X, Y, Z", "Homes all axes, or one axis, as on the Toolhead page. The LED of a key is lit when its axes are homed. During a print, the keys are dim with a lock, and a tap shows Wait until the print ends."},
						{"Last print", "The last print and its result. Reprint starts it again, after a question."},
						{"Pages", "Filament, Toolhead and Machine. Each page has its own section."},
					},
					Wide: []callout{
						{"Home all, X, Y, Z", "Homes all axes, or one axis, as on the Toolhead page. The LED of a key is lit when its axes are homed. During a print, the keys are dim with a lock, and a tap shows Wait until the print ends."},
						{"Last print", "The last print, as on its page of the history: the result, the time, the filament, the roll and the cost. Reprint starts it again, after a question. Preheat and Cool down are in Filament."},
						{"Model", "The model of the last print, in the color of its roll. Tap it to open the page of the print in the history."},
						{"Pages", "The key panel: Main (this screen), Files, Filament, Toolhead and Machine. The LED of the page that is open is lit. Each page has its own section."},
					}},
				{Name: "keypad", Caption: "The keypad of a temperature",
					Setup: func(s *manualState) { s.p.State_ = stIdle; s.a.OpenPrintScreen("keypad") },
					Callouts: []callout{
						{"Value", "The temperature that you type."},
						{"Presets", "Off, and the temperatures of the materials."},
						{"Keys", "The digits. C clears the value, DEL removes the last digit."},
						{"Set", "Sets the heater. Cancel closes the keypad with no change."},
					}},
			}},
		{ID: "files", Title: "Files",
			Intro: []string{
				"The files are the G-code files that the slicer sent, or that you uploaded in the browser. The newest file is at the top.",
				"Each file shows what the slicer wrote into it: the print time, the filament and the layer height. Cura, PrusaSlicer, OrcaSlicer and Bambu Studio write this data. A thumbnail shows when the slicer adds one.",
			},
			Figures: []figure{
				{Name: "files", Caption: "The file list", Setup: page("files"),
					Callouts: []callout{
						{"Count", "The number of files."},
						{"Picture", "The thumbnail of the slicer, or the shape of a part."},
						{"File", "The name, the print time, the filament and the layer height."},
						{"Date", "When the file arrived."},
						{"On the deck, SD card", "The files on the deck, or the files on the SD card in the display of the printer. The keys show when printer.cfg has the [printdeck_sdcard] section."},
					},
					Notes: []string{"Drag the list up and down. Tap a file to open it."}},
				{Name: "sd-card", Caption: "The SD card of the printer", Setup: page("sd-card"),
					Callouts: []callout{
						{"On the deck, SD card", "The LED of SD card is lit: the list shows the files of the card. On the deck goes back to the files on the deck."},
						{"Count", "The folder of the card that shows, and its number of G-code files."},
						{"Folder", "A folder. Tap it to open it. Up goes back to the folder above."},
						{"File", "A G-code file, with its size and date. Tap it to copy it to the deck. At the end of the copy, the deck asks to print it."},
					},
					Notes: []string{"During the copy, the page shows its progress and a Stop key. The copy goes to the files on the deck."}},
				{Name: "file", Caption: "A file", Setup: page("file"),
					Callouts: []callout{
						{"Picture", "The thumbnail of the file."},
						{"Slicer data", "The print time, the filament, the layer height, the temperatures, the slicer and the date."},
						{"Print", "Starts the print, after a question."},
						{"Delete", "Removes the file, after a question. The file that prints now cannot be removed."},
					}},
			}},
		{ID: "printing", Title: "Print: during a print",
			Intro: []string{"During a print, the deck shows the screen of the phase of the print. The deck knows the phase from the printer and from the G-code of the file."},
			Figures: []figure{
				{Name: "preparing", Caption: "Preparing",
					Setup: func(s *manualState) {
						s.p.State_ = printer.State{Reachable: true, Text: "Printing", Connected: true, Printing: true, File: "benchy.gcode",
							Activity: "Heating bed  52/60°", Preparing: true, HasWipe: true, Tool: 24, Bed: 52, BedTarget: 60}
						s.a.OpenPrintScreen("")
					},
					Callouts: []callout{
						{"Display", "The status screen of the printer display: the nozzle, the bed and the fan, the progress, the time left, and the time on the clock when the print ends. Tap the nozzle or the bed to set its temperature."},
						{"Steps", "The steps of the start macro of your printer, in its order. The deck follows the commands that the printer runs: the step that runs has a lit LED, and a done step has a dot."},
						{"Help", "Opens Wi-Fi and help: the network, the address and the password of the deck."},
						{"Cancel print", "Stops the print, after a question."},
					},
					Tall: []callout{
						{"Menu", "Shows the other screens while the print runs. See Look around during a print."},
					}},
				{Name: "first-layer", Caption: "The first layer",
					Setup: func(s *manualState) {
						s.p.State_ = printer.State{Reachable: true, Text: "Printing", Connected: true, Printing: true, File: "benchy.gcode",
							Activity: "Printing", Layer: 1, Layers: 27, Completion: 2, HasTimeLeft: true, TimeLeft: 100 * time.Minute,
							ZOffset: 0.04, Tool: 210, ToolTarget: 210, Bed: 60, BedTarget: 60}
						s.a.OpenPrintScreen("")
					},
					Callouts: []callout{
						{"Display", "The status screen of the printer display: the nozzle, the bed and the fan, the progress, the time left, and the time on the clock when the print ends. Tap the nozzle or the bed to set its temperature."},
						{"− and +", "− moves the nozzle closer to the bed, + away from it. A line that is too round and thin needs −. A line that is flat and rough needs +."},
						{"Z offset", "The height of the nozzle above its normal height. Watch the first lines while they print."},
						{"Save", "Writes the Z offset into the config of the printer. Save config in Maintenance then writes the file."},
						{"Step", "How far one tap moves the nozzle, in mm."},
						{"Pause, Cancel", "Pauses the print, or stops it after a question. After a cancel, the head moves up and to the middle, and the bed moves to the front."},
						{"Help", "Opens Wi-Fi and help: the network, the address and the password of the deck."},
						{"Layer", "The first layer of the model."},
					},
					Tall: []callout{
						{"Menu", "Shows the other screens while the print runs. See Look around during a print."},
					}},
				{Name: "printing", Caption: "Printing",
					Setup: func(s *manualState) {
						s.p.State_ = stPrinting
						s.p.State_.HasLive, s.p.State_.LiveSpeed, s.p.State_.LiveExtrude = true, 30, 1.0
						s.p.LayerTimes_ = demoLayerTimes(s.p.State_.Layers)
						s.a.OpenPrintScreen("")
						s.a.DemoLayers()
					},
					Callouts: []callout{
						{"Display", "The status screen of the printer display: the nozzle, the bed and the fan, the progress, the time left, and the time on the clock when the print ends. Tap the nozzle or the bed to set its temperature."},
						{"Pause", "Pauses the print."},
						{"Tune", "Changes the print while it runs. See Tune."},
						{"Cancel", "Stops the print, after a question."},
						{"Layer", "The layer that prints now, and the kind of the move now with the live speed of the head."},
						{"Roll", "The roll in the printer: a dot in its color, the filament that is left on it, its name, and its grams. The filament of the print counts while it prints. Cura gives a roll its color."},
						{"Layer times", "A bar for each layer: its time. The layers that are done are dark, with the time that they took. The layer now is orange, and the layers to come are light, with the time of the slicer. At the right, the time of the layer now and its plan, and the mean time of a layer. A short layer can need more cooling."},
						{"Model", "The model up to the layer that prints now, in the color of the roll."},
						{"Layer slider", "Drag it to look at another layer. Live goes back to the layer that prints."},
					},
					Tall: []callout{
						{"Menu", "Shows the other screens while the print runs. See Look around during a print."},
					},
					Wide: []callout{
						{"Pages", "During a print, Main is the print. The other keys open their pages while the print goes on, and Main comes back to the print."},
					}},
				{Name: "pause-prompt", Caption: "A pause of the file",
					Setup: func(s *manualState) {
						st := stPrinting
						st.Paused, st.PauseActive, st.PauseCheck = true, true, true
						st.PausePrompt = "Put the panel in the pocket on the bed, top up."
						s.p.State_ = st
						s.a.OpenPrintScreen("")
					},
					Callouts: []callout{
						{"Where", "The message of the file, for example for a part in a pocket on the bed (Print on a part in the Cura plugin)."},
						{"Check the height", "A paper test on the part. The print goes on from the measured height."},
						{"Resume", "Primes the nozzle and continues. With no height check, the deck asks first."},
					}},
				{Name: "color-change", Caption: "A color change",
					Setup: func(s *manualState) {
						st := stPrinting
						st.Paused, st.PauseActive = true, true
						st.PausePrompt, st.PauseRoll = "Load #2 Navy", "2"
						s.p.State_ = st
						s.a.OpenPrintScreen("")
					},
					Callouts: []callout{
						{"Where", "The roll of the next color, from the Colors tool of the Cura plugin."},
						{"Change filament", "Opens the filament change with that roll. Unload, load the new filament, and tap Done."},
						{"Resume", "Draws a short prime line at the front of the bed, then goes back to the part and continues."},
					}},
				{Name: "finishing", Caption: "The end of the print",
					Setup: func(s *manualState) {
						st := printer.State{Reachable: true, Text: "Printing", Connected: true, Printing: true, File: "benchy.gcode",
							Activity: "Cooling the bed  52°", Finishing: true, Layer: 27, Layers: 27, Completion: 100,
							Tool: 120, Bed: 52, BedTarget: 45, PrepN: 1,
							HasBedCoolLeft: true, BedCoolLeft: 4 * time.Minute, BedCoolTo: 45}
						st.Prep[0] = printer.PrepStep{Kind: "cool-bed", Name: "Cool the bed", Now: true}
						s.p.State_ = st
						s.a.OpenPrintScreen("")
					},
					Callouts: []callout{
						{"Steps", "The steps of the end macro of your printer, for example the cooling of the bed before the print moves to the front."},
						{"Model", "The finished model."},
					},
					Tall: []callout{
						{"Menu", "Shows the other screens while the end macro runs. The print bar at the bottom comes back here. The deck shows the done screen at the end of the print."},
					}},
				{Name: "away", Caption: "Filament during a print",
					Setup: func(s *manualState) {
						s.p.State_ = stPrinting
						s.a.OpenPrintScreen("filament")
						s.a.DemoAway()
					},
					Callouts: []callout{
						{"Roll", "The roll in the printer, and the filament that is left on it."},
						{"Lasts", "The filament of the rest of the print, and what is left on the roll after it. Red text shows a roll that runs out before the end."},
						{"Change filament", "Pauses the print, and opens the filament change. At its end, the nozzle goes back to the temperature of the print: tap Resume."},
					},
					Tall: []callout{
						{"Print bar", "The layer and the time left of the print, with Pause at its left. Tap the bar to go back to the screen of the print."},
					},
					Wide: []callout{
						{"Pages", "The key panel opens each page, as when the printer does not print."},
					}},
				{Name: "tune", Caption: "Tune",
					Setup: func(s *manualState) { s.p.State_ = stPrinting; s.a.OpenPrintScreen("tune") },
					Callouts: []callout{
						{"Help", "Opens Wi-Fi and help: the network, the address and the password of the deck."},
						{"Speed", "The speed of the print, in percent of the speed of the slicer."},
						{"Flow", "The amount of plastic, in percent."},
						{"Z offset", "The height of the nozzle above its normal height. Save writes it into the config of the printer, as on the first layer screen."},
						{"Fan", "The part cooling fan."},
					},
					Tall: []callout{
						{"Temperatures", "Tap a card to change its temperature."},
					},
					Wide: []callout{
						{"Display", "Tap the nozzle or the bed on the display to change its temperature."},
					}},
				{Name: "paused", Caption: "Paused",
					Setup: func(s *manualState) {
						st := stPrinting
						st.Paused, st.Message = true, "Paused by user"
						s.p.State_ = st
						s.a.OpenPrintScreen("")
					},
					Callouts: []callout{
						{"Display", "The status screen of the printer display: the nozzle, the bed and the fan, the progress, the time left, and the time on the clock when the print ends. Tap the nozzle or the bed to set its temperature."},
						{"Where", "The layer of the pause, and the message of the printer."},
						{"Resume", "Continues the print."},
						{"Change filament", "The guided filament change, for a new color. See Change the filament."},
						{"Move head", "Opens Toolhead, to move the head out of the way."},
						{"Tune", "Changes the print."},
						{"Cancel", "Stops the print, after a question."},
					}},
				{Name: "done", Caption: "Done",
					Setup: func(s *manualState) {
						s.p.State_ = printer.State{Reachable: true, Text: "Operational", Connected: true, Tool: 80, Bed: 48,
							Finished: "complete", FinishedFile: "benchy.gcode", FinishedPath: "benchy.gcode", FinishedTime: 102 * time.Minute,
							FinishedFilament: 4100, HasBedCoolLeft: true, BedCoolLeft: 9 * time.Minute, BedCoolTo: printer.ReleaseBed}
						s.p.Img = s.done
						s.a.OpenPrintScreen("")
					},
					Callouts: []callout{
						{"Model", "The model of the print."},
						{"Result", "Done, Cancelled or Failed, the file, the time and the filament of the print."},
						{"Bed", "The temperature of the bed, and the time until the part comes off easily (35°). The deck learns the time from the cooling after each print. The bar is full at 35°."},
						{"Reprint", "Prints the file again."},
						{"Done", "Closes this screen."},
					}},
			}},
		{ID: "filament", Title: "Filament",
			Intro: []string{"Set the loaded filament here, so that Preheat uses its temperatures. The guided change heats the nozzle, unloads the old filament and loads the new one.",
				"The deck keeps a list of your rolls of filament. Weigh a new roll with its reel, and the deck subtracts the weight of the empty reel. Each print subtracts the filament that it used from the roll in the printer."},
			Figures: []figure{
				{Name: "filament", Caption: "The Filament page", Setup: page("filament"),
					Callouts: []callout{
						{"Rolls", "Opens the list of your rolls. On the list, the key stays in its place with its LED lit, and a tap comes back here."},
						{"Loaded", "The roll that is in the printer and the filament on it, or the material and its temperatures."},
						{"Material", "Tap the material that is in the printer. The deck asks before it changes the loaded material."},
						{"Change filament", "The guided change: choose, heat, unload, insert, load, check. The printer homes while the nozzle heats. Turn this off on the Settings page."},
						{"Load, Unload", "Only the load, or only the unload."},
						{"Extrude, Retract", "Pushes or pulls 10 mm. The nozzle must be at 170° or more."},
						{"Heat, Cool down", "Heats the nozzle for the loaded filament, or turns the heaters off."},
					}},
				{Name: "swap", Caption: "The guided change, step 1",
					Setup: func(s *manualState) { s.p.State_ = stIdle; s.a.OpenPrintScreen("swap") },
					Callouts: []callout{
						{"Step", "The step of the change."},
						{"Rolls", "Your rolls. Tap the roll that goes in. The nozzle heats for its material."},
						{"New roll", "Adds a roll: its material, its name, its weight on a scale, the weight of an empty reel, and its price."},
						{"Other", "A filament with no roll: choose the material or type a temperature."},
						{"Close", "Stops the change. After the nozzle starts to heat, Close also turns the nozzle off. During a pause, the nozzle goes back to the temperature of the print."},
					}},
				{Name: "new-roll", Caption: "A new roll",
					Setup: func(s *manualState) { s.p.State_ = stIdle; s.a.OpenPrintScreen("new-roll") },
					Callouts: []callout{
						{"Material", "The material of the roll."},
						{"Name", "A name that you type, for example the color and the brand."},
						{"On the scale", "The weight of the roll with its reel, in grams."},
						{"Empty reel", "The weight of an empty reel of the same brand. The deck keeps the last one."},
						{"Price", "The price of the roll in dollars. The deck shows the cost of each print from it."},
						{"Save", "Adds the roll and puts it in the printer."},
					}},
				{Name: "rolls", Caption: "The rolls", Setup: page("rolls"),
					Callouts: []callout{
						{"New roll", "Adds a roll without a filament change."},
					}},
				{Name: "roll", Caption: "One roll", Setup: page("roll"),
					Notes: []string{"Each roll has a number, for example #3. The deck does not use a number again. For a label with a QR code, open printdeck.local/rolls/labels in a browser, and print the sheet on round labels of 2 inches (Avery 22807). Choose the first free label of the sheet, so a sheet lasts for many rolls. A phone camera on the label opens the page of the roll: Load opens the filament change on the deck, with the roll."},
					Callouts: []callout{
						{"Details", "The filament on the roll, the filament that prints used, and the price."},
						{"Color", "The color of the roll. Tap it to choose a color. The print screen and Cura show it."},
						{"Full roll", "The filament of a full roll as it is sold: 1 kg unless you change it. Tap it to type it, for example 250 g or 2 kg. The bar and the price per gram use it, so a roll that you added part used shows how much of a full roll is left."},
						{"Left", "The filament on the roll, as a part of a full roll."},
						{"Prints", "The number of prints that used the roll, and their filament. Tap it for the list of the prints. A tap on a print opens its page."},
						{"Weigh again", "Sets the filament from the weight on a scale."},
						{"Remove", "Removes an empty roll from the list."},
					}},
				{Name: "roll-color", Caption: "The color of a roll",
					Setup: func(s *manualState) { page("roll")(s); s.a.DemoPickColor("#1f2a5a") },
					Notes: []string{"A tap on a color sets it, and closes the page. The Cura plugin gives the material of the roll in Cura the same color. A color that you set in Cura comes back to the deck."},
					Callouts: []callout{
						{"Colors", "The colors of filament. The color of the roll now is lit."},
						{"No color", "Removes the color."},
					}},
			}},
		{ID: "move", Title: "Toolhead",
			Figures: []figure{
				{Name: "move", Caption: "The Toolhead page", Setup: page("move"),
					Callouts: []callout{
						{"Position", "X, Y and Z of the head. A lit LED shows a homed axis."},
						{"Home", "Homes all axes, or one axis. Home after each start of the printer."},
						{"Joystick", "Hold a finger on the pad to move the head in X and Y. The farther from the middle, the faster it moves."},
						{"Z", "Moves Z up or down by the step: 0.1, 1 or 10 mm."},
						{"Park, Center, Motors off", "Park moves the head to the back left. Center moves it to the middle of the bed. Motors off lets you move the head by hand. During a pause, Home, Center and Motors off are off, so that the print can continue. Park lifts the nozzle 10 mm before it moves."},
					}},
			}},
		{ID: "maintenance", Title: "Maintenance",
			Intro: []string{"Maintenance lists the calibrations that your printer has. The list comes from the config of the printer. Open it from Machine."},
			Figures: []figure{
				{Name: "maintenance", Caption: "The Maintenance page", Setup: page("maintenance"),
					Callouts: []callout{
						{"Bed tilt", "Probes the bed, and corrects its tilt (BED_TILT_CALIBRATE). A printer with a bed mesh shows Bed mesh."},
						{"Probe Z offset", "The paper test for the height of the nozzle (PROBE_CALIBRATE)."},
						{"PID tune", "Heats the nozzle to 210° and the bed to 60°, and finds the PID settings of each heater (PID_CALIBRATE). Save config then writes them."},
						{"Nozzle change", "Heats the nozzle to 250° and raises the head, to change the nozzle."},
						{"Firmware", "Writes the Klipper firmware of this Printdeck to the board of the printer. Orange text shows old firmware. See below."},
						{"Save config", "Writes the results of the calibrations into printer.cfg. Klipper restarts. Orange text shows unsaved results."},
					},
					Tall: []callout{
						{"Pressure advance test", "Prints a square tower, 30 mm high, with thin walls. The pressure advance goes up with the height: 0.005 for each mm. Measure the height where the corners are sharpest."},
						{"Pressure advance", "Type the height of the sharpest corners. The deck sets the pressure advance from it, and writes it in printer.cfg."},
					}},
				{Name: "maintenance-more", Caption: "The Maintenance page, further down", Layout: "wide",
					Setup: func(s *manualState) { page("maintenance")(s); s.a.DemoScroll(9999) },
					Callouts: []callout{
						{"Pressure advance test", "Prints a square tower, 30 mm high, with thin walls. The pressure advance goes up with the height: 0.005 for each mm. Look at the corners: low on the tower they bulge, high on the tower they are rounded. Measure the height where they are sharpest."},
						{"Pressure advance", "Type the height of the sharpest corners. The deck sets the pressure advance from it, and writes it in printer.cfg."},
					}},
				{Name: "flash", Caption: "The firmware flash", Setup: page("flash"),
					Notes: []string{"The deck stops Klipper, resets the board into its bootloader, writes the firmware, and reads it back to check it. Then Klipper starts again. The setup screen and the Klipper error screen also show this key when the board has no Klipper firmware or old firmware. Printdeck can flash boards with an atmega2560 and an STK500v2 bootloader now, for example the RAMBo and RAMPS boards."},
					Callouts: []callout{
						{"Step", "Starting the bootloader, Writing, then Checking."},
						{"Progress", "The part of the firmware that is done."},
					}},
				{Name: "action", Caption: "An action runs", Setup: page("action"),
					Notes: []string{"While an action runs, the deck shows it on the deck and in each browser, and the other keys are off. The display of the printer shows the name of the action and a moving bar. When the action ends, a short message says that it is done. When it fails, the deck shows the error of Klipper until you tap OK."},
					Callouts: []callout{
						{"Action", "The time since the start, a bar that moves while the action runs, and the temperatures."},
						{"Stop the printer", "Stops the printer at once, after one question. Then use Firmware restart."},
					}},
				{Name: "paper", Caption: "The paper test",
					Setup: func(s *manualState) {
						s.p.State_ = printer.State{Reachable: true, Text: "Operational", Connected: true, ManualProbe: true, ManualZ: 0.425}
						s.a.OpenPrintScreen("")
					},
					Callouts: []callout{
						{"Height", "The height of the nozzle now."},
						{"Steps", "Moves the nozzle down (−) or up (+), in mm."},
						{"Accept", "Takes the height. Save config then writes it."},
						{"Abort", "Stops the test with no change."},
					},
					Notes: []string{"This screen shows by itself while Klipper runs a paper test, also when it started from a macro."}},
			}},
		{ID: "machine", Title: "Machine",
			Figures: []figure{
				{Name: "machine", Caption: "The Machine page", Setup: page("machine"),
					Callouts: []callout{
						{"Information", "The version of Klipper, the state of the firmware of the board, the network address, and the totals."},
						{"Maintenance", "The calibrations that the printer has, the firmware, and Save config. See Maintenance."},
						{"Macros", "The macros of the printer. Tap one to run it."},
						{"Console", "The G-code terminal of Klipper. See below."},
						{"Restart Klipper", "Restarts the Klipper host, which reads printer.cfg again."},
						{"Firmware restart", "Restarts the firmware of the board, for example after a stop."},
					},
					Tall: []callout{
						{"Simulated printer", "Runs a simulated printer in place of the real printer. See Try the deck with a simulated printer."},
						{"Prints", "The totals of all prints. History on that page lists each print. See below."},
					},
					Notes: []string{"The rows are in groups: the printer, the history of the prints, and the deck. Drag the list up to see the rows further down."}},
				{Name: "machine-prints", Caption: "The Machine page, further down", Layout: "wide",
					// The list scrolled to the fourth row of the printer: 24 + 5 + 3 × (60 + 5) pixels.
					Setup: func(s *manualState) { s.p.State_ = stIdle; s.a.OpenPrintScreen("machine"); s.a.DemoScroll(224) },
					Callouts: []callout{
						{"Simulated printer", "Runs a simulated printer in place of the real printer. See Try the deck with a simulated printer."},
						{"Prints", "The totals of all prints. History on that page lists each print. See below."},
					}},
				{Name: "machine-more", Caption: "The Machine page, at the bottom",
					Setup: func(s *manualState) { s.p.State_ = stIdle; s.a.OpenPrintScreen("machine"); s.a.DemoScroll(9999) },
					Callouts: []callout{
						{"Settings", "The theme, the clock, and the homing in a filament change. See below."},
						{"Software update", "The version of the deck, and a new version when there is one. See Update the Printdeck."},
						{"Wi-Fi and help", "The network, the password and the addresses."},
						{"System monitor", "Graphs of the CPU, the memory and the writes to the SD card. See below."},
					}},
				{Name: "settings", Caption: "The Settings page",
					Setup: settingsSetup,
					Callouts: []callout{
						{"Theme", "The colors of the screen: day, night, or auto (night from 20:00 to 7:00). Tap it to change them."},
						{"Clock", "The clock of the screens and the display of the printer: 12 hours (2:24 PM) or 24 hours (14:24). Tap it to change it."},
						{"Home in a filament change", "On: the printer homes all axes while the nozzle heats for a filament change. A change during a paused print does not home. Tap it to turn it on or off."},
						{"Screen", "The turn of the screen: 0° or 180° for a display that is mounted wide, 90° or 270° for a display that is mounted tall. A tap asks to turn it a quarter turn more, then the deck restarts in the new turn. The touch turns with the screen. A print goes on during the restart. This setting wins over display_rotate in printdeck.txt."},
						{"Camera", "The USB camera of the deck, for example a webcam on the frame of the printer. Tap the row for the Camera page. See below."},
					},
					Tall: []callout{
						{"Phones", "The phones with the Printdeck app that get the push messages of the prints. The app pairs with the deck over the network, with the address and the API key. The messages are encrypted from the deck to the phone. Tap the row for the list; a tap on a phone asks to remove it."},
						{"Notifications", "Push messages to your phone and watch: print done, print failed, a pause, the end of the first layer, and a roll that runs short. Install the ntfy app on the phone, and subscribe to a topic with a name that is hard to guess. Type the same topic here, or the address of a topic on your own ntfy server. A test message goes out when you save it. An empty topic turns the messages off."},
						{"Text", "The size of the small text on a tall screen: auto, large, or normal. Auto is large on the Touch Display 2, which has smaller pixels. Tap it to change it."},
					}},
				{Name: "settings-more", Caption: "The Settings page, at the bottom", Layout: "wide",
					Setup: func(s *manualState) { settingsSetup(s); s.a.DemoScroll(9999) },
					Callouts: []callout{
						{"Phones", "The phones with the Printdeck app that get the push messages of the prints. The app pairs with the deck over the network, with the address and the API key. The messages are encrypted from the deck to the phone. Tap the row for the list; a tap on a phone asks to remove it."},
						{"Notifications", "Push messages to your phone and watch: print done, print failed, a pause, the end of the first layer, and a roll that runs short. Install the ntfy app on the phone, and subscribe to a topic with a name that is hard to guess. Type the same topic here, or the address of a topic on your own ntfy server. A test message goes out when you save it. An empty topic turns the messages off."},
					}},
				{Name: "camera", Caption: "The Camera page",
					Setup: func(s *manualState) {
						s.p.State_ = stIdle
						s.a.Camera = ui.NewDemoCamera()
						s.a.OpenPrintScreen("camera")
					},
					Callouts: []callout{
						{"Picture", "The picture of the camera, a few times a second while the page is open. Aim the camera at the bed with it."},
						{"State", "The camera and its format, or the state: no camera, no signal (an HDMI source that is off), or an error."},
						{"Camera", "Auto (the first USB camera that the deck finds), one of the cameras, or Off. Tap it to change it."},
						{"Turn", "Turns the picture 180°, for a camera that is mounted upside down."},
					},
					Notes: []string{"The camera is for the apps: the iPhone and the watch, the desktop window, Cura and the other OctoPrint clients show it (see Slicers). The camera runs only while somebody looks, and stops a few seconds after. The pictures stay in memory: the deck writes none to the SD card. With the camera Off, or with no camera, the webcam of the apps is the screen of the deck."}},
				{Name: "console", Caption: "The console",
					Setup: func(s *manualState) {
						s.p.State_ = stIdle
						s.p.Console_ = []printer.ConsoleLine{
							{Kind: "in", Text: "G28"},
							{Kind: "in", Text: "M105"},
							{Kind: "out", Text: "ok T:24.1 /0.0 B:23.4 /0.0"},
							{Kind: "in", Text: "G1 X400"},
							{Kind: "error", Text: "Move out of range: 400.000 150.000 10.000 [0.000]"},
							{Kind: "in", Text: "STATUS"},
							{Kind: "out", Text: "Klipper state: Ready"},
						}
						s.a.OpenPrintScreen("console")
					},
					Callouts: []callout{
						{"Replies", "Your commands (amber, after >), the replies of Klipper, and errors (red). Drag down to read older lines."},
						{"Type a command", "Opens the keyboard. It shows your last command, so you can change it and send it again."},
						{"Quick keys", "Home (G28), Status (STATUS) and Help (HELP, the list of the commands)."},
					}},
				{Name: "simulator", Caption: "A simulated printer in use",
					Setup: func(s *manualState) {
						s.p.State_ = stIdle
						s.p.State_.Simulated = "LulzBot TAZ Workhorse"
						s.a.OpenPrintScreen("simulator")
					},
					Callouts: []callout{
						{"Simulated", "The orange band at the top of each screen shows that the printer is simulated. No real printer moves."},
						{"Real printer", "Puts your printer.cfg back, and restarts Klipper."},
						{"Simulated LulzBot TAZ Workhorse", "The TAZ Workhorse: a probe on four washers, and bed tilt. Here it is in use."},
						{"Simulated LulzBot Mini 2", "The Mini 2: a probe on four washers, and bed tilt."},
						{"Simulated Creality CR-10 V3", "The CR-10 V3: endstops on each axis, and no probe."},
					}},
				{Name: "monitor", Caption: "The System monitor", Setup: page("monitor"),
					Callouts: []callout{
						{"CPU", "The share of the time that the CPU is busy: now, the average and the peak of the graph."},
						{"Memory", "The memory in use, of the memory of the deck."},
						{"Writes", "The writes to the SD card in the last 60 seconds, and the amount. The bars are the writes a minute. The deck writes to the card as little as it can."},
						{"1 h", "The time of the graphs: 10 minutes, 1 hour or 24 hours. The right end is now."},
					},
					Notes: []string{"The deck keeps a sample every 2 seconds for 1 hour, and a sample a minute for 24 hours. The samples are in memory only, and a restart clears them."}},
				{Name: "update", Caption: "Software update", Setup: page("update"),
					Callouts: []callout{
						{"Versions", "The version of this deck, the newest version, and the channel."},
						{"Notes", "What the new version changes."},
						{"Install", "Downloads the new version, checks it, and installs it, after a question. The deck restarts."},
						{"Check now", "Checks for a new version now."},
						{"Beta updates", "On: the deck also gets the betas, the versions with new features for tests. Off: only the stable versions."},
					}},
				{Name: "stats", Caption: "Statistics", Setup: page("stats"),
					Callouts: []callout{
						{"Totals", "The number of prints, the success rate, the print time and the filament."},
						{"Results", "Done (amber), cancelled and failed (red)."},
						{"Details", "The filament in grams (for 1.75 mm PLA), the average and the longest print, the prints of the last 7 and 30 days, and the file that you print most."},
						{"History", "Each print, with its result. Tap a print to see its model, its time and its filament, and to print it again. Back goes to Statistics."},
					}},
			}},
		{ID: "web", Title: "The deck in the browser",
			Intro: []string{
				"Open http://printdeck.local in a browser on the same network, and log in with the password of the deck. The browser shows the same screens as the touch screen, sharp at the pixel density of the phone. Each browser has its own screens, so a phone can show the files while the deck shows the print.",
				"On a phone, use Add to Home Screen: the deck then opens as an app, with no browser bars.",
				"The page http://printdeck.local/system shows the version, the services, the logs and the API key for slicers. It also installs an update: choose the update file, then Install. The deck writes the other system slot and restarts into it. If the new system does not start, the deck goes back to the old one.",
				"The same page makes a backup of your data and restores it. See Back up your data.",
			}},
		{ID: "lcd", Title: "The display of the printer",
			Intro: []string{
				"When the printer has a display, the deck draws it. The display shows the status screen, the same picture as the idle screen of the deck.",
				"Turn or click the knob to open the menu. The menu has the same pages and keys as the touch screen: the knob moves the cursor, and a click taps the key. The touch screen shows the cursor with an orange ring. The top item goes back: Status on the home menu, Back on a page. A long click also goes back. With no turn for 20 seconds, the display shows the status screen again.",
				"While an action runs, the display shows an animation.",
			},
			Table: [][2]string{
				{"lcd-status", "The status screen"},
				{"lcd-printing", "During a print"},
				{"lcd-home", "The home menu: turn the knob to choose"},
				{"lcd-list", "A page: Maintenance"},
				{"lcd-action", "An action runs"},
			}},
		{ID: "slicers", Title: "Slicers",
			Intro: []string{
				"The deck answers the OctoPrint API that slicers use to send files and to start prints.",
				"In Cura, add the deck with the OctoPrint Connection plugin. The deck asks \"Allow Cura to use the Printdeck?\". Tap Allow.",
				"In OrcaSlicer or PrusaSlicer, add a printer of the type OctoPrint with the address http://printdeck.local, and the API key of the system page.",
				"The webcam of the OctoPrint API is the camera of the deck (Settings, then Camera): http://printdeck.local/webcam/stream and /webcam/snapshot, with the API key. The parameter w gives smaller pictures (for example w=320), and fps fewer frames a second (for example fps=2). With no camera, the webcam is the screen of the deck.",
			}},
		{ID: "settings", Title: "Settings",
			Intro: []string{"The file printdeck.txt on the PRINTDECK partition holds the settings. Put the SD card in a computer to change it. The deck reads it at each start, then clears the Wi-Fi password in it."},
			Table: [][2]string{
				{"wifi_ssid, wifi_password, wifi_country", "The Wi-Fi network"},
				{"hostname, timezone", "The name on the network, and the time zone"},
				{"display_rotate", "The turn of the screen: 90 or 270 turns a wide display to tall, 0 or 180 shows the native shape. Empty is 0 for a tall display (the Touch Display 2) and 270 for a wide display"},
				{"touch_invert_x, touch_invert_y, touch_swap_xy", "1 turns an axis of the touch screen"},
				{"preheat_tool, preheat_bed", "The temperatures of Preheat with no filament set"},
				{"night_from, night_to", "The hours of the night colors"},
				{"printer.cfg (a file)", "A Klipper config. The deck takes it at the next start."},
			}},
		{ID: "problems", Title: "Problems",
			Figures: []figure{
				{Name: "setup", Caption: "No printer yet",
					Setup: func(s *manualState) { s.a.OpenPrintScreen("setup") },
					Callouts: []callout{
						{"Board", "The board that the deck found on USB, its MCU, and its Klipper firmware."},
						{"Choose printer", "Opens the list of Klipper's example configs. See below."},
						{"Search again", "Looks for the board again, for example after you connect the printer."},
						{"Simulated printer", "Runs a simulated printer, so you can try the deck with no printer."},
						{"Your own config", "You can also copy your own printer.cfg to the PRINTDECK partition, and start the deck again."},
						{"Wi-Fi and help", "The network, the address and the password of the deck."},
						{"Software update", "Installs a new version of Printdeck OS. A new version can fix the problem."},
					}},
				{Name: "configs", Caption: "Choose your printer",
					Setup: func(s *manualState) { s.a.OpenPrintScreen("configs") },
					Callouts: []callout{
						{"Chip", "The list shows the printers with the same chip as your board."},
						{"Printer", "The brand, the model, the year and the chip. Tap your printer. The deck writes its config as printer.cfg, with the port of your board, and restarts Klipper. The old printer.cfg stays as printer.cfg.bak."},
						{"Show all printers", "Shows the printers for all chips."},
					}},
				{Name: "error", Caption: "A Klipper error",
					Setup: func(s *manualState) { s.a.OpenPrintScreen("error") },
					Callouts: []callout{
						{"Message", "What Klipper reports, with all its lines."},
						{"Restart Klipper", "Restarts Klipper. It reads printer.cfg again."},
						{"Wi-Fi and help", "The network, the address and the password of the deck."},
						{"Software update", "Installs a new version of Printdeck OS. A new version can fix the problem."},
					}},
			}},
	}
}

// settingsSetup shows the Settings page with each row: the Screen row of the Printdeck OS, the
// notifications, the phones and the camera.
func settingsSetup(s *manualState) {
	s.p.State_ = stIdle
	s.a.OpenPrintScreen("settings")
	// The Screen row shows on the Printdeck OS, which restarts the deck.
	s.a.Rotate, s.a.OnRotate = 90, func(int) {}
	if s.a.Wide {
		s.a.Rotate = 180
	}
	s.a.NotifyTopic, s.a.OnNotify = "printdeck-7f3a", func(string) {}
	s.a.Phones = &notify.Phones{} // no file: no paired phone
	s.a.Camera = ui.NewDemoCamera()
}

// manualSections are the guides of the manual, by chapter: the things that a user wants to do,
// with the steps and the pictures for each.
func manualSections() []section {
	parts := map[string]section{}
	figs := map[string]figure{}
	for _, p := range manualParts() {
		parts[p.ID] = p
		for _, f := range p.Figures {
			figs[f.Name] = f
		}
	}
	fig := func(names ...string) []figure {
		var out []figure
		for _, n := range names {
			f, ok := figs[n]
			if !ok {
				log.Fatalf("manual: no figure %q", n)
			}
			out = append(out, f)
		}
		return out
	}
	const (
		meet   = "Meet your Printdeck"
		setup  = "Set up"
		prints = "Print a part"
		care   = "Look after the printer"
	)
	return []section{
		{Chapter: setup, ID: "wifi", Title: "Connect to Wi-Fi",
			Intro: []string{"At the first start, the deck makes its password and opens the setup."},
			Steps: []string{
				"Switch on the Printdeck. The setup opens.",
				"Tap your Wi-Fi network, then type its password.",
				"Wait until the deck is connected, then tap Next.",
				"Write down the address and the password, then tap Finish.",
			},
			Figures: fig("setup-wifi", "setup-done")},
		{Chapter: setup, ID: "printer", Title: "Choose your printer",
			Intro: []string{"A new Printdeck has no printer config. The deck then shows the printer setup."},
			Steps: []string{
				"Connect the printer to the Printdeck with a USB cable, and switch the printer on.",
				"Wait until the deck finds the board. Tap Search again if it does not show.",
				"Tap Choose printer. The printers of the brand of your board are at the top.",
				"Tap your printer, then Use. Klipper restarts with its config.",
			},
			Figures: fig("setup", "configs")},
		{Chapter: setup, ID: "firmware", Title: "Install the Klipper firmware",
			Intro: []string{"The board of the printer must run the Klipper firmware of this Printdeck. The deck writes it through the USB cable."},
			Steps: []string{
				"Open Machine, then Maintenance, then tap Firmware. A new board shows Install Klipper firmware on the setup screen.",
				"Tap Write. Do not switch off the printer until the deck shows the result.",
				"Tap OK. Klipper restarts and connects to the board.",
			},
			Figures: fig("flash")},

		{Chapter: meet, ID: "deck", Title: "The deck",
			Intro: []string{
				"Printdeck is a touch screen and print server for Klipper printers. It runs Klipper. It shows the print, and it controls the printer.",
				"When the printer does not print, the deck shows the idle screen. The largest key is the action that you need most: Print a file.",
				"The keys are the keys of a machine. A key with an LED has two states, and its LED is lit while it is on: the page that is open, the material in the printer, Preheat. A key with no LED does an action: an orange ring shows that it took the tap.",
			},
			Tall: []string{
				"The deck shows the display of the printer at the top of each screen. The keys of the pages are at the bottom of the idle screen. Each page has its title and Back at the top, and its actions at the bottom.",
			},
			Wide: []string{
				"Each screen has the same places. At the left, the display of the printer, and under it the key panel: Main (the idle screen), Files, Filament, Toolhead and Machine. At the right, the page that is open: its title and Back at the top, and its actions in one row at the bottom, with the main action at the right.",
			},
			Figures: fig("home")},
		{Chapter: meet, ID: "lcd", Title: "The display of the printer",
			Intro: parts["lcd"].Intro, Table: parts["lcd"].Table},
		{Chapter: meet, ID: "web", Title: "The deck in a browser", Intro: parts["web"].Intro},

		{Chapter: prints, ID: "send", Title: "Send a file from the slicer", Intro: parts["slicers"].Intro},
		{Chapter: prints, ID: "start-print", Title: "Start a print", Intro: parts["files"].Intro,
			Steps: []string{
				"Tap Print a file on the idle screen.",
				"Tap the file. Check the print time, the filament and the temperatures.",
				"Tap Print, then Print again.",
			},
			Figures: fig("files", "file", "sd-card")},
		{Chapter: prints, ID: "watch", Title: "Watch the print", Intro: parts["printing"].Intro,
			Steps: []string{
				"Watch the first layer. Use − and + to set the height of the nozzle while it prints.",
				"Tap Save to keep the Z offset for the next prints.",
				"Use Tune to change the speed, the flow or the fan while the print runs.",
				"When the print is done, wait for the bed to cool, then take the part off.",
			},
			Figures: fig("preparing", "first-layer", "printing", "tune", "paused", "pause-prompt", "finishing", "done")},
		{Chapter: prints, ID: "colors", Title: "Print in two colors",
			Intro: []string{
				"The Colors tool of the Cura plugin gives a model the roll of another color, for example raised letters on a plate. The print pauses at the first layer of that model, and the deck asks for its roll.",
				"On Resume the deck draws a short line at the front of the bed, behind the prime line of the start. The ooze of the new filament stays on that line. The nozzle then goes back to the part with the filament pulled back a little, and pushes it forward at the first point of the layer. Each change of a print draws its line 2 mm behind the line before it.",
			},
			Steps: []string{
				"In Cura, put the letters on the part. Select the letters, open the Colors tool, and tap their roll.",
				"Print. At the first layer of the letters, the printer parks and the deck shows Change filament in orange.",
				"Tap Change filament. Unload, load the roll of the letters, and tap Done.",
				"Pull the ooze off the nozzle with tweezers, then tap Resume.",
			},
			Figures: fig("color-change")},
		{Chapter: prints, ID: "away", Title: "Look around during a print",
			Intro: []string{
				"The other screens are read only for the printer while it prints: the files, the filament and the rolls, the history. A tap cannot change the print by mistake. The deck goes back to the print at a pause, at a new print, and after 2 minutes with no touch.",
			},
			Tall: []string{
				"Menu on the screens of the print shows the other screens while the print runs. The print bar at the bottom of each screen shows the print. Tap it to go back.",
			},
			Wide: []string{
				"During a print, Main is the print. The key panel opens the other pages as at any time, and Main goes back to the print.",
			},
			Figures: fig("away")},

		{Chapter: "Your iPhone and Apple Watch", ID: "phone", Title: "The iPhone and Apple Watch apps",
			Intro: []string{
				"The Printdeck app for iPhone shows the deck as its touch screen: the display of the printer, the print, the roll and the model. It can pause, resume and cancel a print, and start a file of the deck. The watch app shows the print on a blue dot-matrix display.",
				"The apps talk to the deck on the local network. The watch reaches the deck on Wi-Fi, or through the iPhone when it is near.",
			},
			Steps: []string{
				"Open Printdeck on the iPhone. It finds the deck on the network. Tap Connect.",
				"Tap Allow on the touch screen of the deck. The app gets its own key, and sends it to the watch.",
				"For push messages, open Menu, then Notifications, and tap Pair for notifications. Allow the notifications.",
				"To add the complication, edit the watch face and choose Printdeck.",
				"To add a widget, touch and hold the Home Screen or the Lock Screen, tap Edit, and choose Printdeck.",
			},
			After: []string{
				"The deck sends a push message when a print is done, when it fails, at a pause, after the first layer, and when a roll runs short. Each message is encrypted on the deck. Only the paired phone can read it. The watch shows the notifications of the iPhone. Settings, then Phones, on the deck lists the paired phones.",
			},
			Table: [][2]string{
				{"iPhone app", "The display of the printer, the keys, the roll and the model of the print. Pause, Resume and Cancel during a print. Print a file opens the files of the deck."},
				{"Watch app", "The progress, the time left and the end. Turn the Digital Crown for the model, the layer times and the filament. Tap for Pause, Resume or Cancel."},
				{"Complications", "The progress and the time left on the watch face: a rectangle, a ring of dots, a corner and a line of text."},
				{"Home Screen widgets", "The small widget shows the status of the watch. The medium widget shows the status screen of the deck."},
				{"Lock Screen widgets", "The rectangle, the circle and the line of text of the complications."},
				{"Live Activity", "During a print, the model, the time left, the bar, the end and the layer on the Lock Screen and in the Dynamic Island."},
				{"Notifications", "Print done, print failed, paused, first layer done, and a roll that runs short."},
			}},
		{Chapter: "Change the filament", ID: "filament", Title: "Change the filament", Intro: parts["filament"].Intro,
			Steps: []string{
				"Open Filament, then tap Change filament.",
				"Tap the roll that goes in, or tap New roll and weigh it. The nozzle heats for its material.",
				"Tap Unload, and hold the filament lightly while it comes out.",
				"Push the new filament into the extruder until it stops, then tap Load.",
				"Check that clean plastic of the new color comes out, then tap Done.",
			},
			Figures: fig("filament", "swap", "new-roll", "rolls", "roll", "roll-color")},
		{Chapter: "Move and heat", ID: "move", Title: "Move the head and heat",
			Steps: []string{
				"Open Toolhead, and tap Home all after each start of the printer.",
				"Hold the joystick to move the head. Use Z for the height.",
				"Tap a temperature on the idle screen to set it with the keypad.",
			},
			Figures: fig("move", "keypad")},

		{Chapter: "Level and calibrate", ID: "calibrate", Title: "Level and calibrate", Intro: parts["maintenance"].Intro,
			Steps: []string{
				"Open Machine, then Maintenance.",
				"Tap Bed tilt or Bed mesh to probe the bed. The deck shows the action until it ends.",
				"Tap Probe Z offset for the paper test. Move the nozzle down until a sheet of paper drags a little, then tap Accept.",
				"Tap PID tune after you change a heater or a fan. A PID tune runs for about 10 minutes.",
				"Tap Save config to write the results. Klipper restarts.",
				"Tap Pressure advance test to print the tower. Then tap Pressure advance, and type the height of its sharpest corners.",
			},
			Figures: fig("maintenance", "maintenance-more", "action", "paper")},

		{Chapter: care, ID: "machine", Title: "Look after the printer",
			Intro:   []string{"Machine has the information of the printer, the history of the prints, the macros, the console and the settings."},
			Figures: fig("machine", "machine-prints", "machine-more", "settings", "settings-more", "camera", "stats", "console", "monitor")},
		{Chapter: care, ID: "update", Title: "Update the Printdeck",
			Intro: []string{
				"The deck checks for a new version once a day. Machine then shows Software update in orange.",
				"Each version has a signature. The deck installs only a version with a valid signature of Printdeck. It writes the other system slot and restarts into it. If the new system does not start, the deck goes back to the old one.",
			},
			Steps: []string{
				"Open Machine, then Software update.",
				"Read the notes of the new version, then tap Install. The deck does not install during a print.",
				"Wait while the deck downloads and writes the update. It restarts into the new version.",
			},
			Figures: fig("update")},
		{Chapter: care, ID: "backup", Title: "Back up your data",
			Intro: []string{
				"A backup is one file with your data: the rolls, the print history, the settings, the slicer API keys, the paired phones and the Klipper config. It can also have the G-code files. Make a backup before you change the SD card, and keep it on a computer.",
				"A backup password encrypts the file. Without the password, nobody can open the backup, and the Printdeck cannot restore it. The password cannot be recovered, so keep it safe.",
				"The backup does not have the password of the Printdeck or the Wi-Fi network. A restore keeps the password and the Wi-Fi network of the Printdeck that you restore to.",
			},
			Steps: []string{
				"Open http://printdeck.local/system in a browser, and log in with the password of the deck.",
				"Under Backup, type a backup password of 8 characters or more two times. Select Include the G-code files to put them in the backup.",
				"Click Download the backup. The browser saves a file with the name printdeck-backup-NAME-DATE.pdbk.",
				"To restore, open the same page. Under Restore, type the backup password, choose the backup file, then click Restore. Do not restore during a print.",
				"The Printdeck checks the whole file before it changes a file, so a wrong password or a damaged file changes no data. After the restore, Klipper and the Printdeck restart.",
			}},

		{Chapter: care, ID: "simulator", Title: "Try the deck with a simulated printer",
			Intro: []string{
				"The deck can run a simulated printer in place of the real printer. Klipper then runs the config of a real printer on a board in the deck. The board heats, homes, probes and prints in real time, but no motor moves. Use it to learn the deck, or to test a file from the slicer. You need no cable and no printer.",
				"The deck keeps your printer.cfg as printer.cfg.real while the simulated printer is in use. Real printer puts it back with no change. An orange band at the top of each screen shows the simulated printer.",
			},
			Steps: []string{
				"Open Machine, then Simulated printer. A new deck with no printer has a Simulated printer key on the setup screen.",
				"Tap the printer to simulate, then Simulate. Klipper restarts with the simulated printer.",
				"Print as on a real printer: send a file from the slicer, or tap Print a file.",
				"To stop the simulation, open Machine, then Simulated printer, and tap Real printer, then Go back. Klipper restarts with your printer.cfg.",
			},
			After:   []string{"The deck does not change the printer during a print. Wait until the print ends."},
			Figures: fig("simulator")},

		{Chapter: "When something goes wrong", ID: "problems", Title: "When something goes wrong",
			Intro: []string{"When Klipper stops with an error, the deck shows all the lines of its message."},
			Steps: []string{
				"Read the message. It often names the setting in printer.cfg.",
				"After a stop or a power cut of the printer, tap Firmware restart.",
				"After a change to printer.cfg, tap Restart Klipper.",
			},
			Figures: fig("error")},
		{Chapter: "When something goes wrong", ID: "settings", Title: "Settings",
			Intro: parts["settings"].Intro, Table: parts["settings"].Table},
	}
}

func manual(args []string) {
	if len(args) < 1 {
		log.Fatal("usage: printdeck manual DIR [wide|lulzbot|cura|large]")
	}
	dir := args[0]
	mode := ""
	if len(args) > 1 {
		mode = args[1]
	}
	if mode == "cura" {
		curaBooklet(dir)
		return
	}
	wide := mode == "wide" || mode == "lulzbot"
	os.MkdirAll(filepath.Join(dir, "img"), 0o755)
	// The pictures show the firmware keys of the OS image.
	fwDir, _ := os.MkdirTemp("", "printdeck-firmware")
	defer os.RemoveAll(fwDir)
	os.WriteFile(filepath.Join(fwDir, "klipper-atmega2560.bin"), []byte{0}, 0o644)
	os.WriteFile(filepath.Join(fwDir, "version"), []byte(printer.HostVersion+"\n"), 0o644)
	board.FirmwareDir = fwDir
	a, p := demoApp()
	a.Now = demoNow
	a.Wide = wide
	if mode == "large" {
		a.TextSize = "large"
	}
	w, h := a.Screen().W, a.Screen().H
	a.Setup = &ui.DemoSetup{
		Nets: []wifi.Network{{SSID: "Extremely", Signal: -48, Secure: true}, {SSID: "Workshop", Signal: -63, Secure: true},
			{SSID: "Guest", Signal: -71}, {SSID: "NETGEAR-5G", Signal: -82, Secure: true}},
		Status_: wifi.Status{State: "COMPLETED", SSID: "Extremely", IP: "192.168.76.119"},
	}
	a.Updater = &ui.DemoUpdater{S: ui.UpdateStatus{Current: "v0.3.0", Latest: "v0.4.0", Newer: true, Checked: true,
		Notes: demoNotes}}
	st := &manualState{a: a, p: p, part: partPreview(0.42), done: partPreview(1)}
	secs := manualSections()
	// The paragraphs and the figures of this layout.
	for i := range secs {
		sec := &secs[i]
		if wide {
			sec.Intro = append(sec.Intro, sec.Wide...)
		} else {
			sec.Intro = append(sec.Intro, sec.Tall...)
		}
		var figs []figure
		for _, f := range sec.Figures {
			if f.Layout == "" || f.Layout == map[bool]string{true: "wide", false: "tall"}[wide] {
				figs = append(figs, f)
			}
		}
		sec.Figures = figs
	}
	const k = 2 // the pictures are drawn at twice the size
	for si := range secs {
		for fi := range secs[si].Figures {
			f := &secs[si].Figures[fi]
			*p = *demoPrinterCopy(p)
			p.Img = st.part
			a.ResetScreens()
			f.Setup(st)
			c, rec := gfx.NewRecording(w, h)
			a.Draw(c)
			// The callouts of the parts of this layout.
			if wide {
				f.Callouts = append(f.Callouts, f.Wide...)
			} else {
				f.Callouts = append(f.Callouts, f.Tall...)
			}
			// Each callout points at the middle of its part, wherever the layout puts it.
			parts := make([]image.Rectangle, len(f.Callouts))
			for i, co := range f.Callouts {
				r, ok := keysRect(a, co.Title)
				if !ok && mode == "large" {
					// The large text can push a part off the screen, into a list that scrolls.
					continue
				}
				if !ok {
					log.Fatalf("manual: %s: the screen has no part %q", f.Name, co.Title)
				}
				parts[i] = r
			}
			f.Badges = layoutBadges(parts, a.Parts(), w, h)
			out := gfx.New(w*k, h*k)
			if err := gfx.ReplayScaled(out, rec.JSON(), gfx.ImageByID, k); err != nil {
				log.Fatal(err)
			}
			writePNG(filepath.Join(dir, "img", f.Name+".png"), out.RGBA)
		}
	}
	// The pictures of the printer display.
	for name, scr := range map[string]*lcd.Screen{
		"lcd-status": {Status: &lcd.Status{State: "READY PLA", Clock: "2:24PM", Message: "XYZ HOMED", Tool: 24, Bed: 23,
			Position: "X110 Y110 Z5.00", Hint: "Click for menu"}},
		"lcd-printing": {Status: &lcd.Status{State: "PRINTING", Clock: "2:24PM", Tool: 209, ToolT: 210, Bed: 60, BedT: 60,
			Fan: 1, Printing: true, Progress: 0.42, Elapsed: "0:58", Left: "1:23", Done: "3:47PM"}},
		"lcd-home": {Menu: &lcd.Menu{Title: "Printdeck", Carousel: true, Selected: 1, Items: []lcd.MenuItem{
			{Label: "Status", Icon: "status"}, {Label: "Print a file", Icon: "print"}, {Label: "Preheat PLA", Icon: "preheat"}}}},
		"lcd-list": {Menu: &lcd.Menu{Title: "Maintenance", Selected: 2, Items: []lcd.MenuItem{{Label: "Back", Icon: "back"},
			{Label: "Bed tilt", Icon: "bed"}, {Label: "Probe Z offset", Icon: "probe"}, {Label: "PID tune", Icon: "pid"},
			{Label: "Nozzle change", Icon: "maint"}, {Label: "Firmware", Icon: "chip"}, {Label: "Save config", Icon: "save"}}}},
		"lcd-action": {Anim: "work", Title: "Bed tilt", Caption: "Running 0:42"},
	} {
		writePNG(filepath.Join(dir, "img", name+".png"), scr.Image(3, 3))
	}
	tpl := template.Must(template.New("manual").Funcs(template.FuncMap{
		"pct":        func(v, of int) string { return fmt.Sprintf("%.2f%%", 100*float64(v)/float64(of)) },
		"inc":        func(i int) int { return i + 1 },
		"W":          func() int { return w },
		"G":          func() int { return gutter },
		"VW":         func() int { return w + 2*gutter },
		"H":          func() int { return h },
		"date":       func() string { return time.Now().Format("January 2006") },
		"newChapter": func(i int) bool { return i == 0 || secs[i].Chapter != secs[i-1].Chapter },
	}).Parse(manualHTML))
	ed := plainEdition
	if mode == "lulzbot" {
		*p = *demoPrinterCopy(p)
		p.State_ = stIdle
		ed = lulzEdition(dir, a, w, h)
	}
	writeBooklet(dir, secs, w, h, ed)
	f, err := os.Create(filepath.Join(dir, "index.html"))
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := tpl.Execute(f, map[string]any{"Sections": secs, "Version": osVersion(), "Wide": wide}); err != nil {
		log.Fatal(err)
	}
}

// demoPrinterCopy keeps the fixed data of the demo printer (machine, history, files) for each
// picture, and resets its state.
func demoPrinterCopy(p *ui.DemoPrinter) *ui.DemoPrinter {
	c := *p
	c.View = -1
	return &c
}

func writePNG(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, img)
}
