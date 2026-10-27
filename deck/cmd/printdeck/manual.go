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
	"printdeck/internal/camera"
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
						{"Networks", "Tap your network, then type its password."},
						{"Scan", "Scans for the networks again."},
						{"Hidden", "For a hidden network: type its name and its password."},
						{"Next", "Goes to the summary when the deck is connected."},
					}},
				{Name: "setup-done", Caption: "The setup: step 2",
					Setup: func(s *manualState) { s.a.OpenSetupScreen("done") },
					Callouts: []callout{
						{"Address", "The address of the deck. Open it in a browser."},
						{"Password", "The password of the web pages. Machine > Wi-Fi and help shows it again."},
						{"Finish", "Closes the setup. The setup shows only at the first start."},
					},
					Notes: []string{"To set the Wi-Fi before the first start, put the SD card in a computer. Fill in printdeck.txt on the PRINTDECK partition."}},
			}},
		{ID: "home", Title: "Print: the idle screen",
			Intro: []string{"When the printer does not print, the deck shows the idle screen. The largest key is the action that you need most: Print a file."},
			Figures: []figure{
				{Name: "home", Caption: "The idle screen", Setup: idle,
					Callouts: []callout{
						{"Nozzle", "The nozzle temperature and its target. Tap it to set the temperature."},
						{"Bed", "The same for the bed."},
						{"Fan", "The part cooling fan. Its blades turn at its speed."},
						{"Status", "The head position, the printer state, the loaded filament and the clock."},
						{"Print a file", "Opens the files. See Files."},
					},
					Tall: []callout{
						{"Preheat", "Heats the nozzle and the bed for the loaded filament. Tap it again to turn them off."},
						{"Cool down", "Turns off both heaters."},
						{"Home all, X, Y, Z", "Homes all axes, or one axis. A lit LED shows a homed axis."},
						{"Last print", "The last print and its result. Reprint starts it again."},
						{"Pages", "Filament, Toolhead and Machine."},
					},
					Wide: []callout{
						{"Home all, X, Y, Z", "Homes all axes, or one axis. A lit LED shows a homed axis."},
						{"Last print", "The last print and its cost. Reprint starts it again. Preheat and Cool down are on the Filament page."},
						{"Model", "Tap it to open the print in the history."},
						{"Pages", "The lit LED shows the open page."},
					}},
				{Name: "keypad", Caption: "The keypad of a temperature",
					Setup: func(s *manualState) { s.p.State_ = stIdle; s.a.OpenPrintScreen("keypad") },
					Callouts: []callout{
						{"Value", "The value."},
						{"Presets", "Off, and the material temperatures."},
						{"Keys", "C clears, DEL removes a digit."},
						{"Set", "Sets the heater."},
					}},
			}},
		{ID: "files", Title: "Files",
			Intro: []string{
				"The newest file is at the top. Each file shows the print time, the filament and the layer height from the slicer. Cura, PrusaSlicer, OrcaSlicer and Bambu Studio write this data.",
			},
			Figures: []figure{
				{Name: "files", Caption: "The file list", Setup: page("files"),
					Callouts: []callout{
						{"Count", "The number of files."},
						{"Picture", "The thumbnail."},
						{"File", "The name and the slicer data."},
						{"Date", "When the file arrived."},
						{"On the deck, SD card", "The files on the deck, or on the SD card of the printer display ([printdeck_sdcard])."},
					},
					Notes: []string{"Tap a file to open it."}},
				{Name: "sd-card", Caption: "The SD card of the printer", Setup: page("sd-card"),
					Callouts: []callout{
						{"On the deck, SD card", "The files on the card, or on the deck."},
						{"Count", "The open folder."},
						{"Folder", "Tap a folder to open it."},
						{"File", "Tap a file to copy it to the deck."},
					},
					Notes: []string{"Stop ends the copy."}},
				{Name: "file", Caption: "A file", Setup: page("file"),
					Callouts: []callout{
						{"Picture", "The thumbnail."},
						{"Slicer data", "The data from the slicer."},
						{"Print", "Starts the print."},
						{"Delete", "Removes the file."},
					}},
			}},
		{ID: "printing", Title: "Print: during a print",
			Intro: []string{"The deck shows a screen for each phase of the print."},
			Figures: []figure{
				{Name: "preparing", Caption: "Preparing",
					Setup: func(s *manualState) {
						s.p.State_ = printer.State{Reachable: true, Text: "Printing", Connected: true, Printing: true, File: "benchy.gcode",
							Activity: "Heating bed  52/60°", Preparing: true, HasWipe: true, Tool: 24, Bed: 52, BedTarget: 60}
						s.a.OpenPrintScreen("")
					},
					Callouts: []callout{
						{"Display", "The printer display. Tap the nozzle or the bed to set its temperature."},
						{"Steps", "The steps of the start macro. The step that runs has a lit LED."},
						{"Help", "The network, the address and the password."},
						{"Cancel print", "Stops the print."},
					},
					Tall: []callout{
						{"Menu", "Opens the other screens."},
					}},
				{Name: "first-layer", Caption: "The first layer",
					Setup: func(s *manualState) {
						s.p.State_ = printer.State{Reachable: true, Text: "Printing", Connected: true, Printing: true, File: "benchy.gcode",
							Activity: "Printing", Layer: 1, Layers: 27, Completion: 2, HasTimeLeft: true, TimeLeft: 100 * time.Minute,
							ZOffset: 0.04, Tool: 210, ToolTarget: 210, Bed: 60, BedTarget: 60}
						s.a.OpenPrintScreen("")
					},
					Callouts: []callout{
						{"Display", "The printer display. Tap the nozzle or the bed to set its temperature."},
						{"− and +", "A round, thin line needs − (closer). A flat, rough line needs + (farther)."},
						{"Z offset", "The nozzle height above its normal height."},
						{"Save", "Keeps the Z offset for the next prints."},
						{"Step", "How far one tap moves the nozzle, in mm."},
						{"Pause, Cancel", "Pauses the print, or stops it after a question."},
						{"Help", "The network, the address and the password."},
						{"Layer", "The first layer of the model."},
					},
					Tall: []callout{
						{"Menu", "Opens the other screens."},
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
						{"Display", "The printer display. Tap the nozzle or the bed to set its temperature."},
						{"Pause", "Pauses the print."},
						{"Tune", "Changes the speed, the flow and the fan."},
						{"Cancel", "Stops the print."},
						{"Layer", "The layer now and the head speed."},
						{"Roll", "The roll in the printer and its filament."},
						{"Layer times", "A bar for each layer. Done layers are dark, the layer now is orange, and the next layers are light."},
						{"Model", "The model up to the layer now."},
						{"Layer slider", "Drag it to see another layer. Live goes back to the layer that prints."},
					},
					Tall: []callout{
						{"Menu", "Opens the other screens."},
					},
					Wide: []callout{
						{"Pages", "During a print, Main goes back to the print."},
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
						{"Where", "The message of the file."},
						{"Check the height", "A paper test on the part."},
						{"Resume", "Primes the nozzle and continues."},
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
						{"Where", "The roll of the next color."},
						{"Change filament", "Opens the filament change with that roll."},
						{"Resume", "Primes the nozzle at the front of the bed, then continues."},
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
						{"Steps", "The steps of the end macro."},
						{"Model", "The finished model."},
					},
					Tall: []callout{
						{"Menu", "Opens the other screens while the end macro runs."},
					}},
				{Name: "away", Caption: "Filament during a print",
					Setup: func(s *manualState) {
						s.p.State_ = stPrinting
						s.a.OpenPrintScreen("filament")
						s.a.DemoAway()
					},
					Callouts: []callout{
						{"Roll", "The roll in the printer and its filament."},
						{"Lasts", "The filament left after the print. Red text shows a roll that runs out."},
						{"Change filament", "Pauses the print and opens the filament change. Then tap Resume."},
					},
					Tall: []callout{
						{"Print bar", "Tap the bar to go back to the print."},
					},
					Wide: []callout{
						{"Pages", "The key panel opens each page."},
					}},
				{Name: "tune", Caption: "Tune",
					Setup: func(s *manualState) { s.p.State_ = stPrinting; s.a.OpenPrintScreen("tune") },
					Callouts: []callout{
						{"Help", "The network, the address and the password."},
						{"Speed", "The print speed, in percent of the slicer speed."},
						{"Flow", "The amount of plastic, in percent."},
						{"Z offset", "The nozzle height above its normal height. Save keeps it."},
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
						{"Display", "The printer display. Tap the nozzle or the bed to set its temperature."},
						{"Where", "The layer and the message."},
						{"Resume", "Continues the print."},
						{"Change filament", "For a new color."},
						{"Move head", "Opens Toolhead."},
						{"Tune", "Changes the print."},
						{"Cancel", "Stops the print."},
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
						{"Result", "Done, Cancelled or Failed."},
						{"Bed", "The time until the part comes off easily (35°)."},
						{"Reprint", "Prints the file again."},
						{"Done", "Closes this screen."},
					}},
			}},
		{ID: "filament", Title: "Filament",
			Intro: []string{"Set the loaded filament here, so Preheat uses its temperatures. The deck keeps a list of your rolls. Each print subtracts its filament from the roll in the printer."},
			Figures: []figure{
				{Name: "filament", Caption: "The Filament page", Setup: page("filament"),
					Callouts: []callout{
						{"Rolls", "Opens the list of your rolls."},
						{"Loaded", "The roll or the material in the printer."},
						{"Material", "Tap the material in the printer."},
						{"Change filament", "The guided change: choose, heat, unload, insert, load, check."},
						{"Load, Unload", "Only the load, or only the unload."},
						{"Extrude, Retract", "Pushes or pulls 10 mm. The nozzle must be at 170° or more."},
						{"Heat, Cool down", "Heats the nozzle, or turns the heaters off."},
					}},
				{Name: "swap", Caption: "The guided change, step 1",
					Setup: func(s *manualState) { s.p.State_ = stIdle; s.a.OpenPrintScreen("swap") },
					Callouts: []callout{
						{"Step", "The step now."},
						{"Rolls", "Tap the roll that goes in."},
						{"New roll", "Adds a roll and weighs it."},
						{"Other", "A filament with no roll."},
						{"Close", "Stops the change and turns off the nozzle."},
					}},
				{Name: "new-roll", Caption: "A new roll",
					Setup: func(s *manualState) { s.p.State_ = stIdle; s.a.OpenPrintScreen("new-roll") },
					Callouts: []callout{
						{"Material", "The material of the roll."},
						{"Name", "For example the color and the brand."},
						{"On the scale", "The weight of the roll with its reel."},
						{"Empty reel", "The weight of an empty reel of the same brand."},
						{"Price", "The roll price, for the cost of each print."},
						{"Save", "Adds the roll as the loaded roll."},
					}},
				{Name: "rolls", Caption: "The rolls", Setup: page("rolls"),
					Callouts: []callout{
						{"New roll", "Adds a roll with no filament change."},
					}},
				{Name: "roll", Caption: "One roll", Setup: page("roll"),
					Notes: []string{"Each roll has a number, for example #3. For QR code labels, open printdeck.local/rolls/labels and print the sheet on 2 inch round labels (Avery 22807). A phone camera on a label opens the page of the roll. Load there opens the filament change on the deck."},
					Callouts: []callout{
						{"Details", "The filament left, the filament used, and the price."},
						{"Color", "Tap it to choose a color. The print screen and Cura show it."},
						{"Full roll", "The filament of a full roll: 1 kg by default. Tap it to change it."},
						{"Left", "The part of a full roll that is left."},
						{"Prints", "Tap it for the prints that used the roll."},
						{"Weigh again", "Sets the filament from a new weight."},
						{"Remove", "Removes an empty roll from the list."},
					}},
				{Name: "roll-color", Caption: "The color of a roll",
					Setup: func(s *manualState) { page("roll")(s); s.a.DemoPickColor("#1f2a5a") },
					Notes: []string{"Tap a color to set it. The Cura plugin gives the roll material in Cura the same color."},
					Callouts: []callout{
						{"Colors", "Tap a color."},
						{"No color", "Removes the color."},
					}},
			}},
		{ID: "move", Title: "Toolhead",
			Figures: []figure{
				{Name: "move", Caption: "The Toolhead page", Setup: page("move"),
					Callouts: []callout{
						{"Position", "A lit LED shows a homed axis."},
						{"Home", "Home after each start of the printer."},
						{"Joystick", "Hold a finger on the pad to move the head in X and Y."},
						{"Z", "Moves Z by the step: 0.1, 1 or 10 mm."},
						{"Park, Center, Motors off", "Park lifts the nozzle 10 mm and moves the head to the back left. Center moves it to the bed center. Motors off frees the head."},
					}},
			}},
		{ID: "maintenance", Title: "Maintenance",
			Intro: []string{"Maintenance lists the calibrations of your printer. Open it from Machine."},
			Figures: []figure{
				{Name: "maintenance", Caption: "The Maintenance page", Setup: page("maintenance"),
					Callouts: []callout{
						{"Bed tilt", "Probes the bed and corrects its tilt. A printer with a bed mesh shows Bed mesh."},
						{"Probe Z offset", "The paper test for the nozzle height."},
						{"PID tune", "Heats the nozzle to 210° and the bed to 60°, and tunes each heater."},
						{"Nozzle change", "Heats the nozzle to 250° and raises the head."},
						{"Firmware", "Writes the Klipper firmware. Orange text shows old firmware."},
						{"Save config", "Writes the results into printer.cfg. Klipper restarts. Orange text shows unsaved results."},
					},
					Tall: []callout{
						{"Pressure advance test", "Prints a 30 mm tower. The pressure advance goes up 0.005 for each mm. Measure the height of the sharpest corners."},
						{"Pressure advance", "Type the height of the sharpest corners. The deck writes the value in printer.cfg."},
					}},
				{Name: "maintenance-more", Caption: "The Maintenance page, further down", Layout: "wide",
					Setup: func(s *manualState) { page("maintenance")(s); s.a.DemoScroll(9999) },
					Callouts: []callout{
						{"Pressure advance test", "Prints a 30 mm tower. The pressure advance goes up 0.005 for each mm. Measure the height of the sharpest corners."},
						{"Pressure advance", "Type the height of the sharpest corners. The deck writes the value in printer.cfg."},
					}},
				{Name: "flash", Caption: "The firmware flash", Setup: page("flash"),
					Notes: []string{"The deck stops Klipper, writes the firmware, checks it, and starts Klipper again. Printdeck flashes atmega2560 boards with an STK500v2 bootloader, for example RAMBo and RAMPS."},
					Callouts: []callout{
						{"Step", "Starting the bootloader, Writing, then Checking."},
						{"Progress", "The part of the firmware that is done."},
					}},
				{Name: "action", Caption: "An action runs", Setup: page("action"),
					Notes: []string{"The other keys are locked while an action runs. If the action fails, the deck shows the Klipper error until you tap OK."},
					Callouts: []callout{
						{"Action", "The time since the start."},
						{"Stop the printer", "Stops the printer at once. Then tap Firmware restart."},
					}},
				{Name: "paper", Caption: "The paper test",
					Setup: func(s *manualState) {
						s.p.State_ = printer.State{Reachable: true, Text: "Operational", Connected: true, ManualProbe: true, ManualZ: 0.425}
						s.a.OpenPrintScreen("")
					},
					Callouts: []callout{
						{"Height", "The height of the nozzle now."},
						{"Steps", "Moves the nozzle down (−) or up (+)."},
						{"Accept", "Keeps the height. Then tap Save config."},
						{"Abort", "Stops the test with no change."},
					},
					Notes: []string{"This screen opens by itself for each paper test, also from a macro."}},
			}},
		{ID: "machine", Title: "Machine",
			Figures: []figure{
				{Name: "machine", Caption: "The Machine page", Setup: page("machine"),
					Callouts: []callout{
						{"Information", "The versions, the address and the totals."},
						{"Maintenance", "The calibrations and the firmware."},
						{"Macros", "Tap a macro to run it."},
						{"Console", "The G-code console."},
						{"Restart Klipper", "Reads printer.cfg again."},
						{"Firmware restart", "Restarts the board, for example after a stop."},
					},
					Tall: []callout{
						{"Simulated printer", "Runs a simulated printer."},
						{"Prints", "The totals and the history of all prints."},
					},
					Notes: []string{"Drag the list up to see more rows."}},
				{Name: "machine-prints", Caption: "The Machine page, further down", Layout: "wide",
					// The list scrolled to the fourth row of the printer: 24 + 5 + 3 × (60 + 5) pixels.
					Setup: func(s *manualState) { s.p.State_ = stIdle; s.a.OpenPrintScreen("machine"); s.a.DemoScroll(224) },
					Callouts: []callout{
						{"Simulated printer", "Runs a simulated printer."},
						{"Prints", "The totals and the history of all prints."},
					}},
				{Name: "machine-more", Caption: "The Machine page, at the bottom",
					Setup: func(s *manualState) { s.p.State_ = stIdle; s.a.OpenPrintScreen("machine"); s.a.DemoScroll(9999) },
					Callouts: []callout{
						{"Settings", "The theme, the clock, the screen, the cameras and the messages."},
						{"Software update", "Shows and installs a new version."},
						{"Wi-Fi and help", "The network, the password and the addresses."},
						{"System monitor", "Graphs of the CPU, the memory and the SD card."},
					}},
				{Name: "settings", Caption: "The Settings page",
					Setup: settingsSetup,
					Callouts: []callout{
						{"Theme", "Day, night, or auto."},
						{"Clock", "12 or 24 hours."},
						{"Home in a filament change", "On: the printer homes while the nozzle heats for a filament change."},
						{"Screen", "Tap it to turn the screen a quarter turn. The deck restarts, and a print continues. This setting overrides display_rotate."},
						{"Camera", "Opens the live picture."},
						{"Cameras", "Names the cameras and sets their uses."},
					},
					Tall: []callout{
						{"Phones", "The phones that get push messages. Tap a phone in the list to remove it."},
						{"Notifications", "Push messages through ntfy. In the ntfy app, subscribe to a topic with a name that is hard to guess. Type the same topic here. An empty topic turns the messages off."},
						{"Text", "The size of the small text: auto, large, or normal."},
					}},
				{Name: "settings-more", Caption: "The Settings page, at the bottom", Layout: "wide",
					Setup: func(s *manualState) { settingsSetup(s); s.a.DemoScroll(9999) },
					Callouts: []callout{
						{"Phones", "The phones that get push messages. Tap a phone in the list to remove it."},
						{"Notifications", "Push messages through ntfy. In the ntfy app, subscribe to a topic with a name that is hard to guess. Type the same topic here. An empty topic turns the messages off."},
					}},
				{Name: "camera", Caption: "The Camera page",
					Setup: func(s *manualState) {
						s.p.State_ = stIdle
						s.a.Camera = manualCameras()
						s.a.OpenPrintScreen("camera")
					},
					Callouts: []callout{
						{"Tabs", "A tab for each camera that is on. Both shows all the cameras."},
						{"Picture", "Use it to aim the camera at the bed."},
						{"Turn", "Turns the picture 180°."},
						{"Camera settings", "The name, on or off, and the uses of this camera."},
					},
					Notes: []string{"A camera runs only while somebody looks at it. With no camera, the apps show the deck screen.",
						"If a camera shows too little USB bandwidth, move it to another USB port."}},
				{Name: "cameras", Caption: "The Cameras page",
					Setup: func(s *manualState) {
						s.p.State_ = stIdle
						s.a.Camera = manualCameras()
						s.a.OpenPrintScreen("cameras")
					},
					Callouts: []callout{
						{"Front", "Each camera, with its uses. Tap a camera to change it."},
					}},
				{Name: "camera-settings", Caption: "The settings of one camera",
					Setup: func(s *manualState) {
						s.p.State_ = stIdle
						s.a.Camera = manualCameras()
						s.a.OpenPrintScreen("camera-settings")
					},
					Callouts: []callout{
						{"Name", "Type a name, such as Top."},
						{"On", "Turns the camera on or off. Its uses move to another camera."},
						{"Apps", "Cura and the apps show this camera first."},
						{"Time lapse", "The camera of the time lapse."},
						{"Pictures", "The first layer picture, and the pictures on the phone."},
					},
					Notes: []string{"Each use is on one camera. To move a use, check it on the other camera."}},
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
						{"Replies", "Your commands, the replies, and errors in red."},
						{"Type a command", "Opens the keyboard."},
						{"Quick keys", "Home (G28), Status (STATUS) and Help (HELP)."},
					}},
				{Name: "simulator", Caption: "A simulated printer in use",
					Setup: func(s *manualState) {
						s.p.State_ = stIdle
						s.p.State_.Simulated = "LulzBot TAZ Workhorse"
						s.a.OpenPrintScreen("simulator")
					},
					Callouts: []callout{
						{"Simulated", "An orange band shows a simulated printer."},
						{"Real printer", "Puts your printer.cfg back and restarts Klipper."},
						{"Simulated LulzBot TAZ Workhorse", "A probe on four washers, and bed tilt. Here it is in use."},
						{"Simulated LulzBot Mini 2", "A probe on four washers, and bed tilt."},
						{"Simulated Creality CR-10 V3", "Endstops on each axis, and no probe."},
					}},
				{Name: "monitor", Caption: "The System monitor", Setup: page("monitor"),
					Callouts: []callout{
						{"CPU", "The CPU use."},
						{"Memory", "The memory in use."},
						{"Writes", "The SD card writes a minute."},
						{"1 h", "10 minutes, 1 hour or 24 hours."},
					},
					Notes: []string{"The samples stay in memory. A restart clears them."}},
				{Name: "update", Caption: "Software update", Setup: page("update"),
					Callouts: []callout{
						{"Versions", "This version and the newest version."},
						{"Notes", "What the new version changes."},
						{"Install", "Installs the new version, after a question. The deck restarts."},
						{"Check now", "Checks for a new version now."},
						{"Beta updates", "On: the deck also gets beta versions."},
					}},
				{Name: "stats", Caption: "Statistics", Setup: page("stats"),
					Callouts: []callout{
						{"Totals", "The prints, the success rate, the time and the filament."},
						{"Results", "Done (amber), cancelled and failed (red)."},
						{"Details", "The filament, the average and longest print, and the recent prints."},
						{"History", "Each print with its result. Tap a print to see it or print it again."},
					}},
			}},
		{ID: "web", Title: "The deck in the browser",
			Intro: []string{
				"Open http://printdeck.local in a browser on the same network. Log in with the password of the deck. The browser shows the screens of the touch screen. Each browser has its own screens.",
				"On a phone, tap Add to Home Screen. The deck then opens as an app.",
				"The page http://printdeck.local/system shows the version, the logs and the API key for slicers. It also installs an update file, and makes a backup. See Back up your data.",
			}},
		{ID: "lcd", Title: "The display of the printer",
			Intro: []string{
				"The deck draws the printer display. The display shows the same status as the deck.",
				"Turn or click the knob to open the menu. The menu has the same pages and keys as the touch screen. A click taps the key. The top item goes back, and a long click also goes back. After 20 seconds with no turn, the display shows the status screen again.",
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
				"The deck uses the OctoPrint API, so slicers can send files and start prints.",
				"Cura: add the deck with the OctoPrint Connection plugin. The deck asks \"Allow Cura to use the Printdeck?\". Tap Allow.",
				"OrcaSlicer or PrusaSlicer: add a printer of the type OctoPrint, with the address http://printdeck.local and the API key of the system page.",
				"The webcam is the camera with the use Apps: http://printdeck.local/webcam/stream and /webcam/snapshot, with the API key. Add camera=2 for the second camera, w=320 for smaller pictures, or fps=2 for fewer frames. With no camera, the webcam shows the deck screen.",
			}},
		{ID: "settings", Title: "Settings",
			Intro: []string{"The file printdeck.txt on the PRINTDECK partition holds the settings. Put the SD card in a computer to change it. The deck reads it at each start, and clears the Wi-Fi password in it."},
			Table: [][2]string{
				{"wifi_ssid, wifi_password, wifi_country", "The Wi-Fi network"},
				{"hostname, timezone", "The name on the network, and the time zone"},
				{"display_rotate", "The screen turn: 90 or 270 turns a wide display to tall, 0 or 180 keeps the native shape. Empty is 0 for a tall display (the Touch Display 2) and 270 for a wide display"},
				{"touch_invert_x, touch_invert_y, touch_swap_xy", "1 turns an axis of the touch screen"},
				{"preheat_tool, preheat_bed", "The temperatures of Preheat with no filament set"},
				{"night_from, night_to", "The hours of the night colors"},
				{"printer.cfg (a file)", "A Klipper config. The deck imports it at the next start."},
			}},
		{ID: "problems", Title: "Problems",
			Figures: []figure{
				{Name: "setup", Caption: "No printer yet",
					Setup: func(s *manualState) { s.a.OpenPrintScreen("setup") },
					Callouts: []callout{
						{"Board", "The board on USB and its firmware."},
						{"Choose printer", "Opens the list of Klipper example configs."},
						{"Search again", "Looks for the board again."},
						{"Simulated printer", "Runs a simulated printer, with no real printer."},
						{"Your own config", "Or copy your printer.cfg to the PRINTDECK partition, and restart."},
						{"Wi-Fi and help", "The network, the address and the password."},
						{"Software update", "Installs a new version. It can fix the problem."},
					}},
				{Name: "configs", Caption: "Choose your printer",
					Setup: func(s *manualState) { s.a.OpenPrintScreen("configs") },
					Callouts: []callout{
						{"Chip", "The printers with the chip of your board."},
						{"Printer", "Tap your printer. The deck writes printer.cfg and restarts Klipper. The old file stays as printer.cfg.bak."},
						{"Show all printers", "Shows the printers for all chips."},
					}},
				{Name: "error", Caption: "A Klipper error",
					Setup: func(s *manualState) { s.a.OpenPrintScreen("error") },
					Callouts: []callout{
						{"Message", "The Klipper error."},
						{"Restart Klipper", "Reads printer.cfg again."},
						{"Wi-Fi and help", "The network, the address and the password."},
						{"Software update", "Installs a new version. It can fix the problem."},
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
	s.a.Camera = manualCameras()
}

// manualCameras are the two demo cameras with the names of the manual: Front, with the use Apps,
// and Top, with the time lapse and the pictures.
func manualCameras() *ui.DemoCamera {
	c := ui.NewDemoCamera()
	front, top := c.Cams[0].Dev.ID, c.Cams[1].Dev.ID
	c.SetSettings(camera.Settings{Cameras: []camera.CamSettings{{ID: front, Name: "Front"}, {ID: top, Name: "Top"}},
		Main: front, Lapse: top, Picture: top})
	return c
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
			Steps: []string{
				"Switch on the Printdeck. The setup opens.",
				"Tap your Wi-Fi network, then type its password.",
				"Wait until the deck is connected, then tap Next.",
				"Write down the address and the password, then tap Finish.",
			},
			Figures: fig("setup-wifi", "setup-done")},
		{Chapter: setup, ID: "printer", Title: "Choose your printer",
			Intro: []string{"A new Printdeck has no printer config, so it shows the printer setup."},
			Steps: []string{
				"Connect the printer to the Printdeck with a USB cable, and switch the printer on.",
				"Wait until the deck finds the board. If the board is missing, tap Search again.",
				"Tap Choose printer. The printers of the brand of your board are at the top.",
				"Tap your printer, then Use. Klipper restarts with its config.",
			},
			Figures: fig("setup", "configs")},
		{Chapter: setup, ID: "firmware", Title: "Install the Klipper firmware",
			Intro: []string{"The printer board must run the Klipper firmware of this Printdeck."},
			Steps: []string{
				"Open Machine, then Maintenance, then tap Firmware. A new board shows Install Klipper firmware on the setup screen.",
				"Tap Write. Keep the printer on until the deck shows the result.",
				"Tap OK. Klipper restarts and connects to the board.",
			},
			Figures: fig("flash")},

		{Chapter: meet, ID: "deck", Title: "The deck",
			Intro: []string{
				"The deck shows the idle screen when no print runs. A key with an LED is on while its LED is lit. A key with no LED does an action.",
			},
			Tall: []string{
				"The printer display is at the top of each screen. The page keys are at the bottom of the idle screen. Each page has Back at the top and its actions at the bottom.",
			},
			Wide: []string{
				"The printer display and the key panel are at the left. The open page is at the right, with Back at the top and its actions at the bottom.",
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
				"Watch the first layer. Use − and + to set the nozzle height.",
				"Tap Save to keep the Z offset for the next prints.",
				"Use Tune to change the speed, the flow or the fan.",
				"When the print is done, wait for the bed to cool. Then remove the part.",
			},
			Figures: fig("preparing", "first-layer", "printing", "tune", "paused", "pause-prompt", "finishing", "done")},
		{Chapter: prints, ID: "colors", Title: "Print in two colors",
			Intro: []string{
				"The Colors tool of the Cura plugin gives a model a roll of another color. The print pauses at the first layer of that model.",
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
				"During a print, the other screens are read only. The deck goes back to the print after 2 minutes with no touch.",
			},
			Tall: []string{
				"Tap Menu to open the other screens. Tap the print bar at the bottom to go back to the print.",
			},
			Wide: []string{
				"During a print, Main is the print. The key panel opens the other pages.",
			},
			Figures: fig("away")},
		{Chapter: prints, ID: "timelapse", Title: "Make a time lapse",
			Intro: []string{
				"The deck camera captures a picture at each layer change. At the end of the print, the deck makes a video.",
				"For each picture, the printer retracts, lifts the nozzle and parks the head at the back left. On the TAZ Workhorse the nozzle goes to Z 10 mm or more, over the bed washers. Each picture adds a few seconds.",
			},
			Steps: []string{
				"Aim the camera: Settings, then Camera.",
				"Open Settings, then Time lapse.",
				"Tap Time lapse to turn it on.",
				"With two cameras, open Settings, then Cameras. Tap a camera, and check Time lapse or Pictures.",
				"Tap First layer for a picture of the first layer, or a picture and a pause. At a pause, check the layer, then tap Resume.",
				"Print. After the first layer, the deck shows its picture.",
				"At the end of the print, open http://printdeck.local/system in a browser. The Time lapse part has the video. Click its name to download it.",
			},
			After: []string{
				"Cura, PrusaSlicer and OrcaSlicer write the layer comments that the time lapse needs.",
				"Park for the frames is on by default. Off keeps the toolhead in the pictures, with no stops.",
				"The video is a .mov file. The deck keeps the last 3 videos. A backup excludes the videos.",
			}},

		{Chapter: "Your iPhone and Apple Watch", ID: "phone", Title: "The iPhone and Apple Watch apps",
			Intro: []string{
				"The iPhone app shows the deck and controls the print. The watch app shows the print.",
				"The apps use the local network. The watch connects on Wi-Fi, or through the iPhone.",
			},
			Steps: []string{
				"Open Printdeck on the iPhone. It finds the deck on the network. Tap Connect.",
				"Tap Allow on the deck.",
				"For push messages, open Menu, then Notifications, and tap Pair for notifications. Allow the notifications.",
				"To add the complication, edit the watch face and choose Printdeck.",
				"To add a widget, touch and hold the Home Screen or the Lock Screen, tap Edit, and choose Printdeck.",
			},
			After: []string{
				"The deck sends a push message when a print ends or pauses, after the first layer, and when a roll runs short. Settings, then Phones, lists the paired phones.",
			},
			Table: [][2]string{
				{"iPhone app", "The deck screen and its keys. With two cameras, tap a camera name on the camera page, or swipe, to switch. The watch shows the same camera."},
				{"Watch app", "The progress and the time left. Tap for Pause, Resume or Cancel."},
				{"Complications", "The progress on the watch face."},
				{"Home Screen widgets", "The status of the print."},
				{"Lock Screen widgets", "The same as the complications."},
				{"Live Activity", "The print on the Lock Screen and in the Dynamic Island."},
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
			Figures: fig("machine", "machine-prints", "machine-more", "settings", "settings-more", "camera", "cameras", "camera-settings", "stats", "console", "monitor")},
		{Chapter: care, ID: "update", Title: "Update the Printdeck",
			Intro: []string{
				"The deck checks for a new version once a day. Machine then shows Software update in orange.",
				"The deck installs only a signed version. If the new system fails to start, the deck goes back to the old one.",
			},
			Steps: []string{
				"Open Machine, then Software update.",
				"Read the notes of the new version, then tap Install. The deck installs only when no print runs.",
				"Wait for the download. The deck restarts into the new version.",
			},
			Figures: fig("update")},
		{Chapter: care, ID: "backup", Title: "Back up your data",
			Intro: []string{
				"A backup is one file with your rolls, history, settings, API keys, paired phones and Klipper config. Make a backup before you change the SD card.",
				"Keep the backup password safe. Without it, no one can restore the backup.",
				"The backup excludes the deck password and the Wi-Fi network.",
			},
			Steps: []string{
				"Open http://printdeck.local/system in a browser, and log in with the password of the deck.",
				"Under Backup, type a backup password of 8 characters or more, two times. To add the G-code files, select Include the G-code files.",
				"Click Download the backup. The browser saves a file with the name printdeck-backup-NAME-DATE.pdbk.",
				"To restore, open the same page when no print runs. Under Restore, type the backup password, choose the file, then click Restore.",
				"Klipper and the Printdeck restart. A wrong password or a damaged file changes no data.",
			}},

		{Chapter: care, ID: "simulator", Title: "Try the deck with a simulated printer",
			Intro: []string{
				"The simulated printer heats, homes, probes and prints in real time, and no motor moves. Use it to learn the deck or to test a file.",
				"Your printer.cfg stays as printer.cfg.real until you tap Real printer.",
			},
			Steps: []string{
				"Open Machine, then Simulated printer. The setup screen also has this key.",
				"Tap the printer to simulate, then Simulate. Klipper restarts with the simulated printer.",
				"Print as on a real printer: send a file from the slicer, or tap Print a file.",
				"To stop the simulation, open Machine, then Simulated printer, and tap Real printer, then Go back. Klipper restarts with your printer.cfg.",
			},
			After:   []string{"The deck changes the printer only when no print runs."},
			Figures: fig("simulator")},

		{Chapter: "When something goes wrong", ID: "problems", Title: "When something goes wrong",
			Intro: []string{"When Klipper stops with an error, the deck shows the message."},
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
