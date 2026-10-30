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
}
