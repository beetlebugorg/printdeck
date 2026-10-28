package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"printdeck/internal/cloud"
)

// The share links. POST /api/v1/shares makes a share: the page s/<id> (a copy of
// assets/share.html) and the record shares/<id>.json, which is private. The deck uploads the
// files of the share under s/<id>/ with a grant. After the end of the print, the sweep (Sweep)
// deletes the share at its expire time.

// ShareTTL is how long a share stays after the end of its print.
const ShareTTL = 24 * time.Hour

// MaxShareFile is the largest file of a share: a camera frame or a time lapse frame.
const MaxShareFile = 4 << 20

// GrantTTL is how long a grant works.
const GrantTTL = time.Hour

// SharePage is the key of the share page in the bucket. pwf deploy uploads it.
const SharePage = "assets/share.html"

// Granter makes upload grants.
type Granter interface {
	Grant(ctx context.Context, prefix string, maxBytes int64, ttl time.Duration) (cloud.Grant, error)
}

// Share is the record of a share, shares/<id>.json.
type Share struct {
	ID      string    `json:"id"`
	Deck    string    `json:"deck"`
	Job     string    `json:"job"`
	Printer string    `json:"printer"`
	Created time.Time `json:"created"`
	Ended   time.Time `json:"ended,omitzero"`
	Expires time.Time `json:"expires,omitzero"`
}

func shareKey(id string) string { return "shares/" + id + ".json" }

// shareURL is the address of the page of a share.
func (s *Server) shareURL(id string) string { return s.base() + "/s/" + id }

// base is the base URL of the links.
func (s *Server) base() string {
	if s.Base != "" {
		return s.Base
	}
	return "https://" + s.Domain
}

// routeShares serves /api/v1/shares and /api/v1/shares/<id>/...
func (s *Server) routeShares(ctx context.Context, r *http.Request, body []byte, rest string) (any, error) {
	d, err := s.deck(ctx, r, body)
	if err != nil {
		return nil, err
	}
	if rest == "" {
		if r.Method != http.MethodPost {
			return nil, fail(http.StatusMethodNotAllowed, "no such request")
		}
		return s.newShare(ctx, d, body)
	}
	id, action, _ := strings.Cut(rest, "/")
	sh, err := s.ownShare(ctx, d, id)
	if err != nil {
		return nil, err
	}
	switch {
	case action == "grant" && r.Method == http.MethodPost:
		return s.Grants.Grant(ctx, "s/"+sh.ID+"/", MaxShareFile, GrantTTL)
	case action == "end" && r.Method == http.MethodPost:
		var in cloud.EndRequest
		json.Unmarshal(body, &in)
		now := s.now()
		if in.Ended.IsZero() || in.Ended.After(now) || now.Sub(in.Ended) > ShareTTL {
			in.Ended = now
		}
		sh.Ended, sh.Expires = in.Ended.UTC(), in.Ended.Add(ShareTTL).UTC()
		if err := s.putJSON(ctx, shareKey(sh.ID), sh); err != nil {
			return nil, err
		}
		return cloud.EndReply{Expires: sh.Expires}, nil
	case action == "" && r.Method == http.MethodDelete:
		return nil, s.deleteShare(ctx, sh.ID)
	}
	return nil, fail(http.StatusMethodNotAllowed, "no such request")
}

// newShare makes a share of the deck d.
func (s *Server) newShare(ctx context.Context, d *Deck, body []byte) (any, error) {
	var in cloud.ShareRequest
	if json.Unmarshal(body, &in) != nil {
		return nil, fail(http.StatusBadRequest, "bad JSON")
	}
	in.Job, in.Printer = clip(in.Job, 120), clip(in.Printer, 64)
	page, _, err := s.Store.Get(ctx, SharePage)
	if err != nil {
		return nil, errors.New("no share page in the bucket: run pwf deploy")
	}
	sh := Share{ID: newID(13), Deck: d.ID, Job: in.Job, Printer: in.Printer, Created: s.now().UTC()}
	if err := s.putJSON(ctx, shareKey(sh.ID), sh); err != nil {
		return nil, err
	}
	if _, err := s.Store.Put(ctx, "s/"+sh.ID, page, PutOptions{ContentType: "text/html; charset=utf-8",
		CacheControl: "public, max-age=60"}); err != nil {
		return nil, err
	}
	g, err := s.Grants.Grant(ctx, "s/"+sh.ID+"/", MaxShareFile, GrantTTL)
	if err != nil {
		return nil, err
	}
	return cloud.ShareReply{ID: sh.ID, URL: s.shareURL(sh.ID), Grant: g}, nil
}

// ownShare returns the share id of the deck d. A share of another deck, or none, is 410.
func (s *Server) ownShare(ctx context.Context, d *Deck, id string) (*Share, error) {
	if !validID(id) {
		return nil, fail(http.StatusGone, "no such share")
	}
	b, _, err := s.Store.Get(ctx, shareKey(id))
	if errors.Is(err, ErrNotFound) {
		return nil, fail(http.StatusGone, "no such share")
	}
	if err != nil {
		return nil, err
	}
	var sh Share
	if err := json.Unmarshal(b, &sh); err != nil {
		return nil, err
	}
	if sh.Deck != d.ID {
		return nil, fail(http.StatusGone, "no such share")
	}
	return &sh, nil
}

// deleteShare deletes the page, the files and the record of a share.
func (s *Server) deleteShare(ctx context.Context, id string) error {
	keys, err := s.Store.List(ctx, "s/"+id+"/")
	if err != nil {
		return err
	}
	for _, k := range append(keys, "s/"+id, shareKey(id)) {
		if err := s.Store.Delete(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

// Sweep deletes the shares after their expire time, and the shares with no end after 3 days: a
// deck that stopped during a print. A schedule runs it every 15 minutes.
func (s *Server) Sweep(ctx context.Context) error {
	keys, err := s.Store.List(ctx, "shares/")
	if err != nil {
		return err
	}
	now := s.now()
	for _, k := range keys {
		b, _, err := s.Store.Get(ctx, k)
		if err != nil {
			continue
		}
		var sh Share
		if json.Unmarshal(b, &sh) != nil || sh.ID == "" {
			continue
		}
		if !sh.Expires.IsZero() && now.After(sh.Expires) || sh.Expires.IsZero() && now.Sub(sh.Created) > 3*24*time.Hour {
			if err := s.deleteShare(ctx, sh.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) putJSON(ctx context.Context, key string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.Store.Put(ctx, key, data, PutOptions{ContentType: "application/json"})
	return err
}

// clip trims s and cuts it to n characters.
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
