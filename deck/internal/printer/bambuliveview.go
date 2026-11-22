package printer

import (
	"bytes"
	"encoding/json"
	"log"
)

// LiveViews are the camera fields of a Bambu Lab report. The deck only reads them. Their type is
// not known yet, so they stay as the printer sends them.
type LiveViews struct {
	Supported json.RawMessage `json:"support_liveview_cams"`
	Current   json.RawMessage `json:"current_liveview"`
}

// LiveViews returns the camera fields of the last report. It returns false before the first
// report.
func (b *Bambu) LiveViews() (LiveViews, bool) {
	r, ok := b.Report()
	if !ok {
		return LiveViews{}, false
	}
	return LiveViews{Supported: b.hideCode(r.IPCam.SupportLiveviewCams),
		Current: b.hideCode(r.IPCam.CurrentLiveview)}, true
}

// hideCode returns the raw value, or a marker when the value holds the access code. The API and
// the log show these values, and they must never show the code.
func (b *Bambu) hideCode(raw json.RawMessage) json.RawMessage {
	if code := b.Client.Code; code != "" && bytes.Contains(raw, []byte(code)) {
		return json.RawMessage(`"hidden"`)
	}
	return raw
}

// logLiveViews writes the camera fields to the log when they change.
func (b *Bambu) logLiveViews() {
	v, ok := b.LiveViews()
	if !ok {
		return
	}
	text, err := json.Marshal(v)
	if err != nil {
		return
	}
	b.mu.Lock()
	changed := string(text) != b.liveViews
	b.liveViews = string(text)
	b.mu.Unlock()
	if changed {
		log.Printf("bambu: camera views: %s", text)
	}
}
