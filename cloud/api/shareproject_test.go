package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"printdeck/internal/cloud"
)

// A share of a print in a project: the share page shows the project, the project page shows the
// share while the print runs, the photos of the share go to the print, and after the end the
// share page sends the visitor to the print page.
func TestShareInProject(t *testing.T) {
	d, url := devService(t)
	ctx := context.Background()
	l := connectedLink(t, d, url)
	id := l.Identity()
	var c cloud.Client
	sum, err := c.NewProject(ctx, id, "Garden planters")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	state, project := "printing", sum.ID
	sh := &cloud.Sharer{Link: l, Every: 20 * time.Millisecond,
		Status: func() cloud.ShareStatus {
			mu.Lock()
			defer mu.Unlock()
			return cloud.ShareStatus{State: state, Job: "Pot"}
		},
		Project: func(context.Context) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			return project, nil
		}}
	sh.Start("Pot", "Workhorse", nil)
	waitFor(t, func() bool { return sh.State().URL != "" })
	shareURL, sid := sh.State().URL, sh.State().ID
	ready := func() cloud.ShareReady {
		var rd cloud.ShareReady
		_, b := get(t, shareURL+"/ready.json")
		json.Unmarshal(b, &rd)
		return rd
	}
	if rd := ready(); rd.Project == nil || rd.Project.Name != "Garden planters" || rd.Project.URL != url+"/projects/garden-planters" {
		t.Fatalf("the project on the share page: %+v", rd.Project)
	}
	live := func() []cloud.ProjectLive {
		p, _, err := d.API.loadProject(ctx, sum.ID)
		if err != nil {
			t.Fatal(err)
		}
		v, err := d.API.viewProject(ctx, p, &who{owner: true})
		if err != nil {
			t.Fatal(err)
		}
		return v.Live
	}
	if lv := live(); len(lv) != 1 || lv[0].URL != shareURL || lv[0].Name != "Pot" {
		t.Fatalf("live %+v", lv)
	}

	// The print leaves the project, and comes back.
	mu.Lock()
	project = ""
	mu.Unlock()
	waitFor(t, func() bool { return ready().Project == nil })
	if lv := live(); len(lv) != 0 {
		t.Fatalf("live with no project %+v", lv)
	}
	mu.Lock()
	project = sum.ID
	mu.Unlock()
	waitFor(t, func() bool { return ready().Project != nil })

	// A photo of the share, before the print is in the project.
	owner := sh.State().OwnerURL
	key := owner[strings.Index(owner, "#owner=")+7:]
	post := func(path string) []byte {
		req, _ := http.NewRequest("POST", url+"/api/v1/shares/"+sid+path, bytes.NewReader(nil))
		req.Header.Set(cloud.HeaderShareKey, key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var b bytes.Buffer
		b.ReadFrom(resp.Body)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, b.String())
		}
		return b.Bytes()
	}
	addPhoto := func() {
		var pr cloud.PhotoReply
		json.Unmarshal(post("/photo"), &pr)
		if err := cloud.PostFile(ctx, nil, pr.Grant, "photo", "image/jpeg", "", exifJPEG(t, 40, 20, 1)); err != nil {
			t.Fatal(err)
		}
		post("/photo/" + pr.Photo + "/done")
	}
	addPhoto()

	// The end of the print. The deck adds the print to the project with the share.
	mu.Lock()
	state = "done"
	mu.Unlock()
	waitFor(t, func() bool { return sh.State().Ended })
	up := cloud.PrintUpload{Model: []byte("model"), Request: cloud.PrintRequest{Name: "Pot", Result: "complete",
		Ended: time.Now().UTC(), Seconds: 60, Key: "pot@1", Share: sid}}
	pid, err := c.SendPrint(ctx, id, sum.ID, up)
	if err != nil {
		t.Fatal(err)
	}
	if lv := live(); len(lv) != 0 {
		t.Errorf("live after the end %+v", lv)
	}
	if rd := ready(); rd.Project == nil || rd.Project.URL != url+"/projects/garden-planters/1" {
		t.Fatalf("the print on the share page: %+v", rd.Project)
	}
	addPhoto() // a photo after the print is in the project goes there too
	_, photos, _ := d.API.projectFiles(ctx, sum.ID)
	if len(photos) != 2 || photos[0].Print != pid || photos[1].Print != pid {
		t.Fatalf("the photos of the print %+v", photos)
	}

	// After the end of the share, its page sends the visitor to the print page.
	d.API.Now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	if err := d.API.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if code, body := get(t, shareURL); code != 200 || !strings.Contains(string(body), "/projects/garden-planters/1") {
		t.Fatalf("the moved page: %d %s", code, body)
	}
	for _, p := range []string{"/ready.json", "/photos/"} {
		if keys, _ := d.Store.List(ctx, "s/"+sid+p); len(keys) != 0 {
			t.Errorf("files of the share after the move: %v", keys)
		}
	}
	if err := d.API.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if code, _ := get(t, shareURL); code != 200 {
		t.Fatalf("a second sweep: %d", code)
	}
	_, photos, _ = d.API.projectFiles(ctx, sum.ID)
	if len(photos) != 2 {
		t.Fatalf("the photos after the move: %d", len(photos))
	}

	// With a public address, the page sends the visitor to the public print page.
	d.Store.Put(ctx, PublicPage, []byte("<!doctype html><title>Project · Printdeck</title><!-- preview -->"), PutOptions{ContentType: "text/html"})
	pub, err := d.API.setPublic(ctx, sum.ID, cloud.PublicRequest{On: true})
	if err != nil {
		t.Fatal(err)
	}
	pubURL := pub.(cloud.PublicReply).URL
	if _, body := get(t, shareURL); !strings.Contains(string(body), pubURL+"/1") {
		t.Fatalf("the moved page with a public address: %s", body)
	}

	// The delete of the project deletes the moved share.
	p, _, _ := d.API.loadProject(ctx, sum.ID)
	if err := d.API.deleteProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	if code, _ := get(t, shareURL); code != 404 {
		t.Errorf("the share page after the delete of the project: %d", code)
	}
	if keys, _ := d.Store.List(ctx, "shares/"); len(keys) != 0 {
		t.Errorf("share records after the delete of the project: %v", keys)
	}
}

// A deck that lost its share at a restart sends a live tile with no share. The share and the live
// tile of the same deck and job are one tile, with the link of the share.
func TestShareAndLiveOfOneDeck(t *testing.T) {
	d, url := devService(t)
	ctx := context.Background()
	l := connectedLink(t, d, url)
	id := l.Identity()
	var c cloud.Client
	sum, err := c.NewProject(ctx, id, "Robot")
	if err != nil {
		t.Fatal(err)
	}
	sh := &cloud.Sharer{Link: l, Every: 20 * time.Millisecond,
		Status:  func() cloud.ShareStatus { return cloud.ShareStatus{State: "printing", Job: "Plate"} },
		Project: func(context.Context) (string, error) { return sum.ID, nil }}
	sh.Start("Plate", "Phenom", nil)
	waitFor(t, func() bool { return sh.State().URL != "" })
	defer sh.Stop()
	in := cloud.LiveRequest{Key: "resin-1@1", Name: "Plate", Started: time.Now().UTC(), Percent: 5}
	if _, err := c.SendLive(ctx, id, sum.ID, in); err != nil {
		t.Fatal(err)
	}
	p, _, err := d.API.loadProject(ctx, sum.ID)
	if err != nil {
		t.Fatal(err)
	}
	v, err := d.API.viewProject(ctx, p, &who{owner: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Live) != 1 || v.Live[0].URL != sh.State().URL || v.Live[0].Percent != 5 {
		t.Fatalf("live %+v", v.Live)
	}
}

// An old share of a deck whose live tile names its new share shows no tile, also with another
// name.
func TestOldShareOfDeck(t *testing.T) {
	d, url := devService(t)
	ctx := context.Background()
	l := connectedLink(t, d, url)
	id := l.Identity()
	var c cloud.Client
	sum, err := c.NewProject(ctx, id, "Robot")
	if err != nil {
		t.Fatal(err)
	}
	start := func(job string) *cloud.Sharer {
		sh := &cloud.Sharer{Link: l, Every: 20 * time.Millisecond,
			Status:  func() cloud.ShareStatus { return cloud.ShareStatus{State: "printing", Job: job} },
			Project: func(context.Context) (string, error) { return sum.ID, nil }}
		sh.Start(job, "Phenom", nil)
		waitFor(t, func() bool { return sh.State().URL != "" })
		return sh
	}
	old := start("plate_file_name")
	old.Stop() // the deck lost it at a restart: the share did not end
	cur := start("Shoulder pieces")
	defer cur.Stop()
	in := cloud.LiveRequest{Key: "resin-1@1", Name: "Shoulder pieces", Started: time.Now().UTC(), Percent: 5, Share: cur.State().ID}
	if _, err := c.SendLive(ctx, id, sum.ID, in); err != nil {
		t.Fatal(err)
	}
	p, _, err := d.API.loadProject(ctx, sum.ID)
	if err != nil {
		t.Fatal(err)
	}
	v, err := d.API.viewProject(ctx, p, &who{owner: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Live) != 1 || v.Live[0].URL != cur.State().URL {
		t.Fatalf("live %+v", v.Live)
	}
}

// Two open shares of one deck and no live tile: the deck lost the older share at a restart. Only
// the newest share shows.
func TestNewestShareOfDeck(t *testing.T) {
	d, url := devService(t)
	ctx := context.Background()
	l := connectedLink(t, d, url)
	id := l.Identity()
	var c cloud.Client
	sum, err := c.NewProject(ctx, id, "Robot")
	if err != nil {
		t.Fatal(err)
	}
	start := func() *cloud.Sharer {
		sh := &cloud.Sharer{Link: l, Every: 20 * time.Millisecond,
			Status:  func() cloud.ShareStatus { return cloud.ShareStatus{State: "printing", Job: "Plate"} },
			Project: func(context.Context) (string, error) { return sum.ID, nil }}
		sh.Start("Plate", "Phenom", nil)
		waitFor(t, func() bool { return sh.State().URL != "" })
		return sh
	}
	old := start() // the deck lost it at a restart: the share did not end
	defer old.Stop()
	time.Sleep(10 * time.Millisecond)
	cur := start()
	defer cur.Stop()
	p, _, err := d.API.loadProject(ctx, sum.ID)
	if err != nil {
		t.Fatal(err)
	}
	v, err := d.API.viewProject(ctx, p, &who{owner: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Live) != 1 || v.Live[0].URL != cur.State().URL {
		t.Fatalf("live %+v", v.Live)
	}
}
