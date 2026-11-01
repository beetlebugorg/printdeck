package api

import (
	"context"
	"encoding/binary"
	"math"
	"strings"
	"testing"
	"time"

	"printdeck/internal/cloud"
)

// cubeSTL is a binary STL of a box sx x sy x sz mm on Z 0.
func cubeSTL(sx, sy, sz float32) []byte {
	v := [][3]float32{{0, 0, 0}, {sx, 0, 0}, {sx, sy, 0}, {0, sy, 0}, {0, 0, sz}, {sx, 0, sz}, {sx, sy, sz}, {0, sy, sz}}
	f := [][3]int{{0, 2, 1}, {0, 3, 2}, {4, 5, 6}, {4, 6, 7}, {0, 1, 5}, {0, 5, 4}, {1, 2, 6}, {1, 6, 5}, {2, 3, 7}, {2, 7, 6}, {3, 0, 4}, {3, 4, 7}}
	out := make([]byte, 84+50*len(f))
	binary.LittleEndian.PutUint32(out[80:], uint32(len(f)))
	for i, t := range f {
		for j, k := range t {
			for c := 0; c < 3; c++ {
				binary.LittleEndian.PutUint32(out[84+50*i+12+12*j+4*c:], math.Float32bits(v[k][c]))
			}
		}
	}
	return out
}

// A plan of a pull request writes the comment of Printdeck on the pull request, with what changed
// in each model and a picture; the next plan of the pull request changes the same comment.
func TestPRComment(t *testing.T) {
	d, base := devService(t)
	base = strings.Replace(base, "127.0.0.1", "localhost", 1)
	d.API.Base, d.Grants.URL = base, base+"/upload"
	d.API.OIDC = fakeOIDC{}
	app := &recordDispatch{}
	d.API.GitHub = app
	ctx := context.Background()
	l := connectedLink(t, d, base)
	var c cloud.Client
	var oc cloud.OwnerCodeReply
	c.Do(ctx, l.Identity(), "POST", "/api/v1/owner/code", nil, &oc)
	tok := register(t, base, newSoftKey(t), "owner", oc.Code, "Jeremy").Token
	sum, _ := c.NewProject(ctx, l.Identity(), "Bittlex")
	call(t, base, "PUT", "/api/v1/projects/"+sum.ID+"/repo", tok, cloud.RepoRequest{Repo: "beetlebugorg/bittlex"}, nil)

	plan := func(token string, changed bool, door []byte) string {
		m := cloud.PlanManifest{Parts: []cloud.PlanPart{
			{Name: "Pod door", Files: []string{"door.stl"}, Printer: "taz", Process: "fdm", Profile: "p/q@1", Models: []string{"part1-1.stl"}, Changed: changed},
			{Name: "Cover", Files: []string{"cover.stl"}, Printer: "taz", Process: "fdm", Profile: "p/q@1", Models: []string{"part2-1.stl"}},
		}}
		var up cloud.UploadReply
		if st := call(t, base, "POST", "/api/v1/plans", token, m, &up); st != 200 {
			t.Fatalf("plan %d", st)
		}
		cloud.PostFile(ctx, nil, up.Grant, "part1-1.stl", "model/stl", "", door)
		cloud.PostFile(ctx, nil, up.Grant, "part2-1.stl", "model/stl", "", cubeSTL(20, 20, 5))
		if st := call(t, base, "POST", "/api/v1/plans/"+up.ID+"/done", token, nil, nil); st != 200 {
			t.Fatalf("done %d", st)
		}
		time.Sleep(5 * time.Millisecond)
		return up.ID
	}
	plan("beetlebugorg/bittlex|refs/heads/main|"+strings.Repeat("ab", 20), false, cubeSTL(10, 10, 10))
	if len(app.comments) != 0 {
		t.Fatalf("a comment for a plan of main: %v", app.comments)
	}
	pr := "beetlebugorg/bittlex|refs/pull/7/merge|"
	first := plan(pr+strings.Repeat("cd", 20), true, cubeSTL(10, 10, 12))
	if len(app.comments) != 1 {
		t.Fatalf("comments %v", app.comments)
	}
	body := app.comments[100]
	for _, want := range []string{"beetlebugorg/bittlex#7\n", "<!-- printdeck -->", "1 of 2 parts changed, compared with main at `ababab", "/plans/" + first,
		"| Pod door | 2.0 mm taller · +0.20 cm³ (+20.0%) |", `<img src="` + base + `/gh/`} {
		if !strings.Contains(body, want) {
			t.Errorf("the comment has no %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "| Cover |") {
		t.Error("the comment lists a part with no change")
	}
	// The picture is a public file of the bucket.
	i := strings.Index(body, base+"/gh/")
	key := strings.TrimPrefix(body[i:i+len(base)+40], base+"/")
	if data, _, err := d.API.Store.Get(ctx, key); err != nil || len(data) < 1000 || string(data[1:4]) != "PNG" {
		t.Fatalf("the picture %s: %v", key, err)
	}
	// The next push changes the same comment.
	second := plan(pr+strings.Repeat("ef", 20), true, cubeSTL(12, 10, 12))
	if len(app.comments) != 1 || !strings.Contains(app.comments[100], "/plans/"+second) {
		t.Fatalf("comments after the next push %v", app.comments)
	}
}
