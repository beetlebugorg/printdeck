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
}

// ShareReply is the reply to POST /api/v1/shares.
type ShareReply struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Grant Grant  `json:"grant"`
}

// Grant lets the deck upload files under Prefix until Expires, with an S3 POST form: the form
// has Fields, the field "key" with a key under Prefix, the fields "Content-Type" and
// "Cache-Control", and the file last. A file has at most MaxBytes.
type Grant struct {
	URL      string            `json:"url"`
	Fields   map[string]string `json:"fields"`
	Prefix   string            `json:"prefix"`
	MaxBytes int64             `json:"max_bytes"`
	Expires  time.Time         `json:"expires"`
}

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
