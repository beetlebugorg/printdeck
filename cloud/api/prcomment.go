package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"image/png"
	"log"
	"path"
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

// prRecord is x/<project>/prs/<n>.json: the comment of Printdeck on the pull request n.
type prRecord struct {
	Comment int64     `json:"comment"`
	Updated time.Time `json:"updated"`
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
			var buf bytes.Buffer
			if png.Encode(&buf, diff.Render(800, 600)) == nil {
				name := make([]byte, 16)
				rand.Read(name)
				key := "gh/" + hex.EncodeToString(name) + ".png"
				if _, err := s.Store.Put(ctx, key, buf.Bytes(), PutOptions{ContentType: "image/png",
					CacheControl: "public, max-age=31536000, immutable"}); err == nil {
					d.Image = s.base() + "/" + key
				}
			}
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

// prCommentBody is the text of the comment for the plan rec of a pull request, with the base
// plan (or nil) and the changed models.
func (s *Server) prCommentBody(p *Project, rec, base *PlanRecord, diffs []planDiff) string {
	var b strings.Builder
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
	b.WriteString("<sub>Printdeck writes this comment again at each push to the pull request.</sub>\n")
	return b.String()
}

// mdCell is text for a cell of a Markdown table.
func mdCell(s string) string {
	return strings.NewReplacer("|", "\\|", "\n", " ").Replace(s)
}

// commentPlan writes the comment of Printdeck on the pull request of the ready plan rec. A
// failure goes to the log only: the plan is ready anyway.
func (s *Server) commentPlan(ctx context.Context, p *Project, rec *PlanRecord) {
	if s.GitHub == nil || rec.PR <= 0 || rec.Kind != cloud.BuildTest || p.Repo == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	plans, err := s.loadPlans(ctx, p.ID)
	if err != nil {
		log.Printf("comment: the plans of %s: %v", p.ID, err)
		return
	}
	// A newer plan of the same pull request writes the comment.
	for _, o := range plans {
		if o.ID != rec.ID && o.PR == rec.PR && o.Ready && o.Created.After(rec.Created) {
			return
		}
	}
	base := basePlanOf(plans, rec)
	var diffs []planDiff
	if base != nil {
		diffs = s.planDiffs(ctx, p.ID, rec, base)
	}
	var pr prRecord
	if b, _, err := s.Store.Get(ctx, prKey(p.ID, rec.PR)); err == nil {
		json.Unmarshal(b, &pr)
	}
	id, err := s.GitHub.Comment(ctx, p.Repo, rec.PR, pr.Comment, s.prCommentBody(p, rec, base, diffs))
	if err != nil {
		log.Printf("comment: pull request %d of %s: %v", rec.PR, p.Repo, err)
		return
	}
	if id != pr.Comment {
		if err := s.putJSON(ctx, prKey(p.ID, rec.PR), prRecord{Comment: id, Updated: s.now().UTC()}); err != nil {
			log.Printf("comment: %v", err)
		}
	}
}
