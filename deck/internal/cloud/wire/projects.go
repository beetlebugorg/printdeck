package wire

import (
	"encoding/json"
	"time"
)

// The types of the projects (Parts with Friends) and of the sign-in with passkeys, which the
// deck, the API and the pages share.

// PasskeyRequest is the body of the requests of /api/v1/auth/: the owner code or the invite code,
// the state of the ceremony from its begin, and the credential of the browser (the JSON of a
// PublicKeyCredential) at the finish. Name is the first name of a new person, and Device the
// name of the phone or the computer.
type PasskeyRequest struct {
	Code       string          `json:"code,omitempty"`
	State      string          `json:"state,omitempty"`
	Credential json.RawMessage `json:"credential,omitempty"`
	Name       string          `json:"name,omitempty"`
	Device     string          `json:"device,omitempty"`
}

// PasskeyBegin is the reply to the begin of a ceremony: the options for
// navigator.credentials (with "publicKey"), and the state for the finish.
type PasskeyBegin struct {
	Options json.RawMessage `json:"options"`
	State   string          `json:"state"`
}

// PasskeyInfo is a passkey of the owner, in the reply to GET /api/v1/me/passkeys. ID is the
// credential ID in base64url, and This reports the passkey of the session.
type PasskeyInfo struct {
	ID     string    `json:"id"`
	Device string    `json:"device,omitempty"`
	Added  time.Time `json:"added"`
	This   bool      `json:"this,omitempty"`
}

// SignInReply is the reply to the finish of a ceremony: the session token, for the header
// Authorization: Bearer <token>.
type SignInReply struct {
	Token   string    `json:"token"`
	Kind    string    `json:"kind"` // "owner" or "member"
	Project string    `json:"project,omitempty"`
	Name    string    `json:"name,omitempty"`
	Expires time.Time `json:"expires"`
}

// OwnerCodeReply is the reply to POST /api/v1/owner/code: the code, and the page that makes the
// passkey of the owner with it.
type OwnerCodeReply struct {
	Code    string    `json:"code"`
	URL     string    `json:"url"`
	Expires time.Time `json:"expires"`
}

// ProjectRequest is the body of POST /api/v1/projects and of PATCH /api/v1/projects/<id>.
type ProjectRequest struct {
	Name string `json:"name"`
	Note string `json:"note,omitempty"`
}

// ProjectSummary is a project in a list.
type ProjectSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Slug is the name of the project in its address: /projects/<slug>.
	Slug    string    `json:"slug,omitempty"`
	Note    string    `json:"note,omitempty"`
	Prints  int       `json:"prints"`
	Photos  int       `json:"photos"`
	Members int       `json:"members"`
	Changed time.Time `json:"changed"`
	// URL is the page of the project for the owner and the members.
	URL string `json:"url"`
	// Cover is the URL of a picture of the project (the cover of printdeck.yml, the newest photo or a model), or "".
	Cover string `json:"cover,omitempty"`
}

// ProjectView is a project as its page shows it. The URLs of the files work for one hour. A
// public page has the same fields, with the paths of the files relative to the page, and no
// members.
type ProjectView struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
	// Slug is the name of the project in its address: /projects/<slug>, or /p/<slug> for the
	// public page.
	Slug string `json:"slug,omitempty"`
	Note string `json:"note,omitempty"`
	// Notes are the notes of the people of the project, with the name of the last person who
	// changed them, and the time.
	Notes   string    `json:"notes,omitempty"`
	NotesBy string    `json:"notes_by,omitempty"`
	NotesAt time.Time `json:"notes_at,omitzero"`
	Owner   string    `json:"owner"` // the first name of the owner
	Created time.Time `json:"created"`
	Changed time.Time `json:"changed"`
	// You is the person who asks: "owner", or the ID of the member. IsOwner lets the page show
	// the keys of the owner.
	You     string          `json:"you,omitempty"`
	IsOwner bool            `json:"is_owner,omitempty"`
	// CanSlice: the person who asks can choose the parts of a plan and slice them (the owner, or
	// a member with ProjectMember.Slice).
	CanSlice bool `json:"can_slice,omitempty"`
	Members []ProjectMember `json:"members,omitempty"`
	Prints  []ProjectPrint  `json:"prints"`
	Photos  []ProjectPhoto  `json:"photos"`
	// Repo is the GitHub repository of the project, "owner/name", or "". Plans are its newest
	// plans with their builds, the newest first.
	Repo  string        `json:"repo,omitempty"`
	Plans []ProjectPlan `json:"plans,omitempty"`
	// PlansTotal is the count of all the plans of the project. Plans has the newest of them.
	PlansTotal int `json:"plans_total,omitempty"`
	// Public is the public address of the project, or "" when it has none.
	Public string `json:"public,omitempty"`
	// Live are the prints of the project that print now, or that ended and are not up yet.
	Live []ProjectLive `json:"live,omitempty"`
	// Printers are the printers of the prints and the live tiles of the project.
	Printers []Printer `json:"printers,omitempty"`
	// Queue are the files of the project in the queues of the printers, the next first.
	Queue []ProjectQueued `json:"queue,omitempty"`
	// Docs are the PDFs of the newest ready plan of the default branch, else of the newest ready
	// plan of a tag.
	Docs []ProjectDoc `json:"docs,omitempty"`
	// Cover is the path of the cover picture on a public page, or "".
	Cover string `json:"cover,omitempty"`
	// RepoOnly marks a public page of a public repository only: the owner's public page is off.
	// It has the Parts tab, the docs and the cover, and no prints, photos, notes or names. A
	// public page with Repo has the content of the public repository: Plans has the newest
	// ready plan of the default branch.
	RepoOnly bool `json:"repo_only,omitempty"`
	// RepoPage is the address of the public page of the public repository, for the owner, when
	// the owner's public page is off. The settings say that the page is there.
	RepoPage string `json:"repo_page,omitempty"`
}

// ProjectLive is a print of a project that prints now: its name, the share link or "", the start,
// and the progress that the deck sent last.
type ProjectLive struct {
	Name    string    `json:"name"`
	URL     string    `json:"url,omitempty"`
	Started time.Time `json:"started"`
	// Model is the URL of the picture of the model, or "". LayerPic is the URL of the picture of
	// the layer that a resin printer cures now, or "".
	Model    string    `json:"model,omitempty"`
	LayerPic string    `json:"layer_pic,omitempty"`
	Percent  float64   `json:"percent,omitempty"`
	Layer    int       `json:"layer,omitempty"`
	Layers   int       `json:"layers,omitempty"`
	Left     int       `json:"left,omitempty"` // seconds, or 0 for no estimate
	Paused   bool      `json:"paused,omitempty"`
	Result   string    `json:"result,omitempty"` // "" while it prints; "complete", "cancelled" or "error"
	Updated  time.Time `json:"updated,omitzero"`
	// Printer is the name of the printer, and PrinterID its ID at the service, or "" for a deck
	// from before the printers.
	Printer   string `json:"printer,omitempty"`
	PrinterID string `json:"printer_id,omitempty"`
}

// ProjectMember is a member of a project, or an invite that waits.
type ProjectMember struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Joined  bool      `json:"joined"`
	Invited time.Time `json:"invited"`
	// GitHub is the GitHub username of the member, which the owner sets: the plans of the
	// member's fork come in with it.
	GitHub string `json:"github,omitempty"`
	// Slice: the member can choose the parts of a plan and slice them.
	Slice bool `json:"slice,omitempty"`
}

// MemberEdit is the body of PATCH /api/v1/projects/<id>/members/<member>. Each field that is in
// the body changes: the GitHub username of the member ("" for none), and Slice.
type MemberEdit struct {
	GitHub *string `json:"github,omitempty"`
	Slice  *bool   `json:"slice,omitempty"`
}

// ProjectPrint is a print of a project, from the history record of the deck.
type ProjectPrint struct {
	ID string `json:"id"`
	// N is the number of the print in its project, from 1: the address of its page ends in /<n>.
	N    int    `json:"n,omitempty"`
	Name string `json:"name"`
	// DeckName is the name of the print from the deck, when the owner gave it another name.
	DeckName string `json:"deck_name,omitempty"`
	// Notes are the notes of the people of the project about the print, with the name of the
	// last person who changed them, and the time.
	Notes   string    `json:"notes,omitempty"`
	NotesBy string    `json:"notes_by,omitempty"`
	NotesAt time.Time `json:"notes_at,omitzero"`
	// Delivered is the time when the owner marked the print as delivered, or zero. Source is the
	// link to the source of the model.
	Delivered time.Time `json:"delivered,omitzero"`
	Source    string    `json:"source,omitempty"`
	// Ready is the time of the mark Ready to pick up, or zero, and ReadyNote its note.
	Ready     time.Time `json:"ready,omitzero"`
	ReadyNote string    `json:"ready_note,omitempty"`
	// Test is the time when the owner marked the print as a test print, or zero.
	Test time.Time `json:"test,omitzero"`
	// Discarded is the time when the owner marked the print as not good enough, or zero, and
	// DiscardNote the note of why.
	Discarded   time.Time `json:"discarded,omitzero"`
	DiscardNote string    `json:"discard_note,omitempty"`
	File        string    `json:"file,omitempty"`
	Result      string    `json:"result"` // "complete", "cancelled" or "error"
	Ended       time.Time `json:"ended"`
	Seconds     int       `json:"seconds"`
	Material    string    `json:"material,omitempty"`
	Grams       float64   `json:"grams,omitempty"`
	Roll        string    `json:"roll,omitempty"`
	Printer     string    `json:"printer,omitempty"`
	// PrinterID is the printer at the service, or "" for a print from before the printers.
	PrinterID string `json:"printer_id,omitempty"`
	Note      string `json:"note,omitempty"`
	// The details of the print page: the start, the layers of the file and the last layer that
	// printed, the height of the part in mm, the grams of the file, the estimate of the slicer and
	// the seconds before the first layer, in seconds, the slicer, and the filament.
	Started    time.Time      `json:"started,omitzero"`
	Layers     int            `json:"layers,omitempty"`
	Layer      int            `json:"layer,omitempty"`
	Height     float64        `json:"height,omitempty"`
	GramsTotal float64        `json:"grams_total,omitempty"`
	Estimate   int            `json:"estimate,omitempty"`
	Prep       int            `json:"prep,omitempty"`
	Slicer     string         `json:"slicer,omitempty"`
	Filament   *ShareFilament `json:"filament,omitempty"`
	// Build and Commit are the Printdeck build and the commit of the file, or "".
	Build  string `json:"build,omitempty"`
	Commit string `json:"commit,omitempty"`
	// Model is the URL of the picture of the model, and Plan the URL of plan.json (a SharePlan),
	// or "". Frames is the count of the frames of the time lapse, at FPS frames a second.
	Model string `json:"model,omitempty"`
	Plan  string `json:"plan,omitempty"`
	// Gcode is the URL of the G-code of an FDM print with gzip, for the 3D view, or "".
	Gcode  string `json:"gcode,omitempty"`
	Frames int    `json:"frames,omitempty"`
	FPS    int    `json:"fps,omitempty"`
	// Video is the URL of the MP4 video of the time lapse, and Poster the URL of its last frame,
	// or "". A print with no video has only its frames.
	Video  string `json:"video,omitempty"`
	Poster string `json:"poster,omitempty"`
}

// ProjectPhoto is a photo of a project.
type ProjectPhoto struct {
	ID    string    `json:"id"`
	By    string    `json:"by"` // the first name of the person who added it
	Mine  bool      `json:"mine,omitempty"`
	Added time.Time `json:"added"`
	URL   string    `json:"url"`
	Small string    `json:"small"`
	W     int       `json:"w"`
	H     int       `json:"h"`
	Print string    `json:"print,omitempty"` // the print of the photo, or ""
}

// PrintEdit is the body of PATCH /api/v1/projects/<id>/prints/<print>. Title is the new name of
// the print, or "" for the name from the deck: only the owner can change it. Notes are the notes
// of the print. A field that is nil does not change.
// Delivered marks the print that went to the person who wanted it, and Source is the link to
// the source of the model (for example a file of a commit on GitHub). Only the owner can change
// them too.
type PrintEdit struct {
	Title     *string `json:"title,omitempty"`
	Notes     *string `json:"notes,omitempty"`
	Delivered *bool   `json:"delivered,omitempty"`
	Ready     *bool   `json:"ready,omitempty"` // Ready to pick up, before Delivered
	Test      *bool   `json:"test,omitempty"`  // a test print: to try a setting or a fit, and not to give
	// Discard marks a print that is not good enough, with the note DiscardNote (why).
	Discard     *bool   `json:"discard,omitempty"`
	DiscardNote *string `json:"discard_note,omitempty"`
	Source      *string `json:"source,omitempty"`
}

// NotesEdit is the body of PATCH /api/v1/projects/<id>/notes: the notes of the project.
type NotesEdit struct {
	Notes string `json:"notes"`
}

// FramesReply is the reply to GET /api/v1/projects/<id>/prints/<print>/frames: the URLs of the
// frames of the time lapse, in order. A print with a video has the URL of the video and of its
// last frame in place of the frames.
type FramesReply struct {
	FPS    int      `json:"fps"`
	URLs   []string `json:"urls"`
	Video  string   `json:"video,omitempty"`
	Poster string   `json:"poster,omitempty"`
}

// InviteRequest is the body of POST /api/v1/projects/<id>/invites: the first name of the friend.
type InviteRequest struct {
	Name string `json:"name"`
}

// InviteReply is a new invite: the code once, and the page of the invite.
type InviteReply struct {
	Member  string    `json:"member"`
	URL     string    `json:"url"`
	Expires time.Time `json:"expires"`
}

// InviteView is the reply to POST /api/v1/invites/view: what the invite page shows before Join.
type InviteView struct {
	Project string `json:"project"` // the name of the project
	Owner   string `json:"owner"`
	Name    string `json:"name"` // the first name that the owner typed
}

// PublicRequest is the body of POST /api/v1/projects/<id>/public: On turns the public address on
// or off. The address is /p/<slug> of the project.
type PublicRequest struct {
	On bool `json:"on"`
}

// NameReply is the reply of GET /api/v1/names/<slug>: the project of the address, and the slug of
// its address now. The slug is new after a rename of the project.
type NameReply struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
	// View is the view of the project, for GET /names/<slug>?view=1: the page needs one request.
	View *ProjectView `json:"view,omitempty"`
}

// PublicReply is the public address of a project, or "".
type PublicReply struct {
	URL string `json:"url"`
}

// PrintRequest is the body of POST /api/v1/projects/<id>/prints: a print of the deck for the
// project. The deck then uploads model.jpg, plan.json and lapse/<n>.jpg with the grant, and
// sends done.
type PrintRequest struct {
	Name     string    `json:"name"`
	File     string    `json:"file,omitempty"`
	Result   string    `json:"result"`
	Ended    time.Time `json:"ended"`
	Seconds  int       `json:"seconds"`
	Material string    `json:"material,omitempty"`
	Grams    float64   `json:"grams,omitempty"`
	Roll     string    `json:"roll,omitempty"`
	Printer  string    `json:"printer,omitempty"`
	Note     string    `json:"note,omitempty"`
	// The details of the print page: the start, the layers of the file and the last layer that
	// printed, the height of the part in mm, the grams of the file, the estimate of the slicer and
	// the seconds before the first layer, in seconds, the slicer, and the filament.
	Started    time.Time      `json:"started,omitzero"`
	Layers     int            `json:"layers,omitempty"`
	Layer      int            `json:"layer,omitempty"`
	Height     float64        `json:"height,omitempty"`
	GramsTotal float64        `json:"grams_total,omitempty"`
	Estimate   int            `json:"estimate,omitempty"`
	Prep       int            `json:"prep,omitempty"`
	Slicer     string         `json:"slicer,omitempty"`
	Filament   *ShareFilament `json:"filament,omitempty"`
	// Key is the key of the print on the deck (the file and the end), so that a second upload of
	// the same print replaces the first.
	Key string `json:"key"`
	// Share is the ID of the share link of the print, or "". The service links the share to the
	// print: the photos of the share go to the print, and the link opens the print page after the
	// share ends.
	Share string `json:"share,omitempty"`
	// PrinterID is the printer of the print at the service, or "" for a deck from before the
	// printers.
	PrinterID string `json:"printer_id,omitempty"`
	// Build and Commit are the Printdeck build and the commit of the plan of a file of the slice
	// run of GitHub, from the first lines of the G-code, or "".
	Build  string `json:"build,omitempty"`
	Commit string `json:"commit,omitempty"`
}

// PrintReply is the reply to POST /api/v1/projects/<id>/prints.
type PrintReply struct {
	Print string `json:"print"`
	Grant Grant  `json:"grant"`
}

// PrintDone is the body of POST /api/v1/projects/<id>/prints/<print>/done: the count of the
// frames of the time lapse that the deck uploaded, and their rate.
type PrintDone struct {
	Frames int  `json:"frames"`
	FPS    int  `json:"fps"`
	Model  bool `json:"model"`
	Plan   bool `json:"plan,omitempty"`
	Gcode  bool `json:"gcode,omitempty"` // print.gcode.gz is up
}

// LiveRequest is the body of POST /api/v1/projects/<id>/live: the print of the deck that prints
// now in the project. Key is the file and the start of the print. Model is true when the picture
// of the model for Key is up. Share is the ID of the share link of the print, or "".
type LiveRequest struct {
	Key     string    `json:"key"`
	Name    string    `json:"name"`
	Started time.Time `json:"started"`
	Percent float64   `json:"percent"`
	Layer   int       `json:"layer,omitempty"`
	Layers  int       `json:"layers,omitempty"`
	Left    int       `json:"left,omitempty"`
	Paused  bool      `json:"paused,omitempty"`
	Result  string    `json:"result,omitempty"`
	Model   bool      `json:"model,omitempty"`
	Share   string    `json:"share,omitempty"`
	Printer string    `json:"printer,omitempty"`
	// PrinterID is the printer at the service (PrinterRequest). The project has one live tile for
	// each printer. With no ID, the deck has one live tile in the project.
	PrinterID string `json:"printer_id,omitempty"`
	// WantLayer asks for a grant for the file layer.jpg: the picture of the layer that a resin
	// printer cures now. LayerPic is true when layer.jpg for Key is up.
	WantLayer bool `json:"want_layer,omitempty"`
	LayerPic  bool `json:"layer_pic,omitempty"`
}

// LiveReply is the reply to POST /api/v1/projects/<id>/live. Grant is for the file model.jpg when
// the service has no picture of the model for the key, and LayerGrant for layer.jpg (WantLayer).
type LiveReply struct {
	Grant      *Grant `json:"grant,omitempty"`
	LayerGrant *Grant `json:"layer_grant,omitempty"`
}

// QueueReport is the body of POST /api/v1/printers/<id>/queue: the files of the queue of the deck
// that are in a project of the service. The service keeps the files of each project apart: a
// project sees its own files, their places and their start times, and not the other files.
type QueueReport struct {
	Items []QueueEntry `json:"items"`
}

// QueueEntry is a file of the queue of a deck: its project at the service, its name, its place in
// the queue (1 is next), its print time, and the estimate of its start.
type QueueEntry struct {
	// ID is the ID of the file in the queue of the deck, for a removal from the project page.
	ID      string    `json:"id,omitempty"`
	Project string    `json:"project"`
	Name    string    `json:"name"`
	Place   int       `json:"place"`
	Seconds int       `json:"seconds,omitempty"`
	Start   time.Time `json:"start,omitzero"`
	// File is the path of the file on the deck, for example "builds/<build>-plate-1.gcode" for a
	// plate of a build: the build page shows the plates that are in a queue. "" from an older deck.
	File string `json:"file,omitempty"`
}

// QueueRemove is a removal of a file of the queue of a deck, from the project page: the ID of
// the file in the queue, and the project of the file. GET printers/<id>/removes lists them.
type QueueRemove struct {
	Item    string    `json:"item"`
	Project string    `json:"project"`
	Name    string    `json:"name,omitempty"`
	By      string    `json:"by,omitempty"`
	At      time.Time `json:"at"`
}

// QueueRemoved is the body of POST printers/<id>/removes/done: the removals that the deck did, or
// that name a file that is not in its queue.
type QueueRemoved struct {
	Items []string `json:"items"`
}

// ProjectQueued is a file of a project in the queue of a printer, for the project page.
type ProjectQueued struct {
	// ID is the ID of the file in the queue of the deck, and PrinterID the ID of the printer: the
	// owner removes the file with DELETE projects/<id>/queue/<printer>/<item>. Both are "" from an
	// older deck. Removing is true when a removal waits for the deck.
	ID        string    `json:"id,omitempty"`
	PrinterID string    `json:"printer_id,omitempty"`
	Removing  bool      `json:"removing,omitempty"`
	Printer   string    `json:"printer"`
	Name      string    `json:"name"`
	Place     int       `json:"place"`
	Seconds   int       `json:"seconds,omitempty"`
	Start     time.Time `json:"start,omitzero"`
	Updated   time.Time `json:"updated"`
	// File is the path of the file on the deck (QueueEntry.File), or "".
	File string `json:"file,omitempty"`
}
