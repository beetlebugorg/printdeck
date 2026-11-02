package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
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

// GitHubAPI is what the service asks GitHub, with its GitHub App (githubapp.go).
type GitHubAPI interface {
	// Dispatch starts the slice run of the build in the repository (workflow_dispatch). title is
	// the title of the run on GitHub.
	Dispatch(ctx context.Context, repo, build, title string) error
	// OpenPull returns the number of the open pull request into base from the branch of headRepo
	// whose head is the commit sha, or 0 for none.
	OpenPull(ctx context.Context, base, headRepo, branch, sha string) (int, error)
	// Comment writes the comment of Printdeck on the pull request pr of repo: it changes the
	// comment id, or makes a new comment for 0 or a comment that is gone. It returns the ID.
	Comment(ctx context.Context, repo string, pr int, id int64, body string) (int64, error)
	// PullState returns the state of the pull request pr of repo: "open", "closed" or "merged".
	PullState(ctx context.Context, repo string, pr int) (string, error)
}

// PlanRecord is x/<project>/plans/<id>.json.
type PlanRecord struct {
	ID     string `json:"id"`
	Repo   string `json:"repo"`
	Commit string `json:"commit"`
	Ref    string `json:"ref"`
	Kind   string `json:"kind"`
	PR     int    `json:"pr,omitempty"`
	Tag    string `json:"tag,omitempty"`
	Branch string `json:"branch,omitempty"`
	RunID  string `json:"run_id,omitempty"`
	// Fork is the fork of a plan of a member's pull request, and Author the member's GitHub
	// username: the repository of the token of the plan is Fork.
	Fork    string           `json:"fork,omitempty"`
	Author  string           `json:"author,omitempty"`
	Created time.Time        `json:"created"`
	Ready   bool             `json:"ready"`
	Parts   []cloud.PlanPart `json:"parts"`
	// Docs are the PDFs of the docs of printdeck.yml, in the folder of the plan.
	Docs []cloud.PlanDoc `json:"docs,omitempty"`
	// Base is the plan that a plan of a pull request compares with, and Diffs the changed models
	// against it, for the comment on the pull request (prcomment.go).
	Base  string     `json:"base,omitempty"`
	Diffs []planDiff `json:"diffs,omitempty"`
}

// BuildRecord is x/<project>/builds/<id>.json.
type BuildRecord struct {
	ID       string        `json:"id"`
	Plan     string        `json:"plan"`
	Repo     string        `json:"repo"`
	Commit   string        `json:"commit"`
	Created  time.Time     `json:"created"`
	State    string        `json:"state"`
	Error    string        `json:"error,omitempty"`
	RunID    string        `json:"run_id,omitempty"` // the slice run on GitHub
	Progress string        `json:"progress,omitempty"`
	Phase    string        `json:"phase,omitempty"`
	Plate    int           `json:"plate,omitempty"`
	Plates   []plateRecord `json:"plates"`
}

// plateRecord is a plate of a build record, with its state.
type plateRecord struct {
	cloud.BuildPlate
	State  string    `json:"state,omitempty"`
	By     string    `json:"by,omitempty"`
	At     time.Time `json:"at,omitzero"`
	Reason string    `json:"reason,omitempty"`
	// Sent is the printer that the owner sent the plate to (its ID), SentAt the time, and Taken the
	// time when its deck put the plate in its queue.
	Sent   string    `json:"sent,omitempty"`
	SentAt time.Time `json:"sent_at,omitzero"`
	Taken  time.Time `json:"taken,omitzero"`
	// Pic is the public address of the picture of the plate, for the comment on a pull request.
	Pic string `json:"pic,omitempty"`
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

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
	var fork *forkPlan
	if err != nil && kind == "plans" {
		// A token of a repository with no project: maybe a member's fork with a pull request.
		// A fork only sends plans: a build needs a token of the repository of the project.
		var ferr error
		if fork, ferr = s.forkProject(ctx, c); ferr != nil {
			return nil, ferr
		}
		p, err = fork.project, nil
	}
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
		return s.newPlan(ctx, p, c, in, fork)
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
	case kind == "builds" && action == "progress" && r.Method == http.MethodPost:
		var in cloud.BuildProgress
		json.Unmarshal(body, &in)
		b, err := s.actionBuild(ctx, p, c, id)
		if err != nil {
			return nil, err
		}
		if b.State != cloud.BuildSlicing {
			return nil, fail(http.StatusConflict, "the build is not slicing")
		}
		b.Progress = clip(strings.TrimSpace(in.Text), 200)
		b.Phase, b.Plate = "", 0
		switch in.Phase {
		case cloud.PhaseSetup, cloud.PhaseSlice, cloud.PhaseUpload:
			b.Phase = in.Phase
		}
		if b.Phase == cloud.PhaseSlice && in.Plate >= 1 && in.Plate <= len(b.Plates) {
			b.Plate = in.Plate
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
		s.commentBuild(ctx, p, b)
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
func (s *Server) newPlan(ctx context.Context, p *Project, c *GitHubClaims, in cloud.PlanManifest, fork *forkPlan) (any, error) {
	if len(in.Parts) == 0 || len(in.Parts) > MaxPlanParts {
		return nil, fail(http.StatusBadRequest, fmt.Sprintf("a plan has 1 to %d parts", MaxPlanParts))
	}
	rec := PlanRecord{ID: newID(10), Repo: c.Repository, Commit: c.SHA, Ref: c.Ref, RunID: clip(c.RunID, 30), Created: s.now().UTC()}
	if in.Commit != "" && fork == nil {
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
	switch {
	case fork != nil:
		// The plan of a member's pull request from a fork: the repository of the project, the
		// pull request that GitHub found, and the commit of the token.
		rec.Repo, rec.Fork, rec.Author = p.Repo, c.Repository, c.RepositoryOwner
		rec.Kind, rec.PR, rec.Branch = cloud.BuildTest, fork.pr, fork.branch
	case rec.Kind == cloud.BuildBranch && s.GitHub != nil:
		// A branch of the repository with an open pull request: the plan of the pull request.
		// An error (for example the App is not installed on the repository) leaves a plan of the
		// branch. The log says why: with no pull request, the Builds tab does not list the plan.
		n, err := s.GitHub.OpenPull(ctx, c.Repository, c.Repository, rec.Branch, rec.Commit)
		if err != nil {
			log.Printf("plan of %s %s: the pull request: %v", c.Repository, rec.Branch, err)
		} else if n > 0 {
			rec.Kind, rec.PR = cloud.BuildTest, n
		}
	}
	names := map[string]bool{}
	for _, pp := range in.Parts {
		pp.Name, pp.Variant, pp.Printer, pp.Material, pp.Profile, pp.Prebuilt =
			clip(strings.TrimSpace(pp.Name), 120), clip(strings.TrimSpace(pp.Variant), 40), clip(pp.Printer, 40), clip(pp.Material, 80),
			clip(pp.Profile, 200), clip(pp.Prebuilt, 300)
		key := strings.ToLower(pp.Name) + "\x00" + strings.ToLower(pp.Variant)
		if pp.Name == "" || pp.Printer == "" || names[key] {
			return nil, fail(http.StatusBadRequest, "each part (and variant) needs a printer and a name of its own")
		}
		names[key] = true
		pp.MaterialName = clip(strings.TrimSpace(pp.MaterialName), 120)
		if !hexColor.MatchString(pp.MaterialColor) {
			pp.MaterialColor = ""
		}
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
		if !sha256Re.MatchString(pp.Digest) {
			pp.Digest = ""
		}
		pp.Count = max(1, min(pp.Count, MaxPartCount))
		rec.Parts = append(rec.Parts, pp)
	}
	docs, err := planDocs(in.Docs)
	if err != nil {
		return nil, err
	}
	rec.Docs = docs
	g, err := s.Grants.Grant(ctx, planDir(p.ID, rec.ID), MaxBuildFile, buildGrantTTL)
	if err != nil {
		return nil, err
	}
	if err := s.putJSON(ctx, planKey(p.ID, rec.ID), rec); err != nil {
		return nil, err
	}
	return cloud.UploadReply{ID: rec.ID, URL: s.projectURL(p) + "/plans/" + rec.ID, Grant: g}, nil
}

// planDocs checks the PDFs of a plan manifest, and returns them with their fields clipped.
func planDocs(in []cloud.PlanDoc) ([]cloud.PlanDoc, error) {
	if len(in) > MaxPlanDocs {
		return nil, fail(http.StatusBadRequest, fmt.Sprintf("a plan has at most %d docs", MaxPlanDocs))
	}
	var out []cloud.PlanDoc
	names := map[string]bool{}
	for _, d := range in {
		d.Name, d.File = clip(strings.TrimSpace(d.Name), 120), clip(d.File, 300)
		switch {
		case d.Name == "" || d.File == "":
			return nil, fail(http.StatusBadRequest, "each doc needs a name and a file")
		case !fileName.MatchString(d.PDF) || !strings.HasSuffix(d.PDF, ".pdf") || names[d.PDF]:
			return nil, fail(http.StatusBadRequest, "a bad PDF name: "+d.PDF)
		case d.Thumb != "" && (!fileName.MatchString(d.Thumb) || !strings.HasSuffix(d.Thumb, ".png") || names[d.Thumb]):
			return nil, fail(http.StatusBadRequest, "a bad picture name: "+d.Thumb)
		case d.Size <= 0 || d.Size > MaxDocFile || !sha256Re.MatchString(d.SHA256):
			return nil, fail(http.StatusBadRequest, "a doc needs a size of at most 50 MB and a SHA-256")
		}
		names[d.PDF], names[d.Thumb] = true, true
		d.Pages = max(0, min(d.Pages, 10000))
		out = append(out, d)
	}
	return out, nil
}

// planDone checks the models of the plan id and makes it ready. The plates that wait in the
// builds of older plans of the same pull request or branch get the state replaced.
func (s *Server) planDone(ctx context.Context, p *Project, c *GitHubClaims, id string) (any, error) {
	rec, err := s.loadPlan(ctx, p.ID, id)
	if err != nil {
		return nil, err
	}
	from := rec.Repo
	if rec.Fork != "" {
		from = rec.Fork
	}
	if !strings.EqualFold(from, c.Repository) {
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
	for i, d := range rec.Docs {
		if !have[d.PDF] {
			return nil, fail(http.StatusBadRequest, "the PDF "+d.PDF+" is not up")
		}
		// A doc with no picture shows an icon on the pages.
		if !have[d.Thumb] {
			rec.Docs[i].Thumb = ""
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
	s.commentPlan(ctx, p, rec)
	return s.projectPlan(ctx, p, rec), nil
}

// replaceOlder marks the waiting plates of the builds of the older plans of the same pull
// request or branch as replaced, when a part of the plate changed in rec (stillSame).
func (s *Server) replaceOlder(ctx context.Context, p *Project, rec *PlanRecord) error {
	plans, err := s.loadPlans(ctx, p.ID)
	if err != nil {
		return err
	}
	older := map[string]*PlanRecord{}
	for _, o := range plans {
		if o.ID != rec.ID && o.Kind == rec.Kind && o.PR == rec.PR && o.Branch == rec.Branch && o.Created.Before(rec.Created) {
			older[o.ID] = o
		}
	}
	builds, err := s.loadBuilds(ctx, p.ID)
	if err != nil {
		return err
	}
	for _, b := range builds {
		old := older[b.Plan]
		if old == nil {
			continue
		}
		changed := false
		for i := range b.Plates {
			if b.Plates[i].State == cloud.PartWaiting && !stillSame(old, rec, b.Plates[i].Parts) {
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

// replaceByBuild marks the waiting plates of the older builds of the same plan, pull request or
// branch as replaced, when the new build nb has all the parts of the plate: the owner sliced them
// again, so the old plates wait for nothing.
func (s *Server) replaceByBuild(ctx context.Context, p *Project, plan *PlanRecord, nb *BuildRecord) error {
	key := func(pp cloud.PlatePart) string {
		return strings.ToLower(pp.Name) + "\x00" + strings.ToLower(pp.Variant)
	}
	parts := map[string]bool{}
	for _, pl := range nb.Plates {
		for _, pp := range pl.Parts {
			parts[key(pp)] = true
		}
	}
	plans, err := s.loadPlans(ctx, p.ID)
	if err != nil {
		return err
	}
	same := map[string]bool{}
	for _, o := range plans {
		if o.ID == plan.ID || o.Kind == plan.Kind && o.PR == plan.PR && o.Branch == plan.Branch && o.Tag == plan.Tag &&
			!o.Created.After(plan.Created) {
			same[o.ID] = true
		}
	}
	builds, err := s.loadBuilds(ctx, p.ID)
	if err != nil {
		return err
	}
	for _, b := range builds {
		if b.ID == nb.ID || !same[b.Plan] {
			continue
		}
		changed := false
		for i := range b.Plates {
			pl := &b.Plates[i]
			if pl.State != cloud.PartWaiting || len(pl.Parts) == 0 {
				continue
			}
			all := true
			for _, pp := range pl.Parts {
				all = all && parts[key(pp)]
			}
			if all {
				pl.State, changed = cloud.PartReplaced, true
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

// newBuild makes a build of the plan with the plates of the owner or of a member who can slice,
// and starts the slice run.
func (s *Server) newBuild(ctx context.Context, p *Project, w *who, planID string, in cloud.BuildRequest) (any, error) {
	if err := canSlice(w); err != nil {
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
				plate = cloud.BuildPlate{Printer: part.Printer, Process: part.Process, Profile: part.Profile, Material: part.Material,
					MaterialName: part.MaterialName, MaterialColor: part.MaterialColor}
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
			plate.Parts = append(plate.Parts, cloud.PlatePart{Part: pp.Part, Count: count, Name: part.Name, Variant: part.Variant})
		}
		b.Plates = append(b.Plates, plateRecord{BuildPlate: plate})
	}
	if err := s.putJSON(ctx, buildKey(p.ID, b.ID), b); err != nil {
		return nil, err
	}
	if err := s.replaceByBuild(ctx, p, plan, &b); err != nil {
		return nil, err
	}
	if s.GitHub != nil {
		if err := s.GitHub.Dispatch(ctx, plan.Repo, b.ID, runTitle(plan, &b)); err != nil {
			b.State, b.Error = cloud.BuildFailed, "GitHub did not start the slice run: "+err.Error()
			if err := s.putJSON(ctx, buildKey(p.ID, b.ID), b); err != nil {
				return nil, err
			}
		}
	}
	s.commentBuild(ctx, p, &b)
	return s.projectBuild(ctx, p, &b), nil
}

// runTitle is the title of the slice run on GitHub: the parts of the plates and the plan, for
// example "Pod door (fine) x2 + Pod panel (fine) · PR #12".
func runTitle(plan *PlanRecord, b *BuildRecord) string {
	var parts []string
	for _, pl := range b.Plates {
		for _, pp := range pl.Parts {
			t := pp.Name
			if pp.Variant != "" {
				t += " (" + pp.Variant + ")"
			}
			if pp.Count > 1 {
				t += fmt.Sprintf(" x%d", pp.Count)
			}
			parts = append(parts, t)
		}
	}
	where := plan.Branch
	switch plan.Kind {
	case cloud.BuildTest:
		where = fmt.Sprintf("PR #%d", plan.PR)
	case cloud.BuildRelease:
		where = plan.Tag
	}
	return clip(strings.Join(parts, " + ")+" · "+where, 120)
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
	s.commentBuild(ctx, p, b)
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
	keys, _, err := s.Store.ListDir(ctx, prefix)
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
	var keys []string
	for _, id := range ids {
		if validID(id) {
			keys = append(keys, planKey(project, id))
		}
	}
	var out []*PlanRecord
	for _, b := range s.getAll(ctx, keys) {
		var rec PlanRecord
		if b != nil && json.Unmarshal(b, &rec) == nil {
			out = append(out, &rec)
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
	var keys []string
	for _, id := range ids {
		if validID(id) {
			keys = append(keys, buildKey(project, id))
		}
	}
	var out []*BuildRecord
	for _, b := range s.getAll(ctx, keys) {
		var rec BuildRecord
		if b != nil && json.Unmarshal(b, &rec) == nil {
			out = append(out, &rec)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}

// projectPlan is a plan for the pages, with the addresses of its models, and its builds.
func (s *Server) projectPlan(ctx context.Context, p *Project, rec *PlanRecord) cloud.ProjectPlan {
	out := cloud.ProjectPlan{ID: rec.ID, Repo: rec.Repo, Commit: rec.Commit, Ref: rec.Ref, Kind: rec.Kind, PR: rec.PR,
		Tag: rec.Tag, Branch: rec.Branch, Fork: rec.Fork, Author: rec.Author, Created: rec.Created, Ready: rec.Ready,
		Parts: []cloud.ProjectPlanPart{}}
	if rec.RunID != "" {
		runs := rec.Repo
		if rec.Fork != "" {
			runs = rec.Fork
		}
		out.Run = "https://github.com/" + runs + "/actions/runs/" + rec.RunID
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
	if rec.Ready {
		out.Docs = s.planDocViews(ctx, p, rec)
	}
	return out
}

// planDocViews returns the PDFs of the ready plan rec with their addresses.
func (s *Server) planDocViews(ctx context.Context, p *Project, rec *PlanRecord) []cloud.ProjectDoc {
	var out []cloud.ProjectDoc
	dir := planDir(p.ID, rec.ID)
	for _, d := range rec.Docs {
		v := cloud.ProjectDoc{PlanDoc: d, Plan: rec.ID, Commit: rec.Commit}
		v.URL, _ = s.Grants.FileURL(ctx, dir+d.PDF, urlTTL)
		if d.Thumb != "" {
			v.ThumbURL, _ = s.Grants.FileURL(ctx, dir+d.Thumb, urlTTL)
		}
		repo := rec.Repo
		if rec.Fork != "" {
			repo = rec.Fork
		}
		if repo != "" && hexSHA.MatchString(rec.Commit) {
			v.RepoURL = "https://github.com/" + repo + "/blob/" + rec.Commit + "/" + escapeRepoPath(d.File)
		}
		out = append(out, v)
	}
	return out
}

// stillSame reports whether each part of a plate of the plan old is in the newer plan rec with
// no change: the same digest, or with no digest in one of the plans, no mark Changed in rec.
func stillSame(old, rec *PlanRecord, parts []cloud.PlatePart) bool {
	for _, pp := range parts {
		if pp.Part < 0 || pp.Part >= len(old.Parts) {
			return false
		}
		was := old.Parts[pp.Part]
		found := false
		for _, now := range rec.Parts {
			if !strings.EqualFold(now.Name, was.Name) || !strings.EqualFold(now.Variant, was.Variant) {
				continue
			}
			found = true
			if now.Digest != "" && was.Digest != "" {
				if now.Digest != was.Digest {
					return false
				}
			} else if now.Changed || now.Profile != was.Profile || now.Material != was.Material {
				return false
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// escapeRepoPath escapes each segment of the path of a file in a repository for a link.
func escapeRepoPath(p string) string {
	segs := strings.Split(p, "/")
	for i, seg := range segs {
		segs[i] = url.PathEscape(seg)
	}
	return strings.Join(segs, "/")
}

// projectDocs returns the PDFs of the newest ready plan of the default branch, else of the newest
// ready plan of a tag. plans are the plans of the project, the newest first.
func (s *Server) projectDocs(ctx context.Context, p *Project, plans []*PlanRecord) []cloud.ProjectDoc {
	for _, kind := range []string{cloud.BuildMain, cloud.BuildRelease} {
		for _, rec := range plans {
			if rec.Ready && rec.Kind == kind && rec.Fork == "" {
				if len(rec.Docs) == 0 {
					return nil
				}
				return s.planDocViews(ctx, p, rec)
			}
		}
	}
	return nil
}

// plansView returns the newest plans of the project (at most limit, or all for 0, and at least
// listedShown listed plans), each with its builds, the count of the listed plans (listedPlan), and
// the PDFs of the project (projectDocs).
func (s *Server) plansView(ctx context.Context, p *Project, limit int) ([]cloud.ProjectPlan, int, []cloud.ProjectDoc) {
	// The plans and the builds at the same time.
	var builds []*BuildRecord
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		builds, _ = s.loadBuilds(ctx, p.ID)
	}()
	plans, err := s.loadPlans(ctx, p.ID)
	wg.Wait()
	if err != nil {
		return nil, 0, nil
	}
	// The newest plans, and the newest plans that the Builds tab lists (listedPlan), so that many
	// merges to main do not push them out.
	shown, total := plans, 0
	for _, rec := range plans {
		if listedPlan(rec) {
			total++
		}
	}
	if limit > 0 && len(shown) > limit {
		shown = append([]*PlanRecord(nil), plans[:limit]...)
		n := 0
		for _, rec := range shown {
			if listedPlan(rec) {
				n++
			}
		}
		for _, rec := range plans[limit:] {
			if n >= listedShown {
				break
			}
			if listedPlan(rec) {
				shown = append(shown, rec)
				n++
			}
		}
	}
	// The states of the pull requests, and the names of the printers of the plates, at the same
	// time.
	var prs []int
	seen := map[int]bool{}
	for _, rec := range shown {
		if rec.Kind == cloud.BuildTest && rec.PR > 0 && !seen[rec.PR] {
			seen[rec.PR] = true
			prs = append(prs, rec.PR)
		}
	}
	ids := map[string]bool{}
	for _, b := range builds {
		for _, pl := range b.Plates {
			if pl.Sent != "" {
				ids[pl.Sent] = true
			}
		}
	}
	states := make([]string, len(prs))
	var names map[string]string
	var queued map[string]*cloud.ProjectQueued
	wg.Add(2)
	go func() {
		defer wg.Done()
		names = map[string]string{}
		for _, pr := range s.printersOf(ctx, ids, false) {
			names[pr.ID] = pr.Name
		}
	}()
	go func() {
		defer wg.Done()
		queued = queuedPlates(s.projectQueued(ctx, p.ID))
	}()
	forAll(len(prs), func(i int) error {
		states[i] = s.prState(ctx, p, prs[i])
		return nil
	})
	wg.Wait()
	var out []cloud.ProjectPlan
	for _, rec := range shown {
		pv := s.projectPlan(ctx, p, rec)
		if rec.Kind == cloud.BuildTest && rec.PR > 0 {
			for i, pr := range prs {
				if pr == rec.PR {
					pv.PRState = states[i]
				}
			}
		}
		for _, b := range builds {
			if b.Plan == rec.ID {
				pv.Builds = append(pv.Builds, s.projectBuildNamed(ctx, p, b, names, queued))
			}
		}
		out = append(out, pv)
	}
	return out, total, s.projectDocs(ctx, p, plans)
}

// listedPlan reports that the Builds tab lists the plan: a plan of a pull request or of a release
// (a tag, for example v1.2 or release/1.2). The plans of main and of other branches are many: the
// Overview shows the newest plan of main.
func listedPlan(rec *PlanRecord) bool {
	return rec.Kind == cloud.BuildTest || rec.Kind == cloud.BuildRelease
}

// listedShown is the least count of listed plans in a view of a project: the Builds tab shows the
// newest ones (plansOnProject in cmd/pwf-web).
const listedShown = 3

// projectBuild is a build for the pages, with the addresses of its files.
func (s *Server) projectBuild(ctx context.Context, p *Project, b *BuildRecord) cloud.ProjectBuild {
	return s.projectBuildNamed(ctx, p, b, nil, queuedPlates(s.projectQueued(ctx, p.ID)))
}

// projectBuildNamed is projectBuild with the names of the printers by ID, or nil to read them,
// and the plates in the queues of the printers (queuedPlates).
func (s *Server) projectBuildNamed(ctx context.Context, p *Project, b *BuildRecord, names map[string]string,
	queued map[string]*cloud.ProjectQueued) cloud.ProjectBuild {
	out := cloud.ProjectBuild{ID: b.ID, Plan: b.Plan, Commit: b.Commit, Created: b.Created, State: b.State, Error: b.Error,
		Plates: []cloud.ProjectBuildPlate{}}
	if b.State == cloud.BuildSlicing && s.GitHub == nil && b.RunID == "" {
		out.Start = "gh workflow run printdeck.yml -R " + b.Repo + " -f build=" + b.ID
	}
	if b.RunID != "" {
		out.Run = "https://github.com/" + b.Repo + "/actions/runs/" + b.RunID
	}
	if b.State == cloud.BuildSlicing {
		out.Progress, out.Phase, out.Plate = b.Progress, b.Phase, b.Plate
	}
	dir := buildDir(p.ID, b.ID)
	for i, pl := range b.Plates {
		pp := cloud.ProjectBuildPlate{BuildPlate: pl.BuildPlate, State: pl.State, By: pl.By, At: pl.At, Reason: pl.Reason,
			SentAt: pl.SentAt, TakenAt: pl.Taken, Queued: queued[plateQueueKey(b.ID, i)]}
		if pl.Sent != "" && names != nil {
			pp.SentTo = names[pl.Sent]
		} else if pl.Sent != "" {
			if pr, err := s.loadPrinter(ctx, pl.Sent); err == nil {
				pp.SentTo = pr.Name
			}
		}
		if b.State == cloud.BuildReady {
			if pl.Preview != "" {
				pp.PreviewURL, _ = s.Grants.FileURL(ctx, dir+pl.Preview, urlTTL)
			}
			if pl.Picture != "" {
				pp.PictureURL, _ = s.Grants.FileURL(ctx, dir+pl.Picture, urlTTL)
			} else if pl.Pic != "" {
				// No picture of the slice: the picture of the plate for the comment on the pull
				// request, so each plate card shows its plate.
				pp.PictureURL = pl.Pic
			}
			if pl.File != "" {
				pp.FileURL, _ = s.Grants.FileURL(ctx, dir+pl.File, urlTTL)
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
	s.commentBuild(ctx, p, b)
	return s.projectBuild(ctx, p, b), nil
}
