package cloud

import (
	"time"

	"printdeck/internal/cloud/wire"
)

// The types of the share links. A share is a public page of one print at /s/<id>. The deck
// uploads status.json, the camera frames and the time lapse frames under s/<id>/ with a grant.

// ShareRequest is the body of POST /api/v1/shares.
type ShareRequest struct {
	Job     string `json:"job"`
	Printer string `json:"printer"`
	// Project is the ID of the project of the print at the service, or "".
	Project string `json:"project,omitempty"`
}

// ShareProjectRequest is the body of POST /api/v1/shares/<id>/project: the ID of the project of
// the print at the service, or "" for none.
type ShareProjectRequest struct {
	Project string `json:"project"`
}

// ShareReply is the reply to POST /api/v1/shares. OwnerKey lets the owner add photos and set
// Ready from a phone, with no deck: the deck shows it in the owner link, after "#".
type ShareReply struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	OwnerKey string `json:"owner_key"`
	Grant    Grant  `json:"grant"`
}

// ShareReady is ready.json of a share (wire.ShareReady).
type ShareReady = wire.ShareReady

// SharePhoto is a photo of a share (wire.SharePhoto).
type SharePhoto = wire.SharePhoto

// EndRequest is the body of POST /api/v1/shares/<id>/end.
type EndRequest struct {
	Ended time.Time `json:"ended"`
}

// EndReply is the reply to POST /api/v1/shares/<id>/end: the time of the delete of the share.
type EndReply struct {
	Expires time.Time `json:"expires"`
}

// ShareStatus is status.json of a share (wire.ShareStatus).
type ShareStatus = wire.ShareStatus

// HeaderShareKey is the header of the owner key of a share, for the requests of the owner page.
const HeaderShareKey = "X-Share-Key"

// The types of the share that the pages use too (package wire).
type (
	Grant         = wire.Grant
	ReadyRequest  = wire.ReadyRequest
	PhotoReply    = wire.PhotoReply
	ShareRoll     = wire.ShareRoll
	ShareFilament = wire.ShareFilament
	ShareProject  = wire.ShareProject
)
