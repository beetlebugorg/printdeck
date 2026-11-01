package api

import (
	"context"
	"testing"
	"time"

	"printdeck/internal/cloud"
)

// A deck reports its queue. Each project sees only its own files, with their places and their
// start times; a project that leaves the queue loses its files; another deck cannot report the
// queue of the printer.
func TestQueueOfProjects(t *testing.T) {
	d, url := devService(t)
	ctx := context.Background()
	id := connectedLink(t, d, url).Identity()
	var c cloud.Client
	taz, err := c.RegisterPrinter(ctx, id, cloud.PrinterRequest{Key: "main", Name: "TAZ", Process: "fdm"})
	if err != nil {
		t.Fatal(err)
	}
	robot, _ := c.NewProject(ctx, id, "Robot dog")
	vase, _ := c.NewProject(ctx, id, "Vase")
	start := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	report := cloud.QueueReport{Items: []cloud.QueueEntry{
		{Project: robot.ID, Name: "Pod door", Place: 1, Seconds: 2700, Start: start},
		{Project: vase.ID, Name: "Vase", Place: 2, Seconds: 9000, Start: start.Add(45 * time.Minute)},
		{Project: robot.ID, Name: "Cover", Place: 3, Seconds: 600, Start: start.Add(3 * time.Hour)},
	}}
	if err := c.SendQueue(ctx, id, taz.ID, report); err != nil {
		t.Fatal(err)
	}
	queued := func(project string) []cloud.ProjectQueued {
		p, _, err := d.API.loadProject(ctx, project)
		if err != nil {
			t.Fatal(err)
		}
		v, err := d.API.viewProject(ctx, p, &who{owner: true})
		if err != nil {
			t.Fatal(err)
		}
		return v.Queue
	}
	q := queued(robot.ID)
	if len(q) != 2 || q[0].Name != "Pod door" || q[0].Place != 1 || q[1].Name != "Cover" || q[1].Place != 3 || q[0].Printer != "TAZ" {
		t.Fatalf("robot %+v", q)
	}
	if q := queued(vase.ID); len(q) != 1 || q[0].Name != "Vase" {
		t.Fatalf("vase %+v", q)
	}
	// The vase printed: the next report has the robot only.
	report.Items = report.Items[:1]
	if err := c.SendQueue(ctx, id, taz.ID, report); err != nil {
		t.Fatal(err)
	}
	if q := queued(vase.ID); len(q) != 0 {
		t.Fatalf("vase after it printed %+v", q)
	}
	// A printer of another deck.
	other := connectedLink(t, d, url).Identity()
	if err := c.SendQueue(ctx, other, taz.ID, report); err == nil {
		t.Error("another deck reported the queue")
	}
}

// The owner takes a file out of the queue on the project page; the deck of the printer reads the
// removal, takes the file out, and says so.
func TestQueueRemove(t *testing.T) {
	d, url := devService(t)
	ctx := context.Background()
	id := connectedLink(t, d, url).Identity()
	var c cloud.Client
	taz, _ := c.RegisterPrinter(ctx, id, cloud.PrinterRequest{Key: "main", Name: "TAZ", Process: "fdm"})
	robot, _ := c.NewProject(ctx, id, "Robot dog")
	vase, _ := c.NewProject(ctx, id, "Vase")
	report := cloud.QueueReport{Items: []cloud.QueueEntry{
		{ID: "a1b2c3d4e5f6", Project: robot.ID, Name: "Pod panel", Place: 1},
		{ID: "0f0f0f0f0f0f", Project: vase.ID, Name: "Vase", Place: 2},
		{Project: robot.ID, Name: "From an older deck", Place: 3},
	}}
	if err := c.SendQueue(ctx, id, taz.ID, report); err != nil {
		t.Fatal(err)
	}
	owner := &who{owner: true, name: "Jeremy"}
	view := func() []cloud.ProjectQueued {
		p, _, _ := d.API.loadProject(ctx, robot.ID)
		v, err := d.API.viewProject(ctx, p, owner)
		if err != nil {
			t.Fatal(err)
		}
		return v.Queue
	}
	q := view()
	if len(q) != 2 || q[0].ID != "a1b2c3d4e5f6" || q[0].PrinterID != taz.ID || q[0].Removing || q[1].ID != "" {
		t.Fatalf("queue %+v", q)
	}
	// A file of another project, a file of no report, and a bad ID.
	for _, item := range []string{"0f0f0f0f0f0f", "ffffffffffff", "../x"} {
		if err := d.API.removeQueued(ctx, robot.ID, taz.ID, item, owner); err == nil {
			t.Errorf("removed %s", item)
		}
	}
	if err := d.API.removeQueued(ctx, robot.ID, taz.ID, "a1b2c3d4e5f6", owner); err != nil {
		t.Fatal(err)
	}
	if q := view(); !q[0].Removing {
		t.Fatalf("no removal on the page: %+v", q[0])
	}
	// Another deck cannot read the removals of the printer.
	other := connectedLink(t, d, url).Identity()
	if _, err := c.QueueRemoves(ctx, other, taz.ID); err == nil {
		t.Error("another deck read the removals")
	}
	rms, err := c.QueueRemoves(ctx, id, taz.ID)
	if err != nil || len(rms) != 1 || rms[0].Item != "a1b2c3d4e5f6" || rms[0].Project != robot.ID || rms[0].By != "Jeremy" {
		t.Fatalf("removals %+v %v", rms, err)
	}
	if err := c.QueueRemovesDone(ctx, id, taz.ID, []string{"a1b2c3d4e5f6"}); err != nil {
		t.Fatal(err)
	}
	if rms, _ := c.QueueRemoves(ctx, id, taz.ID); len(rms) != 0 {
		t.Fatalf("removals after done %+v", rms)
	}
	if q := view(); len(q) != 1 || q[0].Name != "From an older deck" {
		t.Fatalf("queue after the removal %+v", q)
	}
}
