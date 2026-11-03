package api

import (
	"testing"
	"time"

	"printdeck/internal/cloud"
)

// The pages put the shop link of a material in a link, so only an https address with a host
// name stays.
func TestShopURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://www.matterhackers.com/store/l/pla?sku=1": "https://www.matterhackers.com/store/l/pla?sku=1",
		" https://esun3d.com/pla ":                        "https://esun3d.com/pla",
		"http://shop.example/x":                           "",
		"javascript:alert(1)":                             "",
		"https://localhost/x":                             "",
		"https://user:pw@shop.example/x":                  "",
		"https://shop.example/a\nb":                       "",
		"//shop.example/x":                                "",
	} {
		if got := shopURL(in); got != want {
			t.Errorf("shopURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShopLinks(t *testing.T) {
	in := []cloud.MaterialLink{{Label: "2.85 mm", URL: "https://shop.example/a"}, {Label: "bad", URL: "http://shop.example/b"},
		{Label: "1.75 mm", URL: "https://shop.example/c"}, {Label: "a label that is far too long", URL: "https://shop.example/d"}}
	got := shopLinks(in)
	if len(got) != 2 || got[0].Label != "2.85 mm" || got[1].URL != "https://shop.example/c" {
		t.Errorf("shopLinks = %+v", got)
	}
}

// Two runs can end in another order than their pushes: the run number decides.
func TestPlanNewer(t *testing.T) {
	now := time.Now()
	old := &PlanRecord{Repo: "o/r", RunID: "100", Created: now.Add(time.Minute)} // ended last
	newer := &PlanRecord{Repo: "o/r", RunID: "101", Created: now}
	if !planNewer(newer, old) || planNewer(old, newer) {
		t.Error("the plan of the later run is not newer")
	}
	a, b := &PlanRecord{Repo: "o/r", Created: now}, &PlanRecord{Repo: "o/r", Created: now.Add(-time.Minute)}
	if !planNewer(a, b) {
		t.Error("plans with no run number do not go by time")
	}
}

// A print that failed 5 minutes before the next print on the same printer is not the next print.
func TestIsUpSameStart(t *testing.T) {
	start := time.Date(2026, 11, 2, 21, 31, 7, 0, time.UTC)
	failed := PrintRecord{}
	failed.PrinterID, failed.Started = "taz", start.Add(-4*time.Minute-46*time.Second)
	rec := &LiveRecord{}
	rec.PrinterID, rec.Started = "taz", start
	if isUp(rec, []PrintRecord{failed}) {
		t.Error("the failed print hid the live tile of the next print")
	}
	done := PrintRecord{}
	done.PrinterID, done.Started = "taz", start.Add(400*time.Millisecond)
	if !isUp(rec, []PrintRecord{done}) {
		t.Error("the print of the live record is not up")
	}
}
