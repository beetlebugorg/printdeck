package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/png"
	"log"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"printdeck/cloud/modeldiff"
	"printdeck/internal/cloud"
)

// The comment of Printdeck on a pull request. The service keeps one comment on each pull request
// of a project, and writes it again when a plan of the pull request is ready: the parts that
// changed, what changed in each model, and a picture of each change. GitHub shows the pictures
// from gh/<random>.png, a public file of the bucket with a name that nobody can guess.

// MaxDiffPictures is the most pictures in a comment. The other changed models have their text only.
const MaxDiffPictures = 8

// planDiff is a changed model of a plan, against the same model in an older plan.
type planDiff struct {
	Label string `json:"label"`
	Text  string `json:"text"`
	Image string `json:"image,omitempty"` // the public address of the picture
}

// prRecord is x/<project>/prs/<n>.json: the comment of Printdeck on the pull request n, and the
// public copies of the photos in it, by the ID of the photo.
type prRecord struct {
	Comment int64             `json:"comment"`
	Updated time.Time         `json:"updated"`
	Photos  map[string]string `json:"photos,omitempty"`
	// State is the state of the pull request on GitHub at Checked: "open", "closed" or "merged".
	State   string    `json:"state,omitempty"`
	Checked time.Time `json:"checked,omitzero"`
}

// prCheckOpen and prCheckClosed are the ages of the state of an open and of a closed pull
// request, after which the project page asks GitHub again. A closed pull request can open again.
const (
	prCheckOpen   = 10 * time.Minute
	prCheckClosed = 6 * time.Hour
)

// prState returns the state of the pull request pr of the project p: "open", "closed", "merged",
// or "" when the service does not know it. It asks GitHub when the state is old.
func (s *Server) prState(ctx context.Context, p *Project, pr int) string {
	var st prRecord
	if b, _, err := s.Store.Get(ctx, prKey(p.ID, pr)); err == nil {
		json.Unmarshal(b, &st)
	}
	age := prCheckOpen
	if st.State != "" && st.State != "open" {
		age = prCheckClosed
	}
	if s.GitHub == nil || p.Repo == "" || s.now().Sub(st.Checked) < age {
		return st.State
	}
	// A job asks GitHub, so the view does not wait for it. The next view shows the new state.
	if s.later(ctx, Job{Kind: "prstate", Project: p.ID, PR: pr}) {
		return st.State
	}
	return s.checkPRState(ctx, p, pr)
}

// checkPRState asks GitHub for the state of the pull request pr of the project p, and keeps it.
func (s *Server) checkPRState(ctx context.Context, p *Project, pr int) string {
	var st prRecord
	if b, _, err := s.Store.Get(ctx, prKey(p.ID, pr)); err == nil {
		json.Unmarshal(b, &st)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	state, err := s.GitHub.PullState(ctx, p.Repo, pr)
	if err != nil {
		log.Printf("pull request %d of %s: %v", pr, p.Repo, err)
		return st.State
	}
	st.State, st.Checked = state, s.now().UTC()
	if err := s.putJSON(ctx, prKey(p.ID, pr), st); err != nil {
		log.Printf("pull request %d of %s: %v", pr, p.Repo, err)
	}
	return st.State
}

// platePrint is the newest print of a plate, with the public address of its first photo, or "".
type platePrint struct {
	rec   PrintRecord
	photo string
}

// plateFile is the form of the file of a print of a plate on a deck: builds/<build>-plate-<n>.
var plateFile = regexp.MustCompile(`(?:^|/)builds/([a-z2-7]{8,64})-plate-([0-9]{1,3})\.`)

// printedPlates returns the newest print of each plate of the builds, by "<build>/<plate>" (from
// 0). The first photo of each print gets a public copy, once: st keeps the copies.
func (s *Server) printedPlates(ctx context.Context, project string, builds []*BuildRecord, st *prRecord) map[string]platePrint {
	out := map[string]platePrint{}
	if len(builds) == 0 {
		return out
	}
	ids := map[string]bool{}
	for _, b := range builds {
		ids[b.ID] = true
	}
	prints, photos, err := s.projectFiles(ctx, project)
	if err != nil {
		return out
	}
	for _, pr := range prints {
		m := plateFile.FindStringSubmatch(pr.File)
		if m == nil || !ids[m[1]] || !pr.Done {
			continue
		}
		n, _ := strconv.Atoi(m[2])
		key := m[1] + "/" + strconv.Itoa(n-1)
		if old, ok := out[key]; ok && old.rec.Ended.After(pr.Ended) {
			continue
		}
		out[key] = platePrint{rec: pr}
	}
	for key, pp := range out {
		var first *PhotoRecord
		for i := range photos {
			if ph := &photos[i]; ph.Print == pp.rec.ID && (first == nil || ph.Added.Before(first.Added)) {
				first = ph
			}
		}
		if first == nil {
			continue
		}
		if st.Photos == nil {
			st.Photos = map[string]string{}
		}
		if st.Photos[first.ID] == "" {
			name := make([]byte, 16)
			rand.Read(name)
			dst := "gh/" + hex.EncodeToString(name) + ".jpg"
			if err := s.Store.Copy(ctx, "x/"+project+"/photos/"+first.ID+"-s.jpg", dst,
				PutOptions{ContentType: "image/jpeg", CacheControl: "public, max-age=31536000, immutable"}); err != nil {
				continue
			}
			st.Photos[first.ID] = s.base() + "/" + dst
		}
		pp.photo = st.Photos[first.ID]
		out[key] = pp
	}
	return out
}

// printState is the state of a plate with a print, for the comment.
func printState(pr PrintRecord) string {
	day := pr.Ended.UTC().Format("Jan 2")
	switch {
	case !pr.Discarded.IsZero():
		return "Printed " + day + ", rejected: " + pr.DiscardNote
	case pr.Result == "complete":
		return "Printed " + day
	case pr.Result == "cancelled":
		return "Stopped " + day
	}
	return "Failed " + day
}

// commentPrint writes the comment again when the print pr is a print of a plate of a pull request.
func (s *Server) commentPrint(ctx context.Context, project string, pr PrintRecord) {
	m := plateFile.FindStringSubmatch(pr.File)
	if m == nil || s.GitHub == nil {
		return
	}
	p, _, err := s.loadProject(ctx, project)
	if err != nil {
		return
	}
	b, err := s.loadBuild(ctx, project, m[1])
	if err != nil {
		return
	}
	s.commentBuild(ctx, p, b)
}

func prKey(project string, pr int) string { return fmt.Sprintf("x/%s/prs/%d.json", project, pr) }

// basePlanOf returns the older plan to compare rec with: the newest older ready plan of main or of
// a tag, else the newest older ready plan, as the plan page does. plans are the newest first.
func basePlanOf(plans []*PlanRecord, rec *PlanRecord) *PlanRecord {
	var any *PlanRecord
	for _, o := range plans {
		if o.ID == rec.ID || !o.Ready || !o.Created.Before(rec.Created) {
			continue
		}
		if o.Kind == cloud.BuildMain || o.Kind == cloud.BuildRelease {
			return o
		}
		if any == nil {
			any = o
		}
	}
	return any
}

// diffPair is a model of a changed part of rec, and the same file in the base plan.
type diffPair struct {
	label, oldKey, newKey string
}

// diffPairs returns the models of the changed parts of rec, each with the same file in base. A file
// of more than one variant of a part shows once.
func diffPairs(project string, rec, base *PlanRecord) []diffPair {
	var out []diffPair
	seen := map[string]bool{}
	for _, pp := range rec.Parts {
		if !pp.Changed || len(pp.Models) != len(pp.Files) {
			continue
		}
		var old *cloud.PlanPart
		for i := range base.Parts {
			bp := &base.Parts[i]
			if bp.Name == pp.Name && len(bp.Models) == len(bp.Files) && (old == nil || bp.Variant == pp.Variant) {
				old = bp
			}
		}
		if old == nil {
			continue
		}
		for j, f := range pp.Files {
			for k, of := range old.Files {
				if of != f || seen[pp.Name+"|"+f] {
					continue
				}
				seen[pp.Name+"|"+f] = true
				label := pp.Name
				if pp.Variant != "" {
					label += " (" + pp.Variant + ")"
				}
				if len(pp.Files) > 1 {
					label += " · " + path.Base(f)
				}
				out = append(out, diffPair{label: label, oldKey: planDir(project, base.ID) + old.Models[k],
					newKey: planDir(project, rec.ID) + pp.Models[j]})
			}
		}
	}
	return out
}

// planDiffs compares each changed model of rec with the base plan, and saves a picture of each
// change, up to MaxDiffPictures.
func (s *Server) planDiffs(ctx context.Context, project string, rec, base *PlanRecord) []planDiff {
	var out []planDiff
	for _, pr := range diffPairs(project, rec, base) {
		d := planDiff{Label: pr.label}
		if !s.smallFile(ctx, pr.oldKey) || !s.smallFile(ctx, pr.newKey) {
			d.Text = "The models are too large to compare"
			out = append(out, d)
			continue
		}
		a, _, err1 := s.Store.Get(ctx, pr.oldKey)
		b, _, err2 := s.Store.Get(ctx, pr.newKey)
		ta, err3 := modeldiff.ReadSTL(a)
		tb, err4 := modeldiff.ReadSTL(b)
		if err := firstErr(err1, err2, err3, err4); err != nil {
			d.Text = "The models did not load"
			out = append(out, d)
			continue
		}
		diff := modeldiff.Compare(ta, tb)
		d.Text = diff.Text()
		if !diff.Same && len(out) < MaxDiffPictures {
			d.Image = s.publicPicture(ctx, diff.Render(800, 600))
		}
		out = append(out, d)
	}
	return out
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// shortSHA is the first 7 digits of a commit.
func shortSHA(c string) string {
	if len(c) > 7 {
		return c[:7]
	}
	return c
}

// clockText is a time for a person: "3:45".
func clockText(sec int) string {
	return fmt.Sprintf("%d:%02d", sec/3600, sec/60%60)
}

// plateState is the state of a plate for the comment.
func (s *Server) plateState(ctx context.Context, pl plateRecord) string {
	switch pl.State {
	case cloud.PartWaiting, "":
		return "Waits for approval"
	case cloud.PartRejected:
		return "Rejected: " + pl.Reason
	case cloud.PartReplaced:
		return "Replaced by a newer plan"
	case cloud.PartApproved:
		if pl.Sent == "" {
			return "Approved"
		}
		name := "the printer"
		if pr, err := s.loadPrinter(ctx, pl.Sent); err == nil {
			name = "the " + pr.Name
		}
		if pl.Taken.IsZero() {
			return "Sent to " + name
		}
		return "In the queue of " + name
	}
	return pl.State
}

// prCommentBody is the text of the comment for the newest plan rec of a pull request, with the
// base plan (or nil), its changed models, and the builds of the pull request (the newest first).
func (s *Server) prCommentBody(ctx context.Context, p *Project, rec, base *PlanRecord, builds []*BuildRecord, printed map[string]platePrint) string {
	var b strings.Builder
	diffs := rec.Diffs
	planURL := s.projectURL(p) + "/plans/" + rec.ID
	b.WriteString("<!-- printdeck -->\n")
	fmt.Fprintf(&b, "### Printdeck · the plan of `%s`\n\n", shortSHA(rec.Commit))
	changed := 0
	for _, pp := range rec.Parts {
		if pp.Changed {
			changed++
		}
	}
	against := "nothing"
	if base != nil {
		switch {
		case base.Kind == cloud.BuildMain:
			against = fmt.Sprintf("main at `%s`", shortSHA(base.Commit))
		case base.Kind == cloud.BuildRelease && base.Tag != "":
			against = fmt.Sprintf("the tag `%s`", base.Tag)
		default:
			against = fmt.Sprintf("the plan of `%s`", shortSHA(base.Commit))
		}
	}
	fmt.Fprintf(&b, "%d of %d parts changed, compared with %s. **[Choose the parts and the plates](%s)**\n\n",
		changed, len(rec.Parts), against, planURL)
	if changed > 0 {
		text := map[string]string{}
		for _, d := range diffs {
			name, _, _ := strings.Cut(d.Label, " · ")
			if text[name] == "" {
				text[name] = d.Text
			}
		}
		b.WriteString("| Part | What changed |\n|---|---|\n")
		for _, pp := range rec.Parts {
			if !pp.Changed {
				continue
			}
			label := pp.Name
			if pp.Variant != "" {
				label += " (" + pp.Variant + ")"
			}
			what := text[label]
			if what == "" {
				what = "A new part, or a part with new files"
			}
			fmt.Fprintf(&b, "| %s | %s |\n", mdCell(label), mdCell(what))
		}
		b.WriteString("\n")
	}
	var pics []planDiff
	for _, d := range diffs {
		if d.Image != "" {
			pics = append(pics, d)
		}
	}
	if len(pics) > 0 {
		b.WriteString("<table>\n")
		for i := 0; i < len(pics); i += 2 {
			b.WriteString("<tr>")
			for _, d := range pics[i:min(i+2, len(pics))] {
				fmt.Fprintf(&b, `<td align="center"><img src="%s" width="400" alt="%s"><br><sub>%s</sub></td>`,
					html.EscapeString(d.Image), html.EscapeString(d.Label), html.EscapeString(d.Label+": "+d.Text))
			}
			b.WriteString("</tr>\n")
		}
		b.WriteString("</table>\n\n<sub>Gray: no change. Green: new material. Red: material that is gone.</sub>\n\n")
	}
	// The builds of the plan.
	for n, bd := range builds {
		if n == maxCommentBuilds {
			fmt.Fprintf(&b, "<sub>%d older builds are on the plan page.</sub>\n\n", len(builds)-n)
			break
		}
		url := s.projectURL(p) + "/builds/" + bd.ID
		of := ""
		if bd.Commit != rec.Commit {
			of = fmt.Sprintf(" of `%s`", shortSHA(bd.Commit))
		}
		switch bd.State {
		case cloud.BuildSlicing:
			fmt.Fprintf(&b, "#### [Build `%s`](%s)%s: slicing\n\n", bd.ID, url, of)
			continue
		case cloud.BuildFailed:
			fmt.Fprintf(&b, "#### [Build `%s`](%s)%s: failed\n\n%s\n\n", bd.ID, url, of, mdCell(bd.Error))
			continue
		}
		plates := fmt.Sprintf("%d plates", len(bd.Plates))
		if len(bd.Plates) == 1 {
			plates = "1 plate"
		}
		fmt.Fprintf(&b, "#### [Build `%s`](%s)%s: %s\n\n", bd.ID, url, of, plates)
		b.WriteString("| | Plate | Time | Material | State |\n|---|---|---|---|---|\n")
		for i, pl := range bd.Plates {
			var parts []string
			for _, pp := range pl.Parts {
				name := pp.Name
				if pp.Variant != "" {
					name += " (" + pp.Variant + ")"
				}
				if pp.Count > 1 {
					name += fmt.Sprintf(" x%d", pp.Count)
				}
				parts = append(parts, name)
			}
			pic, state := "", s.plateState(ctx, pl)
			if pl.Pic != "" {
				pic = fmt.Sprintf(`<img src="%s" width="160" alt="Plate %d">`, html.EscapeString(pl.Pic), i+1)
			}
			// A print of the plate: its result, and its first photo in place of the picture.
			if pp, ok := printed[bd.ID+"/"+strconv.Itoa(i)]; ok {
				state = printState(pp.rec)
				if pp.photo != "" {
					pic = fmt.Sprintf(`<img src="%s" width="160" alt="The print of plate %d">`, html.EscapeString(pp.photo), i+1)
				}
			}
			mat := pl.MaterialName
			if mat == "" {
				mat = pl.Material
			}
			switch {
			case pl.ML > 0:
				mat += fmt.Sprintf(", %.1f ml", pl.ML)
			case pl.Grams > 0:
				mat += fmt.Sprintf(", %.0f g", pl.Grams)
			}
			fmt.Fprintf(&b, "| %s | %d: %s | %s | %s | %s |\n", pic, i+1, mdCell(strings.Join(parts, ", ")),
				clockText(pl.Seconds), mdCell(mat), mdCell(state))
		}
		b.WriteString("\n")
	}
	b.WriteString("<sub>Printdeck writes this comment again at each push, slice and approval.</sub>\n")
	return b.String()
}

// maxCommentBuilds is the most builds in a comment.
const maxCommentBuilds = 3

// mdCell is text for a cell of a Markdown table.
func mdCell(s string) string {
	return strings.NewReplacer("|", "\\|", "\n", " ", "\r", "").Replace(s)
}

// Job is work of the service after a request. Kind "prstate" asks GitHub for the state of the
// pull request PR of the project. Kind "comment" writes the comment of the pull request PR, and
// with a Plan compares the models of the plan first. Kind "lapse" makes the video of the time
// lapse of the Print (lapse.go).
type Job struct {
	Kind    string `json:"kind"`
	Project string `json:"project"`
	Plan    string `json:"plan,omitempty"`
	PR      int    `json:"pr,omitempty"`
	Print   string `json:"print,omitempty"`
}

// jobTime is the most time of a job in its own run of the function, and inlineJobTime the most
// time of a job in a request.
const (
	jobTime       = 50 * time.Second
	inlineJobTime = 20 * time.Second
)

// later starts the job with s.Later, and reports that it started. With no s.Later, or when it
// fails, the caller runs the job in the request.
func (s *Server) later(ctx context.Context, job Job) bool {
	if s.Later == nil {
		return false
	}
	if err := s.Later(ctx, job); err != nil {
		log.Printf("job %s: %v", job.Kind, err)
		return false
	}
	return true
}

// RunJob runs a job that s.Later started.
func (s *Server) RunJob(ctx context.Context, job Job) {
	if job.Kind == "lapse" {
		if !validID(job.Project) || !validID(job.Print) {
			log.Printf("job: a bad job")
			return
		}
		ctx, cancel := context.WithTimeout(ctx, lapseJobTime)
		defer cancel()
		if err := s.LapseVideo(ctx, job.Project, job.Print); err != nil {
			log.Printf("job: the lapse of %s/%s: %v", job.Project, job.Print, err)
		}
		return
	}
	ctx, cancel := context.WithTimeout(ctx, jobTime)
	defer cancel()
	if job.Kind != "comment" && job.Kind != "prstate" || !validID(job.Project) || job.PR <= 0 {
		log.Printf("job: a bad job")
		return
	}
	p, _, err := s.loadProject(ctx, job.Project)
	if err != nil {
		log.Printf("job: the project: %v", err)
		return
	}
	if job.Kind == "prstate" {
		if s.GitHub != nil && p.Repo != "" {
			s.checkPRState(ctx, p, job.PR)
		}
		return
	}
	if job.Plan == "" {
		s.updatePR(ctx, p, job.PR)
		return
	}
	rec, err := s.loadPlan(ctx, p.ID, job.Plan)
	if err != nil {
		log.Printf("job: the plan: %v", err)
		return
	}
	s.commentPlanNow(ctx, p, rec)
}

// commentPlan compares the changed models of the ready plan rec of a pull request with its base
// plan, keeps the result in the plan, and writes the comment.
func (s *Server) commentPlan(ctx context.Context, p *Project, rec *PlanRecord) {
	if s.GitHub == nil || rec.PR <= 0 || rec.Kind != cloud.BuildTest || p.Repo == "" {
		return
	}
	if s.later(ctx, Job{Kind: "comment", Project: p.ID, Plan: rec.ID, PR: rec.PR}) {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, inlineJobTime)
	defer cancel()
	s.commentPlanNow(ctx, p, rec)
}

// commentPlanNow is commentPlan in this run.
func (s *Server) commentPlanNow(ctx context.Context, p *Project, rec *PlanRecord) {
	if s.GitHub == nil || rec.PR <= 0 || rec.Kind != cloud.BuildTest || p.Repo == "" {
		return
	}
	plans, err := s.loadPlans(ctx, p.ID)
	if err != nil {
		log.Printf("comment: the plans of %s: %v", p.ID, err)
		return
	}
	if base := basePlanOf(plans, rec); base != nil {
		rec.Base = base.ID
		rec.Diffs = s.planDiffs(ctx, p.ID, rec, base)
		if err := s.putJSON(ctx, planKey(p.ID, rec.ID), rec); err != nil {
			log.Printf("comment: %v", err)
		}
	}
	s.updatePR(ctx, p, rec.PR)
}

// commentBuild writes the comment of the pull request of the build b again, when its plan is
// of a pull request.
func (s *Server) commentBuild(ctx context.Context, p *Project, b *BuildRecord) {
	if s.GitHub == nil || p.Repo == "" {
		return
	}
	rec, err := s.loadPlan(ctx, p.ID, b.Plan)
	if err != nil || rec.PR <= 0 || rec.Kind != cloud.BuildTest {
		return
	}
	if s.later(ctx, Job{Kind: "comment", Project: p.ID, PR: rec.PR}) {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, inlineJobTime)
	defer cancel()
	s.updatePR(ctx, p, rec.PR)
}

// platePictures draws the pictures of the FDM plates of the ready build b that have none, and
// keeps them in the build. plans are the plans of the project, for the beds.
func (s *Server) platePictures(ctx context.Context, project string, b *BuildRecord, plans []*PlanRecord) {
	if b.State != cloud.BuildReady {
		return
	}
	var rec *PlanRecord
	for _, o := range plans {
		if o.ID == b.Plan {
			rec = o
		}
	}
	changed := false
	for i := range b.Plates {
		pl := &b.Plates[i]
		if pl.Pic != "" || pl.Process != "fdm" || !strings.HasSuffix(pl.File, ".gcode") || i >= MaxDiffPictures || pl.Size > MaxPictureFile {
			continue
		}
		data, _, err := s.Store.Get(ctx, buildDir(project, b.ID)+pl.File)
		if err != nil {
			continue
		}
		c := color.RGBA{0x55, 0x5b, 0x63, 0xff}
		if h, err := strconv.ParseUint(strings.TrimPrefix(pl.MaterialColor, "#"), 16, 32); err == nil && len(pl.MaterialColor) == 7 {
			c = color.RGBA{uint8(h >> 16), uint8(h >> 8), uint8(h), 0xff}
		}
		var bx, by float64
		if rec != nil && len(pl.Parts) > 0 && pl.Parts[0].Part < len(rec.Parts) {
			bx, by = rec.Parts[pl.Parts[0].Part].BedX, rec.Parts[pl.Parts[0].Part].BedY
		}
		img := modeldiff.RenderGcode(data, c, bx, by, 640, 480)
		if img == nil {
			continue
		}
		if u := s.publicPicture(ctx, img); u != "" {
			pl.Pic, changed = u, true
		}
	}
	if changed {
		if err := s.putJSON(ctx, buildKey(project, b.ID), b); err != nil {
			log.Printf("comment: %v", err)
		}
	}
}

// publicPicture saves the picture as gh/<random>.png, and returns its public address, or "".
func (s *Server) publicPicture(ctx context.Context, img image.Image) string {
	var buf bytes.Buffer
	if png.Encode(&buf, img) != nil {
		return ""
	}
	name := make([]byte, 16)
	rand.Read(name)
	key := "gh/" + hex.EncodeToString(name) + ".png"
	if _, err := s.Store.Put(ctx, key, buf.Bytes(), PutOptions{ContentType: "image/png",
		CacheControl: "public, max-age=31536000, immutable"}); err != nil {
		return ""
	}
	return s.base() + "/" + key
}

// updatePR writes the comment of Printdeck on the pull request pr of the project: from its newest
// ready plan, the base plan of that plan, and its builds. A failure goes to the log only.
func (s *Server) updatePR(ctx context.Context, p *Project, pr int) {
	plans, err := s.loadPlans(ctx, p.ID)
	if err != nil {
		log.Printf("comment: the plans of %s: %v", p.ID, err)
		return
	}
	var rec, base *PlanRecord
	for _, o := range plans {
		if o.Ready && o.PR == pr && o.Kind == cloud.BuildTest {
			rec = o
			break
		}
	}
	if rec == nil {
		return
	}
	for _, o := range plans {
		if o.ID == rec.Base {
			base = o
		}
	}
	// The builds of all the plans of the pull request, the newest first: an approved plate of an
	// older plan stays.
	ofPR := map[string]string{} // the commit of each plan of the pull request
	for _, o := range plans {
		if o.PR == pr && o.Kind == cloud.BuildTest {
			ofPR[o.ID] = o.Commit
		}
	}
	var builds []*BuildRecord
	if all, err := s.loadBuilds(ctx, p.ID); err == nil {
		for _, b := range all {
			if _, ok := ofPR[b.Plan]; ok {
				builds = append(builds, b)
			}
		}
	}
	for n, bd := range builds {
		if n < maxCommentBuilds {
			s.platePictures(ctx, p.ID, bd, plans)
		}
	}
	var st prRecord
	if b, _, err := s.Store.Get(ctx, prKey(p.ID, pr)); err == nil {
		json.Unmarshal(b, &st)
	}
	photos := len(st.Photos)
	printed := s.printedPlates(ctx, p.ID, builds, &st)
	id, err := s.GitHub.Comment(ctx, p.Repo, pr, st.Comment, s.prCommentBody(ctx, p, rec, base, builds, printed))
	if err != nil {
		log.Printf("comment: pull request %d of %s: %v", pr, p.Repo, err)
		return
	}
	if id != st.Comment || len(st.Photos) != photos {
		st.Comment, st.Updated = id, s.now().UTC()
		if err := s.putJSON(ctx, prKey(p.ID, pr), st); err != nil {
			log.Printf("comment: %v", err)
		}
	}
}
