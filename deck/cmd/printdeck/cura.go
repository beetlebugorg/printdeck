package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"printdeck/internal/lcd"
)

// The booklet of the Cura plugin (slicer/cura-plugin/), in the style of the manual.
//
//	printdeck manual DIR cura
//
// The pictures of Cura are screenshots in DIR/img. The deck draws only the cover. DIR/index.html
// opens DIR/booklet.html.

// curaPluginJSON is plugin.json of the plugin, from the folder of the deck.
const curaPluginJSON = "../slicer/cura-plugin/Printdeck/plugin.json"

// The screenshots of Cura in DIR/img.
var curaShots = []string{"cura-tab", "cura-tune", "cura-connecting", "cura-button", "cura-messages", "cura-panel", "cura-pocket",
	"cura-cube", "cube"}

const (
	chCuraSetup = "Setup"
	chCuraPrint = "Printing"
	chCuraPart  = "Print on a part"
)

func curaBooklet(dir string) {
	for _, n := range curaShots {
		if _, err := os.Stat(filepath.Join(dir, "img", n+".png")); err != nil {
			log.Fatalf("cura booklet: %v", err)
		}
	}
	version := ""
	if b, err := os.ReadFile(curaPluginJSON); err == nil {
		var p struct{ Version string }
		if json.Unmarshal(b, &p) == nil {
			version = p.Version
		}
	}
	back := []string{
		"The Printdeck plugin for Cura is part of the Printdeck repository, in slicer/cura-plugin/.",
		"Plugin version " + version + ". https://github.com/beetlebugorg/printdeck",
	}
	ed := bookEdition{Kind: "CURA PLUGIN", For: "For Cura 5 and the Printdeck", Code: "PD-CURA-1",
		Title: "Printdeck Cura plugin booklet", Guides: curaGuides(), Back: back,
		Status: &lcd.Screen{Status: &lcd.Status{State: "PRINTING", Clock: "2:24PM", Tool: 209, ToolT: 210, Bed: 60, BedT: 60,
			Fan: 1, Printing: true, Progress: 0.42, Elapsed: "0:58", Left: "1:23", Done: "3:47PM"}}}
	writeBooklet(dir, nil, 800, 480, ed)
	index := `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Printdeck Cura plugin booklet</title>
<meta http-equiv="refresh" content="0; url=booklet.html">
</head>
<body><p><a href="booklet.html">The booklet of the Printdeck plugin for Cura</a></p></body>
</html>
`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(index), 0o644); err != nil {
		log.Fatal(err)
	}
}

// The blocks of a guide, with their estimated heights (see bookPageH). A line of a paragraph
// has about 95 characters, and a line of a list about 88.

func (g *bookGuide) title() { g.add(bookBlock{Kind: "title", Text: g.Title}, bookTitleH) }

func (g *bookGuide) para(s string) {
	g.add(bookBlock{Kind: "para", Text: s}, lines(len(s), 95)*bookLine+0.1)
}

func (g *bookGuide) note(s string) {
	g.add(bookBlock{Kind: "note", Text: s}, lines(len(s), 78)*0.17+0.18)
}

func (g *bookGuide) sub(s string) { g.add(bookBlock{Kind: "sub", Text: s}, 0.3) }

func (g *bookGuide) items(kind string, items ...string) {
	h := 0.1
	for _, s := range items {
		h += lines(len(s), 88)*0.17 + 0.05
	}
	g.add(bookBlock{Kind: kind, Items: items}, h)
}

func (g *bookGuide) row(k, v string) {
	g.add(bookBlock{Kind: "table", Rows: [][2]string{{k, v}}}, lines(len(v), 44)*0.17+0.12)
}

// shot is a screenshot with its caption, the badges, and the callouts in two columns.
func (g *bookGuide) shot(img, caption string, h float64, marks []mark, cs ...callout) {
	ch := 0.0
	for _, c := range cs {
		ch += lines(len(c.Title)+len(c.Text)+6, 40)*bookPartLine + 0.07
	}
	if len(cs) > 0 {
		ch = ch/2 + 0.12
	}
	ht := 0.0
	if caption != "" {
		ht = bookFigTitleH
	}
	g.add(bookBlock{Kind: "shot", Text: caption, Images: []string{img}, ImageH: h, Marks: marks, Callouts: cs},
		0.1+ht+h+ch)
}

func curaGuides() []bookGuide {
	what := bookGuide{Chapter: chCuraSetup, Title: "What the plugin does"}
	what.title()
	what.para("The Printdeck plugin connects Cura to the Printdeck. It uses the printer of the OctoPrint connection " +
		"plugin, with the same address and API key. After the setup, the plugin works by itself. A message in Cura " +
		"tells you what it did.")
	what.shot("cura-tab", "The deck in the Printdeck tab of Cura", 2.45, nil)
	what.items("list",
		"The print button says Print to Printdeck. After a send, Cura opens the Printdeck tab.",
		"The Printdeck tab shows the touch screen of the deck, live. You click it as you tap the touch screen.",
		"When the roll in the deck changes, Cura changes to the material of the roll.",
		"After a slice, Cura warns you when the roll is too short for the model.",
		"During a print, the Preview of Cura shows the layer that prints.",
		"The Print on a part tool prints new models on a part that is printed already.")
	what.para("The plugin needs Cura 5. Cura LulzBot Edition (Cura 4.13) does not show the tab and the tool.")

	install := bookGuide{Chapter: chCuraSetup, Title: "Install the plugin"}
	install.title()
	install.sub("From the release")
	install.items("olist",
		"Open the releases of the Printdeck: https://github.com/beetlebugorg/printdeck/releases.",
		"Download the file Printdeck.curapackage from the latest release.",
		"Drag the file onto the Cura window. Cura installs the plugin.",
		"Close Cura, and start it again.")
	install.sub("From the repository")
	install.items("olist",
		"In Cura, select Help > Show Configuration Folder.",
		"Copy the folder slicer/cura-plugin/Printdeck of the Printdeck repository into the plugins folder of the configuration folder.",
		"Close Cura, and start it again.")
	install.note("To make the package yourself, run slicer/cura-plugin/package.sh. It writes Printdeck.curapackage.")

	conn := bookGuide{Chapter: chCuraSetup, Title: "Connect the Printdeck"}
	conn.title()
	conn.para("The plugin uses the printer of the OctoPrint connection plugin. The deck answers the OctoPrint API, " +
		"so Cura sees it as an OctoPrint printer named Printdeck. Install the OctoPrint connection plugin first.")
	conn.items("olist",
		"In Cura, open the Marketplace. Install OctoPrint Connection, and restart Cura.",
		"Select Preferences > Configure Cura > Printers. Select your printer, and click Connect OctoPrint.",
		"Select Printdeck in the list. If the list does not show it, click Add, and type the address printdeck.local.",
		"Click Request to get the API key.",
		"The touch screen asks \"Allow Cura to use the Printdeck?\". Tap Allow.",
		"Click Connect, and close the preferences.")
	conn.para("When Cura connects, a card next to the slice button shows the roll in the printer: its color, its " +
		"name, and the filament that is left on it. After a slice, the card shows the filament of the model.")
	conn.note("The system page of the deck also shows the API key for slicers: http://printdeck.local/system.")

	send := bookGuide{Chapter: chCuraPrint, Title: "Send a print"}
	send.title()
	send.shot("cura-button", "The print button", 0.95, nil)
	send.items("olist",
		"Slice the model as usual.",
		"Click Print to Printdeck. Cura sends the file to the deck.",
		"Cura opens the Printdeck tab. The tab shows the deck as the print starts.")
	send.para("The OctoPrint connection starts the print after the upload when its option \"Automatically start print " +
		"job after uploading\" is on. Each file that Cura sends has the material of Cura in its header.")

	tab := bookGuide{Chapter: chCuraPrint, Title: "The Printdeck tab"}
	tab.title()
	tab.para("The Printdeck tab is next to Monitor. It shows the screens of the deck, live. Click a key in the tab as " +
		"you tap it on the touch screen.")
	tab.shot("cura-tab", "", 2.45, []mark{{1, 58.4, 2.2}, {2, 11.5, 13}, {3, 14.5, 52.5}, {4, 27.5, 52.5}, {5, 40.5, 52.5},
		{6, 47.5, 73.5}, {7, 88, 14}},
		callout{"Printdeck", "The tab of the plugin, next to Monitor."},
		callout{"Printer display", "The temperatures, the fan, and the progress of the print."},
		callout{"Pause", "Pauses the print."},
		callout{"Tune", "Opens the speed, the flow, the Z offset, the fan and the heaters."},
		callout{"Cancel", "Cancels the print. The deck asks first."},
		callout{"The file", "The name of the file, the layer, and the time left."},
		callout{"The layer", "The model up to the layer that prints now."})
	tab.shot("cura-tune", "Tune, after a click on Tune", 2.45, nil)
	tab.para("While the tab connects to the deck, it shows the start screen of the deck with the words \"Connecting to " +
		"printdeck.local\". The dots of the word print from the bottom up, one layer at a time. The tab gets the " +
		"screens only while it shows.")
	tab.shot("cura-connecting", "The start screen, while the tab connects", 2.2, nil)
	tab.para("During a print, the key Show in Preview is under the screen. It opens the file of the print in the Preview.")
	tab.sub("When the tab shows no deck")
	tab.row("Cura has no connection to a Printdeck", "Connect the printer with the OctoPrint connection plugin. See Connect the Printdeck.")
	tab.row("The Printdeck refused the screen", "Update Printdeck OS, and check the API key of the OctoPrint connection.")

	roll := bookGuide{Chapter: chCuraPrint, Title: "The roll and the material"}
	roll.title()
	roll.para("The deck keeps a list of your rolls of filament: Filament on the touch screen. The plugin reads the roll " +
		"in the printer every 10 seconds.")
	roll.sub("Cura follows the roll")
	roll.para("When the roll in the deck changes, Cura changes to the material of the roll. A message tells you, for " +
		"example: \"The Printdeck has Black (PLA). Cura now prints with Generic PLA.\" When Cura has no profile for " +
		"that material on this printer, the message tells you, and Cura keeps its material.")
	roll.sub("A short roll")
	roll.para("After a slice, Cura compares the grams of the model with the grams left on the roll. When the roll is " +
		"too short, a message stays until you close it: \"The model needs 120 g of filament. Black has 80 g left.\"")
	roll.sub("A file for another material")
	roll.para("Cura writes its material into the header of each file that it sends. Before the deck prints a file for " +
		"another material, the touch screen asks first, for example: \"The file is for Black ABS. White (PLA) is in " +
		"the printer.\" Tap Print anyway to print the file. To print with the correct material, change the roll first.")

	follow := bookGuide{Chapter: chCuraPrint, Title: "Follow the print"}
	follow.title()
	follow.para("During a print of the file that Cura has open, the Preview shows the layer that prints. The Preview " +
		"goes to the next layer when the deck starts it. Between two layers, you can look at other layers.")
	follow.shot("cura-messages", "The messages of the plugin", 1.75, nil)
	follow.sub("After a restart of Cura")
	follow.items("olist",
		"Start Cura during a print. Cura connects to the deck, and shows the message \"The Printdeck prints "+
			"<file>. Cura can show it in the Preview, at the layer that prints.\"",
		"Click Show the print. Cura downloads the file from the deck, and opens it in the Preview.",
		"The Preview follows the layer that prints.")
	follow.para("Cura shows no print time for a G-code file. The Printdeck tab shows the time left. When the print " +
		"ends, Cura shows a message.")

	part := bookGuide{Chapter: chCuraPart, Title: "Print on a part"}
	part.title()
	part.para("The Print on a part tool prints new models on a part that is printed already, for example a logo in " +
		"another color on a lid. The part goes into a pocket that the deck prints on the bed, so its place is exact. " +
		"The deck then checks the height of the part with a paper test.")
	part.shot("cura-panel", "The tool", 2.6, []mark{{1, 5.8, 5.8}, {2, 46, 12.3}, {3, 55.8, 21.4}, {4, 58.4, 30.5},
		{5, 67.9, 39.5}, {6, 90.8, 48.1}, {7, 74.2, 70}, {8, 50, 76.7}, {9, 51.3, 93.3}},
		callout{"Print on a part", "The tool, at the bottom of the left toolbar of Prepare."},
		callout{"Set as base", "The selected model is the base: the part that is printed already. Cura does not print it, and adds the pocket."},
		callout{"Place on the base", "Puts the selected model on the top of the base."},
		callout{"Place as in the files", "Puts the selected model where its file has it, for files from one assembly."},
		callout{"The pocket is on the bed", "For the next color. The file then has no pocket."},
		callout{"Clearance and Wall", "The gap around the part, and the height of the pocket wall, in mm."},
		callout{"Add text", "Raised text on the top of the base: the text, its height, and how high it stands, in mm."},
		callout{"Message on the deck", "The text of the pause on the touch screen."},
		callout{"Clear the base", "The base is a usual model again, and Cura removes the pocket."})

	prep := bookGuide{Chapter: chCuraPart, Title: "Prepare the job"}
	prep.title()
	prep.items("olist",
		"Open the part and the new models in Cura. Lay the part on the bed with its face for the new models up.",
		"Keep the part away from the front and the left edge. The print primes the nozzle there.",
		"Select the part, and click Set as base. Cura adds the pocket: a low wall around the part, with open corners.",
		"Select a new model, and click Place on the base. The model goes to the top of the part, and stays at that height.",
		"Move the model on the face as usual. To print into a pocket of the part, lower the model with the Z of the Move tool.")
	prep.shot("cura-pocket", "A part in its pocket", 1.2, nil)
	prep.sub("Files from one assembly")
	prep.para("When the part and the new models come from one assembly, their files share one frame. Select the new " +
		"models, and click Place as in the files. The models go to their exact place on the part, also after you turned the part.")
	prep.sub("Lettering")
	prep.para("Type the text, its size, and its height over the face (Raised). Then click Add text. The text goes on " +
		"the top of the part, at its middle. Move and turn it as usual.")
	prep.note("While a part is the base, the tool turns off \"Ensure models are kept apart\" in the preferences of " +
		"Cura. That setting pushes the pocket away from the new models. Click Clear the base to turn it on again.")

	run := bookGuide{Chapter: chCuraPart, Title: "At the printer"}
	run.title()
	run.items("olist",
		"Click Print to Printdeck. The deck prints the pocket on the bed.",
		"The nozzle lifts, and the print pauses. The touch screen shows the message of the pause.",
		"Put the part in the pocket.",
		"For another color, change the filament now.",
		"Tap Check the height. The nozzle goes over the part.",
		"Put a sheet of paper under the nozzle. Lower the nozzle until the paper drags a little, then tap Accept.",
		"Tap Resume. The new models print on the part.")
	run.note("The height check sets the height of the new models on the real part. After the check, the screen shows " +
		"the difference from the file, for example +0.12 mm. If you tap Resume with no height check, the deck asks first.")
	run.sub("The next color")
	run.para("Keep the part in its pocket. In the tool, check The pocket is on the bed, and send the next file. That " +
		"file has no pocket. It homes Z up first, does not probe or heat the bed, and purges beside the pocket.")
	run.note("The pause and the height check need the deck bundle with [printdeck_pause]: v0.2.0-beta.1-9 or later.")

	cube := bookGuide{Chapter: chCuraPart, Title: "Test print: the cube"}
	cube.title()
	cube.para("The files are in slicer/test-prints/onpart/ of the repository. The test is a 20 mm cube, then a cross " +
		"on its top in another color. The cross is 1 mm high, with arms of 14 x 3 mm.")
	cube.add(bookBlock{Kind: "image", Images: []string{"cube", "cura-cube"}, ImageH: 0.9}, 1.05)
	cube.sub("Job 1: the cube")
	cube.items("olist",
		"Open cube.stl in Cura, and print it (about 16 minutes).",
		"Take the cube off the bed.")
	cube.sub("Job 2: the holder and the cross")
	cube.items("olist",
		"Clear the build plate. Open cube.stl and cross.stl.",
		"Select the cube, and click Set as base. Cura adds the holder (the pocket) around the cube, and does not print the cube.",
		"Select the cross, and click Place as in the files. The cross goes on the top of the cube.",
		"Print. The deck prints the holder, then lifts the nozzle and pauses.",
		"Put the cube in the holder.",
		"Change the filament to the second color.",
		"Tap Check the height, and do the paper test on the top of the cube.",
		"Tap Resume. The deck prints the cross.")
	cube.sub("What to look at")
	cube.items("list",
		"XY: the cross is at the center of the top. Measure the 3 mm from each arm end to the edge of the cube.",
		"Z: the first layer of the cross sticks to the cube. It is not squashed, and it is not in the air.",
		"The purge after the filament change does not go on the cube.")

	return []bookGuide{what, install, conn, send, tab, roll, follow, part, prep, run, cube}
}
