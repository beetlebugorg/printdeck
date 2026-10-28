//go:build js && wasm

// Command pwf-web is the logic of the pages of the cloud service, compiled to WebAssembly. The
// page loads it with assets/pwf.js (wasm_exec.js of the Go release, and the lines that start
// it). The body of the page names the page: data-page="share" is a share link.
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
	}
	select {}
}

func byID(id string) js.Value { return doc.Call("getElementById", id) }

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
	view   string
	views  string // the views of the buttons, joined
	frames []js.Value
	fps    int
	shown  int
	canvas js.Value
	id     string
	owner  string // the owner key from the address, or ""
	ready  wire.ShareReady
	photos string // the photos that show, joined
	ended  bool
}

// runShare polls the status of the share at base and draws the page.
func runShare(base string) {
	s := &share{base: base, canvas: byID("lapse"), fps: 12}
	s.id = strings.TrimSuffix(strings.TrimPrefix(base, "/s/"), "/")
	if h := js.Global().Get("location").Get("hash").String(); strings.HasPrefix(h, "#owner=") {
		s.owner = strings.TrimPrefix(h, "#owner=")
		s.ownerPanel()
	}
	go s.play()
	go func() {
		for {
			s.poll()
			time.Sleep(5 * time.Second)
		}
	}()
}

func (s *share) poll() {
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
		setText("expires", "View only. Shared from a Printdeck. This link stops at "+localDay(rd.Expires)+".")
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
			s.poll()
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
	"done": "Done", "cancelled": "Cancelled", "error": "Stopped",
}

func (s *share) render(st wire.ShareStatus) {
	ended := st.State == "done" || st.State == "cancelled" || st.State == "error"
	if st.Job != "" {
		setText("job", st.Job)
		doc.Set("title", st.Job+" · Printdeck")
	}
	sub := "This page updates by itself. Leave it open."
	if st.Printer != "" {
		sub = "Printed on " + st.Printer + ". " + sub
	}
	setText("sub", sub)
	label := stateText[st.State]
	if label == "" {
		label = st.State
	}
	pill := byID("pill")
	if ended || st.State == "paused" {
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

	// The camera.
	if len(st.Views) > 0 {
		setHidden("camBox", false)
		if s.view == "" || !contains(st.Views, s.view) {
			s.view = st.Views[0]
		}
		s.viewButtons(st.Views)
		byID("cam").Set("src", fmt.Sprintf("%scam-%s.jpg?f=%d", s.base, s.view, st.Frame))
		age := time.Since(st.Updated).Round(time.Second)
		tag := "Live · updated " + fmt.Sprint(int(age.Seconds())) + " s ago"
		if ended {
			tag = "The last picture"
		}
		setText("camTag", tag)
	}

	// The readout.
	setText("leftLab", "Time left")
	setText("left", clock(st.TimeLeft))
	setText("leftSub", "")
	if st.TimeLeft >= 0 && !ended {
		setText("leftSub", "Done about "+localTime(st.Updated.Add(time.Duration(st.TimeLeft)*time.Second)))
	}
	if ended {
		setText("leftLab", "Print time")
		if !st.Started.IsZero() && !st.Ended.IsZero() {
			setText("left", clock(int(st.Ended.Sub(st.Started).Seconds())))
		}
	}
	if st.Layers > 0 {
		setText("layer", fmt.Sprint(st.Layer))
		setText("layers", fmt.Sprintf("of %d", st.Layers))
	}
	if st.ReadyLeft >= 0 {
		ready := st.Updated.Add(time.Duration(st.ReadyLeft) * time.Second)
		setText("ready", strings.Fields(localTime(ready))[0])
		setText("readySub", strings.TrimPrefix(localTime(ready), strings.Fields(localTime(ready))[0]))
	} else {
		setText("ready", "-")
		setText("readySub", "")
	}
	pct := int(math.Round(st.Progress * 100))
	byID("bar").Get("style").Set("width", fmt.Sprintf("%d%%", pct))
	byID("barWrap").Call("setAttribute", "aria-valuenow", pct)
	setText("pct", fmt.Sprintf("%d%%", pct))
	setText("activity", st.Activity)

	// The details.
	if st.Material != "" {
		setText("material", st.Material)
	}
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
		setText("expires", "View only. Shared from a Printdeck. This link stops at "+localDay(st.Expires)+".")
	}

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
		setHidden("lapse", false)
		note := fmt.Sprintf("%d frames, one for each layer or more.", st.Lapse)
		if !ended {
			note = fmt.Sprintf("%d frames so far. A new frame comes with each layer.", st.Lapse)
		}
		setText("lapseNote", note)
	}
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

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// viewTitles are the names of the camera views on the page.
var viewTitles = map[string]string{"main": "Camera", "full": "Whole picture", "part": "The part", "toolhead": "Toolhead"}

// viewButtons draws a button for each view when the share has more than one.
func (s *share) viewButtons(views []string) {
	key := strings.Join(views, ",") + "|" + s.view
	if key == s.views {
		return
	}
	s.views = key
	box := byID("views")
	box.Set("textContent", "")
	if len(views) < 2 {
		return
	}
	for _, v := range views {
		b := doc.Call("createElement", "button")
		b.Set("type", "button")
		title := viewTitles[v]
		if title == "" {
			title = v
		}
		b.Set("textContent", title)
		b.Call("setAttribute", "aria-pressed", fmt.Sprint(v == s.view))
		view := v
		b.Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any {
			s.view = view
			s.views = ""
			go s.poll()
			return nil
		}))
		box.Call("appendChild", b)
	}
}

// play shows the time lapse frames on the canvas in a loop.
func (s *share) play() {
	ctx := s.canvas.Call("getContext", "2d")
	for {
		fps := max(1, s.fps)
		time.Sleep(time.Second / time.Duration(fps))
		n := len(s.frames)
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
