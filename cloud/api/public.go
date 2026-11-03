package api

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"strconv"
	"strings"

	"printdeck/internal/cloud"
)

// The public view of a project: p/<slug>/page.json, with copies of the pictures, the time lapse
// videos (or the frames of a print with no video) and the photos. The project page,
// /projects/<slug>, shows it to a visitor, at the same address as for the members. p/<slug> is
// a copy of assets/public.html, which goes to the project page: the old links work. The copy
// gives no access to the API. The owner turns it on and off. A rename
// moves it to the new slug (movePublic). Each change of the project writes the copies again
// (publish).

// PublicPage is the key of the public page in the bucket. pwf deploy uploads it.
const PublicPage = "assets/public.html"

// publicURL is the address of a public project for a visitor: the project page itself.
func (s *Server) publicURL(pub string) string { return s.base() + "/projects/" + pub }

// setPublic turns the public address of a project on or off.
func (s *Server) setPublic(ctx context.Context, id string, in cloud.PublicRequest) (any, error) {
	p, _, err := s.loadProject(ctx, id)
	if err != nil {
		return nil, err
	}
	if p, err = s.ensureSlug(ctx, p); err != nil {
		return nil, err
	}
	old := p.Public
	if in.On && old != "" {
		// Write the page again: an earlier publish can stop before its end.
		if err := s.publish(ctx, p); err != nil {
			return nil, err
		}
		return cloud.PublicReply{URL: s.publicURL(old)}, nil
	}
	if !in.On && old == "" {
		return cloud.PublicReply{}, nil
	}
	pub := ""
	if in.On {
		pub = p.Slug
	} else if err := s.deleteOldPublic(ctx, p); err != nil {
		return nil, err
	}
	p, err = s.changeProject(ctx, id, func(p *Project) error {
		p.Public = pub
		if pub == "" {
			p.OldPublic = nil
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.refreshShares(ctx, p); err != nil {
		return nil, err
	}
	if old != "" {
		if err := s.deletePrefix(ctx, "p/"+old+"/"); err != nil {
			return nil, err
		}
		if err := s.Store.Delete(ctx, "p/"+old); err != nil {
			return nil, err
		}
	}
	if pub == "" {
		return cloud.PublicReply{}, nil
	}
	if err := s.publish(ctx, p); err != nil {
		return nil, err
	}
	return cloud.PublicReply{URL: s.publicURL(pub)}, nil
}

// publish writes the public page of the project p: the page, page.json, and the copies of the
// files. It copies only the files that are not there yet, and deletes the copies of removed
// files.
func (s *Server) publish(ctx context.Context, p *Project) error {
	if p.Public == "" {
		return nil
	}
	dst := "p/" + p.Public + "/"
	prints, photos, err := s.projectFiles(ctx, p.ID)
	if err != nil {
		return err
	}
	// The page has the notes, which the owner chose to publish, and no names of members.
	view := cloud.ProjectView{Name: p.Name, Note: p.Note, Notes: p.Notes, NotesAt: p.NotesAt,
		Owner: s.ownerName(ctx), Created: p.Created, Changed: p.Changed,
		Prints: []cloud.ProjectPrint{}, Photos: []cloud.ProjectPhoto{}}
	want := map[string]string{} // the copy and its source
	for _, pr := range prints {
		pp := projectPrint(pr)
		pp.File, pp.Roll = "", "" // the names of the files and the rolls stay private
		pp.NotesBy = ""
		src := "x/" + p.ID + "/prints/" + pr.ID + "/"
		rel := "prints/" + pr.ID + "/"
		if pr.Model {
			pp.Model = rel + "model.jpg"
			want[dst+pp.Model] = src + "model.jpg"
		}
		if pr.Plan {
			pp.Plan = rel + "plan.json"
			want[dst+pp.Plan] = src + "plan.json"
		}
		switch {
		case pr.Video:
			pp.Video, pp.Poster = rel+"lapse.mp4", rel+"lapse.jpg"
			want[dst+pp.Video], want[dst+pp.Poster] = src+"lapse.mp4", src+"lapse.jpg"
		default:
			for n := range pr.Frames {
				f := "lapse/" + strconv.Itoa(n) + ".jpg"
				want[dst+rel+f] = src + f
			}
		}
		view.Prints = append(view.Prints, pp)
	}
	view.Printers = s.printersOf(ctx, printerIDs(prints, nil), true)
	for _, ph := range photos {
		src := "x/" + p.ID + "/photos/" + ph.ID
		rel := "photos/" + ph.ID
		want[dst+rel+".jpg"], want[dst+rel+"-s.jpg"] = src+".jpg", src+"-s.jpg"
		view.Photos = append(view.Photos, cloud.ProjectPhoto{ID: ph.ID, Added: ph.Added, URL: rel + ".jpg",
			Small: rel + "-s.jpg", W: ph.W, H: ph.H, Print: ph.Print})
	}
	have, err := s.Store.List(ctx, dst)
	if err != nil {
		return err
	}
	// A project can have more than a thousand files, so the deletes and the copies run at the
	// same time. One at a time, they took more than the time limit of the function.
	got := map[string]bool{}
	var gone, copies []string
	for _, k := range have {
		got[k] = true
		if _, ok := want[k]; !ok && k != dst+"page.json" {
			gone = append(gone, k)
		}
	}
	for to := range want {
		if !got[to] {
			copies = append(copies, to)
		}
	}
	if err := forAll(len(gone), func(i int) error { return s.Store.Delete(ctx, gone[i]) }); err != nil {
		return err
	}
	if err := forAll(len(copies), func(i int) error {
		to := copies[i]
		err := s.Store.Copy(ctx, want[to], to, PutOptions{CacheControl: "public, max-age=31536000, immutable"})
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}); err != nil {
		return err
	}
	data, _ := json.Marshal(view)
	if _, err := s.Store.Put(ctx, dst+"page.json", data, PutOptions{ContentType: "application/json", CacheControl: "public, max-age=60"}); err != nil {
		return err
	}
	page, _, err := s.Store.Get(ctx, PublicPage)
	if err != nil {
		return errors.New("no public page in the bucket: run pwf deploy")
	}
	_, err = s.Store.Put(ctx, "p/"+p.Public, PublicPageFor(page, s.publicURL(p.Public), p.Name),
		PutOptions{ContentType: "text/html; charset=utf-8", CacheControl: "public, max-age=60"})
	return err
}

// Publish writes the public page of the project p in the store st, for the service at base, as
// a change of the project does. pwf deploy uses it to write each public page again.
func Publish(ctx context.Context, st Store, base string, p *Project) error {
	return (&Server{Store: st, Base: base}).publish(ctx, p)
}

// PublicPageFor is the public page of the project with the name at url: the page with the name,
// and the tags of Open Graph for the preview of the link in message apps.
func PublicPageFor(page []byte, url, name string) []byte {
	esc := html.EscapeString
	tags := strings.Join([]string{
		`<meta property="og:site_name" content="Printdeck">`,
		`<meta property="og:type" content="website">`,
		`<meta property="og:title" content="` + esc(name) + `">`,
		`<meta property="og:description" content="A project of 3D prints with Printdeck: the prints, the time lapses and the photos.">`,
		`<meta property="og:url" content="` + esc(url) + `">`,
	}, "\n")
	out := strings.Replace(string(page), "<title>Project · Printdeck</title>", "<title>"+esc(name)+" · Printdeck</title>", 1)
	return []byte(strings.Replace(out, previewMark, tags, 1))
}

// PublicPages returns the projects with a public address, for pwf deploy: it writes their pages
// again from the new public page.
func PublicPages(ctx context.Context, st Store) ([]Project, error) {
	keys, err := st.List(ctx, "x/")
	if err != nil {
		return nil, err
	}
	var out []Project
	for _, k := range keys {
		if !strings.HasSuffix(k, "/project.json") || strings.Count(k, "/") != 2 {
			continue
		}
		b, _, err := st.Get(ctx, k)
		if err != nil {
			continue
		}
		var p Project
		if json.Unmarshal(b, &p) == nil && p.Public != "" {
			out = append(out, p)
		}
	}
	return out, nil
}
