package octoapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"printdeck/internal/printer"
)

// The plugin sends a G-code file, plain or with gzip, and gets the time of its print.
func TestEstimateAPI(t *testing.T) {
	s, ts, _ := newTest(t)
	url := ts.URL + "/api/plugin/printdeck/estimate"
	if r, _ := do(t, "POST", url, mainKey, strings.NewReader("G28\n"), ""); r.StatusCode != http.StatusNotFound {
		t.Errorf("no estimates: %d", r.StatusCode)
	}
	var got []byte
	var fail error
	s.Estimate = func(ctx context.Context, data []byte) (printer.FileEstimate, error) {
		got = append([]byte(nil), data...)
		return printer.FileEstimate{Moves: 5850, Prep: 690, Total: 6540, Planned: 5625, PlanFactor: 1.04,
			PlanPrints: 7, Steps: []string{"home", "heat-bed"}}, fail
	}
	src := "PRINT_START BED=60 EXTRUDER=210\nG1 X10 E1 F600\n"

	if r, _ := do(t, "POST", url, "", strings.NewReader(src), ""); r.StatusCode != http.StatusForbidden {
		t.Errorf("no key: %d", r.StatusCode)
	}
	r, m := do(t, "POST", url, mainKey, strings.NewReader(src), "text/plain")
	if r.StatusCode != http.StatusOK || string(got) != src {
		t.Fatalf("plain: %d, the deck got %q", r.StatusCode, got)
	}
	if m["moves"] != 5850.0 || m["prep"] != 690.0 || m["total"] != 6540.0 || m["planned"] != 5625.0 ||
		m["plan_factor"] != 1.04 || m["plan_prints"] != 7.0 || len(m["steps"].([]any)) != 2 {
		t.Errorf("answer %v", m)
	}

	var zipped bytes.Buffer
	zw := gzip.NewWriter(&zipped)
	zw.Write([]byte(src))
	zw.Close()
	got = nil
	if r, m := postEncoded(t, url, zipped.Bytes(), "gzip"); r.StatusCode != http.StatusOK || string(got) != src || m["total"] != 6540.0 {
		t.Errorf("gzip: %d %v, the deck got %q", r.StatusCode, m, got)
	}
	if r, _ := postEncoded(t, url, []byte(src), "gzip"); r.StatusCode != http.StatusBadRequest {
		t.Errorf("a body that is not gzip: %d", r.StatusCode)
	}
	if r, _ := postEncoded(t, url, []byte(src), "br"); r.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("another encoding: %d", r.StatusCode)
	}

	// A file larger than the limit, plain or after gzip.
	defer func(n int64) { estimateLimit = n }(estimateLimit)
	estimateLimit = int64(len(src)) - 1
	if r, _ := do(t, "POST", url, mainKey, strings.NewReader(src), ""); r.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("large: %d", r.StatusCode)
	}
	if r, _ := postEncoded(t, url, zipped.Bytes(), "gzip"); r.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("large after gzip: %d", r.StatusCode)
	}
	estimateLimit = 1 << 20

	// The deck has no motion limits of the printer yet.
	fail = printer.ErrNoLimits
	if r, m := do(t, "POST", url, mainKey, strings.NewReader(src), ""); r.StatusCode != http.StatusServiceUnavailable || m["error"] == nil {
		t.Errorf("no limits: %d %v", r.StatusCode, m)
	}
}

// postEncoded posts body with the Content-Encoding enc and the main key.
func postEncoded(t *testing.T, url string, body []byte, enc string) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("X-Api-Key", mainKey)
	req.Header.Set("Content-Encoding", enc)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var m map[string]any
	json.NewDecoder(r.Body).Decode(&m)
	return r, m
}
