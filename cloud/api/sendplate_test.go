package api

import (
	"context"
	"testing"
	"time"

	"printdeck/internal/cloud"
)

// The owner sends an approved plate to a printer of the same process. The deck of the printer
// reads it, and says that it took it; then the plate waits no more. A plate that is not approved,
// and a printer of another process, are refused.
func TestSendPlate(t *testing.T) {
	d, url := devService(t)
	ctx := context.Background()
	id := connectedLink(t, d, url).Identity()
	var c cloud.Client
	taz, _ := c.RegisterPrinter(ctx, id, cloud.PrinterRequest{Key: "main", Name: "TAZ", Process: "fdm"})
	phenom, _ := c.RegisterPrinter(ctx, id, cloud.PrinterRequest{Key: "phenom", Name: "Phenom", Process: "resin"})
	sum, _ := c.NewProject(ctx, id, "Robot dog")
	s := d.API
	p, _, err := s.loadProject(ctx, sum.ID)
	if err != nil {
		t.Fatal(err)
	}
	b := BuildRecord{ID: newID(10), Plan: "plan1", Repo: "o/r", State: cloud.BuildReady, Created: time.Now().UTC(),
		Plates: []plateRecord{
			{BuildPlate: cloud.BuildPlate{Printer: "taz", Process: "fdm", File: "plate-1.gcode", Size: 3, SHA256: "abc", Seconds: 600,
				Parts: []cloud.PlatePart{{Name: "Pod door"}}}, State: cloud.PartApproved},
			{BuildPlate: cloud.BuildPlate{Printer: "taz", Process: "fdm", File: "plate-2.gcode"}, State: cloud.PartWaiting},
		}}
	s.putJSON(ctx, buildKey(p.ID, b.ID), b)
	s.Store.Put(ctx, buildDir(p.ID, b.ID)+"plate-1.gcode", []byte("G28"), PutOptions{})
	owner := &who{owner: true, name: "Jeremy"}
	if _, err := s.sendPlate(ctx, p, owner, b.ID, 1, cloud.PlateSend{Printer: taz.ID}); err == nil {
		t.Error("a plate that waits went to a printer")
	}
	if _, err := s.sendPlate(ctx, p, owner, b.ID, 0, cloud.PlateSend{Printer: phenom.ID}); err == nil {
		t.Error("an FDM plate went to a resin printer")
	}
	if _, err := s.sendPlate(ctx, p, &who{name: "Sam"}, b.ID, 0, cloud.PlateSend{Printer: taz.ID}); err == nil {
		t.Error("a member sent a plate")
	}
	v, err := s.sendPlate(ctx, p, owner, b.ID, 0, cloud.PlateSend{Printer: taz.ID})
	if err != nil {
		t.Fatal(err)
	}
	if pb := v.(cloud.ProjectBuild); pb.Plates[0].SentTo != "TAZ" || pb.Plates[0].SentAt.IsZero() {
		t.Fatalf("the plate %+v", pb.Plates[0])
	}
	plates, err := c.SentPlates(ctx, id, taz.ID)
	if err != nil || len(plates) != 1 {
		t.Fatalf("plates %+v %v", plates, err)
	}
	pl := plates[0]
	if pl.ProjectName != "Robot dog" || pl.Name != "Pod door" || pl.URL == "" || pl.Plate != 0 || pl.SHA256 != "abc" || pl.Seconds != 600 {
		t.Fatalf("plate %+v", pl)
	}
	if err := c.TakePlate(ctx, id, taz.ID, cloud.PlateTaken{Project: pl.Project, Build: pl.Build, Plate: pl.Plate}); err != nil {
		t.Fatal(err)
	}
	if plates, _ := c.SentPlates(ctx, id, taz.ID); len(plates) != 0 {
		t.Fatalf("after taken %+v", plates)
	}
	got, _ := s.loadBuild(ctx, p.ID, b.ID)
	if got.Plates[0].Taken.IsZero() {
		t.Error("the plate is not taken")
	}
	// Another deck cannot read the plates of the printer.
	other := connectedLink(t, d, url).Identity()
	if _, err := c.SentPlates(ctx, other, taz.ID); err == nil {
		t.Error("another deck read the plates")
	}
}
