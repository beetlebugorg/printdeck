//go:build js && wasm

package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"syscall/js"
	"time"

	"printdeck/internal/cloud/wire"
)

// The app page (data-page="app"): sign in with a passkey, the projects (/projects), one project
// (/projects/<slug>) and the page of one of its prints (/projects/<slug>/<n>), the passkey of
// the owner (/owner#code=<code>) and an invite (/j#<code>). The page also reads the old
// addresses, /app#x=<id>&p=<print>, and puts the new address in their place. The session token
// stays in sessionStorage, so it goes away when the tab closes.

// app is the state of the app page.
type app struct {
	token, kind, project, name string
	view                       *wire.ProjectView
	print                      string // the print of the print page, or ""
	playing                    int    // the number of the time lapse that plays; a new play ends the old one
	// The plan page: the plan, the choice of each part, the plates (the indexes of the parts of
	// each plate), and the part in the 3D view.
	plan        *wire.ProjectPlan
	planChoices []planChoice
	planPlates  [][]int
	planShown   int
	lastBuild    wire.ProjectBuild // the build of the build page
	previewShown string            // the build of the preview that shows
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
	on("joinKeyBtn", "click", func() { go a.joinWithPasskey() })
	onSubmit("newForm", func() { go a.newProject() })
	a.bindEdits()
	a.bindBuilds()
	onSubmit("inviteForm", func() { go a.invite() })
	on("inviteCopy", "click", func() { go copyText(byID("inviteLink").Get("textContent").String(), "ownerToolsMsg") })
	on("inviteShare", "click", func() { shareLink(byID("inviteLink").Get("textContent").String()) })
	on("publicBtn", "click", func() { go a.togglePublic() })
	on("publicCopy", "click", func() { go copyText(byID("publicLink").Get("textContent").String(), "ownerToolsMsg") })
	on("deleteProject", "click", func() { go a.deleteProject() })
	on("prCopy", "click", func() { go copyText(a.printLink(), "prCopyMsg") })
	on("prShare", "click", func() { shareLink(a.printLink()) })
	on("prRemove", "click", func() { go a.removePrint() })
	onFiles("photoFile", func(files []js.Value) { a.addPhotos(files, "", "photoMsg") })
	onFiles("prPhotoFile", func(files []js.Value) { a.addPhotos(files, a.print, "prPhotoMsg") })
	js.Global().Call("addEventListener", "hashchange", js.FuncOf(func(js.Value, []js.Value) any {
		if newView() {
			go a.route()
		}
		return nil
	}))
	onHistory(func() { go a.route() }, "/projects")
	lastView = viewKey()
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
		// An old address.
		args := hashArgs()
		a.showProject(args["x"], 0, args["p"])
	case strings.HasPrefix(path, "/projects/"):
		slug, n, _ := strings.Cut(strings.TrimPrefix(path, "/projects/"), "/")
		if kind, rest, ok := strings.Cut(n, "/"); ok && (kind == "plans" || kind == "builds") && rest != "" {
			var nr wire.NameReply
			if err := a.api("GET", "/names/"+slug, nil, &nr); err != nil {
				a.failed(err)
				return
			}
			if kind == "plans" {
				a.showPlanPage(nr.ID, rest)
			} else {
				a.showBuildPage(nr.ID, rest)
			}
			return
		}
		if pr, ok := strings.CutPrefix(n, "printers/"); ok {
			var nr wire.NameReply
			if err := a.api("GET", "/names/"+slug, nil, &nr); err != nil {
				a.failed(err)
				return
			}
			a.showPrinterPage(nr.ID, pr)
			return
		}
		a.showSlug(slug, n)
	case a.kind == "member" && a.project != "":
		// A member of one project goes to the project. A member of more projects sees the list.
		a.showProject(a.project, 0, "")
	default:
		if path != "/projects" {
			replaceAddress("/projects")
		}
		a.showHome()
	}
}

// projectPath is the address of the project with the slug, or of its print number n when n is
// not 0.
func projectPath(slug string, n int) string {
	if n > 0 {
		return "/projects/" + slug + "/" + strconv.Itoa(n)
	}
	return "/projects/" + slug
}

// The views of the page.
var views = []string{"vSignIn", "vOwner", "vJoin", "vHome", "vMissing", "vProject", "vPublic", "vPrint", "vPrinter", "vPlan", "vBuild"}

func show(id string) {
	setHidden("loading", id != "")
	if v := js.Global().Get("pwfView3d"); id != "vPrinter" && id != "vPlan" && id != "vBuild" && !v.IsUndefined() {
		v.Call("hide")
	}
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
	js.Global().Get("location").Set("href", "/projects")
}

// errUnauthorized is a reply 401: the session ended, or the person has no access.
var errUnauthorized = errors.New("sign in again")

// statusError is a reply of the API that is not 2xx, with its HTTP status.
type statusError struct {
	status int
	msg    string
}

func (e *statusError) Error() string { return e.msg }

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
		return &statusError{status, errText(text, nil)}
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
	replaceAddress("/projects")
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
		setHidden("joinKeyBtn", true)
		return
	}
	setText("joinTitle", v.Owner+" invited you to "+v.Project)
	setText("joinText", "Join to see the prints, the time lapses and the photos, and to add your photos.")
	byID("joinName").Set("value", v.Name)
	// A person who is signed in as a member has a passkey: it is the first choice.
	member := a.kind == "member"
	byID("joinKeyBtn").Get("classList").Call("toggle", "primary", member)
	byID("joinBtn").Get("classList").Call("toggle", "primary", !member)
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
	replaceAddress("/projects")
	a.route()
}

// joinWithPasskey joins the project of the invite with the passkey that the person has already:
// a sign-in with the invite code adds the project to the passkey.
func (a *app) joinWithPasskey() {
	code := strings.TrimPrefix(js.Global().Get("location").Get("hash").String(), "#")
	say("joinMsg", nil, "Waiting for the passkey…")
	var begin wire.PasskeyBegin
	if err := a.api("POST", "/auth/login/begin", struct{}{}, &begin); err != nil {
		say("joinMsg", err, "")
		return
	}
	cred, err := getPasskey(string(begin.Options))
	if err != nil {
		say("joinMsg", err, "")
		return
	}
	var in wire.SignInReply
	if err := a.api("POST", "/auth/login/finish", wire.PasskeyRequest{Code: code, State: begin.State, Credential: json.RawMessage(cred),
		Name: strings.TrimSpace(byID("joinName").Get("value").String())}, &in); err != nil {
		say("joinMsg", err, "")
		return
	}
	a.remember(in)
	replaceAddress("/projects")
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
		if p.Slug != "" {
			card.Set("href", projectPath(p.Slug, 0))
		}
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
	a.drawPrinterList()
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

// failed shows the error of a view: sign in again for 401, and "not found" for a project that
// is not there or not one of the projects of the person (410).
func (a *app) failed(err error) {
	var se *statusError
	if errors.As(err, &se) && se.status == 410 {
		doc.Set("title", "Project not found · Printdeck")
		show("vMissing")
		return
	}
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
	openAddress(projectPath(p.Slug, 0))
}

// showSlug shows the project of the address /projects/<slug>, or the page of its print number n
// when n is not "".
func (a *app) showSlug(slug, n string) {
	var nr wire.NameReply
	if err := a.api("GET", "/names/"+slug, nil, &nr); err != nil {
		a.failed(err)
		return
	}
	num, _ := strconv.Atoi(n)
	a.showProject(nr.ID, num, "")
}

// showProject shows the project id, or the page of its print: the print number n, or the print
// with the ID print. The address bar gets the address of the view: an old address or an old
// slug changes to the new address.
func (a *app) showProject(id string, n int, print string) {
	var v wire.ProjectView
	if err := a.api("GET", "/projects/"+id, nil, &v); err != nil {
		a.failed(err)
		return
	}
	a.view, a.print = &v, ""
	doc.Set("title", v.Name+" · Printdeck")
	for _, p := range v.Prints {
		if n > 0 && p.N == n || print != "" && p.ID == print {
			a.print = p.ID
			replaceAddress(projectPath(v.Slug, p.N))
			a.drawPrint(p)
			show("vPrint")
			return
		}
	}
	replaceAddress(projectPath(v.Slug, 0))
	a.drawProject()
	show("vProject")
	go a.pollProject(id, a.playing)
}

// pollProject draws the project id again each minute, or each 30 s while a print of the project
// prints, until the page shows another view. A tab in the background makes no polls. It draws
// again only the parts that changed: a new draw loads the pictures again.
func (a *app) pollProject(id string, gen int) {
	for {
		wait := time.Minute
		if len(a.view.Live) > 0 {
			wait = 30 * time.Second
		}
		next := time.Now().Add(wait)
		for hidden() || time.Now().Before(next) {
			if a.playing != gen {
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
		var v wire.ProjectView
		err := a.api("GET", "/projects/"+id, nil, &v)
		if a.playing != gen || errors.Is(err, errUnauthorized) {
			return
		}
		if err != nil {
			continue
		}
		old := a.view
		a.view = &v
		if printIDs(old.Prints) != printIDs(v.Prints) {
			drawPrints(&v, func(p wire.ProjectPrint) string { return projectPath(v.Slug, p.N) })
		}
		if photoIDs(old.Photos) != photoIDs(v.Photos) {
			a.drawPhotos()
		}
		drawLive(v.Live)
		if len(v.Live) > 0 {
			setHidden("noPrints", true)
		}
	}
}

// printIDs and photoIDs are the IDs of the prints and the photos, to find a change.
func printIDs(prints []wire.ProjectPrint) string {
	var b strings.Builder
	for _, p := range prints {
		b.WriteString(p.ID + p.Result + " ")
	}
	return b.String()
}

func photoIDs(photos []wire.ProjectPhoto) string {
	var b strings.Builder
	for _, p := range photos {
		b.WriteString(p.ID + " ")
	}
	return b.String()
}

// drawPrint draws the page of the print p of the project.
func (a *app) drawPrint(p wire.ProjectPrint) {
	v := a.view
	byID("prBack").Set("href", projectPath(v.Slug, 0))
	setText("prBack", v.Name)
	setHidden("prDanger", !v.IsOwner)
	setHidden("prOwnerKeys", !v.IsOwner)
	setHidden("prSourceEdit", !v.IsOwner)
	setText("prSourceEdit", "Set the source")
	if p.Source != "" {
		setText("prSourceEdit", "Change the source")
	}
	setText("prNotesEdit", "Write notes")
	if p.Notes != "" {
		setText("prNotesEdit", "Edit the notes")
	}
	byID("prDeliver").Call("setAttribute", "aria-pressed", strconv.FormatBool(!p.Delivered.IsZero()))
	for _, f := range []string{"prRenameForm", "prNotesForm", "prSourceForm"} {
		setHidden(f, true)
	}
	setHidden("prName", false)
	say("prNotesMsg", nil, "")
	setHidden("prShare", js.Global().Get("navigator").Get("share").IsUndefined())
	setText("prCopy", "Copy link")
	if v.Public != "" {
		setText("prCopy", "Copy public link")
	}
	say("prCopyMsg", nil, "")
	say("prPhotoMsg", nil, "")
	drawPrint(p, v.Photos, func() ([]string, int, error) {
		var fr wire.FramesReply
		err := a.api("GET", "/projects/"+v.ID+"/prints/"+p.ID+"/frames", nil, &fr)
		return fr.URLs, fr.FPS, err
	}, &a.playing)
}

// printLink is the address of the print page: the public address when the project has one, else
// the address for the members.
func (a *app) printLink() string {
	n := 0
	for _, p := range a.view.Prints {
		if p.ID == a.print {
			n = p.N
		}
	}
	if a.view.Public != "" {
		return a.view.Public + "/" + strconv.Itoa(n)
	}
	return js.Global().Get("location").Get("origin").String() + projectPath(a.view.Slug, n)
}

// removePrint removes the print of the print page from the project.
func (a *app) removePrint() {
	if !confirm("Remove this print from the project? Its time lapse and its picture go. The deck sends it again only after it leaves and joins the project again.") {
		return
	}
	if err := a.api("DELETE", "/projects/"+a.view.ID+"/prints/"+a.print, nil, nil); err != nil {
		say("prCopyMsg", err, "")
		return
	}
	openAddress(projectPath(a.view.Slug, 0))
}

// The edits of the pages: the name, the mark Delivered and the source of a print (the owner),
// and the notes of a print and of the project (the owner and the members).

func (a *app) bindEdits() {
	// open shows the form f with the value in its field, and hides the elements of hide.
	open := func(f, field, value string, hide ...string) {
		byID(field).Set("value", value)
		setHidden(f, false)
		for _, h := range hide {
			setHidden(h, true)
		}
		byID(field).Call("focus")
	}
	on("prRename", "click", func() {
		p := a.printNow()
		setText("prDeckName", "")
		if p.DeckName != "" {
			setText("prDeckName", "The name from the deck: "+p.DeckName+". Save an empty name to use it again.")
		}
		open("prRenameForm", "prRenameInput", p.Name, "prName")
	})
	on("prRenameCancel", "click", func() { setHidden("prRenameForm", true); setHidden("prName", false) })
	onSubmit("prRenameForm", func() {
		t := strings.TrimSpace(byID("prRenameInput").Get("value").String())
		go a.editPrint(wire.PrintEdit{Title: &t}, "prCopyMsg")
	})
	on("prDeliver", "click", func() {
		d := a.printNow().Delivered.IsZero()
		go a.editPrint(wire.PrintEdit{Delivered: &d}, "prCopyMsg")
	})
	on("prNotesEdit", "click", func() { open("prNotesForm", "prNotesInput", a.printNow().Notes, "prNotesText") })
	on("prNotesCancel", "click", func() { setHidden("prNotesForm", true); setHidden("prNotesText", false) })
	onSubmit("prNotesForm", func() {
		n := byID("prNotesInput").Get("value").String()
		go a.editPrint(wire.PrintEdit{Notes: &n}, "prNotesMsg")
	})
	on("prSourceEdit", "click", func() { open("prSourceForm", "prSourceInput", a.printNow().Source, "prSourceRow") })
	on("prSourceCancel", "click", func() { setHidden("prSourceForm", true); setHidden("prSourceRow", a.printNow().Source == "") })
	onSubmit("prSourceForm", func() {
		src := strings.TrimSpace(byID("prSourceInput").Get("value").String())
		go a.editPrint(wire.PrintEdit{Source: &src}, "prNotesMsg")
	})
	on("pNotesEdit", "click", func() { open("pNotesForm", "pNotesInput", a.view.Notes, "pNotesText") })
	on("pNotesCancel", "click", func() { setHidden("pNotesForm", true); setHidden("pNotesText", false) })
	onSubmit("pNotesForm", func() {
		n := byID("pNotesInput").Get("value").String()
		go func() {
			say("pNotesMsg", nil, "Saving…")
			if err := a.api("PATCH", "/projects/"+a.view.ID+"/notes", wire.NotesEdit{Notes: n}, nil); err != nil {
				say("pNotesMsg", err, "")
				return
			}
			say("pNotesMsg", nil, "")
			a.reload()
		}()
	})
}

// printNow returns the print of the print page that shows.
func (a *app) printNow() wire.ProjectPrint {
	if a.view != nil {
		for _, p := range a.view.Prints {
			if p.ID == a.print {
				return p
			}
		}
	}
	return wire.ProjectPrint{}
}

// editPrint sends the edit of the print that shows, and draws the print again. msg is the element
// of the messages.
func (a *app) editPrint(in wire.PrintEdit, msg string) {
	say(msg, nil, "Saving…")
	if err := a.api("PATCH", "/projects/"+a.view.ID+"/prints/"+a.print, in, nil); err != nil {
		say(msg, err, "")
		return
	}
	say(msg, nil, "")
	a.showProject(a.view.ID, 0, a.print)
}

// drawProject draws the view of the project.
func (a *app) drawProject() {
	v := a.view
	a.drawBuilds(v)
	drawNotes("p", v.Notes, v.NotesBy, v.NotesAt)
	setHidden("pNotesForm", true)
	setHidden("pNotesText", false)
	setText("pNotesEdit", "Write notes")
	if v.Notes != "" {
		setText("pNotesEdit", "Edit the notes")
	}
	setText("pName", v.Name)
	setText("pNote", v.Note)
	setHidden("pNote", v.Note == "")
	setText("pMeta", "A project of "+v.Owner)
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
	setHidden("publicBox", !on)
	setText("publicLink", v.Public)
	a.drawPhotos()
	drawPrints(v, func(p wire.ProjectPrint) string { return projectPath(v.Slug, p.N) })
	drawProjectPrinters(v)
	drawLive(v.Live)
	if len(v.Live) > 0 {
		setHidden("noPrints", true)
	}
}

func (a *app) drawPhotos() {
	v := a.view
	box := byID("photos")
	box.Set("textContent", "")
	for _, ph := range v.Photos {
		fig := photoFigure(ph)
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

// photoFigure is a photo in the grid: the small copy, a link to the photo, and its author. A tap
// on the link opens the photo viewer (lightbox.js), and the address of the photo has its ID.
func photoFigure(ph wire.ProjectPhoto) js.Value {
	fig := el("figure", "", "")
	link := el("a", "", "")
	link.Set("href", ph.URL)
	link.Get("dataset").Set("photo", ph.ID)
	caption := ph.By
	if !ph.Added.IsZero() {
		caption = strings.TrimPrefix(caption+" · "+ph.Added.Local().Format("Jan 2, 2006"), " · ")
	}
	link.Get("dataset").Set("caption", caption)
	img := el("img", "", "")
	img.Set("src", ph.Small)
	img.Set("alt", "A photo of "+ph.By)
	img.Set("loading", "lazy")
	link.Call("appendChild", img)
	fig.Call("appendChild", link)
	fig.Call("appendChild", el("figcaption", "", ph.By))
	return fig
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

func (a *app) togglePublic() {
	var out wire.PublicReply
	if err := a.api("POST", "/projects/"+a.view.ID+"/public", wire.PublicRequest{On: a.view.Public == ""}, &out); err != nil {
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
	openAddress("/projects")
}

// reload reads the project again.
func (a *app) reload() {
	var v wire.ProjectView
	if a.api("GET", "/projects/"+a.view.ID, nil, &v) == nil {
		a.view = &v
		a.drawProject()
	}
}

// addPhotos uploads the files, one at a time, as photos of the print (or "" for the project). msg
// is the element of the messages.
func (a *app) addPhotos(files []js.Value, print, msg string) {
	for i, f := range files {
		say(msg, nil, fmt.Sprintf("Sending photo %d of %d…", i+1, len(files)))
		if err := a.addPhoto(f, print); err != nil {
			say(msg, err, "")
			return
		}
	}
	say(msg, nil, "")
	if print != "" {
		a.showProject(a.view.ID, 0, print)
		say(msg, nil, "The photos are on the page.")
		return
	}
	a.reload()
}

func (a *app) addPhoto(file js.Value, print string) error {
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
	return a.api("POST", "/projects/"+a.view.ID+"/photos/"+pr.Photo+"/done", struct {
		Print string `json:"print,omitempty"`
	}{print}, nil)
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

// The public page (data-page="public") at /p/<slug>: page.json next to the page, with the paths
// of the files relative to it. /p/<slug>/<n> shows the page of the print number n. The old
// address of a print, #p=<print>, changes to the new address.
func runPublic() {
	parts := strings.Split(strings.Trim(js.Global().Get("location").Get("pathname").String(), "/"), "/")
	if len(parts) < 2 {
		setText("loading", "THIS PROJECT IS NOT PUBLIC NOW")
		return
	}
	home := "/p/" + parts[1]
	base := home + "/"
	text, err := fetchText(base + "page.json")
	var v wire.ProjectView
	if err != nil || json.Unmarshal([]byte(text), &v) != nil {
		setText("loading", "THIS PROJECT IS NOT PUBLIC NOW")
		return
	}
	for i := range v.Prints {
		if v.Prints[i].Model != "" {
			v.Prints[i].Model = base + v.Prints[i].Model
		}
		if v.Prints[i].Plan != "" {
			v.Prints[i].Plan = base + v.Prints[i].Plan
		}
	}
	for i := range v.Photos {
		v.Photos[i].URL, v.Photos[i].Small = base+v.Photos[i].URL, base+v.Photos[i].Small
	}
	setText("pName", v.Name)
	setText("pNote", v.Note)
	setHidden("pNote", v.Note == "")
	setText("pMeta", "A project of "+v.Owner)
	drawNotes("p", v.Notes, v.NotesBy, v.NotesAt)
	setHidden("pNotesCard", v.Notes == "")
	box := byID("photos")
	for _, ph := range v.Photos {
		box.Call("appendChild", photoFigure(ph))
	}
	setHidden("photosCard", len(v.Photos) == 0)
	printPath := func(p wire.ProjectPrint) string { return base + strconv.Itoa(p.N) }
	drawPrints(&v, printPath)
	byID("prBack").Set("href", home)
	setText("prBack", v.Name)
	setHidden("prShare", js.Global().Get("navigator").Get("share").IsUndefined())
	on("prCopy", "click", func() { go copyText(js.Global().Get("location").Get("href").String(), "prCopyMsg") })
	on("prShare", "click", func() { shareLink(js.Global().Get("location").Get("href").String()) })
	playing := 0
	route := func() {
		playing++
		parts := strings.Split(strings.Trim(js.Global().Get("location").Get("pathname").String(), "/"), "/")
		n := 0
		if len(parts) > 2 {
			n, _ = strconv.Atoi(parts[2])
		}
		id := hashArgs()["p"]
		for _, p := range v.Prints {
			if n > 0 && p.N == n || id != "" && p.ID == id {
				replaceAddress(printPath(p))
				say("prCopyMsg", nil, "")
				drawPrint(p, v.Photos, func() ([]string, int, error) {
					urls := make([]string, p.Frames)
					for n := range urls {
						urls[n] = fmt.Sprintf("%sprints/%s/lapse/%d.jpg", base, p.ID, n)
					}
					return urls, p.FPS, nil
				}, &playing)
				show("vPrint")
				return
			}
		}
		replaceAddress(home)
		doc.Set("title", v.Name+" · Printdeck")
		show("vPublic")
	}
	js.Global().Call("addEventListener", "hashchange", js.FuncOf(func(js.Value, []js.Value) any {
		if newView() {
			go route()
		}
		return nil
	}))
	onHistory(func() { go route() }, home)
	lastView = viewKey()
	route()
}

// The addresses of the pages. A link to a view of the same page opens the view with no new load
// of the page (onHistory).

// openAddress adds the address u to the history of the tab, and shows its view: the listener of
// onHistory runs.
func openAddress(u string) {
	js.Global().Get("history").Call("pushState", nil, "", u)
	js.Global().Call("dispatchEvent", js.Global().Get("PopStateEvent").New("popstate"))
}

// replaceAddress puts the address u in the address bar in place of the address of the view, with
// no new entry in the history. The address keeps only the hash of a photo (lightbox.js).
func replaceAddress(u string) {
	loc := js.Global().Get("location")
	hash := loc.Get("hash").String()
	if !strings.HasPrefix(hash, "#photo=") {
		hash = ""
	}
	if loc.Get("pathname").String() != u || loc.Get("hash").String() != hash {
		js.Global().Get("history").Call("replaceState", nil, "", u+hash)
	}
	lastView = viewKey()
}

// lastView is the address of the view that shows, with no hash of a photo.
var lastView string

// viewKey returns the address of the view now: the path and the hash, with no hash of a photo.
func viewKey() string {
	loc := js.Global().Get("location")
	hash := loc.Get("hash").String()
	if strings.HasPrefix(hash, "#photo=") {
		hash = ""
	}
	return loc.Get("pathname").String() + loc.Get("search").String() + hash
}

// newView reports that the address shows another view: a change of only the photo of the
// address (lightbox.js) shows the same view.
func newView() bool {
	k := viewKey()
	if k == lastView {
		return false
	}
	lastView = k
	return true
}

// onHistory runs show when the tab goes back or forward in its history, and for a click on a
// link to an address at prefix or under it. The click opens the address with openAddress. A
// click with a modifier key, for a new tab or window, works as a plain link.
func onHistory(show func(), prefix string) {
	js.Global().Call("addEventListener", "popstate", js.FuncOf(func(js.Value, []js.Value) any {
		if newView() {
			show()
		}
		return nil
	}))
	doc.Call("addEventListener", "click", js.FuncOf(func(_ js.Value, args []js.Value) any {
		e := args[0]
		if e.Get("defaultPrevented").Bool() || e.Get("button").Int() != 0 || e.Get("metaKey").Bool() ||
			e.Get("ctrlKey").Bool() || e.Get("shiftKey").Bool() || e.Get("altKey").Bool() {
			return nil
		}
		a := e.Get("target").Call("closest", "a")
		if a.IsNull() || a.Get("origin").String() != js.Global().Get("location").Get("origin").String() {
			return nil
		}
		p := a.Get("pathname").String()
		if p != prefix && !strings.HasPrefix(p, prefix+"/") {
			return nil
		}
		e.Call("preventDefault")
		openAddress(p)
		return nil
	}))
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

// onFiles calls f with the files that the person picks in the file input id, and empties the
// input, so that the same file can go again.
func onFiles(id string, f func([]js.Value)) {
	byID(id).Call("addEventListener", "change", js.FuncOf(func(this js.Value, _ []js.Value) any {
		list := this.Get("files")
		files := make([]js.Value, list.Get("length").Int())
		for i := range files {
			files[i] = list.Index(i)
		}
		this.Set("value", "")
		go f(files)
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
