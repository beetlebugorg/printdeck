package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"printdeck/internal/cloud"
)

// LiveRecord is x/<project>/live/<deck>.json: the print of a deck that prints now in the project,
// as the deck sent it last. Pic is the ID of the folder of the picture of the model.
type LiveRecord struct {
	cloud.LiveRequest
	Deck string `json:"deck"`
	// Slot is the name of the file of the record: the ID of its printer, or the ID of the deck for
	// a deck from before the printers. A record from before the slots has none: its slot is Deck.
	Slot    string    `json:"slot,omitempty"`
	Pic     string    `json:"pic"`
	Updated time.Time `json:"updated"`
}

// liveStale is the age of a live record of a print that prints, after which the project shows
// it no more: the deck lost the network or stopped. liveEnded is the same age for a print that
// ended: the deck sends the print to the project in about 10 minutes.
const (
	liveStale = 5 * time.Minute
	liveEnded = time.Hour
)

// liveKey is the file of the live tile of the slot (a printer or a deck) in the project.
func liveKey(project, slot string) string { return "x/" + project + "/live/" + slot + ".json" }

func livePicDir(project, slot, pic string) string {
	return "x/" + project + "/live/" + slot + "-" + pic + "/"
}

// slot is the slot of the record.
func (rec *LiveRecord) slot() string {
	if rec.Slot != "" {
		return rec.Slot
	}
	return rec.Deck
}

// printerIDs returns the IDs of the printers of the prints and the live tiles.
func printerIDs(prints []PrintRecord, live []cloud.ProjectLive) map[string]bool {
	ids := map[string]bool{}
	for _, pr := range prints {
		if pr.PrinterID != "" {
			ids[pr.PrinterID] = true
		}
	}
	for _, l := range live {
		if l.PrinterID != "" {
			ids[l.PrinterID] = true
		}
	}
	return ids
}

// loadLive returns the live record of the deck in the project, or nil.
func (s *Server) loadLive(ctx context.Context, project, deck string) (*LiveRecord, error) {
	b, _, err := s.Store.Get(ctx, liveKey(project, deck))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rec LiveRecord
	if json.Unmarshal(b, &rec) != nil {
		return nil, nil
	}
	return &rec, nil
}

// postLive keeps the progress of the print of the deck d in the project p. The reply has a grant
// for the picture of the model when the service has no picture of the print.
func (s *Server) postLive(ctx context.Context, p *Project, d *Deck, in cloud.LiveRequest) (any, error) {
	in.Key, in.Name, in.Printer = clip(in.Key, 200), clip(in.Name, 120), clip(in.Printer, 64)
	switch in.Result {
	case "", "complete", "cancelled", "error":
	default:
		return nil, fail(http.StatusBadRequest, "bad result")
	}
	if in.Key == "" || in.Name == "" {
		return nil, fail(http.StatusBadRequest, "a live print needs a key and a name")
	}
	in.Percent = min(max(in.Percent, 0), 100)
	if in.Share != "" && !validID(in.Share) {
		in.Share = ""
	}
	in.PrinterID = s.printerOf(ctx, d, in.PrinterID)
	slot := d.ID
	if in.PrinterID != "" {
		slot = in.PrinterID
	}
	old, err := s.loadLive(ctx, p.ID, slot)
	if err != nil {
		return nil, err
	}
	rec := LiveRecord{LiveRequest: in, Deck: d.ID, Slot: slot, Updated: s.now().UTC()}
	if old != nil && old.Key == in.Key && old.Pic != "" {
		rec.Pic = old.Pic
		if old.Model {
			rec.Model = true
		} else if rec.Model {
			// The deck says that the picture is up: check it once.
			files, err := s.Store.List(ctx, livePicDir(p.ID, slot, rec.Pic))
			if err != nil {
				return nil, err
			}
			rec.Model = len(files) > 0
		}
	} else {
		if old != nil && old.Pic != "" {
			if err := s.deletePrefix(ctx, livePicDir(p.ID, slot, old.Pic)); err != nil {
				return nil, err
			}
		}
		rec.Pic, rec.Model = newID(8), false
	}
	if err := s.putJSON(ctx, liveKey(p.ID, slot), rec); err != nil {
		return nil, err
	}
	var out cloud.LiveReply
	if in.WantLayer {
		// The picture of the layer changes during the print: a grant at each update.
		g, err := s.Grants.Grant(ctx, livePicDir(p.ID, slot, rec.Pic), MaxPrintFile, GrantTTL)
		if err != nil {
			return nil, err
		}
		out.LayerGrant = &g
	}
	if !rec.Model {
		g, err := s.Grants.Grant(ctx, livePicDir(p.ID, slot, rec.Pic), MaxPrintFile, GrantTTL)
		if err != nil {
			return nil, err
		}
		out.Grant = &g
	}
	return out, nil
}

// deleteLive removes the live record of the slot in the project, and its picture.
func (s *Server) deleteLive(ctx context.Context, project, slot string) error {
	old, err := s.loadLive(ctx, project, slot)
	if err != nil || old == nil {
		return err
	}
	if old.Pic != "" {
		if err := s.deletePrefix(ctx, livePicDir(project, slot, old.Pic)); err != nil {
			return err
		}
	}
	return s.Store.Delete(ctx, liveKey(project, slot))
}

// endLive removes the live record of the printer (or of the deck d, for "") after its print came
// to the project.
func (s *Server) endLive(ctx context.Context, project string, d *Deck, printer string) error {
	slot := d.ID
	if printer != "" {
		slot = printer
	}
	old, err := s.loadLive(ctx, project, slot)
	if err != nil || old == nil || old.Result == "" {
		return err
	}
	return s.deleteLive(ctx, project, slot)
}

// liveView returns the prints of the project p that print now: the live records of the decks,
// and the share links that no live record has. prints are the prints of the project: a live
// record of a print that is in the project already does not show.
func (s *Server) liveView(ctx context.Context, p *Project, prints []PrintRecord) []cloud.ProjectLive {
	var out []cloud.ProjectLive
	shown := map[string]bool{}
	keys, _ := s.Store.List(ctx, "x/"+p.ID+"/live/")
	now := s.now()
	var decks []string // the deck of each tile of out
	for _, k := range keys {
		rel := strings.TrimPrefix(k, "x/"+p.ID+"/live/")
		if strings.Contains(rel, "/") || !strings.HasSuffix(rel, ".json") {
			continue
		}
		rec, err := s.loadLive(ctx, p.ID, strings.TrimSuffix(rel, ".json"))
		if err != nil || rec == nil {
			continue
		}
		age := now.Sub(rec.Updated)
		if rec.Result == "" && age > liveStale || age > liveEnded || isUp(rec, prints) {
			continue
		}
		l := cloud.ProjectLive{Name: rec.Name, Started: rec.Started, Percent: rec.Percent, Layer: rec.Layer,
			Layers: rec.Layers, Left: rec.Left, Paused: rec.Paused, Result: rec.Result, Updated: rec.Updated,
			Printer: rec.Printer, PrinterID: rec.PrinterID}
		if rec.Share != "" {
			l.URL = s.shareURL(rec.Share)
			shown[rec.Share] = true
		}
		if rec.Model {
			l.Model, _ = s.Grants.FileURL(ctx, livePicDir(p.ID, rec.slot(), rec.Pic)+"model.jpg", urlTTL)
		}
		if rec.LayerPic && rec.Result == "" {
			l.LayerPic, _ = s.Grants.FileURL(ctx, livePicDir(p.ID, rec.slot(), rec.Pic)+"layer.jpg", urlTTL)
		}
		out = append(out, l)
		decks = append(decks, rec.Deck)
	}
	for _, l := range s.liveShares(ctx, p) {
		if shown[l.id] {
			continue
		}
		// A share of a print that has a live tile from the same deck: the deck lost the share at a
		// restart, so its live tile does not name it. The live tile links to the share.
		dup := false
		for i := range out {
			if i < len(decks) && decks[i] == l.deck && out[i].Name == l.Name {
				dup = true
				if out[i].URL == "" {
					out[i].URL = l.URL
				}
			}
		}
		if !dup {
			out = append(out, l.ProjectLive)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.After(out[j].Started) })
	return out
}

// isUp reports that the print of the live record is in the project: a print of the same printer
// (or of the same deck, with no printer) that started at the same time, within 10 minutes.
func isUp(rec *LiveRecord, prints []PrintRecord) bool {
	for _, pr := range prints {
		d := pr.Started.Sub(rec.Started)
		same := pr.Deck == rec.Deck
		if rec.PrinterID != "" {
			same = pr.PrinterID == rec.PrinterID
		}
		if same && d < 10*time.Minute && d > -10*time.Minute {
			return true
		}
	}
	return false
}
