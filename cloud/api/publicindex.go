package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sort"
	"strings"

	"printdeck/internal/cloud"
)

// PublicIndex is the key of the list of the public projects. The home page reads it with no
// sign-in, so each visitor sees the public projects. It has only the fields of the public pages.
const PublicIndex = "p/index.json"

// writeIndex writes the list of the public projects from the public pages p/<slug>/page.json.
// Each change of a public page writes the list again. An error goes to the log: the public page
// is correct without the list.
func (s *Server) writeIndex(ctx context.Context) {
	if err := s.indexPublic(ctx); err != nil {
		log.Printf("the list of the public projects: %v", err)
	}
}

func (s *Server) indexPublic(ctx context.Context) error {
	_, dirs, err := s.Store.ListDir(ctx, "p/")
	if err != nil {
		return err
	}
	var keys, slugs []string
	for _, d := range dirs {
		slug := strings.TrimSuffix(strings.TrimPrefix(d, "p/"), "/")
		if validSlug(slug) {
			keys, slugs = append(keys, d+"page.json"), append(slugs, slug)
		}
	}
	out := []cloud.PublicProject{}
	for i, b := range s.getAll(ctx, keys) {
		var v cloud.ProjectView
		if b == nil || json.Unmarshal(b, &v) != nil {
			continue
		}
		out = append(out, publicEntry(slugs[i], v))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Changed.Equal(out[j].Changed) {
			return out[i].Changed.After(out[j].Changed)
		}
		return out[i].Name < out[j].Name
	})
	if len(out) == 0 {
		// No public projects: no list, so the store keeps no file of a deleted project.
		if err := s.Store.Delete(ctx, PublicIndex); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		return nil
	}
	data, _ := json.Marshal(out)
	_, err = s.Store.Put(ctx, PublicIndex, data, PutOptions{ContentType: "application/json", CacheControl: "public, max-age=60"})
	return err
}

// publicEntry is the entry of the public page v at the slug in the list. Changed is the newest
// change of the page: the owner's content, or the newest plan of the repository.
func publicEntry(slug string, v cloud.ProjectView) cloud.PublicProject {
	e := cloud.PublicProject{Slug: slug, Name: v.Name, Note: v.Note, Cover: v.Cover, Prints: len(v.Prints),
		Changed: v.Changed, RepoOnly: v.RepoOnly}
	for _, pl := range v.Plans {
		if len(pl.Parts) > e.Parts {
			e.Parts = len(pl.Parts)
		}
		for _, pp := range pl.Parts {
			e.Bed.Add(pp.PlanPart)
		}
		if pl.Created.After(e.Changed) {
			e.Changed = pl.Created
		}
	}
	return e
}
