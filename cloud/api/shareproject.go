package api

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"path"
	"slices"
	"strings"

	"printdeck/internal/cloud"
)

// The share links of the prints of a project. A share of a print of a project shows the project
// on its page, and the project page shows the share while the print runs. When the deck adds the
// print to the project, the photos of the share go to the print. At the end of the share, its
// page sends the visitor to the print page of the project.

// shareTarget is the page of the project p for a share: the print page, or the project page
// before the print is in it. It is the public page when the project has a public address.
func (s *Server) shareTarget(p *Project, print string) string {
	if p.Public != "" {
		u := s.publicURL(p.Public)
		if print != "" {
			u += "#p=" + print
		}
		return u
	}
	u := s.projectURL(p.ID)
	if print != "" {
		u += "&p=" + print
	}
	return u
}

// linkShare links the share sh to the project with the ID project, or to no project for "". It
// saves the share.
func (s *Server) linkShare(ctx context.Context, sh *Share, project string) error {
	if project == sh.Project {
		return s.showShareProject(ctx, sh)
	}
	if sh.Project != "" {
		if err := s.dropShare(ctx, sh.Project, sh.ID); err != nil {
			return err
		}
	}
	sh.Project, sh.Print = project, ""
	if project != "" {
		if _, err := s.changeProject(ctx, project, func(p *Project) error {
			if !slices.Contains(p.Shares, sh.ID) {
				p.Shares = append(p.Shares, sh.ID)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	if err := s.putJSON(ctx, shareKey(sh.ID), sh); err != nil {
		return err
	}
	return s.showShareProject(ctx, sh)
}

// dropShare removes the share id from the list of the project. A project that is gone is no
// error.
func (s *Server) dropShare(ctx context.Context, project, id string) error {
	_, err := s.changeProject(ctx, project, func(p *Project) error {
		p.Shares = slices.DeleteFunc(p.Shares, func(x string) bool { return x == id })
		return nil
	})
	if isGone(err) {
		return nil
	}
	return err
}

// isGone reports an error of a project or a share that does not exist.
func isGone(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusGone
}

// showShareProject writes the project of the share sh to its ready.json, or to its page when the
// share moved.
func (s *Server) showShareProject(ctx context.Context, sh *Share) error {
	var info *cloud.ShareProject
	if sh.Project != "" {
		p, _, err := s.loadProject(ctx, sh.Project)
		if err != nil && !isGone(err) {
			return err
		}
		if p != nil {
			info = &cloud.ShareProject{Name: p.Name, URL: s.shareTarget(p, sh.Print)}
		}
	}
	if sh.Moved {
		if info == nil {
			return s.deleteShare(ctx, sh.ID)
		}
		return s.putMovedPage(ctx, sh, info)
	}
	return s.updateReady(ctx, sh, func(rd *cloud.ShareReady) { rd.Project = info })
}

// putMovedPage writes the page of a share that moved: a page that sends the visitor to the
// print page of the project.
func (s *Server) putMovedPage(ctx context.Context, sh *Share, info *cloud.ShareProject) error {
	esc := html.EscapeString
	title := sh.Job
	if title == "" {
		title = "A print"
	}
	page := `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex">
<meta http-equiv="refresh" content="0; url=` + esc(info.URL) + `">
<title>` + esc(title) + ` · Printdeck</title>
<link rel="stylesheet" href="/assets/site.css">
</head>
<body>
<div class="wrap">
<p>This print is now in the project <a href="` + esc(info.URL) + `">` + esc(info.Name) + `</a>.</p>
</div>
</body>
</html>
`
	_, err := s.Store.Put(ctx, "s/"+sh.ID, []byte(page), PutOptions{ContentType: "text/html; charset=utf-8",
		CacheControl: "public, max-age=60"})
	return err
}

// shareToPrint links the share with the ID id to the print pid of the project p, for the deck d.
// It copies the photos of the share to the print.
func (s *Server) shareToPrint(ctx context.Context, id string, p *Project, d *Deck, pid string) error {
	sh, err := s.loadShare(ctx, id)
	if isGone(err) {
		return nil // the share ended, or the deck stopped it
	}
	if err != nil {
		return err
	}
	if sh.Deck != d.ID {
		return nil
	}
	if sh.Project != p.ID {
		if err := s.linkShare(ctx, sh, p.ID); err != nil {
			return err
		}
	}
	sh.Print = pid
	if err := s.putJSON(ctx, shareKey(sh.ID), sh); err != nil {
		return err
	}
	if !sh.Moved {
		rd, err := s.readReady(ctx, sh.ID)
		if err != nil {
			return err
		}
		for _, ph := range rd.Photos {
			if err := s.copySharePhoto(ctx, sh, ph); err != nil {
				return err
			}
		}
	}
	return s.showShareProject(ctx, sh)
}

// copySharePhoto copies a photo of the share sh to the print of the share in its project. The
// photo keeps its ID, so a second copy does nothing.
func (s *Server) copySharePhoto(ctx context.Context, sh *Share, ph cloud.SharePhoto) error {
	if sh.Project == "" || sh.Print == "" {
		return nil
	}
	pid := strings.TrimSuffix(path.Base(ph.File), ".jpg")
	if !validID(pid) {
		return nil
	}
	base := "x/" + sh.Project + "/photos/" + pid
	if _, _, err := s.Store.Get(ctx, base+".json"); err == nil {
		return nil
	}
	opt := PutOptions{ContentType: "image/jpeg", CacheControl: "private, max-age=31536000, immutable"}
	if err := s.Store.Copy(ctx, "s/"+sh.ID+"/"+ph.File, base+".jpg", opt); err != nil {
		return err
	}
	if err := s.Store.Copy(ctx, "s/"+sh.ID+"/"+ph.Small, base+"-s.jpg", opt); err != nil {
		return err
	}
	return s.putJSON(ctx, base+".json", PhotoRecord{ID: pid, By: s.ownerName(ctx), Added: s.now().UTC(), W: ph.W, H: ph.H,
		Print: sh.Print})
}

// liveShare is a share of a project that runs now, and its ID.
type liveShare struct {
	id string
	cloud.ProjectLive
}

// liveShares returns the shares of the project p that run now.
func (s *Server) liveShares(ctx context.Context, p *Project) []liveShare {
	var out []liveShare
	for _, id := range p.Shares {
		sh, err := s.loadShare(ctx, id)
		if err != nil || sh.Moved || !sh.Ended.IsZero() || sh.Project != p.ID {
			continue
		}
		out = append(out, liveShare{sh.ID, cloud.ProjectLive{Name: sh.Job, URL: s.shareURL(sh.ID), Started: sh.Created}})
	}
	return out
}

// refreshShares writes the project of each share of p again: after a new name or a new public
// address.
func (s *Server) refreshShares(ctx context.Context, p *Project) error {
	for _, id := range p.Shares {
		sh, err := s.loadShare(ctx, id)
		if isGone(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err := s.showShareProject(ctx, sh); err != nil {
			return err
		}
	}
	return nil
}

// moveShare ends a share of a print in a project: it deletes the files of the share, and its
// page sends the visitor to the print page.
func (s *Server) moveShare(ctx context.Context, sh *Share) error {
	keys, err := s.Store.List(ctx, "s/"+sh.ID+"/")
	if err != nil {
		return err
	}
	ups, err := s.Store.List(ctx, "up/"+sh.ID+"/")
	if err != nil {
		return err
	}
	for _, k := range append(keys, ups...) {
		if err := s.Store.Delete(ctx, k); err != nil {
			return err
		}
	}
	sh.Moved, sh.OwnerKey = true, ""
	if err := s.putJSON(ctx, shareKey(sh.ID), sh); err != nil {
		return err
	}
	return s.showShareProject(ctx, sh)
}

// shareProject serves POST /api/v1/shares/<id>/project for the deck of the share.
func (s *Server) shareProject(ctx context.Context, sh *Share, body []byte) (any, error) {
	var in cloud.ShareProjectRequest
	if json.Unmarshal(body, &in) != nil {
		return nil, fail(http.StatusBadRequest, "bad JSON")
	}
	if in.Project != "" {
		if _, _, err := s.loadProject(ctx, in.Project); err != nil {
			return nil, err
		}
	}
	if err := s.linkShare(ctx, sh, in.Project); err != nil {
		return nil, err
	}
	return s.readReady(ctx, sh.ID)
}
