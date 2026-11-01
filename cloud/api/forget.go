package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"printdeck/internal/cloud"
)

// The deletion of the data of a person (PRIVACY.md).
//
//   - A member deletes their account (DELETE /api/v1/me): the service forgets the member in each
//     project of their passkeys, and deletes the passkeys.
//   - The owner deletes the data of a member of a project (DELETE
//     /api/v1/projects/<id>/members/<mid>?data=delete): the service forgets the member in that
//     project.
//   - The owner deletes a passkey of the owner (DELETE /api/v1/me/passkeys/<id>), but not the
//     last one: the service needs an owner.
//
// To forget a member is to delete the member, the invite, the passkeys, the photos of the member
// and their public copies, and the GitHub username. The notes of the project and of the prints
// stay, because other people wrote them too: the name of the member on them becomes
// FormerMember. The fork plans of the member lose the GitHub username of the member.

// FormerMember is the name of a deleted member on the notes and the removals of the queue.
const FormerMember = "A former member"

// ghFile matches the key of a public picture of a comment on a pull request, in a record.
var ghFile = regexp.MustCompile(`gh/[0-9a-f]{32}\.(?:png|jpg)`)

// forgetMember deletes the data of the member mid of the project id, and keeps the project.
func (s *Server) forgetMember(ctx context.Context, id, mid string) error {
	p, _, err := s.loadProject(ctx, id)
	if err != nil {
		return err
	}
	var m *Member
	for i := range p.Members {
		if p.Members[i].ID == mid {
			m = &p.Members[i]
		}
	}
	if m == nil {
		return fail(http.StatusGone, "no such member")
	}
	name, login := m.Name, m.GitHub
	if err := s.removeMember(ctx, id, mid); err != nil {
		return err
	}
	// The photos of the member, and the copies of them in the comments on pull requests.
	gone, err := s.deleteMemberPhotos(ctx, id, mid)
	if err != nil {
		return err
	}
	if err := s.dropPhotoCopies(ctx, id, gone); err != nil {
		return err
	}
	if name != "" {
		if err := s.blankName(ctx, id, name); err != nil {
			return err
		}
	}
	if login != "" {
		if err := s.blankAuthor(ctx, id, login); err != nil {
			return err
		}
	}
	return nil
}

// deleteMemberPhotos deletes the photos that the member mid added to the project, and returns
// their IDs by the print of each.
func (s *Server) deleteMemberPhotos(ctx context.Context, project, mid string) (map[string]string, error) {
	keys, err := s.Store.List(ctx, "x/"+project+"/photos/")
	if err != nil {
		return nil, err
	}
	gone := map[string]string{}
	for _, k := range keys {
		if !strings.HasSuffix(k, ".json") {
			continue
		}
		b, _, err := s.Store.Get(ctx, k)
		if err != nil {
			continue
		}
		var ph PhotoRecord
		if json.Unmarshal(b, &ph) != nil || ph.Member != mid || !validID(ph.ID) {
			continue
		}
		base := strings.TrimSuffix(k, ".json")
		for _, f := range []string{base + ".jpg", base + "-s.jpg", k} {
			if err := s.Store.Delete(ctx, f); err != nil {
				return nil, err
			}
		}
		gone[ph.ID] = ph.Print
	}
	return gone, nil
}

// dropPhotoCopies deletes the public copies of the photos in the comments on the pull requests
// of the project, and writes the comments of their prints again. photos maps each photo to its
// print.
func (s *Server) dropPhotoCopies(ctx context.Context, project string, photos map[string]string) error {
	if len(photos) == 0 {
		return nil
	}
	keys, err := s.Store.List(ctx, "x/"+project+"/prs/")
	if err != nil {
		return err
	}
	for _, k := range keys {
		b, _, err := s.Store.Get(ctx, k)
		if err != nil {
			continue
		}
		var st prRecord
		if json.Unmarshal(b, &st) != nil {
			continue
		}
		changed := false
		for pid, u := range st.Photos {
			if _, ok := photos[pid]; !ok {
				continue
			}
			if f := ghFile.FindString(u); f != "" {
				if err := s.Store.Delete(ctx, f); err != nil {
					return err
				}
			}
			delete(st.Photos, pid)
			changed = true
		}
		if changed {
			if err := s.putJSON(ctx, k, st); err != nil {
				return err
			}
		}
	}
	prints := map[string]bool{}
	for _, print := range photos {
		if print != "" && !prints[print] {
			prints[print] = true
			if pr, err := s.loadPrint(ctx, project, print); err == nil {
				s.commentPrint(ctx, project, *pr)
			}
		}
	}
	return nil
}

// blankName puts FormerMember in place of the name on the notes of the project and of its
// prints, and on the removals of the queues of the printers that wait for a deck.
func (s *Server) blankName(ctx context.Context, project, name string) error {
	if _, err := s.updateProject(ctx, project, false, func(p *Project) error {
		if p.NotesBy == name {
			p.NotesBy = FormerMember
		}
		return nil
	}); err != nil {
		return err
	}
	keys, _, err := s.Store.ListDir(ctx, "x/"+project+"/prints/")
	if err != nil {
		return err
	}
	for _, k := range keys {
		if !strings.HasSuffix(k, ".json") {
			continue
		}
		b, _, err := s.Store.Get(ctx, k)
		if err != nil {
			continue
		}
		var pr PrintRecord
		if json.Unmarshal(b, &pr) != nil || pr.NotesBy != name {
			continue
		}
		pr.NotesBy = FormerMember
		if err := s.putJSON(ctx, k, pr); err != nil {
			return err
		}
	}
	return s.eachRemove(ctx, project, func(k string, rm cloud.QueueRemove) error {
		if rm.By != name {
			return nil
		}
		rm.By = FormerMember
		return s.putJSON(ctx, k, rm)
	})
}

// eachRemove calls f for each removal of the queues of the printers of the project.
func (s *Server) eachRemove(ctx context.Context, project string, f func(key string, rm cloud.QueueRemove) error) error {
	printers, err := s.printers(ctx)
	if err != nil {
		return err
	}
	for _, pr := range printers {
		keys, err := s.Store.List(ctx, removeDir(pr.ID))
		if err != nil {
			return err
		}
		for _, k := range keys {
			b, _, err := s.Store.Get(ctx, k)
			if err != nil {
				continue
			}
			var rm cloud.QueueRemove
			if json.Unmarshal(b, &rm) != nil || rm.Project != project {
				continue
			}
			if err := f(k, rm); err != nil {
				return err
			}
		}
	}
	return nil
}

// blankAuthor removes the GitHub username login from the plans of the project.
func (s *Server) blankAuthor(ctx context.Context, project, login string) error {
	plans, err := s.loadPlans(ctx, project)
	if err != nil {
		return err
	}
	for _, rec := range plans {
		if rec.Author == "" || !strings.EqualFold(rec.Author, login) {
			continue
		}
		rec.Author = ""
		if err := s.putJSON(ctx, planKey(project, rec.ID), rec); err != nil {
			return err
		}
	}
	return nil
}

// deleteMe serves DELETE /api/v1/me for a member: the service forgets the member in each project
// of the session and of its passkeys, and deletes the passkeys.
func (s *Server) deleteMe(ctx context.Context, ss *Session) error {
	if ss.Kind != KindMember {
		return fail(http.StatusBadRequest, "the owner deletes each project, and each passkey but the last")
	}
	todo := ss.projects()
	files := map[string]bool{}
	// The passkeys of the member can have joined more projects after the sign-in.
	for project, mid := range ss.projects() {
		p, _, err := s.loadProject(ctx, project)
		if err != nil {
			continue
		}
		if m := p.member(mid); m != nil {
			for _, f := range m.Keys {
				files[f] = true
			}
		}
	}
	for f := range files {
		b, _, err := s.Store.Get(ctx, f)
		if err != nil {
			continue
		}
		var k Passkey
		if json.Unmarshal(b, &k) != nil {
			continue
		}
		k.normalize()
		for _, ms := range k.Projects {
			todo[ms.Project] = ms.Member
		}
	}
	for project, mid := range todo {
		err := s.forgetMember(ctx, project, mid)
		if err != nil && !isGone(err) {
			return err
		}
		if p, _, err := s.loadProject(ctx, project); err == nil && p.Public != "" {
			if err := s.publish(ctx, p); err != nil {
				return err
			}
		}
	}
	for f := range files {
		if strings.HasPrefix(f, "members/") {
			if err := s.Store.Delete(ctx, f); err != nil {
				return err
			}
		}
	}
	return nil
}

// routeMyPasskeys serves /api/v1/me/passkeys and /api/v1/me/passkeys/<id> for the owner.
func (s *Server) routeMyPasskeys(ctx context.Context, r *http.Request, ss *Session, id string) (any, error) {
	if ss.Kind != KindOwner {
		return nil, fail(http.StatusUnauthorized, "only the owner can do this")
	}
	keys, err := s.Store.List(ctx, "owners/")
	if err != nil {
		return nil, err
	}
	switch {
	case id == "" && r.Method == http.MethodGet:
		out := []cloud.PasskeyInfo{}
		for _, k := range keys {
			b, _, err := s.Store.Get(ctx, k)
			if err != nil {
				continue
			}
			var p Passkey
			if json.Unmarshal(b, &p) != nil {
				continue
			}
			kid := strings.TrimSuffix(strings.TrimPrefix(k, "owners/"), ".json")
			out = append(out, cloud.PasskeyInfo{ID: kid, Device: p.Device, Added: p.Added, This: k == ss.Key})
		}
		slices.SortFunc(out, func(a, b cloud.PasskeyInfo) int { return a.Added.Compare(b.Added) })
		return out, nil
	case id != "" && r.Method == http.MethodDelete:
		if _, err := base64.RawURLEncoding.DecodeString(id); err != nil {
			return nil, fail(http.StatusGone, "no such passkey")
		}
		file := "owners/" + id + ".json"
		if !slices.Contains(keys, file) {
			return nil, fail(http.StatusGone, "no such passkey")
		}
		if len(keys) < 2 {
			return nil, fail(http.StatusConflict, "this is the last passkey of the owner: the service needs one")
		}
		return nil, s.Store.Delete(ctx, file)
	}
	return nil, fail(http.StatusMethodNotAllowed, "no such request")
}

// deleteProjectExtras deletes the files of the project p outside x/<id>/ that deleteProject
// does not delete itself: the public pictures of the comments on the pull requests, and the
// removals of the queues of the printers. Call it before the delete of x/<id>/: the records
// there name the pictures.
func (s *Server) deleteProjectExtras(ctx context.Context, p *Project) error {
	var pics []string
	for _, dir := range []string{"prs/", "plans/", "builds/"} {
		keys, _, err := s.Store.ListDir(ctx, "x/"+p.ID+"/"+dir)
		if err != nil {
			return err
		}
		for _, b := range s.getAll(ctx, keys) {
			pics = append(pics, ghFile.FindAllString(string(b), -1)...)
		}
	}
	slices.Sort(pics)
	pics = slices.Compact(pics)
	if err := forAll(len(pics), func(i int) error { return s.Store.Delete(ctx, pics[i]) }); err != nil {
		return err
	}
	return s.eachRemove(ctx, p.ID, func(k string, _ cloud.QueueRemove) error { return s.Store.Delete(ctx, k) })
}

// ownerKeyLive reports that the passkey file of an owner session still exists. A session from
// before the passkey files in the sessions has no file, and works until it ends.
func (s *Server) ownerKeyLive(ctx context.Context, ss *Session) bool {
	if ss.Kind != KindOwner || ss.Key == "" {
		return true
	}
	_, _, err := s.Store.Get(ctx, ss.Key)
	return err == nil
}
