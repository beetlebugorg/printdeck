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
