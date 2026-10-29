//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"syscall/js"
	"time"

	"printdeck/internal/cloud/wire"
)

// The prints of a project on the app page and the public page: the numbers of the project, a
// tile for each print, and the page of one print. The page of a print is the share page of a
// finished print: the time lapse, the readout, the model with the chart of the layers, the
// details, the filament and the photos.

// hashArgs is the address after # as keys and values: "x=<id>&p=<print>".
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
		tile := el("a", "tile", "")
		tile.Set("href", href(p))
		pic := el("div", "pic", "")
		if p.Model != "" {
			img := el("img", "", "")
			img.Set("src", p.Model)
			img.Set("alt", "")
			img.Set("loading", "lazy")
			pic.Call("appendChild", img)
		}
		badges := el("div", "badges", "")
		if t := resultText[p.Result]; t != "" {
			badges.Call("appendChild", el("span", "fail", t))
		}
		if p.Frames > 0 {
			badges.Call("appendChild", el("span", "", "Time lapse"))
		}
		pic.Call("appendChild", badges)
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
		body.Call("appendChild", lines)
		tile.Call("appendChild", body)
		box.Call("appendChild", tile)
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
				facts = append(facts, "About "+leftText(l.Left)+" left")
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

// drawPrint fills the page of the print p. photos are the photos of its project, and frames
// returns the URLs of the frames of its time lapse.
func drawPrint(p wire.ProjectPrint, photos []wire.ProjectPhoto, frames func() ([]string, int, error), playing *int) {
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
	renderFilament(p.Filament)

	// The photos of the print.
	box := byID("prPhotos")
	box.Set("textContent", "")
	for _, ph := range photos {
		if ph.Print == p.ID {
			box.Call("appendChild", photoFigure(ph.URL, ph.Small, ph.By))
		}
	}

	// The time lapse.
	setHidden("lapseBox", p.Frames == 0)
	if p.Frames > 0 {
		*playing++
		go playFrames(frames, *playing, playing)
	}
}

// lapseAheadFrames is the count of frames that the browser loads ahead of the frame that shows.
const lapseAheadFrames = 24

// playFrames plays the time lapse on the canvas in a loop, with the key Pause and the slider. It
// loads the frames a few at a time ahead of the play. A new play (playing != n) ends it.
func playFrames(frames func() ([]string, int, error), n int, playing *int) {
	urls, fps, err := frames()
	if err != nil || len(urls) == 0 {
		setText("lapseTag", "No time lapse")
		return
	}
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
