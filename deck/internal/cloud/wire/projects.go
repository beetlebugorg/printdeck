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
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Note    string    `json:"note,omitempty"`
	Prints  int       `json:"prints"`
	Photos  int       `json:"photos"`
	Members int       `json:"members"`
	Changed time.Time `json:"changed"`
	// URL is the page of the project for the owner and the members.
	URL string `json:"url"`
	// Cover is the URL of a picture of the project (the newest photo or model), or "".
	Cover string `json:"cover,omitempty"`
}

// ProjectView is a project as its page shows it. The URLs of the files work for one hour. A
// public page has the same fields, with the paths of the files relative to the page, and no
// members.
type ProjectView struct {
	ID      string    `json:"id,omitempty"`
	Name    string    `json:"name"`
	Note    string    `json:"note,omitempty"`
	Owner   string    `json:"owner"` // the first name of the owner
	Created time.Time `json:"created"`
	Changed time.Time `json:"changed"`
	// You is the person who asks: "owner", or the ID of the member. IsOwner lets the page show
	// the keys of the owner.
	You     string          `json:"you,omitempty"`
	IsOwner bool            `json:"is_owner,omitempty"`
	Members []ProjectMember `json:"members,omitempty"`
	Prints  []ProjectPrint  `json:"prints"`
	Photos  []ProjectPhoto  `json:"photos"`
	// Public is the public address of the project, or "" when it has none.
	Public string `json:"public,omitempty"`
	// Live are the share links of the prints of the project that print now.
	Live []ProjectLive `json:"live,omitempty"`
}

// ProjectLive is a print of a project that prints now: its name, the share link, and the start
// of the share.
type ProjectLive struct {
	Name    string    `json:"name"`
	URL     string    `json:"url"`
	Started time.Time `json:"started"`
}

// ProjectMember is a member of a project, or an invite that waits.
type ProjectMember struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Joined  bool      `json:"joined"`
	Invited time.Time `json:"invited"`
}

// ProjectPrint is a print of a project, from the history record of the deck.
type ProjectPrint struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	File     string    `json:"file,omitempty"`
	Result   string    `json:"result"` // "complete", "cancelled" or "error"
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
	// Model is the URL of the picture of the model, and Plan the URL of plan.json (a SharePlan),
	// or "". Frames is the count of the frames of the time lapse, at FPS frames a second.
	Model  string `json:"model,omitempty"`
	Plan   string `json:"plan,omitempty"`
	Frames int    `json:"frames,omitempty"`
	FPS    int    `json:"fps,omitempty"`
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

// FramesReply is the reply to GET /api/v1/projects/<id>/prints/<print>/frames: the URLs of the
// frames of the time lapse, in order.
type FramesReply struct {
	FPS  int      `json:"fps"`
	URLs []string `json:"urls"`
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
// or off, and Renew makes a new address in place of the old one.
type PublicRequest struct {
	On    bool `json:"on"`
	Renew bool `json:"renew,omitempty"`
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
}
