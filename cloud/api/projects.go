package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
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
	ID   string `json:"id"`
	Name string `json:"name"`
	Note string `json:"note,omitempty"`
	// Notes are the notes of the people of the project, with the name of the last person who
	// changed them, and the time.
	Notes   string    `json:"notes,omitempty"`
	NotesBy string    `json:"notes_by,omitempty"`
	NotesAt time.Time `json:"notes_at,omitzero"`
	Created time.Time `json:"created"`
	Changed time.Time `json:"changed"`
	Members []Member  `json:"members,omitempty"`
	// Slug is the name of the project in its addresses (names.go), and OldSlugs the slugs of its
	// old names.
	Slug     string   `json:"slug,omitempty"`
	OldSlugs []string `json:"old_slugs,omitempty"`
	// Public is the slug of the public address, or "". OldPublic are the old public addresses,
	// with a page that sends the visitor to the new one.
	Public    string   `json:"public,omitempty"`
	OldPublic []string `json:"old_public,omitempty"`
	// LastPrint is the number of the newest print. Each new print has the next number.
	LastPrint int `json:"last_print,omitempty"`
	// Prints maps the key of a print on the deck to its ID, so that a second upload replaces it.
	Prints map[string]string `json:"prints,omitempty"`
	// Shares are the IDs of the share links of the prints of the project: live, or moved to the
	// print page.
	Shares []string `json:"shares,omitempty"`
	// Repo is the GitHub repository of the project, "owner/name", or "" (builds.go).
	Repo string `json:"repo,omitempty"`
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
	// GitHub is the GitHub username of the member, which the owner sets (builds.go).
	GitHub string `json:"github,omitempty"`
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
	ID string `json:"id"`
	// N is the number of the print in the project, or 0 for a print from before the numbers.
	N      int       `json:"n,omitempty"`
	Deck   string    `json:"deck"`
	Added  time.Time `json:"added"`
	Frames int       `json:"frames"`
	FPS    int       `json:"fps"`
	Model  bool      `json:"model"`
	Plan   bool      `json:"plan,omitempty"`
	Gcode  bool      `json:"gcode,omitempty"` // print.gcode.gz, for the 3D view of the print page
	Done   bool      `json:"done"`
	// The fields of the pages, which stay when the deck sends the print again: Title is the name
	// that the owner gave the print, or "", and it shows in place of Name. Notes are the notes of
	// the people of the project. Delivered is the time when the owner marked the print as
	// delivered, and Source the link to the source of the model.
	Title string `json:"title,omitempty"`
	pageFields
}

// pageFields are the fields of a print that the people change on the pages.
type pageFields struct {
	Notes     string    `json:"notes,omitempty"`
	NotesBy   string    `json:"notes_by,omitempty"`
	NotesAt   time.Time `json:"notes_at,omitzero"`
	Delivered time.Time `json:"delivered,omitzero"`
	Source    string    `json:"source,omitempty"`
	// Ready is the time of the mark Ready to pick up of the share of the print, or zero, and
	// ReadyNote its note.
	Ready     time.Time `json:"ready,omitzero"`
	ReadyNote string    `json:"ready_note,omitempty"`
	// Test is the time of the mark Test print, or zero.
	Test time.Time `json:"test,omitzero"`
	// Discarded is the time of the mark Discard (not good enough), or zero, and DiscardNote why.
	Discarded   time.Time `json:"discarded,omitzero"`
	DiscardNote string    `json:"discard_note,omitempty"`
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
// It tries again after a conflict. The project gets the time of the change.
func (s *Server) changeProject(ctx context.Context, id string, change func(p *Project) error) (*Project, error) {
	return s.updateProject(ctx, id, true, change)
}

// updateProject is changeProject. With touch false, the time of the last change stays: for the
// changes that the people of the project did not make.
func (s *Server) updateProject(ctx context.Context, id string, touch bool, change func(p *Project) error) (*Project, error) {
	for range 5 {
		p, etag, err := s.loadProject(ctx, id)
		if err != nil {
			return nil, err
		}
		if err := change(p); err != nil {
			return nil, err
		}
		if touch {
			p.Changed = s.now().UTC()
		}
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
	// projects are the projects of a member and the member in each, for a request on no
	// project.
	projects map[string]string
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
	if id == "" {
		return &who{projects: ss.projects()}, nil
	}
	mid := ss.memberIn(id)
	if mid == "" {
		return nil, fail(http.StatusUnauthorized, "you are not a member of this project")
	}
	p, _, err := s.loadProject(ctx, id)
	if err != nil {
		return nil, err
	}
	m := p.member(mid)
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

// projectURL is the page of the project p for the owner and the members. A project with no
// slug has the old address, and the page finds its slug.
func (s *Server) projectURL(p *Project) string {
	if p.Slug == "" {
		return s.base() + "/app#x=" + p.ID
	}
	return s.base() + "/projects/" + p.Slug
}

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
		if p, err = s.ensureSlug(ctx, p); err != nil {
			return nil, err
		}
		return s.viewProject(ctx, p, w)
	case action == "" && r.Method == http.MethodPatch:
		if err := ownerOnly(w); err != nil {
			return nil, err
		}
		var in cloud.ProjectRequest
		if json.Unmarshal(body, &in) != nil || clip(in.Name, 80) == "" {
			return nil, fail(http.StatusBadRequest, "a project needs a name")
		}
		p, err := s.changeProject(ctx, id, func(p *Project) error {
			p.Name, p.Note = clip(in.Name, 80), clip(in.Note, 280)
			return nil
		})
		if err != nil {
			return nil, err
		}
		if p, err = s.renameSlug(ctx, p); err != nil {
			return nil, err
		}
		if err := s.refreshShares(ctx, p); err != nil {
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
	case part == "members" && sub != "" && r.Method == http.MethodPatch:
		if err := ownerOnly(w); err != nil {
			return nil, err
		}
		var in cloud.MemberEdit
		if json.Unmarshal(body, &in) != nil {
			return nil, fail(http.StatusBadRequest, "bad JSON")
		}
		login := strings.TrimPrefix(strings.TrimSpace(in.GitHub), "@")
		if login != "" && !githubLogin.MatchString(login) {
			return nil, fail(http.StatusBadRequest, "a GitHub username has letters, digits and hyphens")
		}
		if _, err := s.changeProject(ctx, id, func(p *Project) error {
			for i := range p.Members {
				if p.Members[i].ID == sub {
					p.Members[i].GitHub = login
					return nil
				}
			}
			return fail(http.StatusGone, "no such member")
		}); err != nil {
			return nil, err
		}
		return s.afterChange(ctx, id, w)
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
		pid := strings.TrimSuffix(sub, "/done")
		if err := s.printDone(ctx, id, pid, in); err != nil {
			return nil, err
		}
		printer := ""
		if pr, err := s.loadPrint(ctx, id, pid); err == nil {
			printer = pr.PrinterID
		}
		if err := s.endLive(ctx, id, w.deck, printer); err != nil {
			return nil, err
		}
		return s.afterChange(ctx, id, w)
	case part == "prints" && strings.HasSuffix(sub, "/frames") && r.Method == http.MethodGet:
		return s.printFrames(ctx, id, strings.TrimSuffix(sub, "/frames"))
	case part == "prints" && sub != "" && !strings.Contains(sub, "/") && r.Method == http.MethodPatch:
		var in cloud.PrintEdit
		if json.Unmarshal(body, &in) != nil {
			return nil, fail(http.StatusBadRequest, "bad JSON")
		}
		if err := s.editPrint(ctx, id, sub, w, in); err != nil {
			return nil, err
		}
		return s.afterChange(ctx, id, w)
	case action == "repo" && r.Method == http.MethodPut:
		if err := ownerOnly(w); err != nil {
			return nil, err
		}
		var in cloud.RepoRequest
		if json.Unmarshal(body, &in) != nil {
			return nil, fail(http.StatusBadRequest, "bad JSON")
		}
		if _, err := s.setRepo(ctx, p, in.Repo); err != nil {
			return nil, err
		}
		return s.afterChange(ctx, id, w)
	case action == "plans" && r.Method == http.MethodGet:
		if w.deck != nil {
			return nil, fail(http.StatusUnauthorized, "only a person sees the plans")
		}
		plans, _, _ := s.plansView(ctx, p, 0)
		if plans == nil {
			plans = []cloud.ProjectPlan{}
		}
		return plans, nil
	case part == "plans" && sub != "" && !strings.Contains(sub, "/") && r.Method == http.MethodGet:
		if w.deck != nil {
			return nil, fail(http.StatusUnauthorized, "only a person sees a plan")
		}
		rec, err := s.loadPlan(ctx, id, sub)
		if err != nil {
			return nil, err
		}
		pv := s.projectPlan(ctx, p, rec)
		builds, _ := s.loadBuilds(ctx, id)
		for _, b := range builds {
			if b.Plan == rec.ID {
				pv.Builds = append(pv.Builds, s.projectBuild(ctx, p, b))
			}
		}
		return pv, nil
	case part == "plans" && strings.HasSuffix(sub, "/builds") && r.Method == http.MethodPost:
		var in cloud.BuildRequest
		if json.Unmarshal(body, &in) != nil {
			return nil, fail(http.StatusBadRequest, "bad JSON")
		}
		return s.newBuild(ctx, p, w, strings.TrimSuffix(sub, "/builds"), in)
	case part == "builds" && sub != "" && !strings.Contains(sub, "/") && r.Method == http.MethodGet:
		if w.deck != nil {
			return nil, fail(http.StatusUnauthorized, "only a person sees a build")
		}
		rec, err := s.loadBuild(ctx, id, sub)
		if err != nil {
			return nil, err
		}
		return s.projectBuild(ctx, p, rec), nil
	case part == "builds" && r.Method == http.MethodPost && strings.HasSuffix(sub, "/send"):
		// builds/<build>/plates/<n>/send
		f := strings.Split(sub, "/")
		if len(f) != 4 || f[1] != "plates" {
			return nil, fail(http.StatusMethodNotAllowed, "no such request")
		}
		n, err := strconv.Atoi(f[2])
		if err != nil {
			return nil, fail(http.StatusBadRequest, "bad plate")
		}
		var in cloud.PlateSend
		if json.Unmarshal(body, &in) != nil {
			return nil, fail(http.StatusBadRequest, "bad JSON")
		}
		return s.sendPlate(ctx, p, w, f[0], n, in)
	case part == "builds" && r.Method == http.MethodPost && (strings.HasSuffix(sub, "/approve") || strings.HasSuffix(sub, "/reject")):
		// builds/<build>/plates/<n>/approve or reject
		f := strings.Split(sub, "/")
		if len(f) != 4 || f[1] != "plates" {
			return nil, fail(http.StatusMethodNotAllowed, "no such request")
		}
		n, err := strconv.Atoi(f[2])
		if err != nil {
			return nil, fail(http.StatusBadRequest, "bad plate")
		}
		var in cloud.BuildDecision
		json.Unmarshal(body, &in)
		return s.decidePlate(ctx, p, w, f[0], n, f[3] == "approve", in.Reason)
	case action == "notes" && r.Method == http.MethodPatch:
		if w.deck != nil {
			return nil, fail(http.StatusUnauthorized, "only a person writes notes")
		}
		var in cloud.NotesEdit
		if json.Unmarshal(body, &in) != nil {
			return nil, fail(http.StatusBadRequest, "bad JSON")
		}
		if _, err := s.changeProject(ctx, id, func(p *Project) error {
			p.Notes, p.NotesBy, p.NotesAt = clipNotes(in.Notes), w.name, s.now().UTC()
			return nil
		}); err != nil {
			return nil, err
		}
		return s.afterChange(ctx, id, w)
	case part == "queue" && strings.Count(sub, "/") == 1 && r.Method == http.MethodDelete:
		// The owner takes a file of the project out of the queue of a printer.
		if err := ownerOnly(w); err != nil {
			return nil, err
		}
		printer, item, _ := strings.Cut(sub, "/")
		if err := s.removeQueued(ctx, id, printer, item, w); err != nil {
			return nil, err
		}
		return s.afterChange(ctx, id, w)
	case part == "prints" && sub != "" && r.Method == http.MethodDelete:
		if err := ownerOnly(w); err != nil {
			return nil, err
		}
		if err := s.deletePrint(ctx, id, sub); err != nil {
			return nil, err
		}
		return s.afterChange(ctx, id, w)
	case action == "live" && r.Method == http.MethodPost:
		if w.deck == nil {
			return nil, fail(http.StatusUnauthorized, "only a deck sends a live print")
		}
		var in cloud.LiveRequest
		if json.Unmarshal(body, &in) != nil {
			return nil, fail(http.StatusBadRequest, "bad JSON")
		}
		return s.postLive(ctx, p, w.deck, in)
	case part == "live" && r.Method == http.MethodDelete:
		if w.deck == nil {
			return nil, fail(http.StatusUnauthorized, "only a deck ends a live print")
		}
		// live/<printer> ends the live tile of a printer of the deck, and live the tile of the deck.
		slot := w.deck.ID
		if sub != "" {
			if slot = s.printerOf(ctx, w.deck, sub); slot == "" {
				return nil, fail(http.StatusBadRequest, "no such printer of this deck")
			}
		}
		return nil, s.deleteLive(ctx, id, slot)
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
	if p.Slug, err = s.claimName(ctx, p.ID, slugOf(name)); err != nil {
		return nil, err
	}
	data, _ := json.Marshal(p)
	if _, err := s.Store.Put(ctx, projectKey(p.ID), data, PutOptions{ContentType: "application/json", IfNoneMatch: true}); err != nil {
		return nil, err
	}
	return cloud.ProjectSummary{ID: p.ID, Name: p.Name, Slug: p.Slug, Note: p.Note, Changed: p.Changed, URL: s.projectURL(&p)}, nil
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
		for id := range w.projects {
			ids = append(ids, id)
		}
		sort.Strings(ids)
	}
	out := []cloud.ProjectSummary{}
	for _, id := range ids {
		p, _, err := s.loadProject(ctx, id)
		if err != nil {
			continue
		}
		if !w.owner && p.member(w.projects[id]) == nil {
			continue // the owner removed the member
		}
		if p, err = s.ensureSlug(ctx, p); err != nil {
			return nil, err
		}
		files, _ := s.Store.List(ctx, "x/"+id+"/")
		sum := cloud.ProjectSummary{ID: p.ID, Name: p.Name, Slug: p.Slug, Note: p.Note, Changed: p.Changed, URL: s.projectURL(p)}
		var cover string
		for _, f := range files {
			rel := strings.TrimPrefix(f, "x/"+id+"/")
			switch {
			case strings.HasPrefix(rel, "prints/") && strings.HasSuffix(rel, ".json") && strings.Count(rel, "/") == 1:
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
	v := &cloud.ProjectView{ID: p.ID, Name: p.Name, Slug: p.Slug, Note: p.Note, Notes: p.Notes, NotesBy: p.NotesBy, NotesAt: p.NotesAt,
		Owner: s.ownerName(ctx), Created: p.Created,
		Changed: p.Changed, IsOwner: w.owner, You: w.member}
	if w.owner {
		v.You = KindOwner
		if p.Public != "" {
			v.Public = s.publicURL(p.Public)
		}
	}
	for _, m := range p.Members {
		if w.owner || !m.Joined.IsZero() {
			v.Members = append(v.Members, cloud.ProjectMember{ID: m.ID, Name: m.Name, Joined: !m.Joined.IsZero(), Invited: m.Invited,
				GitHub: m.GitHub})
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
	v.Live = s.liveView(ctx, p, prints)
	v.Repo = p.Repo
	if p.Repo != "" {
		v.Plans, v.PlansTotal, v.Docs = s.plansView(ctx, p, PlansShown)
	}
	v.Printers = s.printersOf(ctx, printerIDs(prints, v.Live), false)
	v.Queue = s.projectQueued(ctx, p.ID)
	v.Prints, v.Photos = []cloud.ProjectPrint{}, []cloud.ProjectPhoto{}
	for _, pr := range prints {
		pp := projectPrint(pr)
		if pr.Model {
			pp.Model = link("x/" + p.ID + "/prints/" + pr.ID + "/model.jpg")
		}
		if pr.Plan {
			pp.Plan = link("x/" + p.ID + "/prints/" + pr.ID + "/plan.json")
		}
		if pr.Gcode {
			pp.Gcode = link("x/" + p.ID + "/prints/" + pr.ID + "/print.gcode.gz")
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
	name := pr.Name
	if pr.Title != "" {
		name = pr.Title
	}
	deck := ""
	if pr.Title != "" {
		deck = pr.Name
	}
	return cloud.ProjectPrint{ID: pr.ID, N: pr.N, PrinterID: pr.PrinterID, Name: name, DeckName: deck,
		Notes: pr.Notes, NotesBy: pr.NotesBy, NotesAt: pr.NotesAt, Delivered: pr.Delivered, Source: pr.Source,
		Ready: pr.Ready, ReadyNote: pr.ReadyNote, Test: pr.Test, Discarded: pr.Discarded, DiscardNote: pr.DiscardNote, File: pr.File, Result: pr.Result, Ended: pr.Ended, Seconds: pr.Seconds,
		Material: pr.Material, Grams: pr.Grams, Roll: pr.Roll, Printer: pr.Printer, Note: pr.Note, Frames: pr.Frames, FPS: pr.FPS,
		Started: pr.Started, Layers: pr.Layers, Layer: pr.Layer, Height: pr.Height, GramsTotal: pr.GramsTotal,
		Estimate: pr.Estimate, Prep: pr.Prep, Slicer: pr.Slicer, Filament: pr.Filament}
}

// projectFiles returns the prints that are done, the newest first, and the photos, the newest
// first. It gives a number to the prints from before the numbers.
func (s *Server) projectFiles(ctx context.Context, id string) ([]PrintRecord, []PhotoRecord, error) {
	var all []*PrintRecord
	var photos []PhotoRecord
	keys, err := s.Store.List(ctx, "x/"+id+"/")
	if err != nil {
		return nil, nil, err
	}
	for _, k := range keys {
		rel := strings.TrimPrefix(k, "x/"+id+"/")
		if !strings.HasSuffix(rel, ".json") || strings.Count(rel, "/") != 1 || strings.HasPrefix(rel, "live/") {
			continue
		}
		b, _, err := s.Store.Get(ctx, k)
		if err != nil {
			continue
		}
		switch {
		case strings.HasPrefix(rel, "prints/"):
			var pr PrintRecord
			if json.Unmarshal(b, &pr) == nil {
				all = append(all, &pr)
			}
		case strings.HasPrefix(rel, "photos/"):
			var ph PhotoRecord
			if json.Unmarshal(b, &ph) == nil {
				photos = append(photos, ph)
			}
		}
	}
	var todo []*PrintRecord
	for _, pr := range all {
		if pr.N == 0 {
			todo = append(todo, pr)
		}
	}
	if err := s.numberPrints(ctx, id, todo); err != nil {
		return nil, nil, err
	}
	var prints []PrintRecord
	for _, pr := range all {
		if pr.Done {
			prints = append(prints, *pr)
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
	return s.putJSON(ctx, file, Passkey{Credential: *cred, Added: s.now().UTC(), Handle: memberHandle(inv.Project, inv.Member),
		Projects: []Membership{{inv.Project, inv.Member}}})
}

// joinWith adds the project of the invite to the passkey k of a member, in the file of the
// passkey, and uses the invite.
func (s *Server) joinWith(ctx context.Context, code string, inv *Invite, file string, k *Passkey, name string) error {
	k.normalize()
	if k.hasProject(inv.Project) {
		return fail(http.StatusConflict, "you are a member of this project already: sign in")
	}
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
	k.Projects = append(k.Projects, Membership{inv.Project, inv.Member})
	return s.putJSON(ctx, file, k)
}

// leaveKey takes the project off the passkey of a member in file. A passkey with no project
// left goes.
func (s *Server) leaveKey(ctx context.Context, file, project string) {
	if !strings.HasPrefix(file, "members/") {
		return
	}
	b, _, err := s.Store.Get(ctx, file)
	if err != nil {
		return
	}
	var k Passkey
	if json.Unmarshal(b, &k) != nil {
		return
	}
	k.normalize()
	k.Projects = slices.DeleteFunc(k.Projects, func(m Membership) bool { return m.Project == project })
	if len(k.Projects) == 0 {
		s.Store.Delete(ctx, file)
		return
	}
	s.putJSON(ctx, file, k)
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
		s.leaveKey(ctx, k, id)
	}
	if gone.Invite != "" {
		s.Store.Delete(ctx, "invites/"+gone.Invite+".json")
	}
	return nil
}

// NotesLimit is the most characters of the notes of a project or a print.
const NotesLimit = 4000

// clipNotes returns the notes with no space at the ends, and at most NotesLimit characters.
func clipNotes(notes string) string {
	notes = strings.TrimSpace(strings.ReplaceAll(notes, "\r\n", "\n"))
	if r := []rune(notes); len(r) > NotesLimit {
		notes = string(r[:NotesLimit])
	}
	return notes
}

// editPrint changes the fields of the print pid of the project id that in has. The owner
// changes all of them, and a member only the notes.
func (s *Server) editPrint(ctx context.Context, id, pid string, w *who, in cloud.PrintEdit) error {
	if w.deck != nil {
		return fail(http.StatusUnauthorized, "only a person edits a print")
	}
	if (in.Title != nil || in.Delivered != nil || in.Ready != nil || in.Test != nil || in.Discard != nil || in.DiscardNote != nil || in.Source != nil) && !w.owner {
		return fail(http.StatusUnauthorized, "only the owner can do this")
	}
	pr, err := s.loadPrint(ctx, id, pid)
	if err != nil {
		return err
	}
	if in.Title != nil {
		pr.Title = clip(strings.TrimSpace(*in.Title), 120)
		if pr.Title == pr.Name {
			pr.Title = ""
		}
	}
	if in.Notes != nil {
		pr.Notes, pr.NotesBy, pr.NotesAt = clipNotes(*in.Notes), w.name, s.now().UTC()
	}
	if in.Delivered != nil {
		switch {
		case !*in.Delivered:
			pr.Delivered = time.Time{}
		case pr.Delivered.IsZero():
			pr.Delivered = s.now().UTC()
		}
	}
	if in.Discard != nil {
		switch {
		case !*in.Discard:
			pr.Discarded, pr.DiscardNote = time.Time{}, ""
		case pr.Discarded.IsZero():
			pr.Discarded = s.now().UTC()
		}
	}
	if in.DiscardNote != nil && !pr.Discarded.IsZero() {
		pr.DiscardNote = clip(strings.TrimSpace(*in.DiscardNote), 500)
	}
	if in.Test != nil {
		switch {
		case !*in.Test:
			pr.Test = time.Time{}
		case pr.Test.IsZero():
			pr.Test = s.now().UTC()
		}
	}
	if in.Ready != nil {
		switch {
		case !*in.Ready:
			pr.Ready, pr.ReadyNote = time.Time{}, ""
		case pr.Ready.IsZero():
			pr.Ready = s.now().UTC()
		}
	}
	if in.Source != nil {
		src := strings.TrimSpace(*in.Source)
		if src != "" {
			u, err := url.Parse(src)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || len(src) > 500 {
				return fail(http.StatusBadRequest, "the source needs a web address, for example a file of a commit on GitHub")
			}
		}
		pr.Source = src
	}
	return s.putJSON(ctx, printKey(id, pid), pr)
}

// newPrint starts the upload of a print of the deck d: the record waits for done.
func (s *Server) newPrint(ctx context.Context, p *Project, d *Deck, in cloud.PrintRequest) (any, error) {
	in.Name, in.File, in.Material, in.Roll, in.Printer, in.Note =
		clip(in.Name, 120), clip(in.File, 200), clip(in.Material, 40), clip(in.Roll, 80), clip(in.Printer, 64), clip(in.Note, 280)
	in.Key, in.Slicer = clip(in.Key, 200), clip(in.Slicer, 64)
	share := in.Share
	in.Share = ""
	if f := in.Filament; f != nil {
		f.Name, f.Brand, f.Type, f.Color = clip(f.Name, 80), clip(f.Brand, 40), clip(f.Type, 40), clip(f.Color, 7)
	}
	switch in.Result {
	case "complete", "cancelled", "error":
	default:
		return nil, fail(http.StatusBadRequest, "bad result")
	}
	pid := p.Prints[in.Key]
	isNew := pid == "" || in.Key == ""
	if isNew {
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
					pid, isNew = old, false // another request came first
				}
				p.Prints[in.Key] = pid
				return nil
			}); err != nil {
				return nil, err
			}
		}
	}
	n, title, page := 0, "", pageFields{}
	if isNew {
		// The prints from before the numbers get theirs first.
		if _, _, err := s.projectFiles(ctx, p.ID); err != nil {
			return nil, err
		}
		q, err := s.updateProject(ctx, p.ID, false, func(q *Project) error {
			q.LastPrint++
			return nil
		})
		if err != nil {
			return nil, err
		}
		n = q.LastPrint
	} else {
		// A print that the deck sends again keeps its number and the fields of the pages.
		if old, err := s.loadPrint(ctx, p.ID, pid); err == nil {
			n, title, page = old.N, old.Title, old.pageFields
		}
		if err := s.deletePrintFiles(ctx, p.ID, pid); err != nil {
			return nil, err
		}
	}
	in.PrinterID = s.printerOf(ctx, d, in.PrinterID)
	rec := PrintRecord{PrintRequest: in, ID: pid, N: n, Deck: d.ID, Added: s.now().UTC(), Title: title, pageFields: page}
	if err := s.putJSON(ctx, printKey(p.ID, pid), rec); err != nil {
		return nil, err
	}
	if share != "" {
		if err := s.shareToPrint(ctx, share, p, d, pid); err != nil {
			return nil, err
		}
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
	pr.Gcode = have["print.gcode.gz"]
	if err := s.putJSON(ctx, printKey(project, print), pr); err != nil {
		return err
	}
	s.commentPrint(ctx, project, *pr)
	return nil
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
	if err := s.putJSON(ctx, base+".json", PhotoRecord{ID: pid, By: w.name, Member: w.member, Added: s.now().UTC(), W: pw, H: ph, Print: print}); err != nil {
		return err
	}
	if print != "" {
		if pr, err := s.loadPrint(ctx, project, print); err == nil {
			s.commentPrint(ctx, project, *pr)
		}
	}
	return nil
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
	if err := s.deleteNames(ctx, p); err != nil {
		return err
	}
	if p.Repo != "" {
		s.Store.Delete(ctx, repoKey(p.Repo))
	}
	if p.Public != "" {
		if err := s.deletePrefix(ctx, "p/"+p.Public+"/"); err != nil {
			return err
		}
		s.Store.Delete(ctx, "p/"+p.Public)
	}
	for _, m := range p.Members {
		for _, k := range m.Keys {
			s.leaveKey(ctx, k, p.ID)
		}
		if m.Invite != "" {
			s.Store.Delete(ctx, "invites/"+m.Invite+".json")
		}
	}
	if err := s.deletePrefix(ctx, "up/x"+p.ID+"/"); err != nil {
		return err
	}
	// The share links of the prints: a live share loses its project, and a moved share goes.
	for _, id := range p.Shares {
		sh, err := s.loadShare(ctx, id)
		if err != nil {
			continue
		}
		if sh.Moved {
			if err := s.deleteShare(ctx, id); err != nil {
				return err
			}
			continue
		}
		sh.Project, sh.Print = "", ""
		if err := s.putJSON(ctx, shareKey(id), sh); err != nil {
			return err
		}
		if err := s.updateReady(ctx, sh, func(rd *cloud.ShareReady) { rd.Project = nil }); err != nil {
			return err
		}
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
