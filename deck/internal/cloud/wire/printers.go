package wire

import "time"

// The printers at the service: each deck registers the printers that it drives or watches, and the
// prints and the live tiles of the projects have the ID of their printer. One project has the
// prints of many printers.

// PrinterRequest is the body of POST /api/v1/printers from a deck. Key names the printer on the
// deck, for example "main": a second request with the same key changes that printer and keeps its
// ID.
type PrinterRequest struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Process string `json:"process"` // "fdm" or "resin"
	Model   string `json:"model,omitempty"`
}

// Printer is a printer at the service. Deck is the ID of the deck that reports it; a public page has
// no deck.
type Printer struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Process string    `json:"process"`
	Model   string    `json:"model,omitempty"`
	Deck    string    `json:"deck,omitempty"`
	Key     string    `json:"key,omitempty"`
	Added   time.Time `json:"added,omitzero"`
	// Live is what the printer does now, in GET /api/v1/printers for the owner, or nil.
	Live *PrinterLive `json:"live,omitempty"`
}

// PrinterLive is what a printer does now, with a project or not: the body of POST
// /api/v1/printers/<id>/live, and the field Live of GET /api/v1/printers. Only the owner reads it:
// the projects see their own prints only.
type PrinterLive struct {
	Printing bool      `json:"printing"`
	Paused   bool      `json:"paused,omitempty"`
	Name     string    `json:"name,omitempty"`
	Started  time.Time `json:"started,omitzero"`
	Percent  float64   `json:"percent,omitempty"` // 0 to 100
	Layer    int       `json:"layer,omitempty"`
	Layers   int       `json:"layers,omitempty"`
	Left     int       `json:"left,omitempty"` // seconds, or 0 for no estimate
	// Result is the result of the last print with no print now: "complete", "cancelled" or
	// "error", or "". Ended is its end.
	Result string    `json:"result,omitempty"`
	Ended  time.Time `json:"ended,omitzero"`
	// Queue is the count of the files in the queue of the deck, and QueueSeconds their time.
	Queue        int       `json:"queue,omitempty"`
	QueueSeconds int       `json:"queue_seconds,omitempty"`
	Updated      time.Time `json:"updated"`
}
