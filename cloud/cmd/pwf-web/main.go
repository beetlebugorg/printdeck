//go:build js && wasm

// Command pwf-web is the logic of the pages of the cloud service, compiled to WebAssembly. The
// page loads it with assets/pwf.js (wasm_exec.js of the Go release, and the lines that start
// it). The body of the page names the page: data-page="share" is a share link, "app" the app
// page of the projects (app.go), and "public" the public page of a project.
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"syscall/js"
	"time"

	"printdeck/internal/cloud/wire"
)

var doc = js.Global().Get("document")

func main() {
	switch doc.Get("body").Call("getAttribute", "data-page").String() {
	case "share":
		runShare(strings.TrimSuffix(js.Global().Get("location").Get("pathname").String(), "/") + "/")
	case "app":
		runApp()
	case "public":
		runPublic()
	}
	select {}
}

// byID returns the element id, or a detached element when the page has none, so that a page
// from an older deploy draws all that it has.
func byID(id string) js.Value {
	e := doc.Call("getElementById", id)
	if e.IsNull() {
		return doc.Call("createElement", "div")
	}
	return e
}

func setText(id, s string) {
	if e := byID(id); !e.IsNull() {
		e.Set("textContent", s)
	}
}

func setHidden(id string, hidden bool) {
	if e := byID(id); !e.IsNull() {
		e.Set("hidden", hidden)
	}
}

// fetchText returns the text of url, or an error.
func fetchText(url string) (string, error) {
	status, text, err := fetchDo("GET", url, nil, js.Undefined())
	if err == nil && status/100 != 2 {
		err = fmt.Errorf("status %d", status)
	}
	return text, err
}

// fetchDo sends a request with the headers and the body (a string, a FormData, or undefined), and
// returns the status and the text of the reply. It waits for the promises of fetch.
func fetchDo(method, url string, headers map[string]string, body js.Value) (int, string, error) {
	type result struct {
		status int
		text   string
		err    error
	}
	ch := make(chan result, 1)
	var status int
	var then, fail, gotText js.Func
	fail = js.FuncOf(func(_ js.Value, args []js.Value) any {
		ch <- result{err: fmt.Errorf("fetch: %s", args[0].Call("toString").String())}
		return nil
	})
	gotText = js.FuncOf(func(_ js.Value, args []js.Value) any {
		ch <- result{status: status, text: args[0].String()}
		return nil
	})
	then = js.FuncOf(func(_ js.Value, args []js.Value) any {
		status = args[0].Get("status").Int()
		args[0].Call("text").Call("then", gotText).Call("catch", fail)
		return nil
	})
	defer then.Release()
	defer fail.Release()
	defer gotText.Release()
	opts := js.Global().Get("Object").New()
	opts.Set("method", method)
	opts.Set("cache", "no-store")
	h := js.Global().Get("Object").New()
	for k, v := range headers {
		h.Set(k, v)
	}
	opts.Set("headers", h)
	if !body.IsUndefined() {
		opts.Set("body", body)
	}
	js.Global().Call("fetch", url, opts).Call("then", then).Call("catch", fail)
	r := <-ch
	return r.status, r.text, r.err
}

// localTime is the time t as the clock of the viewer shows it, for example "3:41 PM".
func localTime(t time.Time) string {
	d := js.Global().Get("Date").New(float64(t.UnixMilli()))
	o := js.Global().Get("Object").New()
	o.Set("hour", "numeric")
	o.Set("minute", "2-digit")
	return d.Call("toLocaleTimeString", js.Undefined(), o).String()
}

// localDay is the day and time of t for the viewer, for example "Wed 3:41 PM".
func localDay(t time.Time) string {
	d := js.Global().Get("Date").New(float64(t.UnixMilli()))
	o := js.Global().Get("Object").New()
	o.Set("weekday", "short")
	o.Set("hour", "numeric")
	o.Set("minute", "2-digit")
	return d.Call("toLocaleString", js.Undefined(), o).String()
}

// clock is a duration in seconds as H:MM:SS.
func clock(sec int) string {
	if sec < 0 {
		return "--:--"
	}
	return fmt.Sprintf("%d:%02d:%02d", sec/3600, sec/60%60, sec%60)
}

// share is the state of a share page.
type share struct {
	base   string
	frames []js.Value
	fps    int
	shown  int
	canvas js.Value
	id     string
	owner  string // the owner key from the address, or ""
	ready  wire.ShareReady
	photos string // the photos that show, joined
	ended  bool
	plan   []float64 // the plan of the slicer for each layer, from plan.json
	// The last status, and the time on this computer when it came. The clocks count on from it
	// between the uploads (tick).
	st   wire.ShareStatus
	seen time.Time
	// The text of the clocks that shows, by ID.
	clocks map[string]string
}

// maxCount is the time that the clocks count on after the last new status: longer than the
// longest time between two polls (maxPoll). After it the deck sends nothing, and the clocks stop.
const maxCount = 3 * time.Minute

// runShare polls the status of the share at base and draws the page.
func runShare(base string) {
	s := &share{base: base, canvas: byID("lapse"), fps: 12}
	s.id = strings.TrimSuffix(strings.TrimPrefix(base, "/s/"), "/")
	if h := js.Global().Get("location").Get("hash").String(); strings.HasPrefix(h, "#owner=") {
		s.owner = strings.TrimPrefix(h, "#owner=")
		s.ownerPanel()
	}
	s.copyLink()
	go s.play()
	go func() {
		for {
			// 4 times a second, so that the clocks go on at each second and skip none.
			time.Sleep(250 * time.Millisecond)
			s.tick()
		}
	}()
	// A tab in the background polls nothing. When it shows again, it polls at once. The wait is
	// short sleeps: a timer of TinyGo in a select can fire seconds late.
	woken := false
	doc.Call("addEventListener", "visibilitychange", js.FuncOf(func(js.Value, []js.Value) any {
		woken = !hidden()
		return nil
	}))
	go func() {
		var readyAt time.Time
		for {
			woken = false
			s.pollStatus()
			if time.Since(readyAt) >= readyEvery || s.ended {
				s.pollReady()
				readyAt = time.Now()
			}
			next := time.Now().Add(s.nextPoll())
			for !woken && (hidden() || time.Now().Before(next)) {
				time.Sleep(250 * time.Millisecond)
			}
		}
	}()
}

// copyLink makes the key Copy link copy the address of the page for viewers: with no owner key.
// With no clipboard (an old browser, or a page with no HTTPS), the page shows the address to copy
// by hand.
func (s *share) copyLink() {
	loc := js.Global().Get("location")
	url := loc.Get("origin").String() + loc.Get("pathname").String()
	byID("copyBtn").Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any {
		clip := js.Global().Get("navigator").Get("clipboard")
		if clip.IsUndefined() || clip.IsNull() {
			setText("copyMsg", url)
			return nil
		}
		ok := js.FuncOf(func(js.Value, []js.Value) any {
			setText("copyMsg", "The link is copied.")
			return nil
		})
		fail := js.FuncOf(func(js.Value, []js.Value) any {
			setText("copyMsg", url)
			return nil
		})
		clip.Call("writeText", url).Call("then", ok, fail)
		return nil
	}))
}

// hidden says if the page is in a tab in the background.
func hidden() bool { return doc.Get("hidden").Truthy() }

// The times between two polls. The page polls once for each layer: just after the layer should
// end. The clocks count between the polls (tick).
const (
	// readyEvery is the time between two polls of ready.json during the print. Ready changes
	// once, at the end.
	readyEvery = 30 * time.Second
	// minPoll and maxPoll are the limits of the time between polls during the print. A layer
	// that takes longer than its plan gets a poll each lateEvery.
	minPoll   = 5 * time.Second
	maxPoll   = 2 * time.Minute
	lateEvery = 15 * time.Second
	// layerMargin is the time after the end of a layer until its status is in the bucket: the
	// deck uploads each 5 seconds.
	layerMargin = 6 * time.Second
)

// nextPoll is the time until the next poll of the status.
func (s *share) nextPoll() time.Duration {
	st := s.st
	switch {
	case s.seen.IsZero():
		return minPoll // no status yet
	case st.State == "done", st.State == "cancelled", st.State == "error":
		return time.Minute
	case st.State == "paused":
		return 30 * time.Second
	case st.State != "printing":
		return 15 * time.Second // heating
	}
	end, ok := s.layerEnd()
	if !ok {
		return 15 * time.Second
	}
	wait := time.Until(end) + layerMargin
	if wait < minPoll {
		// The layer takes longer than its plan.
		if time.Until(end) < 0 {
			return lateEvery
		}
		return minPoll
	}
	return min(wait, maxPoll)
}

// layerEnd is the time on this computer when the layer now should end: its start, and the time
// of its plan, scaled by the measured times of the layers that are done.
func (s *share) layerEnd() (time.Time, bool) {
	st := s.st
	if st.Layer < 1 || st.Layer > len(s.plan) || st.LayerStarted.IsZero() {
		return time.Time{}, false
	}
	plan, done := 0.0, 0.0
	for i, t := range st.LayerTimes {
		if t > 0 && i < len(s.plan) && s.plan[i] > 0 {
			plan, done = plan+s.plan[i], done+t
		}
	}
	scale := 1.0
	if plan > 0 {
		scale = done / plan
	}
	d := time.Duration(s.plan[st.Layer-1] * scale * float64(time.Second))
	// The clock of the deck is the time of the status, at the time it came here.
	return s.seen.Add(st.LayerStarted.Add(d).Sub(st.Updated)), true
}

// pollStatus gets status.json and draws the page.
func (s *share) pollStatus() {
	text, err := fetchText(s.base + "status.json?t=" + fmt.Sprint(time.Now().UnixMilli()))
	if err != nil {
		setText("pillText", "Waiting for the printer")
		return
	}
	var st wire.ShareStatus
	if json.Unmarshal([]byte(text), &st) != nil {
		return
	}
	s.render(st)
}

// pollReady gets ready.json and draws the flag of the owner and the photos.
func (s *share) pollReady() {
	if t, err := fetchText(s.base + "ready.json?t=" + fmt.Sprint(time.Now().UnixMilli())); err == nil {
		var rd wire.ShareReady
		if json.Unmarshal([]byte(t), &rd) == nil {
			s.renderReady(rd)
		}
	}
}

// renderReady draws the flag Ready to pick up, its note, and the photos.
func (s *share) renderReady(rd wire.ShareReady) {
	s.ready = rd
	setHidden("readyBanner", !rd.Ready)
	if rd.Ready {
		line := strings.TrimSpace(rd.Note)
		if line != "" && !strings.ContainsAny(line[len(line)-1:], ".!?") {
			line += "."
		}
		if !rd.At.IsZero() {
			line = strings.TrimSpace(line + " Ready since " + localDay(rd.At) + ".")
		}
		setText("readyText", line)
		setHidden("doneBanner", true)
	}
	if !rd.Expires.IsZero() {
		setText("expires", "View only. Shared with Printdeck. This link stops at "+localDay(rd.Expires)+".")
	}
	var keys []string
	for _, p := range rd.Photos {
		keys = append(keys, p.File)
	}
	if k := strings.Join(keys, ","); k != s.photos {
		s.photos = k
		box := byID("photos")
		box.Set("textContent", "")
		for _, p := range rd.Photos {
			a := doc.Call("createElement", "a")
			a.Set("href", s.base+p.File)
			img := doc.Call("createElement", "img")
			img.Set("src", s.base+p.Small)
			img.Set("alt", "A photo of the part")
			img.Set("loading", "lazy")
			a.Call("appendChild", img)
			box.Call("appendChild", a)
		}
		setHidden("photosCard", len(rd.Photos) == 0)
	}
	if s.owner != "" {
		btn := byID("readyBtn")
		if rd.Ready {
			btn.Set("textContent", "Not ready yet")
			btn.Call("setAttribute", "aria-pressed", "true")
		} else {
			btn.Set("textContent", "Ready to pick up")
			btn.Call("setAttribute", "aria-pressed", "false")
		}
	}
}

// ownerPanel shows the owner part of the page, and handles its photos and its Ready key.
func (s *share) ownerPanel() {
	setHidden("owner", false)
	key := map[string]string{"X-Share-Key": s.owner}
	api := "/api/v1/shares/" + s.id
	byID("photoFile").Call("addEventListener", "change", js.FuncOf(func(this js.Value, _ []js.Value) any {
		files := this.Get("files")
		n := files.Get("length").Int()
		list := make([]js.Value, n)
		for i := range n {
			list[i] = files.Index(i)
		}
		this.Set("value", "")
		go func() {
			for i, f := range list {
				setText("ownerMsg", fmt.Sprintf("Sending photo %d of %d…", i+1, len(list)))
				if err := s.sendPhoto(api, key, f); err != nil {
					setText("ownerMsg", err.Error())
					return
				}
			}
			setText("ownerMsg", "The photos are on the page.")
			s.pollReady()
		}()
		return nil
	}))
	byID("readyBtn").Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any {
		ready := !s.ready.Ready
		note := byID("note").Get("value").String()
		go func() {
			b, _ := json.Marshal(wire.ReadyRequest{Ready: ready, Note: note})
			h := map[string]string{"X-Share-Key": s.owner, "Content-Type": "application/json"}
			status, text, err := fetchDo("POST", api+"/ready", h, js.ValueOf(string(b)))
			if err != nil || status != 200 {
				setText("ownerMsg", "The service did not take it: "+errText(text, err))
				return
			}
			var rd wire.ShareReady
			if json.Unmarshal([]byte(text), &rd) == nil {
				s.renderReady(rd)
			}
			msg := "The page shows Ready to pick up."
			if !rd.Ready {
				msg = "The page shows Print finished."
			}
			setText("ownerMsg", msg)
		}()
		return nil
	}))
}

// sendPhoto uploads one photo with a grant of the service, and adds it to the share.
func (s *share) sendPhoto(api string, key map[string]string, file js.Value) error {
	status, text, err := fetchDo("POST", api+"/photo", key, js.Undefined())
	var pr wire.PhotoReply
	if err != nil || status != 200 || json.Unmarshal([]byte(text), &pr) != nil {
		return fmt.Errorf("The service refused the photo: %s", errText(text, err))
	}
	if t := file.Get("type").String(); t != "image/jpeg" && t != "image/png" {
		return fmt.Errorf("Send the photo as JPEG: %s is %s", file.Get("name").String(), t)
	}
	fd := js.Global().Get("FormData").New()
	for k, v := range pr.Grant.Fields {
		fd.Call("append", k, v)
	}
	fd.Call("append", "key", pr.Grant.Prefix+"photo")
	fd.Call("append", "Content-Type", file.Get("type").String())
	fd.Call("append", "Cache-Control", "no-store")
	fd.Call("append", "file", file)
	status, text, err = fetchDo("POST", pr.Grant.URL, nil, fd)
	if err != nil || status/100 != 2 {
		return fmt.Errorf("The upload failed: %s", errText(text, err))
	}
	status, text, err = fetchDo("POST", api+"/photo/"+pr.Photo+"/done", key, js.Undefined())
	if err != nil || status != 200 {
		return fmt.Errorf("The service refused the photo: %s", errText(text, err))
	}
	return nil
}

// errText is the error of a reply: its JSON error, or err.
func errText(text string, err error) string {
	if err != nil {
		return err.Error()
	}
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(text), &e) == nil && e.Error != "" {
		return e.Error
	}
	if len(text) > 120 {
		text = text[:120]
	}
	return text
}

var stateText = map[string]string{
	"heating": "Heating", "printing": "Printing", "paused": "Paused", "cooling": "Cooling",
	"done": "Done", "cancelled": "Cancelled", "error": "Stopped", "waiting": "Waiting for the printer",
}

func (s *share) render(st wire.ShareStatus) {
	if st.State == "waiting" {
		// The deck has no connection to the printer for a moment. The page keeps the last status,
		// and the clocks stop.
		byID("pill").Get("classList").Call("add", "still")
		setText("pillText", stateText[st.State])
		s.st.State = "paused"
		return
	}
	ended := st.State == "done" || st.State == "cancelled" || st.State == "error"
	if !st.Updated.Equal(s.st.Updated) {
		s.seen = time.Now()
	}
	s.st = st
	if st.Job != "" {
		setText("job", st.Job)
		doc.Set("title", st.Job+" · Printdeck")
	}
	sub := printedOn(st.Printer, st.PrinterModel) + " This page updates by itself. Leave it open."
	setText("sub", sub)
	label := stateText[st.State]
	if label == "" {
		label = st.State
	}
	pill := byID("pill")
	if ended || st.State == "paused" || st.State == "waiting" {
		pill.Get("classList").Call("add", "still")
	} else {
		pill.Get("classList").Call("remove", "still")
		label += " · Live"
	}
	setText("pillText", label)

	// The banner at the end. Ready to pick up is the flag of the owner (renderReady).
	s.ended = ended
	setHidden("doneBanner", !ended || s.ready.Ready)
	if ended {
		title := "Print finished"
		switch st.State {
		case "cancelled":
			title = "The print was cancelled"
		case "error":
			title = "The print stopped"
		}
		setText("doneTitle", title)
		line := ""
		if !st.Ended.IsZero() {
			line = "Ended at " + localTime(st.Ended) + "."
		}
		if st.State == "done" {
			if st.ReadyLeft > 0 {
				line += " The bed cools: the part comes off easily at about " + localTime(st.Updated.Add(time.Duration(st.ReadyLeft)*time.Second)) + "."
			} else {
				line += " The bed is cool."
			}
		}
		setText("doneText", strings.TrimSpace(line))
	}

	// The model: the whole part, with the part that is printed in color.
	if st.Model > 0 {
		setHidden("modelBox", false)
		byID("model").Set("src", fmt.Sprintf("%smodel.jpg?m=%d", s.base, st.Model))
		tag := "The finished part"
		if st.Layers > 0 && !ended {
			tag = fmt.Sprintf("Printed to layer %d of %d", st.Layer, st.Layers)
		}
		setText("modelTag", tag)
	}
	s.renderPrinter(st, ended)

	// The readout.
	setText("leftLab", "Time left")
	setText("leftSub", "")
	if st.TimeLeft >= 0 && !ended {
		setText("leftSub", "Done about "+localTime(st.Updated.Add(time.Duration(st.TimeLeft)*time.Second)))
	}
	if ended {
		setText("leftLab", "Print time")
	}
	s.tick()
	if st.Layers > 0 {
		setText("layer", fmt.Sprint(st.Layer))
	}
	if st.ReadyLeft >= 0 {
		ready := st.Updated.Add(time.Duration(st.ReadyLeft) * time.Second)
		setText("ready", strings.Fields(localTime(ready))[0])
		setText("readySub", strings.TrimPrefix(localTime(ready), strings.Fields(localTime(ready))[0]))
	} else {
		setText("ready", "-")
		setText("readySub", "")
	}
	setText("activity", st.Activity)

	// The details.
	if st.Material != "" {
		setText("material", st.Material)
	}
	// The card of the filament has the material.
	byID("material").Get("parentElement").Set("hidden", st.Filament != nil)
	if st.GramsTotal > 0 {
		setText("grams", grams(st.Grams)+" of "+grams(st.GramsTotal))
	} else if st.Grams > 0 {
		setText("grams", grams(st.Grams))
	}
	setText("tool", temp(st.Tool, st.ToolTarget))
	setText("bed", temp(st.Bed, st.BedTarget))
	if st.MaxHeight > 0 {
		setText("height", fmt.Sprintf("%.1f of %.1f mm", st.Height, st.MaxHeight))
	} else if st.Height > 0 {
		setText("height", fmt.Sprintf("%.1f mm", st.Height))
	}
	if !st.Started.IsZero() {
		setText("started", localDay(st.Started))
	}
	if !st.Expires.IsZero() {
		setText("expires", "View only. Shared with Printdeck. This link stops at "+localDay(st.Expires)+".")
	}

	s.renderFilament(st.Filament)

	// The time lapse.
	if st.LapseFPS > 0 {
		s.fps = st.LapseFPS
	}
	for i := len(s.frames); i < st.Lapse; i++ {
		img := js.Global().Get("Image").New()
		img.Set("src", fmt.Sprintf("%slapse/%d.jpg", s.base, i))
		s.frames = append(s.frames, img)
	}
	if st.Lapse > 0 {
		setHidden("lapseNote", true)
		tag := fmt.Sprintf("Time lapse · %d frames", st.Lapse)
		if !ended {
			tag += " · a new frame each layer"
		}
		setText("lapseTag", tag)
	}
}

// printedOn is the line of the printer, for example "Printed on a LulzBot TAZ Workhorse with
// Printdeck.". The name "a Printdeck" is the default host name of a deck.
func printedOn(name, model string) string {
	if name == "a Printdeck" {
		name = ""
	}
	switch {
	case model != "" && name != "":
		return "Printed on " + name + ", " + article(model) + " " + model + ", with Printdeck."
	case model != "":
		return "Printed on " + article(model) + " " + model + " with Printdeck."
	case name != "":
		return "Printed on " + name + " with Printdeck."
	}
	return "Printed with Printdeck."
}

// article is "an" for a name that starts with a vowel, else "a".
func article(name string) string {
	if name != "" && strings.ContainsRune("AEIOUaeiou", rune(name[0])) {
		return "an"
	}
	return "a"
}

// renderFilament fills the card of the filament. A line with no value hides.
func (s *share) renderFilament(f *wire.ShareFilament) {
	setHidden("filCard", f == nil)
	if f == nil {
		return
	}
	fact := func(id, text string) {
		setText(id, text)
		if e := byID(id); !e.IsNull() {
			e.Get("parentElement").Set("hidden", text == "")
		}
	}
	color := colorName(f.Color)
	name := f.Name
	if name == "" {
		name = strings.TrimSpace(color + " " + f.Type)
	}
	setText("filName", name)
	var sub []string
	for _, v := range []string{f.Brand, f.Type, color} {
		if v != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(v)) {
			sub = append(sub, v)
		}
	}
	setText("filSub", strings.Join(sub, " · "))
	dot := byID("filDot").Get("style")
	if f.Color != "" {
		dot.Set("background", f.Color)
	} else {
		dot.Set("background", "repeating-linear-gradient(45deg, var(--cap) 0 4px, var(--seam) 4px 8px)")
	}
	fact("filBrand", f.Brand)
	fact("filType", f.Type)
	if f.Color != "" {
		fact("filColor", strings.TrimSpace(color+" "+strings.ToLower(f.Color)))
	} else {
		fact("filColor", "")
	}
	fact("filDia", num(f.Diameter, "%.2f mm"))
	fact("filRoll", num(f.RollSize/1000, "%g kg"))
	fact("filLayer", num(f.LayerHeight, "%.2f mm"))
	temps := ""
	switch {
	case f.NozzleTemp > 0 && f.BedTemp > 0:
		temps = fmt.Sprintf("%d °C nozzle, %d °C bed", f.NozzleTemp, f.BedTemp)
	case f.NozzleTemp > 0:
		temps = fmt.Sprintf("%d °C nozzle", f.NozzleTemp)
	}
	fact("filTemps", temps)
}

// num is v in the format, or "" for 0.
func num(v float64, format string) string {
	if v <= 0 {
		return ""
	}
	return fmt.Sprintf(format, v)
}

// colorName is a plain name for a color #rrggbb, for example "Orange", or "".
func colorName(hex string) string {
	if len(hex) != 7 || hex[0] != '#' {
		return ""
	}
	c := hexColor(hex)
	r, g, b := float64(c[0])/255, float64(c[1])/255, float64(c[2])/255
	hi, lo := max(r, g, b), min(r, g, b)
	l := (hi + lo) / 2
	sat := 0.0
	if hi != lo {
		sat = (hi - lo) / (1 - math.Abs(2*l-1))
	}
	switch {
	case sat < 0.15 || hi-lo < 0.08:
		switch {
		case l < 0.15:
			return "Black"
		case l > 0.85:
			return "White"
		}
		return "Gray"
	}
	var hue float64
	switch hi {
	case r:
		hue = math.Mod((g-b)/(hi-lo), 6)
	case g:
		hue = (b-r)/(hi-lo) + 2
	default:
		hue = (r-g)/(hi-lo) + 4
	}
	hue = math.Mod(hue*60+360, 360)
	switch {
	case hue < 15 || hue >= 345:
		if l > 0.7 {
			return "Pink"
		}
		return "Red"
	case hue < 45:
		if l < 0.35 {
			return "Brown"
		}
		return "Orange"
	case hue < 70:
		return "Yellow"
	case hue < 160:
		return "Green"
	case hue < 195:
		return "Teal"
	case hue < 255:
		return "Blue"
	case hue < 290:
		return "Purple"
	}
	return "Pink"
}

// tick shows the clocks: the time left and the time of the layer now. Between two uploads they
// count on from the last status, at the rate of this computer. The time of the deck is the time
// of the status, and the time since it came here, so the clock of this computer can be wrong.
func (s *share) tick() {
	st := s.st
	if s.seen.IsZero() {
		return
	}
	if s.clocks == nil {
		s.clocks = map[string]string{}
	}
	setText := func(id, text string) {
		if s.clocks[id] != text {
			s.clocks[id] = text
			setText(id, text)
		}
	}
	ended := st.State == "done" || st.State == "cancelled" || st.State == "error"
	if ended {
		s.showProgress(st.Progress)
		if st.Layers > 0 {
			setText("layers", fmt.Sprintf("of %d", st.Layers))
		}
		if !st.Started.IsZero() && !st.Ended.IsZero() {
			setText("left", clock(int(st.Ended.Sub(st.Started).Seconds())))
		} else {
			setText("left", clock(st.TimeLeft))
		}
		setText("layerNow", "–")
		return
	}
	since := min(time.Since(s.seen), maxCount)
	if st.State != "printing" {
		since = 0
	}
	left := st.TimeLeft
	if left > 0 {
		left = max(0, left-int(since.Seconds()))
	}
	setText("left", clock(left))
	// The layer, and the time until the next layer.
	if st.Layers > 0 {
		layers := fmt.Sprintf("of %d", st.Layers)
		if end, ok := s.layerEnd(); ok && st.State == "printing" {
			if u := time.Until(end); u >= time.Second {
				layers += " · next in " + mmss(u.Seconds())
			} else {
				layers += " · next soon"
			}
		}
		setText("layers", layers)
	}
	// The progress goes on at the rate of the time left.
	p := st.Progress
	if st.TimeLeft > 0 {
		p += (1 - p) * min(1, since.Seconds()/float64(st.TimeLeft))
	}
	s.showProgress(p)
	now := "–"
	if n := len(s.plan); st.Layer >= 1 && st.Layer <= n {
		if !st.LayerStarted.IsZero() {
			now = mmss(max(0, st.Updated.Add(since).Sub(st.LayerStarted).Seconds()))
		}
		now += " / " + mmss(s.plan[st.Layer-1])
	}
	setText("layerNow", now)
}

// showProgress shows the progress p, 0 to 1, in the bar.
func (s *share) showProgress(p float64) {
	pct := int(math.Round(p * 100))
	text := fmt.Sprintf("%d%%", pct)
	if s.clocks["pct"] == text {
		return
	}
	s.clocks["pct"] = text
	byID("bar").Get("style").Set("width", text)
	byID("barWrap").Call("setAttribute", "aria-valuenow", pct)
	setText("pct", text)
}

// renderPrinter draws the lines of the printer under the progress: the move now and the factors
// of Tune, the roll in the printer, and the chart of the layer times, as the deck shows them.
func (s *share) renderPrinter(st wire.ShareStatus, ended bool) {
	var parts []string
	if st.Move != "" && !ended {
		parts = append(parts, fmt.Sprintf("%s · %.0f mm/s", st.Move, st.Speed))
	}
	if st.Fan > 0 && !ended {
		parts = append(parts, fmt.Sprintf("fan %d%%", st.Fan))
	}
	if st.SpeedPct > 0 && st.SpeedPct != 100 {
		parts = append(parts, fmt.Sprintf("speed %d%%", st.SpeedPct))
	}
	if st.FlowPct > 0 && st.FlowPct != 100 {
		parts = append(parts, fmt.Sprintf("flow %d%%", st.FlowPct))
	}
	setText("moveLine", strings.Join(parts, " · "))
	setHidden("moveLine", len(parts) == 0)

	setHidden("rollLine", st.Roll == nil)
	if r := st.Roll; r != nil {
		dot := byID("rollDot").Get("style")
		if r.Color != "" {
			dot.Set("background", r.Color)
		} else {
			dot.Set("background", "transparent")
		}
		f := 0.0
		if r.Full > 0 {
			f = min(1, r.Left/r.Full)
		}
		byID("rollBar").Get("style").Set("width", fmt.Sprintf("%.0f%%", f*100))
		setText("rollText", fmt.Sprintf("%s · %.0f g", r.Name, r.Left))
	}

	if st.Plan > 0 && len(s.plan) != st.Plan {
		if t, err := fetchText(s.base + "plan.json"); err == nil {
			var p wire.SharePlan
			if json.Unmarshal([]byte(t), &p) == nil {
				s.plan = p.Layers
			}
		}
	}
	setHidden("chartBox", len(s.plan) == 0)
	if len(s.plan) > 0 {
		s.drawChart(st, ended)
	}
}

// drawChart draws a row for each layer, or for each group of layers on a short chart: the
// measured time of a layer that is done, else the time of the slicer. Done layers are dim, the
// layer now is bright, and the layers to come are dark. The numbers are the time of the layer now
// and its plan, and the mean time of a layer.
func (s *share) drawChart(st wire.ShareStatus, ended bool) {
	plan := s.plan
	n := len(plan)
	cur := st.Layer
	if ended {
		cur = n + 1
	}
	at := func(layer int) float64 {
		if layer-1 < len(st.LayerTimes) && st.LayerTimes[layer-1] > 0 {
			return st.LayerTimes[layer-1]
		}
		return plan[layer-1]
	}
	sum, count := 0.0, 0
	for _, t := range st.LayerTimes {
		if t > 0 {
			sum, count = sum+t, count+1
		}
	}
	if count == 0 {
		for _, t := range plan {
			sum += t
		}
		count = n
	}
	setText("layerAvg", "AVG "+mmss(sum/float64(count))+" A LAYER")

	cv := byID("chart")
	dpr := js.Global().Get("devicePixelRatio").Float()
	w := int(cv.Get("clientWidth").Float() * dpr)
	h := int(cv.Get("clientHeight").Float() * dpr)
	if w <= 0 || h <= 0 {
		return
	}
	cv.Set("width", w)
	cv.Set("height", h)
	ctx := cv.Call("getContext", "2d")
	css := js.Global().Call("getComputedStyle", doc.Get("documentElement"))
	col := func(name string) string { return strings.TrimSpace(css.Call("getPropertyValue", name).String()) }
	done, nowCol, later := col("--lcd-dim"), col("--lcd-ink"), col("--lcd-cell")
	// One row for each layer when they fit, else for each group of layers: layer 1 at the bottom,
	// as the layers of the part. The length of a row is the time of its layers.
	gap := max(1, int(dpr))
	per := 1
	for n/per*(2*int(dpr)+gap) > h {
		per++
	}
	rows := (n + per - 1) / per
	rowH := min(int(8*dpr), max(int(2*dpr), h/max(1, rows)-gap))
	vals := make([]float64, rows)
	top := 1.0
	for b := range vals {
		k := 0
		for layer := b*per + 1; layer <= min(n, (b+1)*per); layer++ {
			vals[b] += at(layer)
			k++
		}
		vals[b] /= float64(max(1, k))
		top = max(top, vals[b])
	}
	ctx.Call("clearRect", 0, 0, w, h)
	for b, v := range vals {
		y := h - (b+1)*(rowH+gap)
		if y < 0 {
			break
		}
		first, last := b*per+1, min(n, (b+1)*per)
		c := later
		switch {
		case cur >= first && cur <= last:
			c = nowCol
		case last < cur:
			c = done
		}
		bw := max(1, int(v/top*float64(w)))
		ctx.Set("fillStyle", c)
		ctx.Call("fillRect", 0, y, bw, rowH)
	}
}

// mmss is a time in seconds as m:ss, or h:mm:ss from an hour.
func mmss(sec float64) string {
	t := int(sec + 0.5)
	if t >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", t/3600, t/60%60, t%60)
	}
	return fmt.Sprintf("%d:%02d", t/60, t%60)
}

// hexColor is the color of "#rrggbb", or black.
func hexColor(s string) [3]byte {
	var c [3]byte
	if len(s) == 7 && s[0] == '#' {
		for i := range 3 {
			fmt.Sscanf(s[1+2*i:3+2*i], "%02x", &c[i])
		}
	}
	return c
}

// grams is a weight of filament: one decimal under 10 g.
func grams(g float64) string {
	if g < 10 {
		return fmt.Sprintf("%.1f g", g)
	}
	return fmt.Sprintf("%.0f g", g)
}

// temp is a temperature and its target.
func temp(t, target float64) string {
	if target > 0 {
		return fmt.Sprintf("%.0f / %.0f °C", t, target)
	}
	return fmt.Sprintf("%.0f °C", t)
}

// lapseHold is the pause on the last frame of the time lapse before the loop starts again.
const lapseHold = 1500 * time.Millisecond

// play shows the time lapse frames on the canvas in a loop.
func (s *share) play() {
	ctx := s.canvas.Call("getContext", "2d")
	for {
		n := len(s.frames)
		fps := max(1, s.fps)
		if n < 12 {
			fps = min(fps, 4) // a short loop plays slower, so it does not flicker
		}
		wait := time.Second / time.Duration(fps)
		if n > 0 && s.shown == n-1 {
			wait += lapseHold // a pause on the last frame before the loop starts again
		}
		time.Sleep(wait)
		if n == 0 {
			continue
		}
		s.shown = (s.shown + 1) % n
		img := s.frames[s.shown]
		if !img.Get("complete").Bool() || img.Get("naturalWidth").Int() == 0 {
			continue
		}
		w, h := img.Get("naturalWidth").Int(), img.Get("naturalHeight").Int()
		if s.canvas.Get("width").Int() != w || s.canvas.Get("height").Int() != h {
			s.canvas.Set("width", w)
			s.canvas.Set("height", h)
		}
		ctx.Call("drawImage", img, 0, 0, w, h)
	}
}
