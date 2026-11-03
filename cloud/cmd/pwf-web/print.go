//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"syscall/js"
	"time"

	"printdeck/internal/cloud/wire"
)

// The prints of a project on the app page and the public page: the numbers of the project, a
// tile for each print, and the page of one print. The page of a print is the share page of a
// finished print: the time lapse, the readout, the model with the chart of the layers, the
// details, the filament and the photos.

// hashArgs is the address after # as keys and values: "x=<id>&p=<print>", the old addresses.
func hashArgs() map[string]string {
	out := map[string]string{}
	h := strings.TrimPrefix(js.Global().Get("location").Get("hash").String(), "#")
	for _, kv := range strings.Split(h, "&") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			out[k] = v
		}
	}
	return out
}

// drawStats fills the numbers of the project: the prints, the print time and the filament.
func drawStats(prints []wire.ProjectPrint) {
	setHidden("pStats", len(prints) == 0)
	secs, grams, done := 0, 0.0, 0
	var first time.Time
	materials := map[string]bool{}
	var mats []string
	for _, p := range prints {
		secs += p.Seconds
		grams += p.Grams
		if p.Result == "complete" {
			done++
		}
		if first.IsZero() || p.Ended.Before(first) {
			first = p.Ended
		}
		m := p.Material
		if p.Filament != nil && p.Filament.Type != "" {
			m = p.Filament.Type
		}
		if m != "" && !materials[m] {
			materials[m] = true
			mats = append(mats, m)
		}
	}
	setText("stPrints", fmt.Sprint(len(prints)))
	sub := fmt.Sprintf("%d finished", done)
	if n := len(prints) - done; n > 0 {
		sub += fmt.Sprintf(" · %d stopped", n)
	}
	setText("stPrintsSub", sub)
	setText("stTime", hoursMinutes(secs))
	setText("stTimeSub", "")
	if !first.IsZero() {
		setText("stTimeSub", "since "+shortDate(first))
	}
	setText("stGrams", grams0(grams))
	sort.Strings(mats)
	setText("stGramsSub", strings.Join(mats, ", "))
}

// hoursMinutes is a time in seconds as h:mm.
func hoursMinutes(sec int) string { return fmt.Sprintf("%d:%02d", sec/3600, sec/60%60) }

// grams0 is a weight with no decimals from 10 g, and kg from 1000 g.
func grams0(g float64) string {
	if g >= 1000 {
		return fmt.Sprintf("%.2f kg", g/1000)
	}
	return grams(g)
}

// localDate is the date and time of t for the viewer, for example "Oct 28, 3:41 PM".
func localDate(t time.Time) string {
	d := js.Global().Get("Date").New(float64(t.UnixMilli()))
	o := js.Global().Get("Object").New()
	o.Set("month", "short")
	o.Set("day", "numeric")
	o.Set("hour", "numeric")
	o.Set("minute", "2-digit")
	return d.Call("toLocaleString", js.Undefined(), o).String()
}

// shortDate is the date of t for the viewer, for example "Oct 28".
func shortDate(t time.Time) string {
	d := js.Global().Get("Date").New(float64(t.UnixMilli()))
	o := js.Global().Get("Object").New()
	o.Set("month", "short")
	o.Set("day", "numeric")
	return d.Call("toLocaleDateString", js.Undefined(), o).String()
}

var resultText = map[string]string{"cancelled": "Cancelled", "error": "Stopped"}

// drawTiles draws a tile for each print. href is the address of the page of a print.
func drawTiles(prints []wire.ProjectPrint, href func(wire.ProjectPrint) string) {
	box := byID("prints")
	box.Set("textContent", "")
	setHidden("noPrints", len(prints) > 0)
	for _, p := range prints {
		box.Call("appendChild", printTile(p, href(p)))
	}
}

// liveSrc keeps the address of the picture of each live print. The service signs a new address
// at each poll: the old one shows the same picture, so the page loads it once, until the address
// expires.
var liveSrc = map[string]struct {
	url string
	at  time.Time
}{}

// liveEnded is the line of a live print that ended, by its result.
var liveEnded = map[string]string{"complete": "Finished. It comes here in a few minutes.",
	"cancelled": "Cancelled.", "error": "The print stopped."}

// drawLive draws a tile for each print of the project that prints now. A tile with a share link
// opens the share page of the print.
func drawLive(live []wire.ProjectLive) {
	box := byID("live")
	box.Set("textContent", "")
	setHidden("liveHead", len(live) == 0)
	setHidden("pLiveCard", len(live) == 0) // the overview of the project page
	for _, l := range live {
		tile := el("div", "tile", "")
		if l.URL != "" {
			tile = el("a", "tile", "")
			tile.Set("href", l.URL)
		}
		pic := el("div", "pic", "")
		src := l.Model
		if src == "" && l.URL != "" {
			src = l.URL + "/model.jpg"
		}
		key := l.Name + "@" + l.Started.String()
		if old, ok := liveSrc[key]; ok && time.Since(old.at) < 30*time.Minute {
			src = old.url
		} else if src != "" {
			liveSrc[key] = struct {
				url string
				at  time.Time
			}{src, time.Now()}
		}
		if src != "" {
			img := el("img", "", "")
			img.Set("src", src)
			img.Set("alt", "")
			pic.Call("appendChild", img)
		}
		badges := el("div", "badges", "")
		switch {
		case l.Result == "complete":
			badges.Call("appendChild", el("span", "live", "Done"))
		case l.Result != "":
			badges.Call("appendChild", el("span", "fail", resultText[l.Result]))
		case l.Paused:
			badges.Call("appendChild", el("span", "", "Paused"))
		default:
			badges.Call("appendChild", el("span", "live", "Live"))
		}
		pic.Call("appendChild", badges)
		if l.LayerPic != "" {
			// A resin print: the layer that the printer cures now, over a corner of the model.
			lp := el("img", "layer-pic", "")
			lp.Set("src", l.LayerPic)
			lp.Set("alt", fmt.Sprintf("Layer %d", l.Layer))
			pic.Call("appendChild", lp)
		}
		tile.Call("appendChild", pic)
		body := el("div", "body", "")
		body.Call("appendChild", el("strong", "", l.Name))
		if l.Result == "" && l.Percent > 0 {
			bar := el("div", "bar", "")
			fill := el("i", "", "")
			fill.Get("style").Set("width", fmt.Sprintf("%.1f%%", l.Percent))
			bar.Call("appendChild", fill)
			body.Call("appendChild", bar)
		}
		var facts []string
		if text := liveEnded[l.Result]; text != "" {
			facts = append(facts, text)
		} else {
			now := fmt.Sprintf("%.0f%%", l.Percent)
			if l.Layers > 0 {
				now += fmt.Sprintf(" · layer %d of %d", l.Layer, l.Layers)
			}
			facts = append(facts, now)
			if l.Left > 0 {
				facts = append(facts, "About "+leftText(l.Left)+" left · done "+localWhen(time.Now().Add(time.Duration(l.Left)*time.Second)))
			}
		}
		facts = append(facts, "Started "+localTime(l.Started))
		if l.Printer != "" {
			facts = append(facts, "On "+article(l.Printer)+" "+l.Printer)
		}
		if l.URL != "" {
			facts = append(facts, "Open the live page")
		}
		lines := el("div", "tfacts", "")
		for _, f := range facts {
			lines.Call("appendChild", el("div", "", f))
		}
		body.Call("appendChild", lines)
		tile.Call("appendChild", body)
		box.Call("appendChild", tile)
	}
}

// leftText is a time left in words, for example "45 min" or "2 h 10 min".
func leftText(sec int) string {
	m := (sec + 59) / 60
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	return fmt.Sprintf("%d h %d min", m/60, m%60)
}

// fact sets the value of a line of facts, and hides the line with no value.
func fact(id, text string) {
	setText(id, text)
	byID(id).Get("parentElement").Set("hidden", text == "")
}

// drawPrint fills the page of the print p. photos are the photos of its project, and lapse
// returns the video of its time lapse, or the URLs of its frames.
func drawPrint(p wire.ProjectPrint, photos []wire.ProjectPhoto, lapse func() (wire.FramesReply, error), playing *int) {
	setText("prName", p.Name)
	doc.Set("title", p.Name+" · Printdeck")
	setText("prSub", printedOn("", p.Printer))
	ended := map[string]string{"complete": "Print finished", "cancelled": "The print was cancelled", "error": "The print stopped"}[p.Result]
	setText("doneTitle", ended)
	line := "Ended " + localDate(p.Ended) + "."
	if p.Result != "complete" && p.Layer > 0 && p.Layers > 0 {
		line += fmt.Sprintf(" It stopped at layer %d of %d.", p.Layer, p.Layers)
	}
	setText("doneText", line)

	// The readout.
	setText("left", clock(p.Seconds))
	setText("leftSub", "")
	if p.Estimate > 0 {
		setText("leftSub", "The slicer said "+clock(p.Estimate))
	}
	setText("layer", "-")
	setText("layers", "")
	if p.Layers > 0 {
		setText("layer", fmt.Sprint(max(p.Layer, 1)))
		setText("layers", fmt.Sprintf("of %d", p.Layers))
	}
	setText("gramsBig", "-")
	setText("gramsSub", "")
	if p.Grams > 0 {
		setText("gramsBig", grams(p.Grams))
	}
	if p.GramsTotal > 0 && p.Result != "complete" {
		setText("gramsSub", "of "+grams(p.GramsTotal))
	} else if m := p.Material; m != "" {
		setText("gramsSub", m)
	}
	progress := 1.0
	if p.Result != "complete" {
		progress = 0
		if p.Layers > 0 {
			progress = float64(p.Layer) / float64(p.Layers)
		}
	}
	pct := int(math.Round(progress * 100))
	byID("bar").Get("style").Set("width", fmt.Sprintf("%d%%", pct))
	byID("barWrap").Call("setAttribute", "aria-valuenow", pct)
	setText("pct", fmt.Sprintf("%d%%", pct))
	setText("activity", map[string]string{"complete": "Done", "cancelled": "Cancelled", "error": "Stopped"}[p.Result])

	// The model and the chart of the layers.
	setHidden("modelBox", p.Model == "")
	setHidden("chartBox", true)
	if p.Model != "" {
		byID("model").Set("src", p.Model)
		tag := "The finished part"
		if p.Result != "complete" {
			tag = "The part that printed"
		}
		setText("modelTag", tag)
	}
	setHidden("gview", true)
	setHidden("model", false)
	if p.Gcode != "" {
		showPrintGcode(p)
	}
	if p.Plan != "" {
		go func() {
			t, err := fetchText(p.Plan)
			var plan wire.SharePlan
			if err != nil || json.Unmarshal([]byte(t), &plan) != nil || len(plan.Layers) == 0 {
				return
			}
			setHidden("chartBox", false)
			cur := len(plan.Layers) + 1
			if p.Result != "complete" && p.Layer > 0 {
				cur = p.Layer
			}
			setText("layerNow", fmt.Sprintf("%d LAYERS", len(plan.Layers)))
			drawLayerChart(plan.Layers, nil, cur)
		}()
	}

	// The details.
	fact("started", "")
	if !p.Started.IsZero() {
		fact("started", localDate(p.Started))
	}
	fact("ended", localDate(p.Ended))
	fact("prep", "")
	if p.Prep > 0 {
		fact("prep", mmss(float64(p.Prep)))
	}
	fact("height", num(p.Height, "%.1f mm"))
	fact("material", "")
	if p.Filament == nil {
		fact("material", p.Material)
	}
	fact("slicer", p.Slicer)
	fact("printer", p.Printer)
	fact("file", p.File)
	setText("prNote", p.Note)
	setHidden("prNote", p.Note == "")
	drawPrintNotes(p)
	renderFilament(p.Filament)

	// The photos of the print.
	box := byID("prPhotos")
	box.Set("textContent", "")
	n := 0
	for _, ph := range photos {
		if ph.Print == p.ID {
			box.Call("appendChild", photoFigure(ph))
			n++
		}
	}
	// On the public page the key to add photos is hidden: the card shows only with photos.
	setHidden("prPhotosCard", n == 0 && byID("prPhotoAdd").Get("hidden").Bool())

	// The time lapse: the video, or the frames of a print from before the videos.
	setHidden("lapseBox", p.Frames == 0 && p.Video == "")
	*playing++
	showVideo(false)
	if p.Frames > 0 || p.Video != "" {
		go playLapse(lapse, *playing, playing)
	}
}

// showVideo shows the video element of the time lapse, or the canvas and the player of the
// frames.
func showVideo(on bool) {
	setHidden("lapseVideo", !on)
	setHidden("lapse", on)
	setHidden("lapsePlayer", on)
	if !on {
		v := byID("lapseVideo")
		v.Call("pause")
		v.Call("removeAttribute", "src")
		v.Call("removeAttribute", "poster")
		v.Call("load")
	}
}

// playLapse plays the time lapse of lapse: the video, or the frames.
func playLapse(lapse func() (wire.FramesReply, error), n int, playing *int) {
	fr, err := lapse()
	if *playing != n {
		return
	}
	if err == nil && fr.Video != "" {
		playVideo(fr, n, playing)
		return
	}
	if err != nil || len(fr.URLs) == 0 {
		setText("lapseTag", "No time lapse")
		return
	}
	playFrames(fr.URLs, fr.FPS, n, playing)
}

// playVideo plays the video of the time lapse in a loop with the controls of the browser. A
// viewer who asks for less motion sees the last frame, and starts the video with the controls.
// A new play (playing != n) stops it.
func playVideo(fr wire.FramesReply, n int, playing *int) {
	v := byID("lapseVideo")
	setText("lapseTag", "Time lapse")
	if fr.Poster != "" {
		v.Set("poster", fr.Poster)
	}
	reduce := js.Global().Call("matchMedia", "(prefers-reduced-motion: reduce)").Get("matches").Bool()
	v.Set("autoplay", !reduce)
	v.Set("muted", true)
	v.Set("src", fr.Video)
	// The number of the play on the element: the end of this play pauses only its own video.
	v.Get("dataset").Set("play", n)
	showVideo(true)
	if !reduce {
		// A browser can refuse the play with no gesture: the controls stay for the viewer.
		v.Call("play").Call("catch", js.FuncOf(func(js.Value, []js.Value) any { return nil }))
	}
	for *playing == n {
		time.Sleep(250 * time.Millisecond)
	}
	if v.Get("dataset").Get("play").String() == strconv.Itoa(n) {
		v.Call("pause")
	}
}

// lapseAheadFrames is the count of frames that the browser loads ahead of the frame that shows.
const lapseAheadFrames = 24

// playFrames plays the frames urls of a time lapse at fps on the canvas in a loop, with the key
// Pause and the slider. It loads the frames a few at a time ahead of the play. A new play
// (playing != n) ends it.
func playFrames(urls []string, fps, n int, playing *int) {
	cv := byID("lapse")
	ctx := cv.Call("getContext", "2d")
	seek, btn := byID("lapseSeek"), byID("lapsePlay")
	seek.Set("max", len(urls)-1)
	setText("lapseTag", fmt.Sprintf("Time lapse · %d frames", len(urls)))
	imgs := make([]js.Value, len(urls))
	load := func(i int) js.Value {
		if imgs[i].IsUndefined() {
			imgs[i] = js.Global().Get("Image").New()
			imgs[i].Set("src", urls[i])
		}
		return imgs[i]
	}
	// A viewer who asks for less motion sees the finished part, and plays the time lapse with the key.
	reduce := js.Global().Call("matchMedia", "(prefers-reduced-motion: reduce)").Get("matches").Bool()
	paused, at := reduce, 0
	if reduce {
		at = len(urls) - 1
	}
	shown := -1
	setPaused := func(p bool) {
		paused = p
		btn.Set("textContent", map[bool]string{true: "Play", false: "Pause"}[p])
	}
	setPaused(paused)
	var onBtn, onSeek js.Func
	onBtn = js.FuncOf(func(js.Value, []js.Value) any {
		if paused && at == len(urls)-1 {
			at = 0
		}
		setPaused(!paused)
		return nil
	})
	onSeek = js.FuncOf(func(js.Value, []js.Value) any {
		at = seek.Get("valueAsNumber").Int()
		setPaused(true)
		return nil
	})
	btn.Call("addEventListener", "click", onBtn)
	seek.Call("addEventListener", "input", onSeek)
	defer func() {
		btn.Call("removeEventListener", "click", onBtn)
		seek.Call("removeEventListener", "input", onSeek)
		onBtn.Release()
		onSeek.Release()
	}()
	step := time.Second / time.Duration(max(1, fps))
	for *playing == n {
		for i := at; i < min(len(urls), at+lapseAheadFrames); i++ {
			load(i)
		}
		img := load(at)
		ready := img.Get("complete").Bool() && img.Get("naturalWidth").Int() > 0
		if ready && shown != at {
			w, h := img.Get("naturalWidth").Int(), img.Get("naturalHeight").Int()
			if cv.Get("width").Int() != w || cv.Get("height").Int() != h {
				cv.Set("width", w)
				cv.Set("height", h)
			}
			ctx.Call("drawImage", img, 0, 0, w, h)
			shown = at
			seek.Set("value", at)
			setText("lapsePos", fmt.Sprintf("%d / %d", at+1, len(urls)))
		}
		wait := step
		if !paused && ready {
			if at == len(urls)-1 {
				wait += lapseHold
				at = 0
			} else {
				at++
			}
		}
		if paused || !ready {
			wait = 100 * time.Millisecond
		}
		time.Sleep(wait)
	}
}

// tilePhotos are the photos of each print by its ID, for the small pictures on the tiles.
var tilePhotos map[string][]wire.ProjectPhoto

// setTilePhotos keeps the photos of the prints of the project v for the tiles.
func setTilePhotos(v *wire.ProjectView) {
	tilePhotos = map[string][]wire.ProjectPhoto{}
	for _, ph := range v.Photos {
		if ph.Print != "" {
			tilePhotos[ph.Print] = append(tilePhotos[ph.Print], ph)
		}
	}
}

// printTile is the tile of the print p, a link to href: the picture, the badges, the name, and
// the facts. A print with photos has small pictures of up to three of them in a corner.
func printTile(p wire.ProjectPrint, href string) js.Value {
	tile := el("a", "tile", "")
	tile.Set("href", href)
	pic := el("div", "pic", "")
	if p.Model != "" {
		img := el("img", "", "")
		img.Set("src", p.Model)
		img.Set("alt", "")
		img.Set("loading", "lazy")
		pic.Call("appendChild", img)
	}
	badges := el("div", "badges", "")
	switch {
	case !p.Discarded.IsZero():
		badges.Call("appendChild", el("span", "fail", "Rejected"))
	case !p.Test.IsZero():
		badges.Call("appendChild", el("span", "", "Test"))
	case !p.InUse.IsZero():
		badges.Call("appendChild", el("span", "inuse", "In use"))
	case !p.Delivered.IsZero():
		badges.Call("appendChild", el("span", "ok", "Delivered "+shortDate(p.Delivered)))
	case !p.Ready.IsZero():
		badges.Call("appendChild", el("span", "ready", "Ready to pick up"))
	}
	if t := resultText[p.Result]; t != "" {
		badges.Call("appendChild", el("span", "fail", t))
	}
	if p.Frames > 0 || p.Video != "" {
		badges.Call("appendChild", el("span", "", "Time lapse"))
	}
	pic.Call("appendChild", badges)
	if phs := tilePhotos[p.ID]; len(phs) > 0 {
		row := el("div", "tphotos", "")
		row.Set("title", count(len(phs), "photo", "photos"))
		for i, ph := range phs {
			if i == 3 {
				row.Call("appendChild", el("span", "more", fmt.Sprintf("+%d", len(phs)-3)))
				break
			}
			img := el("img", "", "")
			img.Set("src", ph.Small)
			img.Set("alt", "")
			img.Set("loading", "lazy")
			row.Call("appendChild", img)
		}
		pic.Call("appendChild", row)
	}
	tile.Call("appendChild", pic)
	body := el("div", "body", "")
	body.Call("appendChild", el("strong", "", p.Name))
	// The date on one line, the time and the filament on the next. Each fact stays on one line.
	facts := []string{clock(p.Seconds)}
	if p.Grams > 0 {
		facts = append(facts, grams(p.Grams))
	}
	for i, f := range facts {
		facts[i] = strings.ReplaceAll(f, " ", "\u00a0")
	}
	lines := el("div", "tfacts", "")
	lines.Call("appendChild", el("div", "", shortDate(p.Ended)))
	lines.Call("appendChild", el("div", "", strings.Join(facts, " · ")))
	if name := tilePrinters[p.PrinterID]; name != "" {
		lines.Call("appendChild", el("div", "", name))
	}
	body.Call("appendChild", lines)
	tile.Call("appendChild", body)
	return tile
}

// drawPrintNotes shows the mark Rejected, In use, Delivered or Ready, the source of the model and the notes of the print p.
// The card of the notes hides on a page with no keys to write them (data-edit) and no notes.
func drawPrintNotes(p wire.ProjectPrint) {
	setHidden("prDelivered", p.Delivered.IsZero() && p.Ready.IsZero() && p.Discarded.IsZero() && p.InUse.IsZero())
	byID("prDelivered").Get("classList").Call("toggle", "ready", p.Delivered.IsZero() && p.Discarded.IsZero() && p.InUse.IsZero())
	byID("prDelivered").Get("classList").Call("toggle", "discarded", !p.Discarded.IsZero())
	byID("prDelivered").Get("classList").Call("toggle", "inuse", p.Discarded.IsZero() && !p.InUse.IsZero())
	switch {
	case !p.Discarded.IsZero():
		line := "Rejected " + p.Discarded.Local().Format("Mon Jan 2, 2006")
		if p.DiscardNote != "" {
			line += " · " + p.DiscardNote
		}
		setText("prDelivered", line)
	case !p.InUse.IsZero():
		line := "In use since " + p.InUse.Local().Format("Mon Jan 2, 2006")
		if !p.Delivered.IsZero() {
			line += " · delivered " + p.Delivered.Local().Format("Mon Jan 2")
		}
		setText("prDelivered", line)
	case !p.Delivered.IsZero():
		setText("prDelivered", "Delivered "+p.Delivered.Local().Format("Mon Jan 2, 2006"))
	case !p.Ready.IsZero():
		line := "Ready to pick up since " + p.Ready.Local().Format("Mon Jan 2, 3:04 PM")
		if p.ReadyNote != "" {
			line += " · " + p.ReadyNote
		}
		setText("prDelivered", line)
	}
	setHidden("prSourceRow", p.Source == "")
	src := byID("prSource")
	src.Set("href", p.Source)
	src.Set("textContent", sourceText(p.Source))
	drawNotes("pr", p.Notes, p.NotesBy, p.NotesAt)
	card := byID("prNotesCard")
	public := doc.Get("body").Get("classList").Call("contains", "public").Bool()
	if public || !card.Call("hasAttribute", "data-edit").Bool() {
		card.Set("hidden", p.Notes == "" && p.Source == "")
	}
}

// drawNotes shows the notes with the elements of the prefix: <prefix>NotesText and
// <prefix>NotesBy.
func drawNotes(prefix, notes, by string, at time.Time) {
	setText(prefix+"NotesText", notes)
	line := ""
	if notes != "" && by != "" {
		line = "By " + by
		if !at.IsZero() {
			line += ", " + at.Local().Format("Jan 2, 2006")
		}
	}
	setText(prefix+"NotesBy", line)
	setHidden(prefix+"NotesBy", line == "")
}

// sourceText is the short text of a link to a source: "owner/repo @ 3f2a9c1 · path/part.stl" for
// a file, a tree or a commit on GitHub, and the address for other links.
func sourceText(u string) string {
	rest, ok := strings.CutPrefix(u, "https://github.com/")
	if !ok {
		return strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) < 2 {
		return "github.com/" + rest
	}
	repo := parts[0] + "/" + parts[1]
	if len(parts) < 4 {
		return repo
	}
	switch parts[2] {
	case "blob", "tree", "commit", "raw":
		ref := parts[3]
		if len(ref) == 40 {
			ref = ref[:7]
		}
		out := repo + " @ " + ref
		if len(parts) > 4 {
			out += " · " + strings.Join(parts[4:], "/")
		}
		return out
	case "pull":
		return repo + " · pull request " + parts[3]
	}
	return repo
}

// showPrintGcode shows the G-code of the FDM print p in 3D in place of the picture of the model.
// A print that stopped shows the layers that printed in color, and the others in gray.
func showPrintGcode(p wire.ProjectPrint) {
	v := js.Global().Get("pwfView3d")
	if v.IsUndefined() {
		return
	}
	setHidden("modelBox", false)
	setHidden("gview", false)
	opts := js.Global().Get("Object").New()
	opts.Set("url", p.Gcode)
	opts.Set("compact", true)
	opts.Set("printer", p.Printer)
	if p.Result != "complete" && p.Layer > 0 {
		opts.Set("printed", p.Layer)
	}
	var ok, fail js.Func
	ok = js.FuncOf(func(js.Value, []js.Value) any {
		setHidden("model", true)
		return nil
	})
	fail = js.FuncOf(func(_ js.Value, args []js.Value) any {
		js.Global().Get("console").Call("warn", "the 3D view of the G-code:", args[0])
		setHidden("gview", true)
		setHidden("model", false)
		return nil
	})
	v.Call("showGcode", byID("gview"), opts).Call("then", ok).Call("catch", fail)
}
