package api

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"sort"
	"strings"

	"printdeck/internal/cloud"
)

// The members of the other projects of the owner. The owner adds one of them to a project with
// no invite: the member gets a new member ID in the project, with the same passkeys. The passkey
// files get the project, and the next request of the member sees it (Session.Key).

// person is a member of a project, with the passkeys of the member.
type person struct {
	project, member, name string
	keys                  []string
	joined                int64
	projects              []string
}

// personKey is the key of a person: the passkey files, in order. A person who joined two
// projects with the same passkey is one person.
func personKey(keys []string) string {
	k := slices.Clone(keys)
	sort.Strings(k)
	return strings.Join(k, ",")
}

// memberKeys returns the passkey files of the member m that are passkeys of a member.
func memberKeys(m Member) []string {
	var out []string
	for _, k := range m.Keys {
		if strings.HasPrefix(k, "members/") {
			out = append(out, k)
		}
	}
	return out
}

// knownPeople serves GET /api/v1/projects/<id>/people: the members of the other projects of the
// owner who are not in the project p.
func (s *Server) knownPeople(ctx context.Context, p *Project) (any, error) {
	in := map[string]bool{} // the passkeys of the members and the invites of p
	for _, m := range p.Members {
		for _, k := range m.Keys {
			in[k] = true
		}
	}
	_, dirs, err := s.Store.ListDir(ctx, "x/")
	if err != nil {
		return nil, err
	}
	projects := make([]*Project, len(dirs))
	if err := forAll(len(dirs), func(i int) error {
		id := strings.TrimSuffix(strings.TrimPrefix(dirs[i], "x/"), "/")
		if id == p.ID {
			return nil
		}
		if q, _, err := s.loadProject(ctx, id); err == nil {
			projects[i] = q
		}
		return nil // not a project, or gone
	}); err != nil {
		return nil, err
	}
	people := map[string]*person{}
	for _, q := range projects {
		if q == nil {
			continue
		}
		for _, m := range q.Members {
			keys := memberKeys(m)
			if m.Joined.IsZero() || len(keys) == 0 || slices.ContainsFunc(keys, func(k string) bool { return in[k] }) {
				continue
			}
			key := personKey(keys)
			pp := people[key]
			if pp == nil {
				pp = &person{keys: keys}
				people[key] = pp
			}
			pp.projects = append(pp.projects, q.Name)
			// The name and the member of the newest join.
			if j := m.Joined.Unix(); j >= pp.joined {
				pp.project, pp.member, pp.name, pp.joined = q.ID, m.ID, m.Name, j
			}
		}
	}
	out := []cloud.KnownPerson{}
	for _, pp := range people {
		sort.Strings(pp.projects)
		out = append(out, cloud.KnownPerson{Project: pp.project, Member: pp.member, Name: pp.name, Projects: pp.projects})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Member < out[j].Member
	})
	return out, nil
}

// addKnown adds the member in.Member of the project in.Project to the project id, with no
// invite. The passkey files get the project first: a passkey with a project and no member in
// the project has no access, so a stop between the two writes is safe, and a new try uses the
// same member ID.
func (s *Server) addKnown(ctx context.Context, id string, in cloud.MemberAdd) error {
	if in.Project == id {
		return fail(http.StatusBadRequest, "the person is a member of this project already")
	}
	src, _, err := s.loadProject(ctx, in.Project)
	if err != nil {
		return err
	}
	m := src.member(in.Member)
	if m == nil || len(memberKeys(*m)) == 0 {
		return fail(http.StatusGone, "no such member")
	}
	keys := memberKeys(*m)
	dst, _, err := s.loadProject(ctx, id)
	if err != nil {
		return err
	}
	for _, dm := range dst.Members {
		if slices.ContainsFunc(dm.Keys, func(k string) bool { return slices.Contains(keys, k) }) {
			return fail(http.StatusConflict, m.Name+" is a member of this project already")
		}
	}
	if len(dst.Members) >= MaxMembers {
		return fail(http.StatusBadRequest, "the project has the most members")
	}
	// The member ID: the ID that a passkey has for the project from an earlier try, or a new ID.
	type keyFile struct {
		file string
		k    Passkey
	}
	var files []keyFile
	mid := ""
	for _, f := range keys {
		b, _, err := s.Store.Get(ctx, f)
		if err != nil {
			continue // a passkey that the member removed
		}
		var k Passkey
		if json.Unmarshal(b, &k) != nil {
			continue
		}
		k.normalize()
		for _, ms := range k.Projects {
			if ms.Project == id {
				mid = ms.Member
			}
		}
		files = append(files, keyFile{f, k})
	}
	if len(files) == 0 {
		return fail(http.StatusGone, "the passkeys of "+m.Name+" are gone")
	}
	if mid == "" {
		mid = newID(8)
	}
	var live []string
	for _, kf := range files {
		if !kf.k.hasProject(id) {
			kf.k.Projects = append(kf.k.Projects, Membership{id, mid})
			if err := s.putJSON(ctx, kf.file, kf.k); err != nil {
				return err
			}
		}
		live = append(live, kf.file)
	}
	now := s.now().UTC()
	_, err = s.changeProject(ctx, id, func(p *Project) error {
		if len(p.Members) >= MaxMembers {
			return fail(http.StatusBadRequest, "the project has the most members")
		}
		p.Members = append(p.Members, Member{ID: mid, Name: m.Name, Invited: now, Joined: now, Keys: live, GitHub: m.GitHub})
		return nil
	})
	return err
}
