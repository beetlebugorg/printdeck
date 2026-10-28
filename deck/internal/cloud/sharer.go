package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sync"
	"time"
)

// Sharer runs the share link of the print now: a tap on Share starts it. It uploads status.json
// and the newest camera frame of each view every Every, and each new time lapse frame. At the
// end of the print it uploads the last status and tells the service the end, which deletes the
// share 24 hours later. The frames go up as the camera sent them: the sharer does no encode.
type Sharer struct {
	Link *Link
	// Status returns the state of the print now. The sharer sets Frame, Views, Lapse and Updated.
	// A State of "done", "cancelled" or "error" ends the share.
	Status func() ShareStatus
	// Frame returns the newest frame of a camera view and its number, or nil.
	Frame func(view string) ([]byte, uint64)
	// Views are the camera views of the share.
	Views []string
	// Model returns the picture of the model as a JPEG and its version, or nil. The sharer
	// uploads a new version at most each ModelEvery.
	Model func() ([]byte, uint64)
	// LapseFPS is the rate of the time lapse on the page.
	LapseFPS int
	// Every is the time between two uploads of the status, or 0 for 5 s.
	Every    time.Duration
	OnChange func()
	// HTTP uploads the files, or nil for a client with a timeout of 60 s.
	HTTP *http.Client

	mu  sync.Mutex
	cur *share
}

// share is the share of the sharer.
type share struct {
	id, url string
	owner   string // the owner key
	ready   bool
	grant   Grant
	lapse   [][]byte // the time lapse frames that wait for the upload
	lapseN  int      // the time lapse frames that are up
	frame   int
	seqs    map[string]uint64
	model   int       // the count of uploads of model.jpg
	modelV  uint64    // the version of the last model.jpg
	modelAt time.Time // the time of the last model.jpg
	ended   bool
	expires time.Time
	err     string
	busy    bool
	stop    context.CancelFunc
	wake    chan struct{}
}

// ShareState is the state of the share link, for the screens.
type ShareState struct {
	// Active reports a share: from the tap on Share until Stop or the delete.
	Active bool
	// Busy reports that the deck waits for the service to make the link.
	Busy    bool
	URL     string
	Ended   bool
	Expires time.Time
	Err     string
	// OwnerURL is the page of the share with the owner key, for photos and Ready, and Ready the
	// flag of the owner.
	OwnerURL string
	Ready    bool
}

// State returns the state of the share.
func (s *Sharer) State() ShareState {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.cur
	if c == nil {
		return ShareState{}
	}
	if c.ended && !c.expires.IsZero() && time.Now().After(c.expires) {
		s.cur = nil
		return ShareState{}
	}
	st := ShareState{Active: true, Busy: c.busy, URL: c.url, Ended: c.ended, Expires: c.expires, Err: c.err, Ready: c.ready}
	if c.url != "" && c.owner != "" {
		st.OwnerURL = c.url + "#owner=" + c.owner
	}
	return st
}

func (s *Sharer) changed() {
	if s.OnChange != nil {
		s.OnChange()
	}
}

// Start makes a share link for the job, in the background. lapse are the time lapse frames that
// exist already: the frames of the print so far, or of the video of a print that ended.
func (s *Sharer) Start(job, printer string, lapse [][]byte) {
	id := s.Link.Identity()
	if id == nil {
		return
	}
	s.mu.Lock()
	if s.cur != nil && !s.cur.ended {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &share{busy: true, lapse: lapse, seqs: map[string]uint64{}, stop: cancel, wake: make(chan struct{}, 1)}
	s.cur = c
	s.mu.Unlock()
	s.changed()
	go func() {
		var out ShareReply
		err := s.Link.Client.Do(ctx, id, http.MethodPost, "/api/v1/shares", ShareRequest{Job: job, Printer: printer}, &out)
		s.mu.Lock()
		c.busy = false
		if err != nil {
			c.err = err.Error()
			if s.cur == c {
				s.cur = nil
			}
			s.mu.Unlock()
			s.changed()
			return
		}
		c.id, c.url, c.owner, c.grant = out.ID, out.URL, out.OwnerKey, out.Grant
		s.mu.Unlock()
		s.changed()
		s.run(ctx, id, c)
	}()
}

// SetReady sets the flag Ready to pick up of the share, with a note, in the background.
func (s *Sharer) SetReady(ready bool, note string) {
	id := s.Link.Identity()
	s.mu.Lock()
	c := s.cur
	s.mu.Unlock()
	if id == nil || c == nil || c.id == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var out ShareReady
		err := s.Link.Client.Do(ctx, id, http.MethodPost, "/api/v1/shares/"+c.id+"/ready", ReadyRequest{Ready: ready, Note: note}, &out)
		s.mu.Lock()
		if err != nil {
			c.err = err.Error()
		} else {
			c.ready, c.err = out.Ready, ""
			if !out.Expires.IsZero() {
				c.expires = out.Expires
			}
		}
		s.mu.Unlock()
		s.changed()
	}()
}

// AddLapseFrame queues a new time lapse frame of the print for the upload.
func (s *Sharer) AddLapseFrame(jpeg []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.cur; c != nil && !c.ended {
		c.lapse = append(c.lapse, jpeg)
		select {
		case c.wake <- struct{}{}:
		default:
		}
	}
}

// Stop deletes the share at the service at once.
func (s *Sharer) Stop() {
	s.mu.Lock()
	c := s.cur
	s.cur = nil
	s.mu.Unlock()
	if c == nil {
		return
	}
	c.stop()
	s.changed()
	if id := s.Link.Identity(); id != nil && c.id != "" {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			s.Link.Client.Do(ctx, id, http.MethodDelete, "/api/v1/shares/"+c.id, nil, nil)
		}()
	}
}

// run uploads the files of the share until the end of the print, or Stop.
func (s *Sharer) run(ctx context.Context, id *Identity, c *share) {
	every := s.Every
	if every <= 0 {
		every = 5 * time.Second
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		done, err := s.step(ctx, id, c)
		s.mu.Lock()
		if err != nil {
			c.err = err.Error()
		} else {
			c.err = ""
		}
		s.mu.Unlock()
		s.changed()
		if done || ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-c.wake:
		}
	}
}

// step uploads the new frames, the time lapse frames that wait and the status. It reports the end
// of the share.
func (s *Sharer) step(ctx context.Context, id *Identity, c *share) (bool, error) {
	if time.Until(c.grant.Expires) < 10*time.Minute {
		if err := s.renew(ctx, id, c); err != nil {
			return false, err
		}
	}
	st := s.Status()
	for _, v := range s.Views {
		if s.Frame == nil {
			break
		}
		b, seq := s.Frame(v)
		if b == nil || seq == c.seqs[v] {
			continue
		}
		if err := s.upload(ctx, id, c, "cam-"+v+".jpg", "image/jpeg", "public, max-age=2", b); err != nil {
			return false, err
		}
		c.seqs[v] = seq
		c.frame++
	}
	for {
		s.mu.Lock()
		if len(c.lapse) == 0 {
			s.mu.Unlock()
			break
		}
		b := c.lapse[0]
		s.mu.Unlock()
		name := fmt.Sprintf("lapse/%d.jpg", c.lapseN)
		if err := s.upload(ctx, id, c, name, "image/jpeg", "public, max-age=31536000, immutable", b); err != nil {
			return false, err
		}
		s.mu.Lock()
		c.lapse = c.lapse[1:]
		c.lapseN++
		s.mu.Unlock()
	}
	end := st.State == "done" || st.State == "cancelled" || st.State == "error"
	if s.Model != nil && (end || time.Since(c.modelAt) >= ModelEvery) {
		if b, v := s.Model(); b != nil && (v != c.modelV || c.model == 0) {
			if err := s.upload(ctx, id, c, "model.jpg", "image/jpeg", "public, max-age=5", b); err != nil {
				return false, err
			}
			c.model, c.modelV, c.modelAt = c.model+1, v, time.Now()
		}
	}
	st.Views, st.Frame, st.Lapse, st.LapseFPS, st.Updated = s.Views, c.frame, c.lapseN, s.LapseFPS, time.Now().UTC()
	st.Model = c.model
	if end {
		var out EndReply
		if err := s.Link.Client.Do(ctx, id, http.MethodPost, "/api/v1/shares/"+c.id+"/end", EndRequest{Ended: st.Ended}, &out); err != nil {
			return false, err
		}
		st.Expires = out.Expires
		s.mu.Lock()
		c.ended, c.expires = true, out.Expires
		s.mu.Unlock()
	}
	b, _ := json.Marshal(st)
	if err := s.upload(ctx, id, c, "status.json", "application/json", "public, max-age=2", b); err != nil {
		return false, err
	}
	return end, nil
}

// ModelEvery is the shortest time between two uploads of the picture of the model.
const ModelEvery = 15 * time.Second

// renew gets a new grant.
func (s *Sharer) renew(ctx context.Context, id *Identity, c *share) error {
	var g Grant
	if err := s.Link.Client.Do(ctx, id, http.MethodPost, "/api/v1/shares/"+c.id+"/grant", nil, &g); err != nil {
		return err
	}
	c.grant = g
	return nil
}

// upload sends one file of the share. A refused upload gets a new grant and one more try: the
// credentials of a grant can end before its time.
func (s *Sharer) upload(ctx context.Context, id *Identity, c *share, name, ctype, cache string, data []byte) error {
	err := PostFile(ctx, s.HTTP, c.grant, name, ctype, cache, data)
	var ue *UploadError
	if errors.As(err, &ue) && ue.Status == http.StatusForbidden {
		if err := s.renew(ctx, id, c); err != nil {
			return err
		}
		err = PostFile(ctx, s.HTTP, c.grant, name, ctype, cache, data)
	}
	return err
}

// UploadError is a refused upload.
type UploadError struct {
	Status int
	Body   string
}

func (e *UploadError) Error() string {
	return fmt.Sprintf("the upload failed with status %d", e.Status)
}

// PostFile uploads data as the file name under the prefix of the grant, with an S3 POST form.
func PostFile(ctx context.Context, hc *http.Client, g Grant, name, ctype, cache string, data []byte) error {
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range g.Fields {
		w.WriteField(k, v)
	}
	w.WriteField("key", g.Prefix+name)
	w.WriteField("Content-Type", ctype)
	w.WriteField("Cache-Control", cache)
	fw, err := w.CreateFormFile("file", name)
	if err != nil {
		return err
	}
	fw.Write(data)
	if err := w.Close(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.URL, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return &UploadError{Status: resp.StatusCode, Body: string(b)}
	}
	return nil
}
