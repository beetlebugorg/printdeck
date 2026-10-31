package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"printdeck/internal/cloud"
)

// The plans and the builds of a project (docs/specs/github-builds.md).
//
//   - The plan run of the Action sends POST /api/v1/plans with the parts of printdeck.yml, and
//     uploads the models with the grant of the reply: x/<project>/plans/<id>/. Then it sends
//     POST /api/v1/plans/<id>/done. x/<project>/plans/<id>.json is the plan.
//   - On the plan page, the owner chooses the parts and the plates. POST
//     /api/v1/projects/<id>/plans/<plan>/builds makes a build, x/<project>/builds/<id>.json, and
//     starts the slice run of the repository (Dispatcher).
//   - The slice run gets the plates with GET /api/v1/builds/<id>, slices them, sends the facts
//     of the files with POST /api/v1/builds/<id>/files, uploads the files to
//     x/<project>/builds/<id>/, and sends POST /api/v1/builds/<id>/done (or .../failed).
//   - The owner approves or rejects each plate of the build.

// Dispatcher starts the slice run of a build in the repository (workflow_dispatch of the GitHub
// App).
type Dispatcher interface {
	Dispatch(ctx context.Context, repo, build string) error
}

// PlanRecord is x/<project>/plans/<id>.json.
type PlanRecord struct {
	ID      string           `json:"id"`
	Repo    string           `json:"repo"`
	Commit  string           `json:"commit"`
	Ref     string           `json:"ref"`
	Kind    string           `json:"kind"`
	PR      int              `json:"pr,omitempty"`
	Tag     string           `json:"tag,omitempty"`
	Branch  string           `json:"branch,omitempty"`
	RunID   string           `json:"run_id,omitempty"`
	Created time.Time        `json:"created"`
	Ready   bool             `json:"ready"`
	Parts   []cloud.PlanPart `json:"parts"`
}

// BuildRecord is x/<project>/builds/<id>.json.
type BuildRecord struct {
	ID      string        `json:"id"`
	Plan    string        `json:"plan"`
	Repo    string        `json:"repo"`
	Commit  string        `json:"commit"`
	Created time.Time     `json:"created"`
	State   string        `json:"state"`
	Error   string        `json:"error,omitempty"`
	RunID   string        `json:"run_id,omitempty"` // the slice run on GitHub
	Plates  []plateRecord `json:"plates"`
}

// plateRecord is a plate of a build record, with its state.
type plateRecord struct {
	cloud.BuildPlate
	State  string    `json:"state,omitempty"`
	By     string    `json:"by,omitempty"`
	At     time.Time `json:"at,omitzero"`
	Reason string    `json:"reason,omitempty"`
}

func planKey(project, id string) string  { return "x/" + project + "/plans/" + id + ".json" }
func planDir(project, id string) string  { return "x/" + project + "/plans/" + id + "/" }
func buildKey(project, id string) string { return "x/" + project + "/builds/" + id + ".json" }
func buildDir(project, id string) string { return "x/" + project + "/builds/" + id + "/" }

// routeActions serves the requests of the Action: /api/v1/plans... and /api/v1/builds/....
func (s *Server) routeActions(ctx context.Context, r *http.Request, body []byte, kind, rest string) (any, error) {
	c, err := s.buildClaims(ctx, r)
	if err != nil {
		return nil, err
	}
	p, err := s.projectOfRepo(ctx, c.Repository)
	if err != nil {
		return nil, err
	}
	id, action, _ := strings.Cut(rest, "/")
	if kind == "builds" {
		if err := sliceRun(c); err != nil {
			return nil, err
		}
	}
	switch {
	case kind == "plans" && id == "" && r.Method == http.MethodPost:
		var in cloud.PlanManifest
		if json.Unmarshal(body, &in) != nil {
			return nil, fail(http.StatusBadRequest, "bad JSON")
		}
		return s.newPlan(ctx, p, c, in)
	case kind == "plans" && action == "done" && r.Method == http.MethodPost:
		return s.planDone(ctx, p, c, id)
	case kind == "builds" && action == "" && r.Method == http.MethodGet:
		b, err := s.actionBuild(ctx, p, c, id)
		if err != nil {
			return nil, err
		}
		job := cloud.BuildJob{ID: b.ID, Commit: b.Commit}
		for _, pl := range b.Plates {
			job.Plates = append(job.Plates, pl.BuildPlate)
		}
		return job, nil
	case kind == "builds" && action == "started" && r.Method == http.MethodPost:
		var in cloud.BuildStarted
		json.Unmarshal(body, &in)
		b, err := s.actionBuild(ctx, p, c, id)
		if err != nil {
			return nil, err
		}
		if run := strings.TrimSpace(in.RunID); run != "" && len(run) <= 30 && strings.Trim(run, "0123456789") == "" {
			b.RunID = run
		}
		if b.State == cloud.BuildFailed {
			b.State, b.Error = cloud.BuildSlicing, "" // a run again of a build that failed
		}
		if err := s.putJSON(ctx, buildKey(p.ID, b.ID), b); err != nil {
			return nil, err
		}
		return s.projectBuild(ctx, p, b), nil
	case kind == "builds" && action == "files" && r.Method == http.MethodPost:
		var in cloud.BuildFiles
		if json.Unmarshal(body, &in) != nil {
			return nil, fail(http.StatusBadRequest, "bad JSON")
		}
		return s.buildFiles(ctx, p, c, id, in)
	case kind == "builds" && action == "done" && r.Method == http.MethodPost:
		return s.buildDone(ctx, p, c, id)
	case kind == "builds" && action == "failed" && r.Method == http.MethodPost:
		var in cloud.BuildFailure
		json.Unmarshal(body, &in)
		b, err := s.actionBuild(ctx, p, c, id)
		if err != nil {
			return nil, err
		}
		b.State, b.Error = cloud.BuildFailed, clip(strings.TrimSpace(in.Error), 2000)
		if b.Error == "" {
			b.Error = "the slice run failed"
		}
		if err := s.putJSON(ctx, buildKey(p.ID, b.ID), b); err != nil {
			return nil, err
		}
		return s.projectBuild(ctx, p, b), nil
	}
	return nil, fail(http.StatusMethodNotAllowed, "no such request")
}

// refKind sets the kind of the plan from the ref of the token.
func refKind(rec *PlanRecord, ref string) error {
	switch {
	case strings.HasPrefix(ref, "refs/tags/"):
		rec.Kind, rec.Tag = cloud.BuildRelease, clip(strings.TrimPrefix(ref, "refs/tags/"), 100)
	case pullRef.MatchString(ref):
		rec.Kind = cloud.BuildTest
		rec.PR, _ = strconv.Atoi(pullRef.FindStringSubmatch(ref)[1])
	case strings.HasPrefix(ref, "refs/heads/"):
		rec.Branch = clip(strings.TrimPrefix(ref, "refs/heads/"), 100)
		rec.Kind = cloud.BuildBranch
		if rec.Branch == "main" || rec.Branch == "master" {
			rec.Kind = cloud.BuildMain
		}
	default:
		return fail(http.StatusBadRequest, "a plan needs a branch, a tag or a pull request")
	}
	return nil
}

// newPlan makes the record of a plan of the project p, and returns the grant for its models.
func (s *Server) newPlan(ctx context.Context, p *Project, c *GitHubClaims, in cloud.PlanManifest) (any, error) {
	if len(in.Parts) == 0 || len(in.Parts) > MaxPlanParts {
		return nil, fail(http.StatusBadRequest, fmt.Sprintf("a plan has 1 to %d parts", MaxPlanParts))
	}
	rec := PlanRecord{ID: newID(10), Repo: c.Repository, Commit: c.SHA, Ref: c.Ref, RunID: clip(c.RunID, 30), Created: s.now().UTC()}
	if in.Commit != "" {
		rec.Commit = in.Commit
	}
	if !hexSHA.MatchString(rec.Commit) {
		return nil, fail(http.StatusBadRequest, "the commit needs 40 hex digits")
	}
	if err := refKind(&rec, c.Ref); err != nil {
		return nil, err
	}
	if in.PR > 0 && rec.Kind == cloud.BuildTest && in.PR != rec.PR {
		return nil, fail(http.StatusBadRequest, "the pull request is not the pull request of the token")
	}
	names := map[string]bool{}
	for _, pp := range in.Parts {
		pp.Name, pp.Printer, pp.Material, pp.Profile, pp.Prebuilt =
			clip(strings.TrimSpace(pp.Name), 120), clip(pp.Printer, 40), clip(pp.Material, 80), clip(pp.Profile, 200), clip(pp.Prebuilt, 300)
		if pp.Name == "" || pp.Printer == "" || names[strings.ToLower(pp.Name)] {
			return nil, fail(http.StatusBadRequest, "each part needs a printer and a name of its own")
		}
		names[strings.ToLower(pp.Name)] = true
		if pp.Process != "fdm" && pp.Process != "resin" {
			return nil, fail(http.StatusBadRequest, "the process of a part is fdm or resin")
		}
		if len(pp.Files) > 50 || len(pp.Models) > 50 {
			return nil, fail(http.StatusBadRequest, "a part has at most 50 files")
		}
		for i, f := range pp.Files {
			pp.Files[i] = clip(f, 300)
		}
		for _, f := range pp.Models {
			if !fileName.MatchString(f) {
				return nil, fail(http.StatusBadRequest, "a bad model name: "+f)
			}
		}
		pp.Count = max(1, min(pp.Count, MaxPartCount))
		rec.Parts = append(rec.Parts, pp)
	}
	g, err := s.Grants.Grant(ctx, planDir(p.ID, rec.ID), MaxBuildFile, buildGrantTTL)
	if err != nil {
		return nil, err
	}
	if err := s.putJSON(ctx, planKey(p.ID, rec.ID), rec); err != nil {
		return nil, err
	}
	return cloud.UploadReply{ID: rec.ID, URL: s.projectURL(p) + "/plans/" + rec.ID, Grant: g}, nil
}

// planDone checks the models of the plan id and makes it ready. The plates that wait in the
// builds of older plans of the same pull request or branch get the state replaced.
func (s *Server) planDone(ctx context.Context, p *Project, c *GitHubClaims, id string) (any, error) {
	rec, err := s.loadPlan(ctx, p.ID, id)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(rec.Repo, c.Repository) {
		return nil, fail(http.StatusForbidden, "the plan is of another repository")
	}
	have, err := s.filesIn(ctx, planDir(p.ID, id))
	if err != nil {
		return nil, err
	}
	for _, pp := range rec.Parts {
		for _, m := range pp.Models {
			if !have[m] {
				return nil, fail(http.StatusBadRequest, "the model "+m+" is not up")
			}
		}
	}
	rec.Ready = true
	if err := s.putJSON(ctx, planKey(p.ID, id), rec); err != nil {
		return nil, err
	}
	if rec.Kind == cloud.BuildTest || rec.Kind == cloud.BuildBranch || rec.Kind == cloud.BuildMain {
		if err := s.replaceOlder(ctx, p, rec); err != nil {
			return nil, err
		}
	}
	return s.projectPlan(ctx, p, rec), nil
}

// replaceOlder marks the waiting plates of the builds of the older plans of the same pull
// request or branch as replaced.
func (s *Server) replaceOlder(ctx context.Context, p *Project, rec *PlanRecord) error {
	plans, err := s.loadPlans(ctx, p.ID)
	if err != nil {
		return err
	}
	older := map[string]bool{}
	for _, o := range plans {
		if o.ID != rec.ID && o.Kind == rec.Kind && o.PR == rec.PR && o.Branch == rec.Branch && o.Created.Before(rec.Created) {
			older[o.ID] = true
		}
	}
	builds, err := s.loadBuilds(ctx, p.ID)
	if err != nil {
		return err
	}
	for _, b := range builds {
		if !older[b.Plan] {
			continue
		}
		changed := false
		for i := range b.Plates {
			if b.Plates[i].State == cloud.PartWaiting {
				b.Plates[i].State, changed = cloud.PartReplaced, true
			}
		}
		if changed {
			if err := s.putJSON(ctx, buildKey(p.ID, b.ID), b); err != nil {
				return err
			}
		}
	}
	return nil
}

// filesIn returns the names of the files of a folder.
func (s *Server) filesIn(ctx context.Context, dir string) (map[string]bool, error) {
	keys, err := s.Store.List(ctx, dir)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, k := range keys {
		have[path.Base(k)] = true
	}
	return have, nil
}

// newBuild makes a build of the plan with the plates of the owner, and starts the slice run.
func (s *Server) newBuild(ctx context.Context, p *Project, w *who, planID string, in cloud.BuildRequest) (any, error) {
	if err := ownerOnly(w); err != nil {
		return nil, err
	}
	plan, err := s.loadPlan(ctx, p.ID, planID)
	if err != nil {
		return nil, err
	}
	if !plan.Ready {
		return nil, fail(http.StatusConflict, "the models of the plan are not all up yet")
	}
	if len(in.Plates) == 0 || len(in.Plates) > MaxPlates {
		return nil, fail(http.StatusBadRequest, fmt.Sprintf("a build has 1 to %d plates", MaxPlates))
	}
	b := BuildRecord{ID: newID(10), Plan: plan.ID, Repo: plan.Repo, Commit: plan.Commit, Created: s.now().UTC(), State: cloud.BuildSlicing}
	for n, pr := range in.Plates {
		if len(pr.Parts) == 0 || len(pr.Parts) > MaxPlanParts {
			return nil, fail(http.StatusBadRequest, fmt.Sprintf("plate %d has no parts", n+1))
		}
		var plate cloud.BuildPlate
		for i, pp := range pr.Parts {
			if pp.Part < 0 || pp.Part >= len(plan.Parts) {
				return nil, fail(http.StatusBadRequest, "no such part")
			}
			part := plan.Parts[pp.Part]
			if i == 0 {
				plate = cloud.BuildPlate{Printer: part.Printer, Process: part.Process, Profile: part.Profile, Material: part.Material}
			} else if part.Printer != plate.Printer || part.Profile != plate.Profile || part.Material != plate.Material {
				return nil, fail(http.StatusBadRequest, fmt.Sprintf("plate %d: %s needs another printer, profile or material", n+1, part.Name))
			}
			if (part.Alone || part.Prebuilt != "") && len(pr.Parts) > 1 {
				return nil, fail(http.StatusBadRequest, fmt.Sprintf("plate %d: %s prints alone", n+1, part.Name))
			}
			count := max(1, min(pp.Count, MaxPartCount))
			if part.Prebuilt != "" {
				count = 1
			}
			plate.Parts = append(plate.Parts, cloud.PlatePart{Part: pp.Part, Count: count, Name: part.Name})
		}
		b.Plates = append(b.Plates, plateRecord{BuildPlate: plate})
	}
	if err := s.putJSON(ctx, buildKey(p.ID, b.ID), b); err != nil {
		return nil, err
	}
	if s.GitHub != nil {
		if err := s.GitHub.Dispatch(ctx, plan.Repo, b.ID); err != nil {
			b.State, b.Error = cloud.BuildFailed, "GitHub did not start the slice run: "+err.Error()
			if err := s.putJSON(ctx, buildKey(p.ID, b.ID), b); err != nil {
				return nil, err
			}
		}
	}
	return s.projectBuild(ctx, p, &b), nil
}

// actionBuild returns the build id for the slice run of the token: a build of the repository of
// the token. The commit of a slice run is the commit of the default branch, so the check of the
// workflow (sliceRun) guards the run, and the run checks out the commit of the plan.
func (s *Server) actionBuild(ctx context.Context, p *Project, c *GitHubClaims, id string) (*BuildRecord, error) {
	b, err := s.loadBuild(ctx, p.ID, id)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(b.Repo, c.Repository) {
		return nil, fail(http.StatusForbidden, "the build is of another repository")
	}
	return b, nil
}

// buildFiles records the files of the plates of the slice run, and returns the grant for them.
func (s *Server) buildFiles(ctx context.Context, p *Project, c *GitHubClaims, id string, in cloud.BuildFiles) (any, error) {
	b, err := s.actionBuild(ctx, p, c, id)
	if err != nil {
		return nil, err
	}
	if b.State != cloud.BuildSlicing {
		return nil, fail(http.StatusConflict, "the build is not waiting for its files")
	}
	if len(in.Plates) != len(b.Plates) {
		return nil, fail(http.StatusBadRequest, "the files need one entry for each plate")
	}
	seen := map[string]bool{}
	for i, f := range in.Plates {
		for _, n := range []string{f.File, f.Preview, f.Picture} {
			if n == "" {
				continue
			}
			if !fileName.MatchString(n) || seen[n] {
				return nil, fail(http.StatusBadRequest, "a bad or second file name: "+n)
			}
			seen[n] = true
		}
		if f.File == "" || f.Size <= 0 || f.Size > MaxBuildFile || !sha256Re.MatchString(f.SHA256) {
			return nil, fail(http.StatusBadRequest, "each plate needs its file, with its size (up to 200 MB) and its SHA-256")
		}
		pl := &b.Plates[i]
		pl.File, pl.Size, pl.SHA256, pl.Preview, pl.Picture = f.File, f.Size, f.SHA256, f.Preview, f.Picture
		pl.Seconds, pl.Grams, pl.ML, pl.Layers, pl.LayerHeight, pl.Slicer = f.Seconds, f.Grams, f.ML, f.Layers, f.LayerHeight, clip(f.Slicer, 80)
		if f.Profile != "" && strings.HasPrefix(f.Profile, strings.SplitN(pl.Profile, "@", 2)[0]) {
			pl.Profile = clip(f.Profile, 200) // the profile with its version
		}
	}
	g, err := s.Grants.Grant(ctx, buildDir(p.ID, b.ID), MaxBuildFile, buildGrantTTL)
	if err != nil {
		return nil, err
	}
	if err := s.putJSON(ctx, buildKey(p.ID, b.ID), b); err != nil {
		return nil, err
	}
	return cloud.UploadReply{ID: b.ID, URL: s.projectURL(p) + "/builds/" + b.ID, Grant: g}, nil
}

// buildDone checks the files of the build, and makes it ready: each plate waits for the owner.
func (s *Server) buildDone(ctx context.Context, p *Project, c *GitHubClaims, id string) (any, error) {
	b, err := s.actionBuild(ctx, p, c, id)
	if err != nil {
		return nil, err
	}
	have, err := s.filesIn(ctx, buildDir(p.ID, id))
	if err != nil {
		return nil, err
	}
	for _, pl := range b.Plates {
		if pl.File == "" {
			return nil, fail(http.StatusBadRequest, "send the files of the plates first")
		}
		for _, f := range []string{pl.File, pl.Preview, pl.Picture} {
			if f != "" && !have[f] {
				return nil, fail(http.StatusBadRequest, "the file "+f+" is not up")
			}
		}
	}
	b.State, b.Error = cloud.BuildReady, ""
	for i := range b.Plates {
		if b.Plates[i].State == "" {
			b.Plates[i].State = cloud.PartWaiting
		}
	}
	if err := s.putJSON(ctx, buildKey(p.ID, id), b); err != nil {
		return nil, err
	}
	return s.projectBuild(ctx, p, b), nil
}

func (s *Server) loadPlan(ctx context.Context, project, id string) (*PlanRecord, error) {
	var rec PlanRecord
	if err := s.loadRecord(ctx, planKey(project, id), id, "plan", &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (s *Server) loadBuild(ctx context.Context, project, id string) (*BuildRecord, error) {
	var rec BuildRecord
	if err := s.loadRecord(ctx, buildKey(project, id), id, "build", &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (s *Server) loadRecord(ctx context.Context, key, id, what string, v any) error {
	if !validID(id) {
		return fail(http.StatusGone, "no such "+what)
	}
	b, _, err := s.Store.Get(ctx, key)
	if errors.Is(err, ErrNotFound) {
		return fail(http.StatusGone, "no such "+what)
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// recordIDs returns the IDs of the records of a folder of the project ("plans" or "builds").
func (s *Server) recordIDs(ctx context.Context, project, folder string) ([]string, error) {
	prefix := "x/" + project + "/" + folder + "/"
	keys, err := s.Store.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, k := range keys {
		rel := strings.TrimPrefix(k, prefix)
		if !strings.Contains(rel, "/") && strings.HasSuffix(rel, ".json") {
			out = append(out, strings.TrimSuffix(rel, ".json"))
		}
	}
	return out, nil
}

// loadPlans returns the plans of the project, the newest first.
func (s *Server) loadPlans(ctx context.Context, project string) ([]*PlanRecord, error) {
	ids, err := s.recordIDs(ctx, project, "plans")
	if err != nil {
		return nil, err
	}
	var out []*PlanRecord
	for _, id := range ids {
		if rec, err := s.loadPlan(ctx, project, id); err == nil {
			out = append(out, rec)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}

// loadBuilds returns the builds of the project, the newest first.
func (s *Server) loadBuilds(ctx context.Context, project string) ([]*BuildRecord, error) {
	ids, err := s.recordIDs(ctx, project, "builds")
	if err != nil {
		return nil, err
	}
	var out []*BuildRecord
	for _, id := range ids {
		if rec, err := s.loadBuild(ctx, project, id); err == nil {
			out = append(out, rec)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}

// projectPlan is a plan for the pages, with the addresses of its models, and its builds.
func (s *Server) projectPlan(ctx context.Context, p *Project, rec *PlanRecord) cloud.ProjectPlan {
	out := cloud.ProjectPlan{ID: rec.ID, Repo: rec.Repo, Commit: rec.Commit, Ref: rec.Ref, Kind: rec.Kind, PR: rec.PR,
		Tag: rec.Tag, Branch: rec.Branch, Created: rec.Created, Ready: rec.Ready, Parts: []cloud.ProjectPlanPart{}}
	if rec.RunID != "" {
		out.Run = "https://github.com/" + rec.Repo + "/actions/runs/" + rec.RunID
	}
	dir := planDir(p.ID, rec.ID)
	for _, pp := range rec.Parts {
		part := cloud.ProjectPlanPart{PlanPart: pp}
		if rec.Ready {
			for _, m := range pp.Models {
				u, _ := s.Grants.FileURL(ctx, dir+m, urlTTL)
				part.ModelURLs = append(part.ModelURLs, u)
			}
		}
		out.Parts = append(out.Parts, part)
	}
	return out
}

// plansView returns the newest plans of the project, each with its builds.
func (s *Server) plansView(ctx context.Context, p *Project) []cloud.ProjectPlan {
	plans, err := s.loadPlans(ctx, p.ID)
	if err != nil {
		return nil
	}
	builds, _ := s.loadBuilds(ctx, p.ID)
	var out []cloud.ProjectPlan
	for i, rec := range plans {
		if i >= PlansShown {
			break
		}
		pv := s.projectPlan(ctx, p, rec)
		for _, b := range builds {
			if b.Plan == rec.ID {
				pv.Builds = append(pv.Builds, s.projectBuild(ctx, p, b))
			}
		}
		out = append(out, pv)
	}
	return out
}

// projectBuild is a build for the pages, with the addresses of its files.
func (s *Server) projectBuild(ctx context.Context, p *Project, b *BuildRecord) cloud.ProjectBuild {
	out := cloud.ProjectBuild{ID: b.ID, Plan: b.Plan, Commit: b.Commit, Created: b.Created, State: b.State, Error: b.Error,
		Plates: []cloud.ProjectBuildPlate{}}
	if b.State == cloud.BuildSlicing && s.GitHub == nil && b.RunID == "" {
		out.Start = "gh workflow run printdeck.yml -R " + b.Repo + " -f build=" + b.ID
	}
	if b.RunID != "" {
		out.Run = "https://github.com/" + b.Repo + "/actions/runs/" + b.RunID
	}
	dir := buildDir(p.ID, b.ID)
	for _, pl := range b.Plates {
		pp := cloud.ProjectBuildPlate{BuildPlate: pl.BuildPlate, State: pl.State, By: pl.By, At: pl.At, Reason: pl.Reason}
		if b.State == cloud.BuildReady {
			if pl.Preview != "" {
				pp.PreviewURL, _ = s.Grants.FileURL(ctx, dir+pl.Preview, urlTTL)
			}
			if pl.Picture != "" {
				pp.PictureURL, _ = s.Grants.FileURL(ctx, dir+pl.Picture, urlTTL)
			}
		}
		out.Plates = append(out.Plates, pp)
	}
	return out
}

// decidePlate approves or rejects the plate n of the build id of the project p, for the owner.
func (s *Server) decidePlate(ctx context.Context, p *Project, w *who, id string, n int, approve bool, reason string) (any, error) {
	if err := ownerOnly(w); err != nil {
		return nil, err
	}
	b, err := s.loadBuild(ctx, p.ID, id)
	if err != nil {
		return nil, err
	}
	if b.State != cloud.BuildReady {
		return nil, fail(http.StatusConflict, "the build has no files yet")
	}
	if n < 0 || n >= len(b.Plates) {
		return nil, fail(http.StatusGone, "no such plate")
	}
	pl := &b.Plates[n]
	if pl.State == cloud.PartReplaced {
		return nil, fail(http.StatusConflict, "a newer plan replaced this build")
	}
	pl.State, pl.By, pl.At, pl.Reason = cloud.PartApproved, w.name, s.now().UTC(), ""
	if !approve {
		pl.State, pl.Reason = cloud.PartRejected, clip(strings.TrimSpace(reason), 1000)
		if pl.Reason == "" {
			return nil, fail(http.StatusBadRequest, "a rejection needs a reason")
		}
	}
	if err := s.putJSON(ctx, buildKey(p.ID, id), b); err != nil {
		return nil, err
	}
	return s.projectBuild(ctx, p, b), nil
}
