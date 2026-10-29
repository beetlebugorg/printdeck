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

// The public address of a project: p/<public id>, a copy of assets/public.html, with
// p/<public id>/page.json and copies of the pictures, the time lapse frames and the photos. The
// public ID is separate from the project ID, so the address gives no access to the API. The
// owner turns it on and off, and a new address deletes the old one. Each change of the project
// writes the copies again (publish).

// PublicPage is the key of the public page in the bucket. pwf deploy uploads it.
const PublicPage = "assets/public.html"

// publicURL is the public address of a project.
func (s *Server) publicURL(pub string) string { return s.base() + "/p/" + pub }

// setPublic turns the public address of a project on or off, or makes a new one.
func (s *Server) setPublic(ctx context.Context, id string, in cloud.PublicRequest) (any, error) {
	p, _, err := s.loadProject(ctx, id)
	if err != nil {
		return nil, err
	}
	old := p.Public
	if in.On && old != "" && !in.Renew {
		return cloud.PublicReply{URL: s.publicURL(old)}, nil
	}
	pub := ""
	if in.On {
		pub = newID(13)
	}
	p, err = s.changeProject(ctx, id, func(p *Project) error {
		p.Public = pub
		return nil
	})
	if err != nil {
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
	view := cloud.ProjectView{Name: p.Name, Note: p.Note, Owner: s.ownerName(ctx), Created: p.Created, Changed: p.Changed,
		Prints: []cloud.ProjectPrint{}, Photos: []cloud.ProjectPhoto{}}
	want := map[string]string{} // the copy and its source
	for _, pr := range prints {
		pp := projectPrint(pr)
		pp.File, pp.Roll = "", "" // the names of the files and the rolls stay private
		src := "x/" + p.ID + "/prints/" + pr.ID + "/"
		rel := "prints/" + pr.ID + "/"
		if pr.Model {
			pp.Model = rel + "model.jpg"
			want[dst+pp.Model] = src + "model.jpg"
		}
		for n := range pr.Frames {
			f := "lapse/" + strconv.Itoa(n) + ".jpg"
			want[dst+rel+f] = src + f
		}
		view.Prints = append(view.Prints, pp)
	}
	for _, ph := range photos {
		src := "x/" + p.ID + "/photos/" + ph.ID
		rel := "photos/" + ph.ID
		want[dst+rel+".jpg"], want[dst+rel+"-s.jpg"] = src+".jpg", src+"-s.jpg"
		view.Photos = append(view.Photos, cloud.ProjectPhoto{ID: ph.ID, By: ph.By, Added: ph.Added, URL: rel + ".jpg",
			Small: rel + "-s.jpg", W: ph.W, H: ph.H, Print: ph.Print})
	}
	have, err := s.Store.List(ctx, dst)
	if err != nil {
		return err
	}
	got := map[string]bool{}
	for _, k := range have {
		got[k] = true
		if _, ok := want[k]; !ok && k != dst+"page.json" {
			if err := s.Store.Delete(ctx, k); err != nil {
				return err
			}
		}
	}
	for to, from := range want {
		if got[to] {
			continue
		}
		if err := s.Store.Copy(ctx, from, to, PutOptions{CacheControl: "public, max-age=31536000, immutable"}); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
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
