package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"printdeck/internal/printer"
	"printdeck/internal/remote"
)

type heatBackend struct{ rep printer.HeatReport }

func (h heatBackend) HeatTimes() printer.HeatReport { return h.rep }

// /slicer/heat.json gives the heat log of the printer with a key, and no file with no key or
// login. A printer with no heat log has no file.
func TestHeatFile(t *testing.T) {
	room := 24.0
	rep := printer.HeatReport{
		Heaters: []printer.HeaterTimes{{Heater: "bed", Samples: 4, Fit: &printer.HeatFit{Method: "theil-sen", Intercept: 30, PerDegree: 2, Samples: 4},
			Targets: []printer.TargetTimes{{Target: 60, Samples: 4, Median: 130, Low: 120, High: 145, From: 22, Room: &room}},
			Points:  [][2]float64{{60, 130}}}},
		Cool: []printer.CoolTimes{{Heater: "bed", To: 40, Samples: 3, Median: 900, Low: 840, High: 960, From: 60}},
	}
	rs := remote.New()
	rs.Password = func() string { return "pw" }
	mux := http.NewServeMux()
	mux.Handle("/", rs.Handler())
	registerHeat(mux, rs, heatBackend{rep}, func(r *http.Request) bool { return r.Header.Get("X-Api-Key") == "key" })
	other := http.NewServeMux()
	registerHeat(other, rs, struct{}{}, nil)
	mux.Handle("/printers/two/", http.StripPrefix("/printers/two", other))
	ts := httptest.NewServer(mux)
	defer ts.Close()
	get := func(path, key string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest("GET", ts.URL+path, nil)
		if key != "" {
			req.Header.Set("X-Api-Key", key)
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := get("/slicer/heat.json", ""); r.StatusCode != http.StatusUnauthorized {
		t.Errorf("no key: %d", r.StatusCode)
	}
	if r := get("/slicer/heat.json", "bad"); r.StatusCode != http.StatusUnauthorized {
		t.Errorf("a bad key: %d", r.StatusCode)
	}
	r := get("/slicer/heat.json", "key")
	if r.StatusCode != 200 || r.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("with the key: %d %q", r.StatusCode, r.Header.Get("Content-Type"))
	}
	var got map[string]any
	json.NewDecoder(r.Body).Decode(&got)
	want := map[string]any{
		"heaters": []any{map[string]any{
			"heater": "bed", "samples": 4.0,
			"fit": map[string]any{"method": "theil-sen", "intercept_s": 30.0, "per_degree_s": 2.0, "samples": 4.0},
			"targets": []any{map[string]any{"target": 60.0, "samples": 4.0, "median_s": 130.0, "p25_s": 120.0, "p75_s": 145.0,
				"from": 22.0, "room": 24.0}},
		}},
		"cool": []any{map[string]any{"heater": "bed", "to": 40.0, "samples": 3.0, "median_s": 900.0, "p25_s": 840.0, "p75_s": 960.0, "from": 60.0}},
	}
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(want)
	if string(a) != string(b) {
		t.Errorf("heat.json\n got %s\nwant %s", a, b)
	}
	if r := get("/printers/two/slicer/heat.json", "key"); r.StatusCode != http.StatusUnauthorized {
		t.Errorf("the key of another printer: %d", r.StatusCode)
	}
}

// A printer with no samples has empty lists in the file.
func TestHeatFileEmpty(t *testing.T) {
	var none *printer.HeatLog
	for _, h := range []*printer.HeatLog{{}, none} {
		b, _ := json.Marshal(h.Report())
		if string(b) != `{"heaters":[],"cool":[]}` {
			t.Errorf("empty report %s", b)
		}
	}
}
