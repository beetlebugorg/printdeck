package api

import (
	"context"
	"testing"
	"time"

	"printdeck/internal/cloud"
)

// A print of a deck in a project: the project shows its progress and its picture, then its result
// after the end, until the print comes to the project.
func TestLivePrint(t *testing.T) {
	d, url := devService(t)
	ctx := context.Background()
	l := connectedLink(t, d, url)
	id := l.Identity()
	var c cloud.Client
	sum, err := c.NewProject(ctx, id, "Robot dog")
	if err != nil {
		t.Fatal(err)
	}
	live := func() []cloud.ProjectLive {
		p, _, err := d.API.loadProject(ctx, sum.ID)
		if err != nil {
			t.Fatal(err)
		}
		v, err := d.API.viewProject(ctx, p, &who{member: "m1"})
		if err != nil {
			t.Fatal(err)
		}
		return v.Live
	}
	start := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	in := cloud.LiveRequest{Key: "dog/leg.gcode@1", Name: "Leg", Started: start, Percent: 40, Layer: 80, Layers: 200,
		Left: 5400, Printer: "LulzBot TAZ Workhorse"}
	out, err := c.SendLive(ctx, id, sum.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Grant == nil {
		t.Fatal("no grant for the picture")
	}
	if lv := live(); len(lv) != 1 || lv[0].Name != "Leg" || lv[0].Percent != 40 || lv[0].Model != "" || lv[0].Left != 5400 {
		t.Fatalf("live %+v", lv)
	}

	// The picture of the model goes up once.
	if err := cloud.PostFile(ctx, nil, *out.Grant, "model.jpg", "image/jpeg", "", []byte("jpeg")); err != nil {
		t.Fatal(err)
	}
	in.Model = true
	if out, err = c.SendLive(ctx, id, sum.ID, in); err != nil || out.Grant != nil {
		t.Fatalf("the second send: %+v %v", out, err)
	}
	in.Model, in.Percent = false, 60 // a deck that started again
	if out, err = c.SendLive(ctx, id, sum.ID, in); err != nil || out.Grant != nil {
		t.Fatalf("the send after a start of the deck: %+v %v", out, err)
	}
	if lv := live(); len(lv) != 1 || lv[0].Model == "" || lv[0].Percent != 60 {
		t.Fatalf("live with the picture %+v", lv)
	}

	// A deck that stops sending: the print goes from the page after liveStale.
	d.API.Now = func() time.Time { return time.Now().Add(liveStale + time.Minute) }
	if lv := live(); len(lv) != 0 {
		t.Fatalf("a stale live print %+v", lv)
	}
	d.API.Now = nil

	// The end: the result shows until the print is in the project.
	in.Result, in.Percent = "complete", 100
	if _, err := c.SendLive(ctx, id, sum.ID, in); err != nil {
		t.Fatal(err)
	}
	d.API.Now = func() time.Time { return time.Now().Add(liveStale + time.Minute) }
	if lv := live(); len(lv) != 1 || lv[0].Result != "complete" {
		t.Fatalf("live after the end %+v", lv)
	}
	d.API.Now = nil
	up := cloud.PrintUpload{Request: cloud.PrintRequest{Name: "Leg", Result: "complete", Started: start.Add(time.Minute),
		Ended: time.Now().UTC(), Seconds: 3600, Key: "dog/leg.gcode@2"}}
	if _, err := c.SendPrint(ctx, id, sum.ID, up); err != nil {
		t.Fatal(err)
	}
	if lv := live(); len(lv) != 0 {
		t.Fatalf("live after the print came %+v", lv)
	}
	if rec, err := d.API.loadLive(ctx, sum.ID, id.Deck); err != nil || rec != nil {
		t.Fatalf("the live record after the print came: %+v %v", rec, err)
	}

	// A print that leaves the project.
	in = cloud.LiveRequest{Key: "dog/head.gcode@3", Name: "Head", Started: time.Now().UTC()}
	if _, err := c.SendLive(ctx, id, sum.ID, in); err != nil {
		t.Fatal(err)
	}
	if err := c.EndLive(ctx, id, sum.ID); err != nil {
		t.Fatal(err)
	}
	if lv := live(); len(lv) != 0 {
		t.Fatalf("live after the end of the live print %+v", lv)
	}
}
