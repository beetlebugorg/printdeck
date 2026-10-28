package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
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
	// OwnerKey is the SHA-256 of the owner key, in hex. ReadyAt is the time of Ready, or zero.
	OwnerKey string    `json:"owner_key"`
	ReadyAt  time.Time `json:"ready_at,omitzero"`
}

// MaxSharePhotos is the most photos of a share, and MaxPhotoFile the largest upload of a photo.
const (
	MaxSharePhotos = 20
	MaxPhotoFile   = 25 << 20
)

// expires is the time of the delete: 24 hours after the end of the print or after Ready, the
// later of the two, or zero before the end.
func (sh *Share) expires() time.Time {
	if sh.Ended.IsZero() {
		return time.Time{}
	}
	t := sh.Ended.Add(ShareTTL)
	if r := sh.ReadyAt.Add(ShareTTL); !sh.ReadyAt.IsZero() && r.After(t) {
		t = r
	}
	return t.UTC()
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

// routeShares serves /api/v1/shares and /api/v1/shares/<id>/... The deck of a share signs its
// requests. The owner page uses the owner key of the share (HeaderShareKey) for Ready and photos.
func (s *Server) routeShares(ctx context.Context, r *http.Request, body []byte, rest string) (any, error) {
	if rest == "" {
		d, err := s.deck(ctx, r, body)
		if err != nil {
			return nil, err
		}
		if r.Method != http.MethodPost {
			return nil, fail(http.StatusMethodNotAllowed, "no such request")
		}
		return s.newShare(ctx, d, body)
	}
	id, action, _ := strings.Cut(rest, "/")
	sh, err := s.loadShare(ctx, id)
	if err != nil {
		return nil, err
	}
	if key := r.Header.Get(cloud.HeaderShareKey); key != "" {
		sum := sha256.Sum256([]byte(key))
		if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(sh.OwnerKey)) != 1 {
			return nil, fail(http.StatusUnauthorized, "the owner key of the share is wrong")
		}
		if action != "ready" && !strings.HasPrefix(action, "photo") {
			return nil, fail(http.StatusMethodNotAllowed, "no such request")
		}
	} else {
		d, err := s.deck(ctx, r, body)
		if err != nil {
			return nil, err
		}
		if sh.Deck != d.ID {
			return nil, fail(http.StatusGone, "no such share")
		}
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
		sh.Ended = in.Ended.UTC()
		sh.Expires = sh.expires()
		if err := s.putJSON(ctx, shareKey(sh.ID), sh); err != nil {
			return nil, err
		}
		if err := s.updateReady(ctx, sh, func(*cloud.ShareReady) {}); err != nil {
			return nil, err
		}
		return cloud.EndReply{Expires: sh.Expires}, nil
	case action == "ready" && r.Method == http.MethodPost:
		var in cloud.ReadyRequest
		if json.Unmarshal(body, &in) != nil {
			return nil, fail(http.StatusBadRequest, "bad JSON")
		}
		sh.ReadyAt = time.Time{}
		if in.Ready {
			sh.ReadyAt = s.now().UTC()
		}
		sh.Expires = sh.expires()
		if err := s.putJSON(ctx, shareKey(sh.ID), sh); err != nil {
			return nil, err
		}
		var out cloud.ShareReady
		err := s.updateReady(ctx, sh, func(rd *cloud.ShareReady) {
			rd.Ready, rd.At, rd.Note = in.Ready, sh.ReadyAt, clip(in.Note, 280)
			out = *rd
		})
		return out, err
	case action == "photo" && r.Method == http.MethodPost:
		rd, err := s.readReady(ctx, sh.ID)
		if err != nil {
			return nil, err
		}
		if len(rd.Photos) >= MaxSharePhotos {
			return nil, fail(http.StatusBadRequest, "the share has 20 photos")
		}
		pid := newID(8)
		g, err := s.Grants.Grant(ctx, "up/"+sh.ID+"/"+pid+"/", MaxPhotoFile, 15*time.Minute)
		if err != nil {
			return nil, err
		}
		return cloud.PhotoReply{Photo: pid, Grant: g}, nil
	case strings.HasPrefix(action, "photo/") && strings.HasSuffix(action, "/done") && r.Method == http.MethodPost:
		pid := strings.TrimSuffix(strings.TrimPrefix(action, "photo/"), "/done")
		return s.addPhoto(ctx, sh, pid)
	case action == "" && r.Method == http.MethodDelete:
		return nil, s.deleteShare(ctx, sh.ID)
	}
	return nil, fail(http.StatusMethodNotAllowed, "no such request")
}

// readyKey is the key of ready.json of a share.
func readyKey(id string) string { return "s/" + id + "/ready.json" }

// readReady returns ready.json of a share, or an empty one.
func (s *Server) readReady(ctx context.Context, id string) (cloud.ShareReady, error) {
	var rd cloud.ShareReady
	b, _, err := s.Store.Get(ctx, readyKey(id))
	if errors.Is(err, ErrNotFound) {
		return rd, nil
	}
	if err != nil {
		return rd, err
	}
	json.Unmarshal(b, &rd)
	return rd, nil
}

// updateReady changes ready.json of a share with change, and sets its expire time.
func (s *Server) updateReady(ctx context.Context, sh *Share, change func(*cloud.ShareReady)) error {
	rd, err := s.readReady(ctx, sh.ID)
	if err != nil {
		return err
	}
	change(&rd)
	rd.Expires = sh.Expires
	data, _ := json.Marshal(rd)
	_, err = s.Store.Put(ctx, readyKey(sh.ID), data, PutOptions{ContentType: "application/json", CacheControl: "public, max-age=5"})
	return err
}

// addPhoto cleans the upload of the photo pid, writes the photo and its small copy under
// s/<id>/photos/, and adds it to ready.json.
func (s *Server) addPhoto(ctx context.Context, sh *Share, pid string) (any, error) {
	if !validID(pid) {
		return nil, fail(http.StatusBadRequest, "bad photo")
	}
	up := "up/" + sh.ID + "/" + pid + "/photo"
	data, _, err := s.Store.Get(ctx, up)
	if errors.Is(err, ErrNotFound) {
		return nil, fail(http.StatusBadRequest, "the photo did not arrive")
	}
	if err != nil {
		return nil, err
	}
	defer s.Store.Delete(ctx, up)
	full, small, w, h, err := CleanPhoto(data)
	if err != nil {
		return nil, fail(http.StatusBadRequest, "send the photo as a JPEG or PNG file")
	}
	file, smallFile := "photos/"+pid+".jpg", "photos/"+pid+"-s.jpg"
	opt := PutOptions{ContentType: "image/jpeg", CacheControl: "public, max-age=31536000, immutable"}
	if _, err := s.Store.Put(ctx, "s/"+sh.ID+"/"+file, full, opt); err != nil {
		return nil, err
	}
	if _, err := s.Store.Put(ctx, "s/"+sh.ID+"/"+smallFile, small, opt); err != nil {
		return nil, err
	}
	var out cloud.ShareReady
	err = s.updateReady(ctx, sh, func(rd *cloud.ShareReady) {
		rd.Photos = append(rd.Photos, cloud.SharePhoto{File: file, Small: smallFile, W: w, H: h})
		out = *rd
	})
	return out, err
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
	ownerKey := newID(16)
	sum := sha256.Sum256([]byte(ownerKey))
	sh := Share{ID: newID(13), Deck: d.ID, Job: in.Job, Printer: in.Printer, Created: s.now().UTC(),
		OwnerKey: hex.EncodeToString(sum[:])}
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
	return cloud.ShareReply{ID: sh.ID, URL: s.shareURL(sh.ID), OwnerKey: ownerKey, Grant: g}, nil
}

// loadShare returns the record of the share id. A share that does not exist is 410.
func (s *Server) loadShare(ctx context.Context, id string) (*Share, error) {
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
	return &sh, nil
}

// deleteShare deletes the page, the files and the record of a share.
func (s *Server) deleteShare(ctx context.Context, id string) error {
	keys, err := s.Store.List(ctx, "s/"+id+"/")
	if err != nil {
		return err
	}
	ups, err := s.Store.List(ctx, "up/"+id+"/")
	if err != nil {
		return err
	}
	keys = append(keys, ups...)
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
