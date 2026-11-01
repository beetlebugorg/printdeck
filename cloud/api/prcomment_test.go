package api

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
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
	if !strings.Contains(app.comments[100], "2.0 mm wider") {
		t.Errorf("the comment of the second plan:\n%s", app.comments[100])
	}

	// A build of the plan: the comment has its plate, with a picture of the G-code and its state.
	var bd cloud.ProjectBuild
	req := cloud.BuildRequest{Plates: []cloud.PlateRequest{{Parts: []cloud.PlatePart{{Part: 0, Count: 2}}}}}
	if st := call(t, base, "POST", "/api/v1/projects/"+sum.ID+"/plans/"+second+"/builds", tok, req, &bd); st != 200 {
		t.Fatalf("build %d", st)
	}
	if !strings.Contains(app.comments[100], "): slicing") {
		t.Errorf("no slicing build in the comment")
	}
	gcode := []byte("G90\nM82\nG1 X10 Y10 Z0.2 E0\nG1 X30 Y10 E1\nG1 X30 Y30 E2\nG1 X10 Y30 Z0.4 E3\n")
	h := sha256.Sum256(gcode)
	token := pr + strings.Repeat("ef", 20)
	var up cloud.UploadReply
	files := cloud.BuildFiles{Plates: []cloud.BuildPlate{{File: "plate-1.gcode", Size: int64(len(gcode)), SHA256: hex.EncodeToString(h[:]),
		Seconds: 5400, Grams: 12}}}
	if st := call(t, base, "POST", "/api/v1/builds/"+bd.ID+"/files", token, files, &up); st != 200 {
		t.Fatalf("files %d", st)
	}
	cloud.PostFile(ctx, nil, up.Grant, "plate-1.gcode", "text/plain", "", gcode)
	if st := call(t, base, "POST", "/api/v1/builds/"+bd.ID+"/done", token, nil, nil); st != 200 {
		t.Fatalf("build done %d", st)
	}
	body = app.comments[100]
	for _, want := range []string{"[Build `" + bd.ID + "`](", "): 1 plate", "| 1: Pod door x2 | 1:30 |", "12 g", "Waits for approval", `width="160"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the comment of the build has no %q:\n%s", want, body)
		}
	}
	if st := call(t, base, "POST", "/api/v1/projects/"+sum.ID+"/builds/"+bd.ID+"/plates/0/approve", tok, nil, nil); st != 200 {
		t.Fatalf("approve %d", st)
	}
	if !strings.Contains(app.comments[100], "| Approved |") {
		t.Errorf("the comment after the approval:\n%s", app.comments[100])
	}

	// The deck prints the plate: the comment has the result, then the first photo of the print.
	ended := time.Date(2026, 11, 2, 15, 0, 0, 0, time.UTC)
	up2 := cloud.PrintUpload{Request: cloud.PrintRequest{Name: "Pod door x2", File: "builds/" + bd.ID + "-plate-1.gcode",
		Result: "complete", Ended: ended, Seconds: 5000, Key: "k@1"}}
	pid, err := c.SendPrint(ctx, l.Identity(), sum.ID, up2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(app.comments[100], "| Printed Nov 2 |") {
		t.Fatalf("the comment after the print:\n%s", app.comments[100])
	}
	var ph cloud.PhotoReply
	if st := call(t, base, "POST", "/api/v1/projects/"+sum.ID+"/photos", tok, nil, &ph); st != 200 {
		t.Fatalf("photo grant: %d", st)
	}
	cloud.PostFile(ctx, nil, ph.Grant, "photo", "image/jpeg", "no-store", exifJPEG(t, 64, 48, 1))
	if st := call(t, base, "POST", "/api/v1/projects/"+sum.ID+"/photos/"+ph.Photo+"/done", tok, map[string]string{"print": pid}, nil); st != 200 {
		t.Fatalf("photo done: %d", st)
	}
	body = app.comments[100]
	if !strings.Contains(body, `alt="The print of plate 1"`) || !strings.Contains(body, base+"/gh/") {
		t.Fatalf("no photo in the comment:\n%s", body)
	}
	i = strings.Index(body, `alt="The print of plate 1"`)
	j := strings.LastIndex(body[:i], base+"/gh/")
	key = strings.TrimPrefix(body[j:j+len(base)+40], base+"/")
	if data, _, err := d.API.Store.Get(ctx, key); err != nil || len(data) < 100 {
		t.Fatalf("the public photo %s: %v", key, err)
	}

	// The project page has the state of the pull request, and asks GitHub again after a time.
	prState := func() string {
		p, _, err := d.API.loadProject(ctx, sum.ID)
		if err != nil {
			t.Fatal(err)
		}
		v, err := d.API.viewProject(ctx, p, &who{owner: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, pl := range v.Plans {
			if pl.PR == 7 {
				return pl.PRState
			}
		}
		return "none"
	}
	if st := prState(); st != "open" {
		t.Fatalf("state %q", st)
	}
	app.states = map[int]string{7: "closed"}
	if st := prState(); st != "open" {
		t.Fatalf("state %q before the next check", st)
	}
	d.API.Now = func() time.Time { return time.Now().Add(prCheckOpen + time.Minute) }
	defer func() { d.API.Now = nil }()
	if st := prState(); st != "closed" {
		t.Fatalf("state %q after the next check", st)
	}
}
