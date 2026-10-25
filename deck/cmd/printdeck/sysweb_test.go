package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"printdeck/internal/hostmetrics"
)

func TestMetricsJSON(t *testing.T) {
	m := hostmetrics.Demo(time.Now())
	w := httptest.NewRecorder()
	writeMetricsJSON(w, httptest.NewRequest("GET", "/system/metrics?range=1h", nil), m)
	var out struct {
		Interval float64 `json:"interval_s"`
		Disk     string
		Buckets  []struct {
			CPU    *float64
			Writes *uint64
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Interval != 10 || out.Disk != "mmcblk0" || len(out.Buckets) != 60 {
		t.Fatalf("interval %v, disk %q, %d buckets", out.Interval, out.Disk, len(out.Buckets))
	}
	if out.Buckets[59].CPU == nil || out.Buckets[59].Writes == nil {
		t.Error("the last bucket has no data")
	}
	// No recorder: no buckets, and no error.
	w = httptest.NewRecorder()
	writeMetricsJSON(w, httptest.NewRequest("GET", "/system/metrics", nil), nil)
	if !strings.Contains(w.Body.String(), `"buckets":[]`) {
		t.Errorf("no recorder: %s", w.Body.String())
	}
	if line := metricsLine(m, time.Now()); !strings.Contains(line, "writes") || !strings.Contains(line, "mmcblk0") {
		t.Errorf("metricsLine = %q", line)
	}
	if metricsLine(nil, time.Now()) != "" {
		t.Error("metricsLine with no recorder")
	}
}
