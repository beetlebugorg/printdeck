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
	"time"

	"printdeck/internal/board"
	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/internal/spotify"
	"printdeck/internal/ui"
	"printdeck/internal/wifi"
)

// The manual: each screen of the deck with numbered callouts, as in the booklet of an old game,
// and the text for each part. The deck draws the pictures itself, so the manual follows the UI.
//
//	printdeck manual DIR     write DIR/index.html and the pictures in DIR/img

//go:embed manual.html
var manualHTML string

// callout is a number on a picture: the position in the units of the UI (480 wide), the name of
// the part, and what it does.
type callout struct {
	X, Y        int
	Title, Text string
}

type figure struct {
	Name     string
	Caption  string
	Setup    func(s *manualState)
	Callouts []callout
	Notes    []string // the text after the list
	Badges   []badge  // the numbers in the margins, from layoutBadges
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

// layoutBadges puts the number of each callout in the margin of its side, in the order of the
// height, at least 34 units apart.
func layoutBadges(cs []callout) []badge {
	var left, right []int
	for i, c := range cs {
		if c.X < 240 {
			left = append(left, i)
		} else {
			right = append(right, i)
		}
	}
	out := make([]badge, len(cs))
	place := func(idx []int, isRight bool) {
		sort.Slice(idx, func(a, b int) bool { return cs[idx[a]].Y < cs[idx[b]].Y })
		const gap, top, bottom = 34, 20, 780
		ys := make([]int, len(idx))
		for k, i := range idx {
			ys[k] = max(cs[i].Y, top)
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
			b := badge{N: i + 1, X: cs[i].X, Y: cs[i].Y, BY: ys[k], Right: isRight}
			if isRight {
				b.BX, b.LineStartX = ui.W+gutter/2+4, ui.W+gutter/2+4-15
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
	Intro     []string
	Figures   []figure
	After     []string
	Table     [][2]string // an optional table of two columns, after the text
}

// manualState is the demo deck that the setups change.
type manualState struct {
	a *ui.App
	m *ui.DemoMusic
	p *ui.DemoPrinter
}

// The printer states of the pictures.
var (
	stIdle = printer.State{Reachable: true, Text: "Operational", Connected: true, Tool: 24, Bed: 23, Homed: "xyz",
		X: 110, Y: 110, Z: 5, Axis: [2][3]float64{{0, 0, 0}, {280, 280, 270}}, Firmware: printer.HostVersion + "-20261021_112706-printdeck"}
	stPrinting = printer.State{Reachable: true, Text: "Printing", Connected: true, Printing: true,
		File: "printdeck_body.gcode", FilePath: "printdeck_body.gcode", Activity: "Printing", Layer: 12, Layers: 27,
		HasNow: true, NowKind: "Outer wall", NowSpeed: 30, Completion: 42.4, TimeLeft: 83 * time.Minute, HasTimeLeft: true,
		Elapsed: 75 * time.Minute, FilamentUsed: 25600, Tool: 209.6, ToolTarget: 210, Bed: 60, BedTarget: 60, SpeedPct: 100, FlowPct: 100}
)

func manualSections() []section {
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
				"Printdeck is a touch screen for a 3D printer that runs Klipper. It shows the print, and it controls the printer.",
				"At the first start, the deck makes its password and opens the setup. Connect the deck to your Wi-Fi network there.",
			},
			Figures: []figure{
				{Name: "setup-wifi", Caption: "The setup: step 1",
					Setup: func(s *manualState) { s.a.OpenSetupScreen("wifi") },
					Callouts: []callout{
						{240, 240, "Networks", "The Wi-Fi networks near the deck, with the signal. A lock shows a network with a password. Tap a network, then type its password."},
						{68, 728, "Scan", "Looks for the networks again. The list also scans by itself."},
						{170, 728, "Hidden", "For a network that does not show its name: type the name and the password."},
						{394, 728, "Next", "Goes to the summary when the deck is connected."},
					}},
				{Name: "setup-done", Caption: "The setup: step 2",
					Setup: func(s *manualState) { s.a.OpenSetupScreen("done") },
					Callouts: []callout{
						{240, 150, "Address", "The address of the deck on your network. Open it in a browser for the web pages."},
						{240, 330, "Password", "The password of the web pages. To see it again, open Machine > Wi-Fi and help."},
						{312, 728, "Finish", "Closes the setup. The deck does not open it again."},
					},
					Notes: []string{"You can also set the Wi-Fi before the first start: put the disk in a computer, and fill in printdeck.txt on the PRINTDECK partition."}},
			}},
		{ID: "deck", Title: "The deck",
			Intro: []string{"The Print tab has all the screens of the printer. The status bar is at the top of every screen."},
			Figures: []figure{
				{Name: "status", Caption: "The status bar and the tabs",
					Setup: func(s *manualState) {
						s.p.State_ = stPrinting
					},
					Callouts: []callout{
						{52, 29, "Clock", "The time. Tap it to change the colors: day, night, or auto (night from 20:00 to 7:00)."},
						{425, 29, "Stop", "Shows while the printer prints or heats. It stops the printer at once, after one question. Klipper must restart after a stop."},
						{360, 764, "Print", "The screens of the printer."},
					}},
			}},
		{ID: "home", Title: "Print: the idle screen",
			Intro: []string{"When the printer does not print, the Print tab shows the idle screen. The largest key is the action that you need most: Print a file."},
			Figures: []figure{
				{Name: "home", Caption: "The idle screen", Setup: idle,
					Callouts: []callout{
						{240, 108, "Display", "The state of the printer, the loaded filament, and the homed axes or the message of the printer (M117). It has the colors of the display of a LulzBot."},
						{240, 208, "Temperatures", "The nozzle and the bed now. A heater that is on shows its target and an orange icon. Tap a card to set its temperature."},
						{240, 304, "Print a file", "Opens the files. See Files."},
						{140, 386, "Preheat", "Heats the nozzle and the bed for the loaded filament."},
						{340, 386, "Cool down", "Turns off both heaters."},
						{240, 475, "Last print", "The last print and its result. Reprint starts it again, after a question."},
						{240, 615, "Pages", "Filament, Move, Maintenance and Machine. Each page has its own section."},
					}},
				{Name: "keypad", Caption: "The keypad of a temperature",
					Setup: func(s *manualState) { s.p.State_ = stIdle; s.a.OpenPrintScreen("keypad") },
					Callouts: []callout{
						{240, 160, "Value", "The temperature that you type."},
						{240, 240, "Presets", "Off, and the temperatures of the materials."},
						{240, 460, "Keys", "The digits. C clears the value, DEL removes the last digit."},
						{350, 676, "Set", "Sets the heater. Cancel closes the keypad with no change."},
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
						{130, 124, "Count", "The number of files."},
						{58, 187, "Picture", "The thumbnail of the slicer, or the shape of a part."},
						{260, 190, "File", "The name, the print time, the filament and the layer height."},
						{260, 233, "Date", "When the file arrived."},
					},
					Notes: []string{"Drag the list up and down. Tap a file to open it."}},
				{Name: "file", Caption: "A file", Setup: page("file"),
					Callouts: []callout{
						{240, 254, "Picture", "The thumbnail of the file."},
						{240, 495, "Slicer data", "The print time, the filament, the layer height, the temperatures, the slicer and the date."},
						{160, 674, "Print", "Starts the print, after a question."},
						{386, 674, "Delete", "Removes the file, after a question. The file that prints now cannot be removed."},
					}},
			}},
		{ID: "printing", Title: "Print: during a print",
			Intro: []string{"During a print, the Print tab shows the screen of the phase of the print. The deck knows the phase from the printer and from the G-code of the file."},
			Figures: []figure{
				{Name: "preparing", Caption: "Preparing",
					Setup: func(s *manualState) {
						s.p.State_ = printer.State{Reachable: true, Text: "Printing", Connected: true, Printing: true, File: "benchy.gcode",
							Activity: "Heating bed  52/60°", Preparing: true, HasWipe: true, Tool: 24, Bed: 52, BedTarget: 60}
						s.a.OpenPrintScreen("")
					},
					Callouts: []callout{
						{240, 108, "Display", "The phase and the step."},
						{240, 238, "Steps", "The steps of the start of the print. The step that runs has a lit LED and a bar. A done step has a dot."},
						{240, 496, "Temperatures", "The heaters."},
						{240, 676, "Cancel print", "Stops the print, after a question."},
					}},
				{Name: "first-layer", Caption: "The first layer",
					Setup: func(s *manualState) {
						s.p.State_ = printer.State{Reachable: true, Text: "Printing", Connected: true, Printing: true, File: "benchy.gcode",
							Activity: "Printing", Layer: 1, Layers: 27, Completion: 2, HasTimeLeft: true, TimeLeft: 100 * time.Minute,
							ZOffset: 0.04, Tool: 210, ToolTarget: 210, Bed: 60, BedTarget: 60}
						s.a.OpenPrintScreen("")
					},
					Callouts: []callout{
						{240, 250, "Z offset", "The height of the nozzle above its normal height. Watch the first lines while they print."},
						{58, 250, "− and +", "− moves the nozzle closer to the bed, + away from it. A line that is too round and thin needs −. A line that is flat and rough needs +."},
						{165, 366, "Step", "How far one tap moves the nozzle, in mm."},
						{378, 366, "Save", "Writes the Z offset into the config of the printer. Save config in Maintenance then writes the file."},
						{240, 486, "Layer", "The first layer of the model."},
					}},
				{Name: "printing", Caption: "Printing",
					Setup: func(s *manualState) { s.p.State_ = stPrinting; s.a.OpenPrintScreen("") },
					Callouts: []callout{
						{240, 88, "Display, line 1", "What the printer does, the percent done and the time left."},
						{240, 128, "Display, line 2", "The move now and its speed. Every 4 seconds, the time at which the print ends."},
						{240, 320, "Model", "The model up to the layer that prints now."},
						{240, 494, "Layer slider", "Drag it to look at another layer. Live goes back to the layer that prints."},
						{240, 530, "Progress", "The percent done, the time since the start, and the filament used."},
						{240, 590, "Temperatures", "Tap a card to change its temperature."},
						{100, 666, "Pause", "Pauses the print."},
						{240, 666, "Tune", "Changes the print while it runs. See Tune."},
						{380, 666, "Cancel", "Stops the print, after a question."},
					}},
				{Name: "tune", Caption: "Tune",
					Setup: func(s *manualState) { s.p.State_ = stPrinting; s.a.OpenPrintScreen("tune") },
					Callouts: []callout{
						{380, 182, "Speed", "The speed of the print, in percent of the speed of the slicer."},
						{380, 265, "Flow", "The amount of plastic, in percent."},
						{380, 347, "Z offset", "The height of the nozzle during the print."},
						{380, 452, "Fan", "The part cooling fan."},
						{240, 573, "Temperatures", "Tap a card to change its temperature."},
					}},
				{Name: "paused", Caption: "Paused",
					Setup: func(s *manualState) {
						st := stPrinting
						st.Paused, st.Message = true, "Paused by user"
						s.p.State_ = st
						s.a.OpenPrintScreen("")
					},
					Callouts: []callout{
						{240, 213, "Where", "The layer of the pause, and the heaters."},
						{240, 315, "Resume", "Continues the print."},
						{126, 409, "Change filament", "The guided filament change, for a new color. See Filament."},
						{354, 409, "Move head", "Opens Move, to move the head out of the way."},
						{126, 491, "Tune", "Changes the print."},
						{354, 491, "Cancel", "Stops the print, after a question."},
					}},
				{Name: "done", Caption: "Done",
					Setup: func(s *manualState) {
						s.p.State_ = printer.State{Reachable: true, Text: "Operational", Connected: true, Tool: 80, Bed: 48,
							Finished: "complete", FinishedFile: "benchy.gcode", FinishedPath: "benchy.gcode", FinishedTime: 102 * time.Minute,
							FinishedFilament: 4100}
						s.a.OpenPrintScreen("")
					},
					Callouts: []callout{
						{240, 108, "Result", "Done, Cancelled or Failed, the time and the filament of the print."},
						{240, 509, "Bed", "The temperature of the bed. The bar is full when the part comes off easily (35°)."},
						{126, 635, "Reprint", "Prints the file again."},
						{354, 635, "Done", "Closes this screen."},
					}},
			}},
		{ID: "filament", Title: "Filament",
			Intro: []string{"Set the loaded filament here, so that Preheat uses its temperatures. The guided change heats the nozzle, unloads the old filament and loads the new one."},
			Figures: []figure{
				{Name: "filament", Caption: "The Filament page", Setup: page("filament"),
					Callouts: []callout{
						{240, 149, "Loaded", "The filament that is in the printer, and its temperatures."},
						{240, 236, "Material", "Tap the material that is in the printer."},
						{240, 412, "Change filament", "The guided change: choose, heat, unload, insert, load, check."},
						{240, 499, "Load, Unload", "Only the load, or only the unload."},
						{240, 576, "Extrude, Retract", "Pushes or pulls 10 mm. The nozzle must be at 170° or more."},
						{240, 648, "Heat, Cool down", "Heats the nozzle for the loaded filament, or turns the heaters off."},
					}},
				{Name: "swap", Caption: "The guided change, step 1",
					Setup: func(s *manualState) { s.p.State_ = stIdle; s.a.OpenPrintScreen("swap") },
					Callouts: []callout{
						{120, 126, "Step", "The step of the change."},
						{240, 310, "Materials", "The new filament. The nozzle heats to its temperature, and the deck sets it as the loaded filament."},
						{240, 510, "Other temperature", "A temperature that you type."},
						{422, 88, "Close", "Stops the change."},
					}},
			}},
		{ID: "move", Title: "Move",
			Figures: []figure{
				{Name: "move", Caption: "The Move page", Setup: page("move"),
					Callouts: []callout{
						{240, 149, "Position", "X, Y and Z of the head. A lit LED shows a homed axis."},
						{240, 217, "Home", "Homes all axes, or one axis. Home after each start of the printer."},
						{152, 392, "Joystick", "Hold a finger on the pad to move the head in X and Y. The farther from the middle, the faster it moves."},
						{374, 400, "Z", "Moves Z up or down by the step: 0.1, 1 or 10 mm."},
						{240, 593, "Park, Center, Motors off", "Park moves the head to the back left. Center moves it to the middle of the bed. Motors off lets you move the head by hand."},
					}},
			}},
		{ID: "maintenance", Title: "Maintenance",
			Intro: []string{"Maintenance lists the calibrations that your printer has. The list comes from the config of the printer."},
			Figures: []figure{
				{Name: "maintenance", Caption: "The Maintenance page", Setup: page("maintenance"),
					Callouts: []callout{
						{240, 167, "Bed tilt", "Probes the bed, and corrects its tilt (BED_TILT_CALIBRATE). A printer with a bed mesh shows Bed mesh."},
						{240, 265, "Probe Z offset", "The paper test for the height of the nozzle (PROBE_CALIBRATE)."},
						{240, 363, "PID tune", "Heats the nozzle to 210° and the bed to 60°, and finds the PID settings of each heater (PID_CALIBRATE). Save config then writes them."},
						{240, 461, "Nozzle change", "Heats the nozzle to 250° and raises the head, to change the nozzle."},
						{240, 559, "Firmware", "Writes the Klipper firmware of this Printdeck to the board of the printer. Orange text shows old firmware. See below."},
						{240, 657, "Save config", "Writes the results of the calibrations into printer.cfg. Klipper restarts. Orange text shows unsaved results."},
					}},
				{Name: "flash", Caption: "The firmware flash", Setup: page("flash"),
					Notes: []string{"The deck stops Klipper, resets the board into its bootloader, writes the firmware, and reads it back to check it. Then Klipper starts again. The setup screen and the Klipper error screen also show this key when the board has no Klipper firmware or old firmware. Printdeck can flash boards with an atmega2560 and an STK500v2 bootloader now, for example the RAMBo and RAMPS boards."},
					Callouts: []callout{
						{240, 148, "Step", "Starting the bootloader, Writing, then Checking."},
						{240, 206, "Progress", "The part of the firmware that is done."},
					}},
				{Name: "action", Caption: "An action runs", Setup: page("action"),
					Notes: []string{"While an action runs, the Print tab shows it on the deck and in each browser, and the other keys are off. The display of the printer shows the name of the action and a moving bar. When the action ends, a short message says that it is done. When it fails, the deck shows the error of Klipper until you tap OK."},
					Callouts: []callout{
						{240, 199, "Action", "The time since the start, a bar that moves while the action runs, and the temperatures."},
						{240, 664, "Stop the printer", "Stops the printer at once, after one question. Then use Firmware restart."},
					}},
				{Name: "paper", Caption: "The paper test",
					Setup: func(s *manualState) {
						s.p.State_ = printer.State{Reachable: true, Text: "Operational", Connected: true, ManualProbe: true, ManualZ: 0.425}
						s.a.OpenPrintScreen("")
					},
					Callouts: []callout{
						{240, 108, "Height", "The height of the nozzle now."},
						{240, 430, "Steps", "Moves the nozzle down (−) or up (+), in mm."},
						{126, 605, "Accept", "Takes the height. Save config then writes it."},
						{354, 605, "Abort", "Stops the test with no change."},
					},
					Notes: []string{"This screen shows by itself while Klipper runs a paper test, also when it started from a macro."}},
			}},
		{ID: "machine", Title: "Machine",
			Figures: []figure{
				{Name: "machine", Caption: "The Machine page", Setup: page("machine"),
					Callouts: []callout{
						{240, 186, "Information", "The version of Klipper, the state of the firmware of the board, the network address, and the totals."},
						{240, 297, "Statistics", "The totals of all prints."},
						{240, 373, "Print history", "Each print, with its result. Tap a print to print it again."},
						{240, 449, "Macros", "The macros of the printer. Tap one to run it."},
						{240, 525, "Console", "The G-code terminal of Klipper. See below."},
						{240, 601, "Wi-Fi and help", "The network, the password and the addresses."},
						{126, 677, "Restart Klipper", "Restarts the Klipper host, which reads printer.cfg again."},
						{354, 677, "Firmware restart", "Restarts the firmware of the board, for example after a stop."},
					}},
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
						{240, 300, "Replies", "Your commands (amber, after >), the replies of Klipper, and errors (red). Drag down to read older lines."},
						{240, 611, "Type a command", "Opens the keyboard. It shows your last command, so you can change it and send it again."},
						{240, 685, "Quick keys", "Home (G28), Status (STATUS) and Help (HELP, the list of the commands)."},
					}},
				{Name: "stats", Caption: "Statistics", Setup: page("stats"),
					Callouts: []callout{
						{240, 226, "Totals", "The number of prints, the success rate, the print time and the filament."},
						{240, 348, "Results", "Done (amber), cancelled and failed (red)."},
						{240, 510, "Details", "The filament in grams (for 1.75 mm PLA), the average and the longest print, the prints of the last 7 and 30 days, and the file that you print most."},
					}},
			}},
		{ID: "web", Title: "The deck in the browser",
			Intro: []string{
				"Open http://printdeck.local in a browser on the same network, and log in with the password of the deck. The browser shows the same screens as the touch screen, sharp at the pixel density of the phone. Each browser has its own screens, so a phone can show the files while the deck shows the print.",
				"On a phone, use Add to Home Screen: the deck then opens as an app, with no browser bars.",
				"The page http://printdeck.local/system shows the version, the services, the logs and the API key for slicers. It also installs an update: choose the update file, then Install. The deck writes the other system slot and restarts into it. If the new system does not start, the deck goes back to the old one.",
			}},
		{ID: "lcd", Title: "The display of the printer",
			Intro: []string{"When the printer has a display, it shows what the deck shows: the step of a filament change, the paper test, the page that is open, or the state of the print. The rows below show the temperatures, the print time and the progress. During a filament change or while the printer heats, the bottom row shows an animation."},
			Table: [][2]string{
				{"PRINTING     42%\nEND 15:47   1:23\n~210°   01:15\n~60°  ▓▓▓▓░░", "During a print"},
				{"FILAMENT SWAP   \nINSERT, LOAD    \n~240°        \n>   >   >   >   ", "Filament goes in"},
				{"READY        PLA\nXYZ HOMED       \n~24°         \n~23°", "Idle"},
			}},
		{ID: "slicers", Title: "Slicers",
			Intro: []string{
				"The deck answers the OctoPrint API that slicers use to send files and to start prints.",
				"In Cura, add the deck with the OctoPrint Connection plugin. The deck asks \"Allow Cura to use the Printdeck?\". Tap Allow.",
				"In OrcaSlicer or PrusaSlicer, add a printer of the type OctoPrint with the address http://printdeck.local, and the API key of the system page.",
			}},
		{ID: "settings", Title: "Settings",
			Intro: []string{"The file printdeck.txt on the PRINTDECK partition holds the settings. Put the disk in a computer to change it. The deck reads it at each start, then clears the Wi-Fi password in it."},
			Table: [][2]string{
				{"wifi_ssid, wifi_password, wifi_country", "The Wi-Fi network"},
				{"hostname, timezone", "The name on the network, and the time zone"},
				{"display_rotate", "90 or 270 for a display that is mounted in portrait"},
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
						{240, 215, "Board", "The board that the deck found on USB, its MCU, and its Klipper firmware."},
						{126, 335, "Choose printer", "Opens the list of Klipper's example configs. See below."},
						{354, 335, "Search again", "Looks for the board again, for example after you connect the printer."},
						{240, 400, "Your own config", "You can also copy your own printer.cfg to the PRINTDECK partition, and start the deck again."},
					}},
				{Name: "configs", Caption: "Choose your printer",
					Setup: func(s *manualState) { s.a.OpenPrintScreen("configs") },
					Callouts: []callout{
						{240, 140, "Chip", "The list shows the printers with the same chip as your board."},
						{240, 200, "Printer", "The brand, the model, the year and the chip. Tap your printer. The deck writes its config as printer.cfg, with the port of your board, and restarts Klipper. The old printer.cfg stays as printer.cfg.bak."},
						{240, 680, "Show all printers", "Shows the printers for all chips."},
					}},
				{Name: "error", Caption: "A Klipper error",
					Setup: func(s *manualState) { s.a.OpenPrintScreen("error") },
					Callouts: []callout{
						{240, 200, "Message", "What Klipper reports, with all its lines."},
						{240, 640, "Restart Klipper", "Restarts Klipper. It reads printer.cfg again."},
					}},
			}},
	}
}

func manual(args []string) {
	if len(args) < 1 {
		log.Fatal("usage: printdeck manual DIR")
	}
	dir := args[0]
	os.MkdirAll(filepath.Join(dir, "img"), 0o755)
	// The pictures show the firmware keys of the OS image.
	fwDir, _ := os.MkdirTemp("", "printdeck-firmware")
	defer os.RemoveAll(fwDir)
	os.WriteFile(filepath.Join(fwDir, "klipper-atmega2560.bin"), []byte{0}, 0o644)
	os.WriteFile(filepath.Join(fwDir, "version"), []byte(printer.HostVersion+"\n"), 0o644)
	board.FirmwareDir = fwDir
	a, m, p := demoApp()
	a.Now = func() time.Time { return time.Date(2026, 10, 20, 14, 24, 0, 0, time.Local) }
	a.Setup = &ui.DemoSetup{
		Nets: []wifi.Network{{SSID: "Extremely", Signal: -48, Secure: true}, {SSID: "Workshop", Signal: -63, Secure: true},
			{SSID: "Guest", Signal: -71}, {SSID: "NETGEAR-5G", Signal: -82, Secure: true}},
		Status_: wifi.Status{State: "COMPLETED", SSID: "Extremely", IP: "192.168.76.119"},
	}
	// The manual is about the printer: the status bar shows no song.
	m.State_ = spotify.State{}
	st := &manualState{a, m, p}
	secs := manualSections()
	const k = 2 // the pictures are drawn at twice the size
	for si := range secs {
		for fi := range secs[si].Figures {
			f := &secs[si].Figures[fi]
			f.Badges = layoutBadges(f.Callouts)
			*p = *demoPrinterCopy(p)
			a.ResetScreens()
			a.SetTab(ui.TabPrint)
			f.Setup(st)
			c, rec := gfx.NewRecording(ui.W, ui.BaseH)
			a.Draw(c)
			out := gfx.New(ui.W*k, ui.BaseH*k)
			if err := gfx.ReplayScaled(out, rec.JSON(), gfx.ImageByID, k); err != nil {
				log.Fatal(err)
			}
			for i := range f.Badges {
				f.Badges[i].X = clearSpot(out.RGBA, k, f.Badges[i].X, f.Badges[i].Y, f.Badges[i].Right)
			}
			writePNG(filepath.Join(dir, "img", f.Name+".png"), out.RGBA)
		}
	}
	tpl := template.Must(template.New("manual").Funcs(template.FuncMap{
		"pct":  func(v, of int) string { return fmt.Sprintf("%.2f%%", 100*float64(v)/float64(of)) },
		"inc":  func(i int) int { return i + 1 },
		"W":    func() int { return ui.W },
		"G":    func() int { return gutter },
		"VW":   func() int { return ui.W + 2*gutter },
		"H":    func() int { return ui.BaseH },
		"date": func() string { return time.Now().Format("January 2006") },
	}).Parse(manualHTML))
	f, err := os.Create(filepath.Join(dir, "index.html"))
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := tpl.Execute(f, map[string]any{"Sections": secs, "Version": osVersion()}); err != nil {
		log.Fatal(err)
	}
}

// clearSpot moves the dot of a callout to the side of its number, until the dot covers no text or
// line of the picture, so that the dot does not hide the words of the part. img is drawn at k
// times the units of the UI. It returns the new x, or x when it finds no clear place.
func clearSpot(img *image.RGBA, k, x, y int, right bool) int {
	const r = 7 // the radius of the dot, with room around it
	clear := func(cx int) bool {
		lo, hi := 255, 0
		for py := (y - r) * k; py <= (y+r)*k; py++ {
			for px := (cx - r) * k; px <= (cx+r)*k; px++ {
				if !image.Pt(px, py).In(img.Rect) {
					return false
				}
				c := img.RGBAAt(px, py)
				l := (int(c.R)*299 + int(c.G)*587 + int(c.B)*114) / 1000
				lo, hi = min(lo, l), max(hi, l)
			}
		}
		return hi-lo < 40
	}
	step := 2
	if !right {
		step = -2
	}
	for cx := x; cx >= r && cx <= ui.W-r; cx += step {
		if clear(cx) {
			return cx
		}
	}
	return x
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
