package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"printdeck/internal/cloud"
)

// The projects (Parts with Friends). A project is x/<id>/project.json, which is private, with its
// prints and photos under x/<id>/. The owner makes projects in a browser or on a deck, and
// invites friends. The members see the files with URLs that work for one hour (Granter.URL). A
// project can also have a public address, p/<public id> (public.go).
//
//	x/<id>/project.json                 the name, the members, the invites, the public ID
//	x/<id>/prints/<print>.json          a print, from the history record of the deck
//	x/<id>/prints/<print>/model.jpg     the picture of the model
//	x/<id>/prints/<print>/plan.json     the seconds of each layer from the slicer
//	x/<id>/prints/<print>/lapse/<n>.jpg the frames of the time lapse
//	x/<id>/photos/<photo>.json, .jpg and -s.jpg   a photo, cleaned (CleanPhoto)
//	invites/<sha256 of the code>.json   an invite, until it is used or it expires
//	up/x<id>/<photo>/photo              a photo as uploaded, until the API cleans it

// The limits of a project.
const (
	InviteTTL         = 7 * 24 * time.Hour
	MaxMembers        = 50
	MaxProjectPrints  = 500
	MaxProjectPhotos  = 1000
	MaxPrintFile      = 8 << 20
	MaxLapseFrames    = 2000
	urlTTL            = time.Hour
	maxProjectsForAll = 200
)

// Project is x/<id>/project.json.
type Project struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Note    string    `json:"note,omitempty"`
	Created time.Time `json:"created"`
	Changed time.Time `json:"changed"`
	Members []Member  `json:"members,omitempty"`
	// Public is the ID of the public address, or "".
	Public string `json:"public,omitempty"`
	// Prints maps the key of a print on the deck to its ID, so that a second upload replaces it.
	Prints map[string]string `json:"prints,omitempty"`
}

// Member is a member of a project, or an invite that waits (no Joined).
type Member struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Invited time.Time `json:"invited"`
	Joined  time.Time `json:"joined,omitzero"`
	// Invite is the hash of the invite code until the member joins, and Keys the files of the
	// passkeys of the member.
	Invite string   `json:"invite,omitempty"`
	Keys   []string `json:"keys,omitempty"`
}

func (p *Project) member(id string) *Member {
	for i := range p.Members {
		if p.Members[i].ID == id && !p.Members[i].Joined.IsZero() {
			return &p.Members[i]
		}
	}
	return nil
}

func projectKey(id string) string { return "x/" + id + "/project.json" }

// Invite is invites/<hash>.json.
type Invite struct {
	Project string    `json:"project"`
	Member  string    `json:"member"`
	Name    string    `json:"name"`
	Expires time.Time `json:"expires"`
}

func inviteKey(code string) string { return "invites/" + inviteHash(code) + ".json" }

// PrintRecord is x/<id>/prints/<print>.json.
type PrintRecord struct {
	cloud.PrintRequest
	ID     string    `json:"id"`
	Deck   string    `json:"deck"`
	Added  time.Time `json:"added"`
	Frames int       `json:"frames"`
	FPS    int       `json:"fps"`
	Model  bool      `json:"model"`
	Plan   bool      `json:"plan,omitempty"`
	Done   bool      `json:"done"`
}

// PhotoRecord is x/<id>/photos/<photo>.json.
type PhotoRecord struct {
	ID     string    `json:"id"`
	By     string    `json:"by"`               // the first name
	Member string    `json:"member,omitempty"` // the member, or "" for the owner
	Added  time.Time `json:"added"`
	W      int       `json:"w"`
	H      int       `json:"h"`
	Print  string    `json:"print,omitempty"`
}

// loadProject returns the project id and its ETag. A project that does not exist is 410.
func (s *Server) loadProject(ctx context.Context, id string) (*Project, string, error) {
	if !validID(id) {
		return nil, "", fail(http.StatusGone, "no such project")
	}
	b, etag, err := s.Store.Get(ctx, projectKey(id))
	if errors.Is(err, ErrNotFound) {
		return nil, "", fail(http.StatusGone, "no such project")
	}
	if err != nil {
		return nil, "", err
	}
	var p Project
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, "", err
	}
	return &p, etag, nil
}

// changeProject changes the project id with change, and writes it if no other write came first.
// It tries again after a conflict.
func (s *Server) changeProject(ctx context.Context, id string, change func(p *Project) error) (*Project, error) {
	for range 5 {
		p, etag, err := s.loadProject(ctx, id)
		if err != nil {
			return nil, err
		}
		if err := change(p); err != nil {
			return nil, err
		}
		p.Changed = s.now().UTC()
		data, _ := json.Marshal(p)
		_, err = s.Store.Put(ctx, projectKey(id), data, PutOptions{ContentType: "application/json", IfMatch: etag})
		if errors.Is(err, ErrConflict) {
			continue
		}
		return p, err
	}
	return nil, errors.New("the project changed too often: try again")
}

// who is the person of a project request: the owner (a session of the owner or a deck), or a
// member.
type who struct {
	owner   bool
	deck    *Deck
	member  string
	project string // the project of a member
	name    string
}

// whoFor returns the person of a request on the project id: a deck, an owner session, or a
// session of a member of the project.
func (s *Server) whoFor(ctx context.Context, r *http.Request, body []byte, id string) (*who, error) {
	if r.Header.Get(cloud.HeaderDeck) != "" {
		d, err := s.deck(ctx, r, body)
		if err != nil {
			return nil, err
		}
		return &who{owner: true, deck: d, name: s.ownerName(ctx)}, nil
	}
	ss, err := s.session(ctx, r)
	if err != nil {
		return nil, err
	}
	if ss.Kind == KindOwner {
		return &who{owner: true, name: s.ownerName(ctx)}, nil
	}
	if id != "" && ss.Project != id {
		return nil, fail(http.StatusUnauthorized, "you are not a member of this project")
	}
	p, _, err := s.loadProject(ctx, ss.Project)
	if err != nil {
		return nil, err
	}
	m := p.member(ss.Member)
	if m == nil {
		return nil, fail(http.StatusUnauthorized, "you are not a member of this project")
	}
	return &who{member: m.ID, project: p.ID, name: m.Name}, nil
}

func ownerOnly(w *who) error {
	if !w.owner {
		return fail(http.StatusUnauthorized, "only the owner can do this")
	}
	return nil
}

// projectURL is the page of a project for the owner and the members.
func (s *Server) projectURL(id string) string { return s.base() + "/app#x=" + id }

// routeProjects serves /api/v1/projects and /api/v1/projects/<id>/...
func (s *Server) routeProjects(ctx context.Context, r *http.Request, body []byte, rest string) (any, error) {
	id, action, _ := strings.Cut(rest, "/")
	w, err := s.whoFor(ctx, r, body, id)
	if err != nil {
		return nil, err
	}
	if id == "" {
		switch r.Method {
		case http.MethodGet:
			return s.listProjects(ctx, w)
		case http.MethodPost:
			if err := ownerOnly(w); err != nil {
				return nil, err
			}
			var in cloud.ProjectRequest
			if json.Unmarshal(body, &in) != nil {
				return nil, fail(http.StatusBadRequest, "bad JSON")
			}
			return s.newProject(ctx, in)
		}
		return nil, fail(http.StatusMethodNotAllowed, "no such request")
	}
	p, _, err := s.loadProject(ctx, id)
	if err != nil {
		return nil, err
	}
	part, sub, _ := strings.Cut(action, "/")
	switch {
	case action == "" && r.Method == http.MethodGet:
		return s.viewProject(ctx, p, w)
	case action == "" && r.Method == http.MethodPatch:
		if err := ownerOnly(w); err != nil {
			return nil, err
		}
		var in cloud.ProjectRequest
		if json.Unmarshal(body, &in) != nil || clip(in.Name, 80) == "" {
			return nil, fail(http.StatusBadRequest, "a project needs a name")
		}
		if _, err := s.changeProject(ctx, id, func(p *Project) error {
			p.Name, p.Note = clip(in.Name, 80), clip(in.Note, 280)
			return nil
		}); err != nil {
			return nil, err
		}
		return s.afterChange(ctx, id, w)
	case action == "" && r.Method == http.MethodDelete:
		if err := ownerOnly(w); err != nil {
			return nil, err
		}
		return nil, s.deleteProject(ctx, p)
	case action == "invites" && r.Method == http.MethodPost:
		if err := ownerOnly(w); err != nil {
			return nil, err
		}
		var in cloud.InviteRequest
		json.Unmarshal(body, &in)
		return s.newInvite(ctx, id, clip(in.Name, 40))
	case part == "members" && sub != "" && r.Method == http.MethodDelete:
		if err := ownerOnly(w); err != nil {
			return nil, err
		}
		if err := s.removeMember(ctx, id, sub); err != nil {
			return nil, err
		}
		return s.afterChange(ctx, id, w)
	case action == "public" && r.Method == http.MethodPost:
		if err := ownerOnly(w); err != nil {
			return nil, err
		}
		var in cloud.PublicRequest
		json.Unmarshal(body, &in)
		return s.setPublic(ctx, id, in)
	case action == "prints" && r.Method == http.MethodPost:
		if w.deck == nil {
			return nil, fail(http.StatusUnauthorized, "only a deck adds prints")
		}
		var in cloud.PrintRequest
		if json.Unmarshal(body, &in) != nil {
			return nil, fail(http.StatusBadRequest, "bad JSON")
		}
		return s.newPrint(ctx, p, w.deck, in)
	case part == "prints" && strings.HasSuffix(sub, "/done") && r.Method == http.MethodPost:
		if w.deck == nil {
			return nil, fail(http.StatusUnauthorized, "only a deck adds prints")
		}
		var in cloud.PrintDone
		json.Unmarshal(body, &in)
		if err := s.printDone(ctx, id, strings.TrimSuffix(sub, "/done"), in); err != nil {
			return nil, err
		}
		return s.afterChange(ctx, id, w)
	case part == "prints" && strings.HasSuffix(sub, "/frames") && r.Method == http.MethodGet:
		return s.printFrames(ctx, id, strings.TrimSuffix(sub, "/frames"))
	case part == "prints" && sub != "" && r.Method == http.MethodDelete:
		if err := ownerOnly(w); err != nil {
			return nil, err
		}
		if err := s.deletePrint(ctx, id, sub); err != nil {
			return nil, err
		}
		return s.afterChange(ctx, id, w)
	case action == "photos" && r.Method == http.MethodPost:
		n, err := s.Store.List(ctx, "x/"+id+"/photos/")
		if err != nil {
			return nil, err
		}
		if len(n)/3 >= MaxProjectPhotos {
			return nil, fail(http.StatusBadRequest, "the project has too many photos")
		}
		pid := newID(8)
		g, err := s.Grants.Grant(ctx, "up/x"+id+"/"+pid+"/", MaxPhotoFile, 15*time.Minute)
		if err != nil {
			return nil, err
		}
		return cloud.PhotoReply{Photo: pid, Grant: g}, nil
	case part == "photos" && strings.HasSuffix(sub, "/done") && r.Method == http.MethodPost:
		var in struct {
			Print string `json:"print"`
		}
		json.Unmarshal(body, &in)
		if err := s.projectPhoto(ctx, id, strings.TrimSuffix(sub, "/done"), w, in.Print); err != nil {
			return nil, err
		}
		return s.afterChange(ctx, id, w)
	case part == "photos" && sub != "" && r.Method == http.MethodDelete:
		if err := s.deletePhoto(ctx, id, sub, w); err != nil {
			return nil, err
		}
		return s.afterChange(ctx, id, w)
	}
	return nil, fail(http.StatusMethodNotAllowed, "no such request")
}

// afterChange writes the public page of the project again, and returns the view of the project.
func (s *Server) afterChange(ctx context.Context, id string, w *who) (any, error) {
	p, _, err := s.loadProject(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Public != "" {
		if err := s.publish(ctx, p); err != nil {
			return nil, err
		}
	}
	return s.viewProject(ctx, p, w)
}

// newProject makes a project.
func (s *Server) newProject(ctx context.Context, in cloud.ProjectRequest) (any, error) {
	name := clip(in.Name, 80)
	if name == "" {
		return nil, fail(http.StatusBadRequest, "a project needs a name")
	}
	keys, err := s.Store.List(ctx, "x/")
	if err == nil && countProjects(keys) >= maxProjectsForAll {
		return nil, fail(http.StatusBadRequest, "the service has too many projects")
	}
	now := s.now().UTC()
	p := Project{ID: newID(13), Name: name, Note: clip(in.Note, 280), Created: now, Changed: now}
	data, _ := json.Marshal(p)
	if _, err := s.Store.Put(ctx, projectKey(p.ID), data, PutOptions{ContentType: "application/json", IfNoneMatch: true}); err != nil {
		return nil, err
	}
	return cloud.ProjectSummary{ID: p.ID, Name: p.Name, Note: p.Note, Changed: p.Changed, URL: s.projectURL(p.ID)}, nil
}

func countProjects(keys []string) int {
	n := 0
	for _, k := range keys {
		if strings.HasSuffix(k, "/project.json") && strings.Count(k, "/") == 2 {
			n++
		}
	}
	return n
}

// listProjects returns the projects of the person: all for the owner, the project of a member.
func (s *Server) listProjects(ctx context.Context, w *who) (any, error) {
	var ids []string
	if w.owner {
		keys, err := s.Store.List(ctx, "x/")
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			if strings.HasSuffix(k, "/project.json") && strings.Count(k, "/") == 2 {
				ids = append(ids, strings.TrimSuffix(strings.TrimPrefix(k, "x/"), "/project.json"))
			}
		}
	} else {
		ids = []string{w.project}
	}
	out := []cloud.ProjectSummary{}
	for _, id := range ids {
		p, _, err := s.loadProject(ctx, id)
		if err != nil {
			continue
		}
		files, _ := s.Store.List(ctx, "x/"+id+"/")
		sum := cloud.ProjectSummary{ID: p.ID, Name: p.Name, Note: p.Note, Changed: p.Changed, URL: s.projectURL(p.ID)}
		var cover string
		for _, f := range files {
			rel := strings.TrimPrefix(f, "x/"+id+"/")
			switch {
			case strings.HasPrefix(rel, "prints/") && strings.HasSuffix(rel, ".json"):
				sum.Prints++
			case strings.HasPrefix(rel, "photos/") && strings.HasSuffix(rel, "-s.jpg"):
				sum.Photos++
				cover = f
			case cover == "" && strings.HasSuffix(rel, "/model.jpg"):
				cover = f
			}
		}
		for _, m := range p.Members {
			if !m.Joined.IsZero() {
				sum.Members++
			}
		}
		if cover != "" {
			sum.Cover, _ = s.Grants.FileURL(ctx, cover, urlTTL)
		}
		out = append(out, sum)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Changed.After(out[j].Changed) })
	return out, nil
}

// viewProject returns the project as its page shows it to the person w.
func (s *Server) viewProject(ctx context.Context, p *Project, w *who) (*cloud.ProjectView, error) {
	v := &cloud.ProjectView{ID: p.ID, Name: p.Name, Note: p.Note, Owner: s.ownerName(ctx), Created: p.Created,
		Changed: p.Changed, IsOwner: w.owner, You: w.member}
	if w.owner {
		v.You = KindOwner
		if p.Public != "" {
			v.Public = s.publicURL(p.Public)
		}
	}
	for _, m := range p.Members {
		if w.owner || !m.Joined.IsZero() {
			v.Members = append(v.Members, cloud.ProjectMember{ID: m.ID, Name: m.Name, Joined: !m.Joined.IsZero(), Invited: m.Invited})
		}
	}
	link := func(key string) string {
		u, _ := s.Grants.FileURL(ctx, key, urlTTL)
		return u
	}
	prints, photos, err := s.projectFiles(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	v.Prints, v.Photos = []cloud.ProjectPrint{}, []cloud.ProjectPhoto{}
	for _, pr := range prints {
		pp := projectPrint(pr)
		if pr.Model {
			pp.Model = link("x/" + p.ID + "/prints/" + pr.ID + "/model.jpg")
		}
		if pr.Plan {
			pp.Plan = link("x/" + p.ID + "/prints/" + pr.ID + "/plan.json")
		}
		v.Prints = append(v.Prints, pp)
	}
	for _, ph := range photos {
		base := "x/" + p.ID + "/photos/" + ph.ID
		v.Photos = append(v.Photos, cloud.ProjectPhoto{ID: ph.ID, By: ph.By, Mine: w.owner || ph.Member != "" && ph.Member == w.member,
			Added: ph.Added, URL: link(base + ".jpg"), Small: link(base + "-s.jpg"), W: ph.W, H: ph.H, Print: ph.Print})
	}
	return v, nil
}

// projectPrint is the print of a page, with no URLs.
func projectPrint(pr PrintRecord) cloud.ProjectPrint {
	return cloud.ProjectPrint{ID: pr.ID, Name: pr.Name, File: pr.File, Result: pr.Result, Ended: pr.Ended, Seconds: pr.Seconds,
		Material: pr.Material, Grams: pr.Grams, Roll: pr.Roll, Printer: pr.Printer, Note: pr.Note, Frames: pr.Frames, FPS: pr.FPS,
		Started: pr.Started, Layers: pr.Layers, Layer: pr.Layer, Height: pr.Height, GramsTotal: pr.GramsTotal,
		Estimate: pr.Estimate, Prep: pr.Prep, Slicer: pr.Slicer, Filament: pr.Filament}
}

// projectFiles returns the prints that are done, the newest first, and the photos, the newest
// first.
func (s *Server) projectFiles(ctx context.Context, id string) ([]PrintRecord, []PhotoRecord, error) {
	var prints []PrintRecord
	var photos []PhotoRecord
	keys, err := s.Store.List(ctx, "x/"+id+"/")
	if err != nil {
		return nil, nil, err
	}
	for _, k := range keys {
		rel := strings.TrimPrefix(k, "x/"+id+"/")
		if !strings.HasSuffix(rel, ".json") || strings.Count(rel, "/") != 1 {
			continue
		}
		b, _, err := s.Store.Get(ctx, k)
		if err != nil {
			continue
		}
		switch {
		case strings.HasPrefix(rel, "prints/"):
			var pr PrintRecord
			if json.Unmarshal(b, &pr) == nil && pr.Done {
				prints = append(prints, pr)
			}
		case strings.HasPrefix(rel, "photos/"):
			var ph PhotoRecord
			if json.Unmarshal(b, &ph) == nil {
				photos = append(photos, ph)
			}
		}
	}
	sort.Slice(prints, func(i, j int) bool { return prints[i].Ended.After(prints[j].Ended) })
	sort.Slice(photos, func(i, j int) bool { return photos[i].Added.After(photos[j].Added) })
	return prints, photos, nil
}

// newInvite makes an invite of the project for a friend with the first name.
func (s *Server) newInvite(ctx context.Context, id, name string) (any, error) {
	if name == "" {
		return nil, fail(http.StatusBadRequest, "type the first name of your friend")
	}
	code := newID(16)
	mid := newID(8)
	now := s.now().UTC()
	exp := now.Add(InviteTTL)
	if _, err := s.changeProject(ctx, id, func(p *Project) error {
		if len(p.Members) >= MaxMembers {
			return fail(http.StatusBadRequest, "the project has the most members")
		}
		p.Members = append(p.Members, Member{ID: mid, Name: name, Invited: now, Invite: inviteHash(code)})
		return nil
	}); err != nil {
		return nil, err
	}
	if err := s.putJSON(ctx, inviteKey(code), Invite{Project: id, Member: mid, Name: name, Expires: exp}); err != nil {
		return nil, err
	}
	return cloud.InviteReply{Member: mid, URL: s.base() + "/j#" + code, Expires: exp}, nil
}

// loadInvite returns the invite of the code, or 410 for a code that is used, removed or expired.
func (s *Server) loadInvite(ctx context.Context, code string) (*Invite, error) {
	code = strings.TrimSpace(code)
	if !validID(code) {
		return nil, fail(http.StatusGone, "this invite does not work: it is used, removed or expired")
	}
	b, _, err := s.Store.Get(ctx, inviteKey(code))
	if errors.Is(err, ErrNotFound) {
		return nil, fail(http.StatusGone, "this invite does not work: it is used, removed or expired")
	}
	if err != nil {
		return nil, err
	}
	var inv Invite
	if json.Unmarshal(b, &inv) != nil || s.now().After(inv.Expires) {
		return nil, fail(http.StatusGone, "this invite does not work: it is used, removed or expired")
	}
	return &inv, nil
}

// viewInvite serves POST /api/v1/invites/view: what the invite page shows before Join.
func (s *Server) viewInvite(ctx context.Context, body []byte) (any, error) {
	var in cloud.PasskeyRequest
	json.Unmarshal(body, &in)
	inv, err := s.loadInvite(ctx, in.Code)
	if err != nil {
		return nil, err
	}
	p, _, err := s.loadProject(ctx, inv.Project)
	if err != nil {
		return nil, err
	}
	return cloud.InviteView{Project: p.Name, Owner: s.ownerName(ctx), Name: inv.Name}, nil
}

// join makes the member of the invite a member with the passkey cred, and deletes the invite.
func (s *Server) join(ctx context.Context, code string, inv *Invite, cred *webauthn.Credential, name string) error {
	file := memberKeyFile(cred.ID)
	if err := s.Store.Delete(ctx, inviteKey(code)); err != nil {
		return err
	}
	if _, err := s.changeProject(ctx, inv.Project, func(p *Project) error {
		for i := range p.Members {
			m := &p.Members[i]
			if m.ID == inv.Member && m.Invite == inviteHash(code) {
				m.Joined, m.Invite, m.Keys = s.now().UTC(), "", append(m.Keys, file)
				if name != "" {
					m.Name = name
				}
				return nil
			}
		}
		return fail(http.StatusGone, "this invite does not work: it is used, removed or expired")
	}); err != nil {
		return err
	}
	return s.putJSON(ctx, file, Passkey{Credential: *cred, Added: s.now().UTC(), Project: inv.Project, Member: inv.Member})
}

// removeMember removes a member or an invite, and the passkeys of the member.
func (s *Server) removeMember(ctx context.Context, id, mid string) error {
	var gone Member
	if _, err := s.changeProject(ctx, id, func(p *Project) error {
		for i, m := range p.Members {
			if m.ID == mid {
				gone = m
				p.Members = append(p.Members[:i], p.Members[i+1:]...)
				return nil
			}
		}
		return fail(http.StatusGone, "no such member")
	}); err != nil {
		return err
	}
	for _, k := range gone.Keys {
		if strings.HasPrefix(k, "members/") {
			s.Store.Delete(ctx, k)
		}
	}
	if gone.Invite != "" {
		s.Store.Delete(ctx, "invites/"+gone.Invite+".json")
	}
	return nil
}

// newPrint starts the upload of a print of the deck d: the record waits for done.
func (s *Server) newPrint(ctx context.Context, p *Project, d *Deck, in cloud.PrintRequest) (any, error) {
	in.Name, in.File, in.Material, in.Roll, in.Printer, in.Note =
		clip(in.Name, 120), clip(in.File, 200), clip(in.Material, 40), clip(in.Roll, 80), clip(in.Printer, 64), clip(in.Note, 280)
	in.Key, in.Slicer = clip(in.Key, 200), clip(in.Slicer, 64)
	if f := in.Filament; f != nil {
		f.Name, f.Brand, f.Type, f.Color = clip(f.Name, 80), clip(f.Brand, 40), clip(f.Type, 40), clip(f.Color, 7)
	}
	switch in.Result {
	case "complete", "cancelled", "error":
	default:
		return nil, fail(http.StatusBadRequest, "bad result")
	}
	pid := p.Prints[in.Key]
	if pid == "" || in.Key == "" {
		keys, _ := s.Store.List(ctx, "x/"+p.ID+"/prints/")
		n := 0
		for _, k := range keys {
			if strings.HasSuffix(k, ".json") && strings.Count(strings.TrimPrefix(k, "x/"+p.ID+"/prints/"), "/") == 0 {
				n++
			}
		}
		if n >= MaxProjectPrints {
			return nil, fail(http.StatusBadRequest, "the project has too many prints")
		}
		pid = newID(8)
		if in.Key != "" {
			if _, err := s.changeProject(ctx, p.ID, func(p *Project) error {
				if p.Prints == nil {
					p.Prints = map[string]string{}
				}
				if old := p.Prints[in.Key]; old != "" {
					pid = old // another request came first
				}
				p.Prints[in.Key] = pid
				return nil
			}); err != nil {
				return nil, err
			}
		}
	} else if err := s.deletePrintFiles(ctx, p.ID, pid); err != nil {
		return nil, err
	}
	rec := PrintRecord{PrintRequest: in, ID: pid, Deck: d.ID, Added: s.now().UTC()}
	if err := s.putJSON(ctx, printKey(p.ID, pid), rec); err != nil {
		return nil, err
	}
	g, err := s.Grants.Grant(ctx, "x/"+p.ID+"/prints/"+pid+"/", MaxPrintFile, GrantTTL)
	if err != nil {
		return nil, err
	}
	return cloud.PrintReply{Print: pid, Grant: g}, nil
}

func printKey(project, print string) string { return "x/" + project + "/prints/" + print + ".json" }

// loadPrint returns the record of a print, or 410.
func (s *Server) loadPrint(ctx context.Context, project, print string) (*PrintRecord, error) {
	if !validID(print) {
		return nil, fail(http.StatusGone, "no such print")
	}
	b, _, err := s.Store.Get(ctx, printKey(project, print))
	if errors.Is(err, ErrNotFound) {
		return nil, fail(http.StatusGone, "no such print")
	}
	if err != nil {
		return nil, err
	}
	var pr PrintRecord
	if err := json.Unmarshal(b, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// printDone ends the upload of a print: it counts the frames that came, and shows the print.
func (s *Server) printDone(ctx context.Context, project, print string, in cloud.PrintDone) error {
	pr, err := s.loadPrint(ctx, project, print)
	if err != nil {
		return err
	}
	files, err := s.Store.List(ctx, "x/"+project+"/prints/"+print+"/")
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, f := range files {
		have[strings.TrimPrefix(f, "x/"+project+"/prints/"+print+"/")] = true
	}
	n := 0
	for n < min(in.Frames, MaxLapseFrames) && have["lapse/"+strconv.Itoa(n)+".jpg"] {
		n++
	}
	pr.Frames, pr.FPS, pr.Model, pr.Plan, pr.Done = n, max(1, min(60, in.FPS)), have["model.jpg"], have["plan.json"], true
	return s.putJSON(ctx, printKey(project, print), pr)
}

// printFrames serves GET .../prints/<print>/frames: the URLs of the frames of the time lapse.
func (s *Server) printFrames(ctx context.Context, project, print string) (any, error) {
	pr, err := s.loadPrint(ctx, project, print)
	if err != nil {
		return nil, err
	}
	out := cloud.FramesReply{FPS: pr.FPS, URLs: []string{}}
	for n := range pr.Frames {
		u, err := s.Grants.FileURL(ctx, "x/"+project+"/prints/"+print+"/lapse/"+strconv.Itoa(n)+".jpg", urlTTL)
		if err != nil {
			return nil, err
		}
		out.URLs = append(out.URLs, u)
	}
	return out, nil
}

// deletePrintFiles deletes the files of a print, and keeps its record.
func (s *Server) deletePrintFiles(ctx context.Context, project, print string) error {
	keys, err := s.Store.List(ctx, "x/"+project+"/prints/"+print+"/")
	if err != nil {
		return err
	}
	for _, k := range keys {
		if err := s.Store.Delete(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

// deletePrint removes a print from its project.
func (s *Server) deletePrint(ctx context.Context, project, print string) error {
	if _, err := s.loadPrint(ctx, project, print); err != nil {
		return err
	}
	if err := s.deletePrintFiles(ctx, project, print); err != nil {
		return err
	}
	if err := s.Store.Delete(ctx, printKey(project, print)); err != nil {
		return err
	}
	_, err := s.changeProject(ctx, project, func(p *Project) error {
		for k, v := range p.Prints {
			if v == print {
				delete(p.Prints, k)
			}
		}
		return nil
	})
	return err
}

// projectPhoto cleans the upload of the photo pid of the person w, and adds it to the project.
func (s *Server) projectPhoto(ctx context.Context, project, pid string, w *who, print string) error {
	if !validID(pid) {
		return fail(http.StatusBadRequest, "bad photo")
	}
	up := "up/x" + project + "/" + pid + "/photo"
	data, _, err := s.Store.Get(ctx, up)
	if errors.Is(err, ErrNotFound) {
		return fail(http.StatusBadRequest, "the photo did not arrive")
	}
	if err != nil {
		return err
	}
	defer s.Store.Delete(ctx, up)
	full, small, pw, ph, err := CleanPhoto(data)
	if err != nil {
		return fail(http.StatusBadRequest, "send the photo as a JPEG or PNG file")
	}
	base := "x/" + project + "/photos/" + pid
	opt := PutOptions{ContentType: "image/jpeg", CacheControl: "private, max-age=31536000, immutable"}
	if _, err := s.Store.Put(ctx, base+".jpg", full, opt); err != nil {
		return err
	}
	if _, err := s.Store.Put(ctx, base+"-s.jpg", small, opt); err != nil {
		return err
	}
	if print != "" && !validID(print) {
		print = ""
	}
	return s.putJSON(ctx, base+".json", PhotoRecord{ID: pid, By: w.name, Member: w.member, Added: s.now().UTC(), W: pw, H: ph, Print: print})
}

// deletePhoto removes a photo: the owner removes any photo, a member only their own.
func (s *Server) deletePhoto(ctx context.Context, project, pid string, w *who) error {
	if !validID(pid) {
		return fail(http.StatusGone, "no such photo")
	}
	base := "x/" + project + "/photos/" + pid
	b, _, err := s.Store.Get(ctx, base+".json")
	if errors.Is(err, ErrNotFound) {
		return fail(http.StatusGone, "no such photo")
	}
	if err != nil {
		return err
	}
	var ph PhotoRecord
	json.Unmarshal(b, &ph)
	if !w.owner && (ph.Member == "" || ph.Member != w.member) {
		return fail(http.StatusUnauthorized, "only the owner and the person who added it can remove a photo")
	}
	for _, k := range []string{base + ".jpg", base + "-s.jpg", base + ".json"} {
		if err := s.Store.Delete(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

// deleteProject deletes a project: its files, its public address, its invites and the passkeys
// of its members.
func (s *Server) deleteProject(ctx context.Context, p *Project) error {
	if p.Public != "" {
		if err := s.deletePrefix(ctx, "p/"+p.Public+"/"); err != nil {
			return err
		}
		s.Store.Delete(ctx, "p/"+p.Public)
	}
	for _, m := range p.Members {
		for _, k := range m.Keys {
			if strings.HasPrefix(k, "members/") {
				s.Store.Delete(ctx, k)
			}
		}
		if m.Invite != "" {
			s.Store.Delete(ctx, "invites/"+m.Invite+".json")
		}
	}
	if err := s.deletePrefix(ctx, "up/x"+p.ID+"/"); err != nil {
		return err
	}
	// The record last: a failure in the middle leaves a project that the owner can delete again.
	keys, err := s.Store.List(ctx, "x/"+p.ID+"/")
	if err != nil {
		return err
	}
	for _, k := range keys {
		if k != projectKey(p.ID) {
			if err := s.Store.Delete(ctx, k); err != nil {
				return err
			}
		}
	}
	return s.Store.Delete(ctx, projectKey(p.ID))
}

// deletePrefix deletes the files under prefix.
func (s *Server) deletePrefix(ctx context.Context, prefix string) error {
	keys, err := s.Store.List(ctx, prefix)
	if err != nil {
		return err
	}
	for _, k := range keys {
		if err := s.Store.Delete(ctx, k); err != nil {
			return err
		}
	}
	return nil
}
