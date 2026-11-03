package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path"
	"strings"
	"time"

)

// The limits of the uploads of the Action. A grant lets the Action upload files of up to 200 MB
// each for an hour, with any names under the folder of the plan or the build. These limits keep a
// repository, or the fork of a member, from filling the bucket:
//   - A plan or a build keeps only the files that its record names. The other files go when
//     the Action says that it is done, and the files must fit in a budget.
//   - A project takes at most PlansPerDay plans in a day, and the fork of each member at most
//     ForkPlansPerDay.
//   - The files of a plan that is not ready after PlanStale go at the next plan of the project.
//   - The pictures of the comments on pull requests read files of at most MaxPictureFile.
const (
	MaxPlanBytes    = 1 << 30
	MaxBuildBytes   = 1 << 30
	PlansPerDay     = 100
	ForkPlansPerDay = 20
	PlanStale       = 2 * time.Hour
	MaxPictureFile  = 50 << 20
)

// keepOnly deletes the files in dir that want does not name. When the files of want are larger
// than budget, it deletes all the files in dir and fails.
func (s *Server) keepOnly(ctx context.Context, dir string, want map[string]bool, budget int64) error {
	sizes, err := s.Store.Sizes(ctx, dir)
	if err != nil {
		return err
	}
	var total int64
	var extra []string
	for k, n := range sizes {
		if want[path.Base(k)] && path.Dir(k)+"/" == dir {
			total += n
		} else {
			extra = append(extra, k)
		}
	}
	if total > budget {
		for k := range sizes {
			extra = append(extra, k)
		}
	}
	if err := forAll(len(extra), func(i int) error { return s.Store.Delete(ctx, extra[i]) }); err != nil {
		return err
	}
	if total > budget {
		return fail(http.StatusRequestEntityTooLarge, fmt.Sprintf("the files are %d MB, more than %d MB", total>>20, budget>>20))
	}
	return nil
}

// planFiles are the names of the files of the plan rec.
func planFiles(rec *PlanRecord) map[string]bool {
	want := map[string]bool{}
	for _, pp := range rec.Parts {
		for _, m := range pp.Models {
			want[m] = true
		}
		if pp.SliceFile != "" {
			want[pp.SliceFile] = true
		}
	}
	for _, a := range rec.Assemblies {
		for _, it := range a.Items {
			want[it.Model] = true
		}
	}
	for _, d := range rec.Docs {
		want[d.PDF] = true
		if d.Thumb != "" {
			want[d.Thumb] = true
		}
	}
	if rec.Cover != "" {
		want[rec.Cover] = true
	}
	return want
}

// buildFileNames are the names of the files of the plates of the build b.
func buildFileNames(b *BuildRecord) map[string]bool {
	want := map[string]bool{}
	for _, pl := range b.Plates {
		for _, f := range []string{pl.File, pl.Preview, pl.Picture} {
			if f != "" {
				want[f] = true
			}
		}
	}
	return want
}

// planDay is x/<project>/quota/<date>.json: the count of the plans of the project on a day (UTC),
// and the plans that are maybe not ready yet.
type planDay struct {
	Repo    int            `json:"repo"`
	Forks   map[string]int `json:"forks,omitempty"`
	Pending []pendingPlan  `json:"pending,omitempty"`
}

type pendingPlan struct {
	ID      string    `json:"id"`
	Created time.Time `json:"created"`
}

func planDayKey(project string, t time.Time) string {
	return "x/" + project + "/quota/" + t.UTC().Format("2006-01-02") + ".json"
}

// countPlan counts the new plan id of the project p against the limits of the day, and keeps it
// as pending. author is the member of a fork, or "" for the repository of the project.
func (s *Server) countPlan(ctx context.Context, p *Project, id, author string) error {
	now := s.now().UTC()
	key := planDayKey(p.ID, now)
	for range 5 {
		var d planDay
		data, etag, err := s.Store.Get(ctx, key)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if err == nil {
			json.Unmarshal(data, &d)
		}
		if author == "" {
			if d.Repo >= PlansPerDay {
				return fail(http.StatusTooManyRequests, fmt.Sprintf("the project has %d plans today, the most in a day", d.Repo))
			}
			d.Repo++
		} else {
			author = strings.ToLower(author)
			if d.Forks[author] >= ForkPlansPerDay {
				return fail(http.StatusTooManyRequests, fmt.Sprintf("the fork of %s has %d plans today, the most in a day", author, ForkPlansPerDay))
			}
			if d.Forks == nil {
				d.Forks = map[string]int{}
			}
			d.Forks[author]++
		}
		d.Pending = append(d.Pending, pendingPlan{ID: id, Created: now})
		out, _ := json.Marshal(d)
		opt := PutOptions{ContentType: "application/json", IfMatch: etag}
		if etag == "" {
			opt = PutOptions{ContentType: "application/json", IfNoneMatch: true}
		}
		_, err = s.Store.Put(ctx, key, out, opt)
		if errors.Is(err, ErrConflict) {
			continue
		}
		return err
	}
	return errors.New("the count of the plans changed too often: try again")
}

// sweepPlans deletes the files of the plans of the project p that are not ready after PlanStale,
// from the pending plans of today and yesterday. A plan that is ready leaves the list.
func (s *Server) sweepPlans(ctx context.Context, p *Project) {
	now := s.now().UTC()
	for _, day := range []time.Time{now.Add(-24 * time.Hour), now} {
		key := planDayKey(p.ID, day)
		data, etag, err := s.Store.Get(ctx, key)
		if err != nil {
			continue
		}
		var d planDay
		if json.Unmarshal(data, &d) != nil {
			continue
		}
		var left []pendingPlan
		changed := false
		for _, pp := range d.Pending {
			if now.Sub(pp.Created) < PlanStale {
				left = append(left, pp)
				continue
			}
			changed = true
			rec, err := s.loadPlan(ctx, p.ID, pp.ID)
			if err == nil && rec.Ready {
				continue
			}
			keys, err := s.Store.List(ctx, planDir(p.ID, pp.ID))
			if err == nil {
				err = forAll(len(keys), func(i int) error { return s.Store.Delete(ctx, keys[i]) })
			}
			if err != nil {
				log.Printf("api: the files of the plan %s that is not ready: %v", pp.ID, err)
				left = append(left, pp)
			}
		}
		if !changed {
			continue
		}
		d.Pending = left
		out, _ := json.Marshal(d)
		// A conflict leaves the list for the next plan.
		s.Store.Put(ctx, key, out, PutOptions{ContentType: "application/json", IfMatch: etag})
	}
}

// smallFile is true when the file at key has at most MaxPictureFile bytes.
func (s *Server) smallFile(ctx context.Context, key string) bool {
	sizes, err := s.Store.Sizes(ctx, key)
	if err != nil {
		return false
	}
	n, ok := sizes[key]
	return ok && n <= MaxPictureFile
}

