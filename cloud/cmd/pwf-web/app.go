//go:build js && wasm

package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"syscall/js"
	"time"

	"printdeck/internal/cloud/wire"
)

// The app page (data-page="app"): sign in with a passkey, the projects, one project, the passkey
// of the owner (/owner#code=<code>) and an invite (/j#<code>). The session token stays in
// sessionStorage, so it goes away when the tab closes.

// app is the state of the app page.
type app struct {
	token, kind, project, name string
	view                       *wire.ProjectView
	playing                    int // the number of the time lapse that plays; a new play ends the old one
}

const tokenKey = "pwf-session"

func runApp() {
	a := &app{}
	if v := sessionGet(tokenKey); v != "" {
		var in wire.SignInReply
		if json.Unmarshal([]byte(v), &in) == nil && time.Now().Before(in.Expires) {
			a.token, a.kind, a.project, a.name = in.Token, in.Kind, in.Project, in.Name
		}
	}
	on("signOut", "click", func() { a.signOut() })
	on("signInBtn", "click", func() { go a.signIn() })
	on("ownerBtn", "click", func() { go a.makeOwner() })
	on("joinBtn", "click", func() { go a.join() })
	onSubmit("newForm", func() { go a.newProject() })
	onSubmit("inviteForm", func() { go a.invite() })
	on("inviteCopy", "click", func() { go copyText(byID("inviteLink").Get("textContent").String(), "ownerToolsMsg") })
	on("inviteShare", "click", func() { shareLink(byID("inviteLink").Get("textContent").String()) })
	on("publicBtn", "click", func() { go a.togglePublic(false) })
	on("publicNew", "click", func() { go a.togglePublic(true) })
	on("publicCopy", "click", func() { go copyText(byID("publicLink").Get("textContent").String(), "ownerToolsMsg") })
	on("deleteProject", "click", func() { go a.deleteProject() })
	byID("photoFile").Call("addEventListener", "change", js.FuncOf(func(this js.Value, _ []js.Value) any {
		files := this.Get("files")
		go a.addPhotos(files)
		return nil
	}))
	js.Global().Call("addEventListener", "hashchange", js.FuncOf(func(js.Value, []js.Value) any {
		go a.route()
		return nil
	}))
	a.route()
}

// route shows the view of the address.
func (a *app) route() {
	loc := js.Global().Get("location")
	path := strings.TrimSuffix(loc.Get("pathname").String(), "/")
	hash := strings.TrimPrefix(loc.Get("hash").String(), "#")
	a.playing++
	a.showWho()
	switch {
	case path == "/owner":
		show("vOwner")
	case path == "/j":
		a.showJoin(hash)
	case a.token == "":
		show("vSignIn")
	case strings.HasPrefix(hash, "x="):
		a.showProject(strings.TrimPrefix(hash, "x="))
	case a.kind == "member":
		js.Global().Get("location").Set("hash", "x="+a.project)
	default:
		a.showHome()
	}
}

// The views of the page.
var views = []string{"vSignIn", "vOwner", "vJoin", "vHome", "vProject", "vPublic"}

func show(id string) {
	setHidden("loading", id != "")
	for _, v := range views {
		setHidden(v, v != id)
	}
	js.Global().Call("scrollTo", 0, 0)
}

func (a *app) showWho() {
	setHidden("who", a.token == "")
	setText("whoName", a.name)
}

// remember keeps the session of a sign-in.
func (a *app) remember(in wire.SignInReply) {
	a.token, a.kind, a.project, a.name = in.Token, in.Kind, in.Project, in.Name
	b, _ := json.Marshal(in)
	sessionSet(tokenKey, string(b))
}

func (a *app) signOut() {
	a.token, a.kind, a.project, a.name = "", "", "", ""
	sessionSet(tokenKey, "")
	js.Global().Get("location").Set("href", "/app")
}

// errUnauthorized is a reply 401: the session ended, or the person has no access.
var errUnauthorized = errors.New("sign in again")

// api sends a request to the API with the session, and reads the reply into out.
func (a *app) api(method, path string, in, out any) error {
	headers := map[string]string{"Content-Type": "application/json"}
	if a.token != "" {
		headers["Authorization"] = "Bearer " + a.token
	}
	body := js.Undefined()
	if in != nil {
		b, _ := json.Marshal(in)
		body = js.ValueOf(string(b))
	}
	status, text, err := fetchDo(method, "/api/v1"+path, headers, body)
	if err != nil {
		return err
	}
	if status == 401 && a.token != "" {
		sessionSet(tokenKey, "")
		a.token = ""
		return errUnauthorized
	}
	if status/100 != 2 {
		return errors.New(errText(text, nil))
	}
	if out != nil && status == 200 {
		return json.Unmarshal([]byte(text), out)
	}
	return nil
}

// say shows a message in the element id: an error in red.
func say(id string, err error, ok string) {
	e := byID(id)
	if err != nil {
		e.Get("classList").Call("add", "err")
		e.Set("textContent", err.Error())
		return
	}
	e.Get("classList").Call("remove", "err")
	e.Set("textContent", ok)
}

// The passkeys.

func (a *app) signIn() {
	say("signInMsg", nil, "Waiting for the passkey…")
	var begin wire.PasskeyBegin
	if err := a.api("POST", "/auth/login/begin", struct{}{}, &begin); err != nil {
		say("signInMsg", err, "")
		return
	}
	cred, err := getPasskey(string(begin.Options))
	if err != nil {
		say("signInMsg", err, "")
		return
	}
	var in wire.SignInReply
	if err := a.api("POST", "/auth/login/finish", wire.PasskeyRequest{State: begin.State, Credential: json.RawMessage(cred)}, &in); err != nil {
		say("signInMsg", err, "")
		return
	}
	a.remember(in)
	say("signInMsg", nil, "")
	a.route()
}

func codeOf(hash string) string { return strings.TrimPrefix(hash, "code=") }

func (a *app) makeOwner() {
	code := codeOf(strings.TrimPrefix(js.Global().Get("location").Get("hash").String(), "#"))
	if code == "" {
		say("ownerMsg", errors.New("This address has no code. On the deck: Settings, Cloud, Owner sign-in."), "")
		return
	}
	say("ownerMsg", nil, "Waiting for the passkey…")
	var begin wire.PasskeyBegin
	if err := a.api("POST", "/auth/owner/begin", wire.PasskeyRequest{Code: code}, &begin); err != nil {
		say("ownerMsg", err, "")
		return
	}
	cred, err := createPasskey(string(begin.Options))
	if err != nil {
		say("ownerMsg", err, "")
		return
	}
	name := strings.TrimSpace(byID("ownerName").Get("value").String())
	var in wire.SignInReply
	if err := a.api("POST", "/auth/owner/finish", wire.PasskeyRequest{Code: code, State: begin.State,
		Credential: json.RawMessage(cred), Name: name, Device: deviceName()}, &in); err != nil {
		say("ownerMsg", err, "")
		return
	}
	a.remember(in)
	js.Global().Get("history").Call("replaceState", nil, "", "/app")
	a.route()
}

func (a *app) showJoin(code string) {
	var v wire.InviteView
	err := a.api("POST", "/invites/view", wire.PasskeyRequest{Code: code}, &v)
	show("vJoin")
	if err != nil {
		setText("joinTitle", "This invite does not work")
		setText("joinText", "It is used, removed or expired. Ask for a new invite.")
		setHidden("joinBtn", true)
		return
	}
	setText("joinTitle", v.Owner+" invited you to "+v.Project)
	setText("joinText", "Join to see the prints, the time lapses and the photos, and to add your photos.")
	byID("joinName").Set("value", v.Name)
}

func (a *app) join() {
	code := strings.TrimPrefix(js.Global().Get("location").Get("hash").String(), "#")
	say("joinMsg", nil, "Waiting for the passkey…")
	var begin wire.PasskeyBegin
	if err := a.api("POST", "/auth/invite/begin", wire.PasskeyRequest{Code: code}, &begin); err != nil {
		say("joinMsg", err, "")
		return
	}
	cred, err := createPasskey(string(begin.Options))
	if err != nil {
		say("joinMsg", err, "")
		return
	}
	var in wire.SignInReply
	if err := a.api("POST", "/auth/invite/finish", wire.PasskeyRequest{Code: code, State: begin.State,
		Credential: json.RawMessage(cred), Name: strings.TrimSpace(byID("joinName").Get("value").String()), Device: deviceName()}, &in); err != nil {
		say("joinMsg", err, "")
		return
	}
	a.remember(in)
	js.Global().Get("history").Call("replaceState", nil, "", "/app#x="+in.Project)
	a.route()
}

// The projects.

func (a *app) showHome() {
	var list []wire.ProjectSummary
	if err := a.api("GET", "/projects", nil, &list); err != nil {
		a.failed(err)
		return
	}
	box := byID("projects")
	box.Set("textContent", "")
	for _, p := range list {
		card := el("a", "pcard", "")
		card.Set("href", "/app#x="+p.ID)
		if p.Cover != "" {
			img := el("img", "", "")
			img.Set("src", p.Cover)
			img.Set("alt", "")
			card.Call("appendChild", img)
		} else {
			card.Call("appendChild", el("div", "ph", ""))
		}
		text := el("div", "", "")
		text.Call("appendChild", el("strong", "", p.Name))
		text.Call("appendChild", el("span", "", summaryLine(p)))
		card.Call("appendChild", text)
		box.Call("appendChild", card)
	}
	setHidden("noProjects", len(list) > 0)
	setHidden("newForm", a.kind != "owner")
	show("vHome")
}

func summaryLine(p wire.ProjectSummary) string {
	parts := []string{count(p.Prints, "print", "prints"), count(p.Photos, "photo", "photos")}
	if p.Members > 0 {
		parts = append(parts, count(p.Members, "friend", "friends"))
	}
	return strings.Join(parts, " · ")
}

func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// failed shows the error of a view: sign in again for 401.
func (a *app) failed(err error) {
	if errors.Is(err, errUnauthorized) {
		a.showWho()
		show("vSignIn")
		say("signInMsg", err, "")
		return
	}
	show("vSignIn")
	say("signInMsg", err, "")
}

func (a *app) newProject() {
	in := wire.ProjectRequest{Name: strings.TrimSpace(byID("newName").Get("value").String()),
		Note: strings.TrimSpace(byID("newNote").Get("value").String())}
	var p wire.ProjectSummary
	if err := a.api("POST", "/projects", in, &p); err != nil {
		say("newMsg", err, "")
		return
	}
	byID("newName").Set("value", "")
	byID("newNote").Set("value", "")
	say("newMsg", nil, "")
	js.Global().Get("location").Set("hash", "x="+p.ID)
}

func (a *app) showProject(id string) {
	var v wire.ProjectView
	if err := a.api("GET", "/projects/"+id, nil, &v); err != nil {
		a.failed(err)
		return
	}
	a.view = &v
	a.drawProject()
	show("vProject")
}

// drawProject draws the view of the project.
func (a *app) drawProject() {
	v := a.view
	setText("pName", v.Name)
	setText("pNote", v.Note)
	setHidden("pNote", v.Note == "")
	setText("pMeta", "A project of "+v.Owner+" · "+count(len(v.Prints), "print", "prints"))
	setHidden("backLink", !v.IsOwner)
	setHidden("ownerTools", !v.IsOwner)
	setHidden("dangerZone", !v.IsOwner)
	setHidden("memberList", v.IsOwner || len(v.Members) == 0)
	// The members: the owner sees each with Remove; a member sees the names.
	list := byID("members")
	list.Set("textContent", "")
	var names []string
	for _, m := range v.Members {
		names = append(names, m.Name)
		li := el("li", "", "")
		state := "joined"
		if !m.Joined {
			state = "invited " + localDay(m.Invited)
		}
		left := el("div", "", m.Name+" ")
		left.Call("appendChild", el("span", "", state))
		li.Call("appendChild", left)
		rm := el("button", "link", "Remove")
		rm.Set("type", "button")
		mid, name := m.ID, m.Name
		rm.Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any {
			go a.removeMember(mid, name)
			return nil
		}))
		li.Call("appendChild", rm)
		list.Call("appendChild", li)
	}
	setText("memberNames", strings.Join(append([]string{v.Owner + " (owner)"}, names...), ", "))
	// The public address.
	on := v.Public != ""
	pb := byID("publicBtn")
	pb.Set("textContent", map[bool]string{true: "On", false: "Off"}[on])
	pb.Call("setAttribute", "aria-pressed", fmt.Sprint(on))
	setHidden("publicNew", !on)
	setHidden("publicBox", !on)
	setText("publicLink", v.Public)
	a.drawPhotos()
	a.drawPrints(func(p wire.ProjectPrint) func() ([]string, int, error) {
		return func() ([]string, int, error) {
			var fr wire.FramesReply
			err := a.api("GET", "/projects/"+v.ID+"/prints/"+p.ID+"/frames", nil, &fr)
			return fr.URLs, fr.FPS, err
		}
	}, &a.playing)
}

func (a *app) drawPhotos() {
	v := a.view
	box := byID("photos")
	box.Set("textContent", "")
	for _, ph := range v.Photos {
		fig := photoFigure(ph.URL, ph.Small, ph.By)
		if ph.Mine {
			rm := el("button", "rm", "Remove")
			rm.Set("type", "button")
			pid := ph.ID
			rm.Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any {
				go a.removePhoto(pid)
				return nil
			}))
			fig.Call("appendChild", rm)
		}
		box.Call("appendChild", fig)
	}
}

// photoFigure is a photo in the grid: the small copy, a link to the photo, and its author.
func photoFigure(full, small, by string) js.Value {
	fig := el("figure", "", "")
	link := el("a", "", "")
	link.Set("href", full)
	link.Set("target", "_blank")
	link.Set("rel", "noopener")
	img := el("img", "", "")
	img.Set("src", small)
	img.Set("alt", "A photo of "+by)
	img.Set("loading", "lazy")
	link.Call("appendChild", img)
	fig.Call("appendChild", link)
	fig.Call("appendChild", el("figcaption", "", by))
	return fig
}

// drawPrints draws the prints of the view. frames returns the request for the frames of the time
// lapse of a print; playing counts the plays.
func (a *app) drawPrints(frames func(wire.ProjectPrint) func() ([]string, int, error), playing *int) {
	drawPrintList(a.view.Prints, frames, playing)
}

func drawPrintList(prints []wire.ProjectPrint, frames func(wire.ProjectPrint) func() ([]string, int, error), playing *int) {
	box := byID("prints")
	box.Set("textContent", "")
	setHidden("noPrints", len(prints) > 0)
	for _, p := range prints {
		card := el("article", "print", "")
		pic := el("div", "pic", "")
		img := el("img", "", "")
		if p.Model != "" {
			img.Set("src", p.Model)
		}
		img.Set("alt", "The model of "+p.Name)
		pic.Call("appendChild", img)
		if p.Frames > 0 {
			btn := el("button", "", "Play the time lapse")
			btn.Set("type", "button")
			get := frames(p)
			btn.Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any {
				*playing++
				go playLapse(img, btn, get, *playing, playing)
				return nil
			}))
			pic.Call("appendChild", btn)
		}
		card.Call("appendChild", pic)
		body := el("div", "body", "")
		body.Call("appendChild", el("strong", "", p.Name))
		facts := []string{localDay(p.Ended), clock(p.Seconds)}
		if p.Material != "" {
			facts = append(facts, p.Material)
		}
		if p.Grams > 0 {
			facts = append(facts, grams(p.Grams))
		}
		if p.Printer != "" {
			facts = append(facts, p.Printer)
		}
		body.Call("appendChild", el("div", "facts", strings.Join(facts, " · ")))
		if p.Result != "complete" {
			body.Call("appendChild", el("div", "facts fail", map[string]string{"cancelled": "Cancelled", "error": "Failed"}[p.Result]))
		}
		if p.Note != "" {
			body.Call("appendChild", el("div", "dim", p.Note))
		}
		card.Call("appendChild", body)
		box.Call("appendChild", card)
	}
}

// playLapse plays the time lapse of a print in img, once, then shows the last frame. A new play
// (playing != n) ends it.
func playLapse(img, btn js.Value, get func() ([]string, int, error), n int, playing *int) {
	btn.Set("disabled", true)
	defer btn.Set("disabled", false)
	urls, fps, err := get()
	if err != nil || len(urls) == 0 {
		btn.Set("textContent", "No time lapse")
		return
	}
	// The browser loads the frames ahead of the play.
	pics := make([]js.Value, len(urls))
	for i, u := range urls {
		pics[i] = js.Global().Get("Image").New()
		pics[i].Set("src", u)
	}
	btn.Set("textContent", "Playing")
	step := time.Second / time.Duration(max(1, fps))
	for i := range urls {
		if *playing != n {
			break
		}
		img.Set("src", urls[i])
		time.Sleep(step)
	}
	btn.Set("textContent", "Play the time lapse")
}

// The keys of the owner.

func (a *app) invite() {
	name := strings.TrimSpace(byID("inviteName").Get("value").String())
	var inv wire.InviteReply
	if err := a.api("POST", "/projects/"+a.view.ID+"/invites", wire.InviteRequest{Name: name}, &inv); err != nil {
		say("ownerToolsMsg", err, "")
		return
	}
	byID("inviteName").Set("value", "")
	setText("inviteWho", name)
	setText("inviteLink", inv.URL)
	setHidden("inviteBox", false)
	setHidden("inviteShare", js.Global().Get("navigator").Get("share").IsUndefined())
	say("ownerToolsMsg", nil, "")
	a.reload()
}

func (a *app) removeMember(id, name string) {
	if !confirm("Remove " + name + " from the project? Their passkey stops working here.") {
		return
	}
	var v wire.ProjectView
	if err := a.api("DELETE", "/projects/"+a.view.ID+"/members/"+id, nil, &v); err != nil {
		say("ownerToolsMsg", err, "")
		return
	}
	a.view = &v
	a.drawProject()
}

func (a *app) togglePublic(renew bool) {
	on := a.view.Public == "" || renew
	if renew && !confirm("Make a new public address? The old address stops working.") {
		return
	}
	var out wire.PublicReply
	if err := a.api("POST", "/projects/"+a.view.ID+"/public", wire.PublicRequest{On: on, Renew: renew}, &out); err != nil {
		say("ownerToolsMsg", err, "")
		return
	}
	a.view.Public = out.URL
	a.drawProject()
	say("ownerToolsMsg", nil, "")
}

func (a *app) deleteProject() {
	if !confirm("Delete " + a.view.Name + "? The prints, the photos, the invites and the public address go. This cannot be undone.") {
		return
	}
	if err := a.api("DELETE", "/projects/"+a.view.ID, nil, nil); err != nil {
		say("ownerToolsMsg", err, "")
		return
	}
	js.Global().Get("location").Set("hash", "")
}

// reload reads the project again.
func (a *app) reload() {
	var v wire.ProjectView
	if a.api("GET", "/projects/"+a.view.ID, nil, &v) == nil {
		a.view = &v
		a.drawProject()
	}
}

// addPhotos uploads the files of the input, one at a time.
func (a *app) addPhotos(files js.Value) {
	n := files.Get("length").Int()
	for i := range n {
		f := files.Index(i)
		say("photoMsg", nil, fmt.Sprintf("Sending photo %d of %d…", i+1, n))
		if err := a.addPhoto(f); err != nil {
			say("photoMsg", err, "")
			return
		}
	}
	byID("photoFile").Set("value", "")
	say("photoMsg", nil, "")
	a.reload()
}

func (a *app) addPhoto(file js.Value) error {
	if t := file.Get("type").String(); t != "image/jpeg" && t != "image/png" {
		return fmt.Errorf("Share the photo as JPEG: %s is %s", file.Get("name").String(), t)
	}
	var pr wire.PhotoReply
	if err := a.api("POST", "/projects/"+a.view.ID+"/photos", struct{}{}, &pr); err != nil {
		return err
	}
	fd := js.Global().Get("FormData").New()
	for k, v := range pr.Grant.Fields {
		fd.Call("append", k, v)
	}
	fd.Call("append", "key", pr.Grant.Prefix+"photo")
	fd.Call("append", "Content-Type", file.Get("type").String())
	fd.Call("append", "Cache-Control", "no-store")
	fd.Call("append", "file", file)
	status, text, err := fetchDo("POST", pr.Grant.URL, nil, fd)
	if err != nil || status/100 != 2 {
		return fmt.Errorf("The upload failed: %s", errText(text, err))
	}
	return a.api("POST", "/projects/"+a.view.ID+"/photos/"+pr.Photo+"/done", struct{}{}, nil)
}

func (a *app) removePhoto(id string) {
	if !confirm("Remove this photo?") {
		return
	}
	var v wire.ProjectView
	if err := a.api("DELETE", "/projects/"+a.view.ID+"/photos/"+id, nil, &v); err != nil {
		say("photoMsg", err, "")
		return
	}
	a.view = &v
	a.drawProject()
}

// The public page (data-page="public"): page.json next to the page, with the paths of the files
// relative to it.
func runPublic() {
	base := strings.TrimSuffix(js.Global().Get("location").Get("pathname").String(), "/") + "/"
	text, err := fetchText(base + "page.json")
	var v wire.ProjectView
	if err != nil || json.Unmarshal([]byte(text), &v) != nil {
		setText("loading", "THIS PROJECT IS NOT PUBLIC NOW")
		return
	}
	setText("pName", v.Name)
	setText("pNote", v.Note)
	setHidden("pNote", v.Note == "")
	setText("pMeta", "A project of "+v.Owner+" · "+count(len(v.Prints), "print", "prints"))
	box := byID("photos")
	for _, ph := range v.Photos {
		box.Call("appendChild", photoFigure(base+ph.URL, base+ph.Small, ph.By))
	}
	for i := range v.Prints {
		if v.Prints[i].Model != "" {
			v.Prints[i].Model = base + v.Prints[i].Model
		}
	}
	playing := 0
	drawPrintList(v.Prints, func(p wire.ProjectPrint) func() ([]string, int, error) {
		return func() ([]string, int, error) {
			urls := make([]string, p.Frames)
			for n := range urls {
				urls[n] = fmt.Sprintf("%sprints/%s/lapse/%d.jpg", base, p.ID, n)
			}
			return urls, p.FPS, nil
		}
	}, &playing)
	show("vPublic")
}

// The helpers of the DOM and the browser.

// el makes an element with the class and the text.
func el(tag, class, text string) js.Value {
	e := doc.Call("createElement", tag)
	if class != "" {
		e.Set("className", class)
	}
	if text != "" {
		e.Set("textContent", text)
	}
	return e
}

// on calls f at the event of the element id.
func on(id, event string, f func()) {
	byID(id).Call("addEventListener", event, js.FuncOf(func(js.Value, []js.Value) any {
		f()
		return nil
	}))
}

// onSubmit calls f at the submit of the form id, with no page load.
func onSubmit(id string, f func()) {
	byID(id).Call("addEventListener", "submit", js.FuncOf(func(_ js.Value, args []js.Value) any {
		args[0].Call("preventDefault")
		f()
		return nil
	}))
}

func confirm(q string) bool { return js.Global().Call("confirm", q).Bool() }

func sessionGet(k string) string {
	defer func() { recover() }()
	v := js.Global().Get("sessionStorage").Call("getItem", k)
	if v.IsNull() {
		return ""
	}
	return v.String()
}

func sessionSet(k, v string) {
	defer func() { recover() }()
	if v == "" {
		js.Global().Get("sessionStorage").Call("removeItem", k)
		return
	}
	js.Global().Get("sessionStorage").Call("setItem", k, v)
}

// copyText copies a text to the clipboard, and says so in the element msg.
func copyText(s, msg string) {
	if _, err := await(js.Global().Get("navigator").Get("clipboard").Call("writeText", s)); err != nil {
		say(msg, errors.New("Copy did not work: select the link and copy it"), "")
		return
	}
	say(msg, nil, "The link is copied.")
}

// shareLink opens the share sheet of the phone with the link.
func shareLink(s string) {
	o := js.Global().Get("Object").New()
	o.Set("url", s)
	js.Global().Get("navigator").Call("share", o)
}

// deviceName is a short name of the device, for the list of the passkeys of the owner.
func deviceName() string {
	ua := js.Global().Get("navigator").Get("userAgent").String()
	for _, d := range []string{"iPhone", "iPad", "Android", "Macintosh", "Windows", "Linux"} {
		if strings.Contains(ua, d) {
			return d
		}
	}
	return "A browser"
}

// await waits for a promise, and returns its value or its error.
func await(p js.Value) (js.Value, error) {
	type result struct {
		v   js.Value
		err error
	}
	ch := make(chan result, 1)
	var then, fail js.Func
	then = js.FuncOf(func(_ js.Value, args []js.Value) any {
		ch <- result{v: args[0]}
		return nil
	})
	fail = js.FuncOf(func(_ js.Value, args []js.Value) any {
		ch <- result{err: errors.New(args[0].Call("toString").String())}
		return nil
	})
	defer then.Release()
	defer fail.Release()
	p.Call("then", then).Call("catch", fail)
	r := <-ch
	return r.v, r.err
}

// The WebAuthn calls of the browser. The options from the API have their binary fields in
// base64url; the browser needs ArrayBuffers. The credential goes back as JSON with base64url.

var b64url = base64.RawURLEncoding

// buf is the bytes of a base64url string as a Uint8Array.
func buf(s string) js.Value {
	b, _ := b64url.DecodeString(strings.TrimRight(s, "="))
	u := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(u, b)
	return u
}

// b64 is an ArrayBuffer in base64url.
func b64(v js.Value) string {
	if v.IsNull() || v.IsUndefined() {
		return ""
	}
	u := js.Global().Get("Uint8Array").New(v)
	b := make([]byte, u.Get("length").Int())
	js.CopyBytesToGo(b, u)
	return b64url.EncodeToString(b)
}

// fixIDs changes the field id of each item of the list to an ArrayBuffer.
func fixIDs(list js.Value) {
	if list.IsUndefined() || list.IsNull() {
		return
	}
	for i := range list.Get("length").Int() {
		item := list.Index(i)
		item.Set("id", buf(item.Get("id").String()))
	}
}

// errPasskey is the message of a passkey that the person stopped, or that the browser refused.
func errPasskey(err error) error {
	if strings.Contains(err.Error(), "NotAllowed") || strings.Contains(err.Error(), "AbortError") {
		return errors.New("The passkey did not finish. Try again.")
	}
	if strings.Contains(err.Error(), "InvalidState") {
		return errors.New("This device has a passkey for this already. Sign in with it.")
	}
	return errors.New("The passkey did not work: " + err.Error())
}

// createPasskey makes a passkey with the options of a registration, and returns the credential
// as JSON.
func createPasskey(options string) (string, error) {
	cred := js.Global().Get("navigator").Get("credentials")
	if cred.IsUndefined() || js.Global().Get("PublicKeyCredential").IsUndefined() {
		return "", errors.New("This browser has no passkeys. Use a newer browser.")
	}
	pk := js.Global().Get("JSON").Call("parse", options).Get("publicKey")
	pk.Set("challenge", buf(pk.Get("challenge").String()))
	pk.Get("user").Set("id", buf(pk.Get("user").Get("id").String()))
	fixIDs(pk.Get("excludeCredentials"))
	o := js.Global().Get("Object").New()
	o.Set("publicKey", pk)
	c, err := await(cred.Call("create", o))
	if err != nil {
		return "", errPasskey(err)
	}
	r := c.Get("response")
	resp := map[string]any{"clientDataJSON": b64(r.Get("clientDataJSON")), "attestationObject": b64(r.Get("attestationObject"))}
	if t := r.Get("getTransports"); !t.IsUndefined() {
		var ts []any
		list := r.Call("getTransports")
		for i := range list.Get("length").Int() {
			ts = append(ts, list.Index(i).String())
		}
		resp["transports"] = ts
	}
	return credentialJSON(c, resp), nil
}

// getPasskey signs in with a passkey with the options of a sign-in, and returns the credential as
// JSON.
func getPasskey(options string) (string, error) {
	cred := js.Global().Get("navigator").Get("credentials")
	if cred.IsUndefined() || js.Global().Get("PublicKeyCredential").IsUndefined() {
		return "", errors.New("This browser has no passkeys. Use a newer browser.")
	}
	pk := js.Global().Get("JSON").Call("parse", options).Get("publicKey")
	pk.Set("challenge", buf(pk.Get("challenge").String()))
	fixIDs(pk.Get("allowCredentials"))
	o := js.Global().Get("Object").New()
	o.Set("publicKey", pk)
	c, err := await(cred.Call("get", o))
	if err != nil {
		return "", errPasskey(err)
	}
	r := c.Get("response")
	resp := map[string]any{"clientDataJSON": b64(r.Get("clientDataJSON")), "authenticatorData": b64(r.Get("authenticatorData")),
		"signature": b64(r.Get("signature"))}
	if h := b64(r.Get("userHandle")); h != "" {
		resp["userHandle"] = h
	}
	return credentialJSON(c, resp), nil
}

// credentialJSON is the JSON of a PublicKeyCredential with the response resp.
func credentialJSON(c js.Value, resp map[string]any) string {
	out := map[string]any{"id": c.Get("id").String(), "rawId": b64(c.Get("rawId")), "type": c.Get("type").String(),
		"response": resp, "clientExtensionResults": map[string]any{}}
	if at := c.Get("authenticatorAttachment"); at.Type() == js.TypeString {
		out["authenticatorAttachment"] = at.String()
	}
	b, _ := json.Marshal(out)
	return string(b)
}
