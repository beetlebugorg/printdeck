package api

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"path"
	"strconv"
	"strings"

	"printdeck/internal/cloud"
)

// The public view of a project: p/<slug>/page.json, with copies of its files. The project page,
// /projects/<slug>, shows it to a visitor, at the same address as for the members. p/<slug> is
// a copy of assets/public.html, which goes to the project page: the old links work. The copy
// gives no access to the API. A rename moves it to the new slug (movePublic). Each change of
// the project writes the copies again (publish).
//
// The page has two kinds of content:
//
//   - The owner's public page, which the owner turns on and off: the prints, the time lapses,
//     the photos and the notes, with no names of members.
//   - The content of a public GitHub repository (RepoPublic): the Parts tab of the newest ready
//     plan of the default branch, its docs and its cover. These files are on GitHub already.
//     Its copies are under repo/<plan>/, so a new plan gets new addresses.
//
// Project.Public is the slug of the page when it has one kind or both. Project.RepoOnly marks a
// page with the repository content only.

// PublicPage is the key of the public page in the bucket. pwf deploy uploads it.
const PublicPage = "assets/public.html"

// publicURL is the address of a public project for a visitor: the project page itself.
func (s *Server) publicURL(pub string) string { return s.base() + "/projects/" + pub }

// ownerPublic reports that the owner turned the public page of p on.
func ownerPublic(p *Project) bool { return p.Public != "" && !p.RepoOnly }

// repoShown reports that the public page of p shows the content of its repository.
func repoShown(p *Project) bool { return p.Repo != "" && p.RepoPublic }

// setPublic turns the owner's public page of a project on or off. With a public repository, the
// page stays at its address with the content of the repository only.
func (s *Server) setPublic(ctx context.Context, id string, in cloud.PublicRequest) (any, error) {
	p, _, err := s.loadProject(ctx, id)
	if err != nil {
		return nil, err
	}
	if p, err = s.ensureSlug(ctx, p); err != nil {
		return nil, err
	}
	if in.On && ownerPublic(p) {
		// Write the page again: an earlier publish can stop before its end.
		if err := s.publish(ctx, p); err != nil {
			return nil, err
		}
		return cloud.PublicReply{URL: s.publicURL(p.Public)}, nil
	}
	if !in.On && !ownerPublic(p) {
		return cloud.PublicReply{}, nil
	}
	if !in.On && !repoShown(p) {
		return cloud.PublicReply{}, s.removePublic(ctx, p)
	}
	p, err = s.changeProject(ctx, id, func(q *Project) error {
		if q.Public == "" {
			q.Public = q.Slug
		}
		q.RepoOnly = !in.On
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.refreshShares(ctx, p); err != nil {
		return nil, err
	}
	if err := s.publish(ctx, p); err != nil {
		return nil, err
	}
	if !in.On {
		return cloud.PublicReply{}, nil
	}
	return cloud.PublicReply{URL: s.publicURL(p.Public)}, nil
}

// removePublic deletes the public page of the project p, its copies, and the pages at its old
// addresses.
func (s *Server) removePublic(ctx context.Context, p *Project) error {
	old := p.Public
	if err := s.deleteOldPublic(ctx, p); err != nil {
		return err
	}
	p, err := s.updateProject(ctx, p.ID, false, func(q *Project) error {
		q.Public, q.RepoOnly, q.OldPublic = "", false, nil
		return nil
	})
	if err != nil {
		return err
	}
	if err := s.refreshShares(ctx, p); err != nil {
		return err
	}
	if old == "" {
		return nil
	}
	return s.deletePage(ctx, old)
}

// deletePage deletes the public page at the slug and its copies.
func (s *Server) deletePage(ctx context.Context, slug string) error {
	if err := s.deletePrefix(ctx, "p/"+slug+"/"); err != nil {
		return err
	}
	return s.Store.Delete(ctx, "p/"+slug)
}

// refreshPublic asks GitHub for the visibility of the repository of the project id, and then
// makes the public page match: it writes the page, makes a page for a public repository, or
// deletes the page when no content of the project is public.
func (s *Server) refreshPublic(ctx context.Context, id string) error {
	p, _, err := s.loadProject(ctx, id)
	if err != nil {
		return err
	}
	if p, err = s.refreshRepoPublic(ctx, p); err != nil {
		return err
	}
	switch {
	case p.Public != "" && !p.RepoOnly:
		return s.publish(ctx, p)
	case repoShown(p):
		if p.Public == "" {
			if p, err = s.ensureSlug(ctx, p); err != nil {
				return err
			}
			p, err = s.updateProject(ctx, p.ID, false, func(q *Project) error {
				if q.Public == "" {
					q.Public, q.RepoOnly = q.Slug, true
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return s.publish(ctx, p)
	case p.Public != "":
		return s.removePublic(ctx, p)
	}
	return nil
}

// publishType is the type of each kind of file that the public page copies from a plan. The
// Action of the repository chooses the names and the types of its files, so the copy gets its
// type from this list, and a file with another end is not copied.
var publishType = map[string]string{".stl": "model/stl", ".pdf": "application/pdf", ".png": "image/png",
	".jpg": "image/jpeg"}

// publish writes the public page of the project p: the page, page.json, and the copies of the
// files. It copies only the files that are not there yet, and deletes the copies of removed
// files. The page has the owner's content only when the owner's public page is on, and the
// content of the repository only when it is public. It has no names of members.
func (s *Server) publish(ctx context.Context, p *Project) error {
	if p.Public == "" {
		return nil
	}
	dst := "p/" + p.Public + "/"
	owner, repo := ownerPublic(p), repoShown(p)
	if !owner && !repo {
		// No content is public: the page and its copies go.
		return s.deletePage(ctx, p.Public)
	}
	view := cloud.ProjectView{Name: p.Name, Note: p.Note, Created: p.Created, RepoOnly: !owner,
		Prints: []cloud.ProjectPrint{}, Photos: []cloud.ProjectPhoto{}}
	want := map[string]string{}  // the copy and its source
	types := map[string]string{} // the type of a copy, or none for the type of its name
	if owner {
		if err := s.publishOwner(ctx, p, dst, &view, want); err != nil {
			return err
		}
	}
	if repo {
		if err := s.publishRepo(ctx, p, dst, &view, want, types); err != nil {
			return err
		}
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
		err := s.Store.Copy(ctx, want[to], to, PutOptions{ContentType: types[to], CacheControl: "public, max-age=31536000, immutable"})
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
	desc := "A project of 3D prints with Printdeck: the prints, the time lapses and the photos."
	if !owner {
		desc = "A project of 3D prints with Printdeck: the parts, their 3D models and the docs."
	}
	image := ""
	if view.Cover != "" {
		image = s.base() + "/" + dst + view.Cover
	}
	_, err = s.Store.Put(ctx, "p/"+p.Public, PublicPageFor(page, s.publicURL(p.Public), p.Name, desc, image),
		PutOptions{ContentType: "text/html; charset=utf-8", CacheControl: "public, max-age=60"})
	return err
}

// publishOwner adds the content of the owner's public page to the view of the page at dst: the
// owner's first name, the notes, the prints and the photos, with no names of members. want gets
// the copies.
func (s *Server) publishOwner(ctx context.Context, p *Project, dst string, view *cloud.ProjectView, want map[string]string) error {
	prints, photos, err := s.projectFiles(ctx, p.ID)
	if err != nil {
		return err
	}
	view.Owner, view.Changed = s.ownerName(ctx), p.Changed
	view.Notes, view.NotesAt = p.Notes, p.NotesAt
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
	return nil
}

// publishRepo adds the content of the public repository to the view of the page at dst: the
// link, and the parts, the assemblies, the docs and the cover of the newest ready plan of the
// default branch. A plan of a fork, of another branch, or of an earlier repository stays out.
// want gets the copies, and types their types.
func (s *Server) publishRepo(ctx context.Context, p *Project, dst string, view *cloud.ProjectView,
	want, types map[string]string) error {
	view.Repo = p.Repo
	plans, err := s.loadPlans(ctx, p.ID)
	if err != nil {
		return err
	}
	var rec *PlanRecord
	for _, r := range plans {
		if r.Ready && r.Kind == cloud.BuildMain && r.Fork == "" && strings.EqualFold(r.Repo, p.Repo) {
			rec = r
			break
		}
	}
	if rec == nil {
		return nil
	}
	src, rel := planDir(p.ID, rec.ID), "repo/"+rec.ID+"/"
	// add returns the path of the copy of the file of the plan, or "" for a file that the page
	// does not copy.
	add := func(name string) string {
		t, ok := publishType[strings.ToLower(path.Ext(name))]
		if name == "" || !ok || !fileName.MatchString(name) {
			return ""
		}
		want[dst+rel+name], types[dst+rel+name] = src+name, t
		return rel + name
	}
	plan := cloud.ProjectPlan{ID: rec.ID, Repo: rec.Repo, Commit: rec.Commit, Kind: rec.Kind, Branch: rec.Branch,
		Created: rec.Created, Ready: true, Parts: []cloud.ProjectPlanPart{}}
	for _, pp := range rec.Parts {
		part := cloud.ProjectPlanPart{PlanPart: cloud.PlanPart{Name: pp.Name, Variant: pp.Variant, Files: pp.Files,
			Count: pp.Count, Printer: pp.Printer, Process: pp.Process, MaterialName: pp.MaterialName,
			MaterialColor: pp.MaterialColor, Description: pp.Description, Group: pp.Group, Models: pp.Models,
			SizeX: pp.SizeX, SizeY: pp.SizeY, SizeZ: pp.SizeZ}}
		for _, m := range pp.Models {
			part.ModelURLs = append(part.ModelURLs, add(m))
		}
		plan.Parts = append(plan.Parts, part)
	}
	for _, a := range rec.Assemblies {
		pa := cloud.ProjectAssembly{PlanAssembly: a}
		for _, it := range a.Items {
			pa.ModelURLs = append(pa.ModelURLs, add(it.Model))
		}
		plan.Assemblies = append(plan.Assemblies, pa)
	}
	for _, d := range rec.Docs {
		doc := cloud.ProjectDoc{PlanDoc: d, Plan: rec.ID, Commit: rec.Commit, URL: add(d.PDF), ThumbURL: add(d.Thumb)}
		if doc.URL == "" {
			continue
		}
		if hexSHA.MatchString(rec.Commit) {
			doc.RepoURL = "https://github.com/" + rec.Repo + "/blob/" + rec.Commit + "/" + escapeRepoPath(d.File)
		}
		view.Docs = append(view.Docs, doc)
	}
	view.Plans, view.Cover = []cloud.ProjectPlan{plan}, add(rec.Cover)
	if view.RepoOnly {
		view.Changed = rec.Created
	}
	return nil
}

// Publish writes the public page of the project p in the store st, for the service at base, as
// a change of the project does. pwf deploy uses it to write each public page again.
func Publish(ctx context.Context, st Store, base string, p *Project) error {
	return (&Server{Store: st, Base: base}).publish(ctx, p)
}

// PublicPageFor is the public page of the project with the name at url: the page with the name,
// and the tags of Open Graph for the preview of the link in message apps, with the description
// desc and the picture at the address image ("" for none).
func PublicPageFor(page []byte, url, name, desc, image string) []byte {
	esc := html.EscapeString
	tags := []string{
		`<meta property="og:site_name" content="Printdeck">`,
		`<meta property="og:type" content="website">`,
		`<meta property="og:title" content="` + esc(name) + `">`,
		`<meta property="og:description" content="` + esc(desc) + `">`,
		`<meta property="og:url" content="` + esc(url) + `">`,
	}
	if image != "" {
		tags = append(tags, `<meta property="og:image" content="`+esc(image)+`">`,
			`<meta name="twitter:card" content="summary_large_image">`)
	}
	out := strings.Replace(string(page), "<title>Project · Printdeck</title>", "<title>"+esc(name)+" · Printdeck</title>", 1)
	return []byte(strings.Replace(out, previewMark, strings.Join(tags, "\n"), 1))
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
