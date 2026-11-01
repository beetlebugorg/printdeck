package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"sort"
	"strings"

	"printdeck/internal/cloud"
)

// The addresses of the projects. A project has a slug, a name for its address that comes from
// the name of the project: /projects/<slug> for the owner and the members, and /p/<slug> for
// its public page. names/<slug>.json has the ID of the project of the slug. A rename gives the
// project a new slug. The old slugs stay in names/, so an old address finds the project. The
// prints of a project have a number from 1, in the order of their upload:
// /projects/<slug>/<n>.

// nameRecord is names/<slug>.json.
type nameRecord struct {
	Project string `json:"project"`
}

func nameKey(slug string) string { return "names/" + slug + ".json" }

// maxSlug is the most characters of a slug before its number ("-2").
const maxSlug = 48

// accents maps the Latin letters with accents to plain letters, for slugOf.
var accents = map[rune]string{
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'ä': "a", 'å': "a", 'æ': "ae", 'ç': "c",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ì': "i", 'í': "i", 'î': "i", 'ï': "i",
	'ñ': "n", 'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ö': "o", 'ø': "o", 'œ': "oe",
	'ù': "u", 'ú': "u", 'û': "u", 'ü': "u", 'ý': "y", 'ÿ': "y", 'ß': "ss",
}

// slugOf is the slug of a project name: lowercase letters and digits, with "-" between the
// words, for example "garden-planters" for "Garden planters". Letters with accents lose the
// accents. Other characters go. A name with no letters or digits gives "project".
func slugOf(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		s := ""
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			s = string(r)
		case accents[r] != "":
			s = accents[r]
		}
		if s == "" {
			dash = b.Len() > 0
			continue
		}
		if dash {
			b.WriteByte('-')
			dash = false
		}
		b.WriteString(s)
	}
	out := b.String()
	if len(out) > maxSlug {
		out = strings.TrimRight(out[:maxSlug], "-")
	}
	if out == "" {
		return "project"
	}
	return out
}

// validSlug reports that slug has only the characters of slugOf, and a sane length.
func validSlug(slug string) bool {
	if slug == "" || len(slug) > maxSlug+4 || slug[0] == '-' || slug[len(slug)-1] == '-' {
		return false
	}
	for _, r := range slug {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// slugFits reports that slug is base, or base with a number ("garden-planters-2").
func slugFits(slug, base string) bool {
	if slug == base {
		return true
	}
	n, ok := strings.CutPrefix(slug, base+"-")
	if !ok || n == "" {
		return false
	}
	for _, r := range n {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (s *Server) loadName(ctx context.Context, slug string) (*nameRecord, error) {
	b, _, err := s.Store.Get(ctx, nameKey(slug))
	if err != nil {
		return nil, err
	}
	var rec nameRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// claimName returns a free slug for the project id from base: base, or base with the first free
// number from 2. A slug that is already the slug of the project id is free for it. It writes
// names/<slug>.json.
func (s *Server) claimName(ctx context.Context, id, base string) (string, error) {
	data, _ := json.Marshal(nameRecord{Project: id})
	for i := 1; i <= 100; i++ {
		slug := base
		if i > 1 {
			slug = fmt.Sprintf("%s-%d", base, i)
		}
		rec, err := s.loadName(ctx, slug)
		if err == nil {
			if rec.Project == id {
				return slug, nil
			}
			continue
		}
		if !errors.Is(err, ErrNotFound) {
			return "", err
		}
		_, err = s.Store.Put(ctx, nameKey(slug), data, PutOptions{ContentType: "application/json", IfNoneMatch: true})
		if errors.Is(err, ErrConflict) {
			continue // another project came first
		}
		if err != nil {
			return "", err
		}
		return slug, nil
	}
	return "", errors.New("no free address for the project: change its name")
}

// ensureSlug gives the project p a slug when it has none: the projects from before the slugs.
// A public page then moves to the slug.
func (s *Server) ensureSlug(ctx context.Context, p *Project) (*Project, error) {
	if p.Slug == "" {
		slug, err := s.claimName(ctx, p.ID, slugOf(p.Name))
		if err != nil {
			return nil, err
		}
		p, err = s.updateProject(ctx, p.ID, false, func(q *Project) error {
			if q.Slug == "" {
				q.Slug = slug
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if p.Public != "" && p.Public != p.Slug {
		return s.movePublic(ctx, p)
	}
	return p, nil
}

// renameSlug gives the project p the slug of its new name. The old slug stays in names/ and in
// OldSlugs. A name with the same slug keeps the slug.
func (s *Server) renameSlug(ctx context.Context, p *Project) (*Project, error) {
	base := slugOf(p.Name)
	if p.Slug != "" && slugFits(p.Slug, base) {
		return p, nil
	}
	slug, err := s.claimName(ctx, p.ID, base)
	if err != nil {
		return nil, err
	}
	p, err = s.updateProject(ctx, p.ID, false, func(q *Project) error {
		if q.Slug != "" && q.Slug != slug {
			q.OldSlugs = append(q.OldSlugs, q.Slug)
		}
		q.Slug = slug
		return nil
	})
	if err != nil {
		return nil, err
	}
	if p.Public != "" && p.Public != p.Slug {
		return s.movePublic(ctx, p)
	}
	return p, nil
}

// movePublic moves the public page of the project p to its slug: it writes the page at the
// slug, deletes the old copies, and puts a page at the old address that sends the visitor to
// the new one.
func (s *Server) movePublic(ctx context.Context, p *Project) (*Project, error) {
	old := p.Public
	p, err := s.updateProject(ctx, p.ID, false, func(q *Project) error {
		if q.Public != "" && q.Public != q.Slug {
			q.OldPublic = append(q.OldPublic, q.Public)
			q.Public = q.Slug
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.publish(ctx, p); err != nil {
		return nil, err
	}
	if err := s.refreshShares(ctx, p); err != nil {
		return nil, err
	}
	if err := s.deletePrefix(ctx, "p/"+old+"/"); err != nil {
		return nil, err
	}
	_, err = s.Store.Put(ctx, "p/"+old, movedPage("/p/"+p.Public, p.Name),
		PutOptions{ContentType: "text/html; charset=utf-8", CacheControl: "public, max-age=60"})
	return p, err
}

// movedPage is a page that sends the visitor to the address url of the project with the name.
func movedPage(url, name string) []byte {
	esc := html.EscapeString
	return []byte(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex">
<meta http-equiv="refresh" content="0; url=` + esc(url) + `">
<title>` + esc(name) + ` · Printdeck</title>
<link rel="stylesheet" href="/assets/site.css">
</head>
<body>
<div class="wrap">
<p>This project moved to <a href="` + esc(url) + `">` + esc(url) + `</a>.</p>
</div>
</body>
</html>
`)
}

// deleteNames deletes the slugs of the project p and the pages at its old public addresses.
func (s *Server) deleteNames(ctx context.Context, p *Project) error {
	for _, slug := range append(append([]string(nil), p.OldSlugs...), p.Slug) {
		if slug == "" {
			continue
		}
		if rec, err := s.loadName(ctx, slug); err == nil && rec.Project == p.ID {
			if err := s.Store.Delete(ctx, nameKey(slug)); err != nil {
				return err
			}
		}
	}
	return s.deleteOldPublic(ctx, p)
}

// deleteOldPublic deletes the pages at the old public addresses of the project p.
func (s *Server) deleteOldPublic(ctx context.Context, p *Project) error {
	for _, old := range p.OldPublic {
		if err := s.Store.Delete(ctx, "p/"+old); err != nil {
			return err
		}
	}
	return nil
}

// lookupName serves GET /api/v1/names/<slug>: the project of the slug, for the owner and the
// members of the project. For other people the reply is the same as for no project.
func (s *Server) lookupName(ctx context.Context, r *http.Request, slug string) (any, error) {
	ss, err := s.session(ctx, r)
	if err != nil {
		return nil, err
	}
	gone := fail(http.StatusGone, "no such project")
	if !validSlug(slug) {
		return nil, gone
	}
	rec, err := s.loadName(ctx, slug)
	if errors.Is(err, ErrNotFound) {
		return nil, gone
	}
	if err != nil {
		return nil, err
	}
	if ss.Kind != KindOwner && ss.memberIn(rec.Project) == "" {
		return nil, gone
	}
	p, _, err := s.loadProject(ctx, rec.Project)
	if err != nil {
		return nil, err
	}
	if ss.Kind != KindOwner && p.member(ss.memberIn(rec.Project)) == nil {
		return nil, gone
	}
	if p, err = s.ensureSlug(ctx, p); err != nil {
		return nil, err
	}
	out := cloud.NameReply{ID: p.ID, Slug: p.Slug}
	if r.URL.Query().Get("view") != "" {
		w := &who{owner: true, name: s.ownerName(ctx)}
		if ss.Kind != KindOwner {
			m := p.member(ss.memberIn(rec.Project))
			w = &who{member: m.ID, project: p.ID, name: m.Name}
		}
		if out.View, err = s.viewProject(ctx, p, w); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// numberPrints gives a number to each print in recs, in the order of their upload, after the
// last number of the project id. It writes the records.
func (s *Server) numberPrints(ctx context.Context, id string, recs []*PrintRecord) error {
	if len(recs) == 0 {
		return nil
	}
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].Added.Before(recs[j].Added) })
	first := 0
	if _, err := s.updateProject(ctx, id, false, func(p *Project) error {
		first = p.LastPrint + 1
		p.LastPrint += len(recs)
		return nil
	}); err != nil {
		return err
	}
	for i, rec := range recs {
		pr, err := s.changePrint(ctx, id, rec.ID, func(pr *PrintRecord) error {
			if pr.N != 0 {
				return errNoChange
			}
			pr.N = first + i
			return nil
		})
		if isGone(err) {
			continue
		}
		if err != nil {
			return err
		}
		*rec = *pr
	}
	return nil
}
