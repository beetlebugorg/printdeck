package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"printdeck/internal/cloud"
)

// The plan run slices each part. A plate with one part and a count of 1 gets the file of the
// plan, and the slice run slices only the other plates. A build of only such plates is ready at
// once. A part that does not slice cannot go on a plate.
func TestPlanSlice(t *testing.T) {
	d, base := devService(t)
	base = strings.Replace(base, "127.0.0.1", "localhost", 1)
	d.API.Base, d.Grants.URL = base, base+"/upload"
	d.API.OIDC = fakeOIDC{}
	ctx := context.Background()
	l := connectedLink(t, d, base)
	var c cloud.Client
	var oc cloud.OwnerCodeReply
	if err := c.Do(ctx, l.Identity(), "POST", "/api/v1/owner/code", nil, &oc); err != nil {
		t.Fatal(err)
	}
	tok := register(t, base, newSoftKey(t), "owner", oc.Code, "Jeremy").Token
	sum, err := c.NewProject(ctx, l.Identity(), "Mini")
	if err != nil {
		t.Fatal(err)
	}
	if st := call(t, base, "PUT", "/api/v1/projects/"+sum.ID+"/repo", tok, cloud.RepoRequest{Repo: "beetlebugorg/mini"}, nil); st != 200 {
		t.Fatalf("link: %d", st)
	}

	gcode := []byte(";TIME:2629\nG1 X10\n")
	h := sha256.Sum256(gcode)
	sum256 := hex.EncodeToString(h[:])
	sha := strings.Repeat("ab", 20)
	token := "beetlebugorg/mini|refs/heads/main|" + sha
	part := func(name, file string) cloud.PlanPart {
		return cloud.PlanPart{Name: name, Files: []string{"stl/" + name + ".3mf"}, Printer: "taz", Process: "fdm",
			Profile: "taz-workhorse-se/pla-0.25-grille@1", Material: "white", Models: []string{name + ".stl"},
			Seconds: 2629, Grams: 8.5, Layers: 16, LayerHeight: 0.25, Slicer: "CuraEngine 5.13.0",
			SliceFile: file, SliceSize: int64(len(gcode)), SliceSHA256: sum256}
	}
	grille, strip, knob := part("grille", "part1.gcode"), part("strip", "part2.gcode"), part("knob", "part3.gcode")
	knob.SliceFile = "../part3.gcode" // a bad name: the knob slices with its plate
	broken := part("broken", "")
	broken.SliceError = "CuraEngine failed: bad value"
	manifest := cloud.PlanManifest{Commit: sha, Parts: []cloud.PlanPart{grille, strip, knob, broken}}

	clash := manifest
	a := part("a", "a.gcode")
	a.Models = []string{"a.gcode"}
	clash.Parts = []cloud.PlanPart{a}
	if st := call(t, base, "POST", "/api/v1/plans", token, clash, nil); st != 400 {
		t.Errorf("a slice file with the name of a model: %d", st)
	}

	var up cloud.UploadReply
	if st := call(t, base, "POST", "/api/v1/plans", token, manifest, &up); st != 200 {
		t.Fatalf("plan: %d", st)
	}
	// The plan run sends the G-code of the grille. The file of the strip is not up.
	for _, f := range []string{"grille.stl", "strip.stl", "knob.stl", "broken.stl", "part1.gcode", "stray.gcode"} {
		if err := cloud.PostFile(ctx, nil, up.Grant, f, "application/octet-stream", "", gcode); err != nil {
			t.Fatal(err)
		}
	}
	var pv cloud.ProjectPlan
	if st := call(t, base, "POST", "/api/v1/plans/"+up.ID+"/done", token, nil, &pv); st != 200 {
		t.Fatalf("done: %d", st)
	}
	g, s2, k, b := pv.Parts[0], pv.Parts[1], pv.Parts[2], pv.Parts[3]
	if g.SliceFile != "part1.gcode" || g.Seconds != 2629 || g.Grams != 8.5 || g.Layers != 16 || g.Slicer != "CuraEngine 5.13.0" {
		t.Errorf("the grille: %+v", g.PlanPart)
	}
	if s2.SliceFile != "" || s2.Seconds != 2629 {
		t.Errorf("the strip with no file up: %+v", s2.PlanPart)
	}
	if k.SliceFile != "" {
		t.Errorf("a bad slice file name: %q", k.SliceFile)
	}
	if b.SliceError == "" || b.SliceFile != "" {
		t.Errorf("the broken part: %+v", b.PlanPart)
	}
	have, err := d.API.filesIn(ctx, planDir(sum.ID, up.ID))
	if err != nil || !have["part1.gcode"] || have["stray.gcode"] {
		t.Errorf("the files of the plan: %v %v", have, err)
	}

	builds := "/api/v1/projects/" + sum.ID + "/plans/" + up.ID + "/builds"
	req := func(groups ...[]cloud.PlatePart) cloud.BuildRequest {
		var r cloud.BuildRequest
		for _, gr := range groups {
			r.Plates = append(r.Plates, cloud.PlateRequest{Parts: gr})
		}
		return r
	}
	if st := call(t, base, "POST", builds, tok, req([]cloud.PlatePart{{Part: 3, Count: 1}}), nil); st != 400 {
		t.Errorf("a part that does not slice on a plate: %d", st)
	}

	// The grille alone: the file of the plan, and no slice run.
	var ready cloud.ProjectBuild
	if st := call(t, base, "POST", builds, tok, req([]cloud.PlatePart{{Part: 0, Count: 1}}), &ready); st != 200 {
		t.Fatalf("build: %d", st)
	}
	if ready.State != cloud.BuildReady || ready.Plates[0].State != cloud.PartWaiting || ready.Plates[0].File != "plate-1.gcode" ||
		!ready.Plates[0].Reused || ready.Plates[0].Seconds != 2629 || ready.Plates[0].SHA256 != sum256 {
		t.Fatalf("a build of the grille alone: %+v", ready)
	}
	if got, _, err := d.API.Store.Get(ctx, buildDir(sum.ID, ready.ID)+"plate-1.gcode"); err != nil || string(got) != string(gcode) {
		t.Errorf("the copy of the G-code: %q %v", got, err)
	}

	// Two grilles, the grille alone and the strip: the slice run slices plates 1 and 3.
	var pb cloud.ProjectBuild
	if st := call(t, base, "POST", builds, tok, req([]cloud.PlatePart{{Part: 0, Count: 2}}, []cloud.PlatePart{{Part: 0, Count: 1}},
		[]cloud.PlatePart{{Part: 1, Count: 1}}), &pb); st != 200 || pb.State != cloud.BuildSlicing {
		t.Fatalf("build: %d %+v", st, pb)
	}
	var job cloud.BuildJob
	if st := call(t, base, "GET", "/api/v1/builds/"+pb.ID, token, nil, &job); st != 200 || len(job.Plates) != 2 ||
		job.Plates[0].Number != 1 || job.Plates[1].Number != 3 || job.Plates[0].Parts[0].Count != 2 {
		t.Fatalf("job: %d %+v", st, job)
	}
	entry := func(name string) cloud.BuildPlate {
		return cloud.BuildPlate{File: name, Size: int64(len(gcode)), SHA256: sum256, Seconds: 5000}
	}
	if st := call(t, base, "POST", "/api/v1/builds/"+pb.ID+"/files", token,
		cloud.BuildFiles{Plates: []cloud.BuildPlate{entry("plate-1.gcode"), entry("plate-2.gcode"), entry("plate-3.gcode")}}, nil); st != 400 {
		t.Errorf("an entry for the reused plate: %d", st)
	}
	if st := call(t, base, "POST", "/api/v1/builds/"+pb.ID+"/files", token,
		cloud.BuildFiles{Plates: []cloud.BuildPlate{entry("plate-1.gcode"), entry("plate-2.gcode")}}, nil); st != 400 {
		t.Errorf("the file name of the reused plate: %d", st)
	}
	if st := call(t, base, "POST", "/api/v1/builds/"+pb.ID+"/files", token,
		cloud.BuildFiles{Plates: []cloud.BuildPlate{entry("plate-1.gcode"), entry("plate-3.gcode")}}, &up); st != 200 {
		t.Fatalf("files: %d", st)
	}
	for _, f := range []string{"plate-1.gcode", "plate-3.gcode"} {
		if err := cloud.PostFile(ctx, nil, up.Grant, f, "application/octet-stream", "", gcode); err != nil {
			t.Fatal(err)
		}
	}
	if st := call(t, base, "POST", "/api/v1/builds/"+pb.ID+"/done", token, nil, &pb); st != 200 || pb.State != cloud.BuildReady {
		t.Fatalf("done: %d %+v", st, pb)
	}
	if pb.Plates[0].Seconds != 5000 || pb.Plates[0].Reused || !pb.Plates[1].Reused || pb.Plates[1].Seconds != 2629 ||
		pb.Plates[1].File != "plate-2.gcode" || pb.Plates[2].File != "plate-3.gcode" {
		t.Errorf("the plates: %+v", pb.Plates)
	}
}
