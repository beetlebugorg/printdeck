package api

import (
	"context"
	"testing"
	"time"

	"printdeck/internal/cloud"
)

// A deck registers its printers: the same key keeps the ID. The prints and the live tiles of a
// project have their printer, and two printers of one deck have two live tiles in one project.
func TestPrintersOfProject(t *testing.T) {
	d, url := devService(t)
	ctx := context.Background()
	id := connectedLink(t, d, url).Identity()
	var c cloud.Client

	taz, err := c.RegisterPrinter(ctx, id, cloud.PrinterRequest{Key: "main", Name: "TAZ", Process: "fdm", Model: "LulzBot TAZ Workhorse"})
	if err != nil || taz.ID == "" || taz.Deck != id.Deck {
		t.Fatalf("register %+v %v", taz, err)
	}
	again, err := c.RegisterPrinter(ctx, id, cloud.PrinterRequest{Key: "main", Name: "Workhorse", Process: "fdm"})
	if err != nil || again.ID != taz.ID || again.Name != "Workhorse" {
		t.Fatalf("the same key again %+v %v", again, err)
	}
	phenom, err := c.RegisterPrinter(ctx, id, cloud.PrinterRequest{Key: "phenom", Name: "Phenom", Process: "resin"})
	if err != nil || phenom.ID == taz.ID {
		t.Fatalf("a second printer %+v %v", phenom, err)
	}
	if _, err := c.RegisterPrinter(ctx, id, cloud.PrinterRequest{Key: "x", Name: "X", Process: "laser"}); err == nil {
		t.Error("a printer with a bad process")
	}
	var list []cloud.Printer
	if err := c.Do(ctx, id, "GET", "/api/v1/printers", nil, &list); err != nil || len(list) != 2 {
		t.Fatalf("the printers %+v %v", list, err)
	}

	// The projects of the service, for each deck.
	sum, err := c.NewProject(ctx, id, "Robot dog")
	if err != nil {
		t.Fatal(err)
	}
	if ps, err := c.Projects(ctx, id); err != nil || len(ps) != 1 || ps[0].ID != sum.ID {
		t.Fatalf("the projects %+v %v", ps, err)
	}

	// Two printers print for the project at the same time: two live tiles.
	start := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	for _, p := range []cloud.Printer{again, phenom} {
		in := cloud.LiveRequest{Key: p.Name + "@1", Name: "Part on " + p.Name, Started: start, Percent: 20, PrinterID: p.ID}
		if _, err := c.SendLive(ctx, id, sum.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	view := func() *cloud.ProjectView {
		p, _, err := d.API.loadProject(ctx, sum.ID)
		if err != nil {
			t.Fatal(err)
		}
		v, err := d.API.viewProject(ctx, p, &who{owner: true})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	v := view()
	if len(v.Live) != 2 || len(v.Printers) != 2 {
		t.Fatalf("live %+v printers %+v", v.Live, v.Printers)
	}
	for _, p := range v.Printers {
		if p.Deck != id.Deck || p.Key == "" {
			t.Errorf("the printer of the view of the owner %+v", p)
		}
	}

	// The print of the Phenom comes to the project and ends its live tile; the TAZ tile stays.
	up := cloud.PrintUpload{FPS: 1, Request: cloud.PrintRequest{Name: "Part on Phenom", Result: "complete", Ended: time.Now().UTC(),
		Started: start, Seconds: 3600, Key: "phenom@1", PrinterID: phenom.ID}}
	if _, err := c.SendPrint(ctx, id, sum.ID, up); err != nil {
		t.Fatal(err)
	}
	v = view()
	if len(v.Prints) != 1 || v.Prints[0].PrinterID != phenom.ID {
		t.Fatalf("prints %+v", v.Prints)
	}
	if len(v.Live) != 1 || v.Live[0].PrinterID != again.ID {
		t.Fatalf("live after the print of the Phenom %+v", v.Live)
	}
	if err := c.EndPrinterLive(ctx, id, sum.ID, again.ID); err != nil {
		t.Fatal(err)
	}
	if v = view(); len(v.Live) != 0 {
		t.Fatalf("live after the end %+v", v.Live)
	}

	// A deck cannot send for a printer of another deck: the print has no printer.
	other := connectedLink(t, d, url).Identity()
	up.Request.Key, up.Request.Name = "other@1", "Other"
	if _, err := c.SendPrint(ctx, other, sum.ID, up); err != nil {
		t.Fatal(err)
	}
	for _, pr := range view().Prints {
		if pr.Name == "Other" && pr.PrinterID != "" {
			t.Errorf("a print with the printer of another deck: %+v", pr)
		}
	}
	if err := c.EndPrinterLive(ctx, other, sum.ID, phenom.ID); err == nil {
		t.Error("another deck ended the live tile of a printer")
	}
}

// A deck sends what its printer does now. The list of the printers of the owner has it, and the
// view of a project does not.
func TestPrinterLive(t *testing.T) {
	d, url := devService(t)
	ctx := context.Background()
	id := connectedLink(t, d, url).Identity()
	var c cloud.Client
	taz, _ := c.RegisterPrinter(ctx, id, cloud.PrinterRequest{Key: "main", Name: "TAZ", Process: "fdm"})
	in := cloud.PrinterLive{Printing: true, Name: "Calibration cube", Percent: 42, Layer: 10, Layers: 50, Left: 900, Queue: 2, QueueSeconds: 3600}
	if err := c.SendPrinterLive(ctx, id, taz.ID, in); err != nil {
		t.Fatal(err)
	}
	var list []cloud.Printer
	if err := c.Do(ctx, id, "GET", "/api/v1/printers", nil, &list); err != nil || len(list) != 1 || list[0].Live == nil {
		t.Fatalf("printers %+v %v", list, err)
	}
	if l := list[0].Live; l.Name != "Calibration cube" || l.Percent != 42 || l.Queue != 2 || l.Updated.IsZero() {
		t.Fatalf("live %+v", l)
	}
	other := connectedLink(t, d, url).Identity()
	if err := c.SendPrinterLive(ctx, other, taz.ID, in); err == nil {
		t.Error("another deck sent the state of the printer")
	}
	sum, _ := c.NewProject(ctx, id, "Robot dog")
	p, _, _ := d.API.loadProject(ctx, sum.ID)
	v, _ := d.API.viewProject(ctx, p, &who{owner: true})
	for _, pr := range v.Printers {
		if pr.Live != nil {
			t.Error("a project view has the state of a printer")
		}
	}
}
