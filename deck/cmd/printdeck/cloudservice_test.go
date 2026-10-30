package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"printdeck/internal/cloud"
	"printdeck/internal/gcodestore"
	"printdeck/internal/printer"
)

// The deck registers its printer and reads the projects of the service. A print of a project with
// the name of a project of another printer goes to that project, with the ID of the printer: the
// deck makes no second project.
func TestServiceProjects(t *testing.T) {
	var mu sync.Mutex
	var prints []cloud.PrintRequest
	var reg cloud.PrinterRequest
	made := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/printers":
			json.Unmarshal(body, &reg)
			json.NewEncoder(w).Encode(cloud.Printer{ID: "printer1", Name: reg.Name, Process: reg.Process, Key: reg.Key})
		case r.Method == "GET" && r.URL.Path == "/api/v1/projects":
			json.NewEncoder(w).Encode([]cloud.ProjectSummary{{ID: "shared1", Name: "Garden"}, {ID: "shared2", Name: "Shop wall"}})
		case r.Method == "GET" && r.URL.Path == "/api/v1/projects/shared1":
			json.NewEncoder(w).Encode(cloud.ProjectView{ID: "shared1", Name: "Garden"})
		case r.Method == "POST" && r.URL.Path == "/api/v1/projects":
			made++
			json.NewEncoder(w).Encode(cloud.ProjectSummary{ID: "new1"})
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/prints"):
			var in cloud.PrintRequest
			json.Unmarshal(body, &in)
			in.Note = r.URL.Path // the project of the print, for the test
			prints = append(prints, in)
			json.NewEncoder(w).Encode(cloud.PrintReply{Print: "pr1",
				Grant: cloud.Grant{URL: srv.URL + "/upload", Fields: map[string]string{}, Expires: time.Now().Add(time.Hour)}})
		case r.URL.Path == "/upload":
			w.WriteHeader(204)
		case strings.HasSuffix(r.URL.Path, "/done"):
			w.Write([]byte("{}"))
		default:
			http.Error(w, "no such request", 404)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	id := cloud.Identity{Domain: srv.URL, Deck: "d1", Key: make([]byte, 32), Enrolled: time.Now()}
	b, _ := json.Marshal(id)
	os.WriteFile(filepath.Join(dir, "cloud.json"), b, 0o600)
	now := time.Now()
	recs := []printer.Record{{File: "a.gcode", Path: "a.gcode", Result: "complete", End: now.Add(-20 * time.Minute), Project: "garden"}}
	b, _ = json.Marshal(recs)
	os.WriteFile(filepath.Join(dir, "history.json"), b, 0o600)
	k := printer.NewKlippy(filepath.Join(dir, "none.sock"), &gcodestore.Store{Dir: dir})
	k.Hist = &printer.History{Path: filepath.Join(dir, "history.json")}
	newLink := func() *projectLink {
		return &projectLink{Link: &cloud.Link{Path: filepath.Join(dir, "cloud.json")}, k: k, stateDir: dir,
			model: "LulzBot TAZ Workhorse"}
	}
	p := newLink()
	ctx := context.Background()
	if got := p.Projects(); len(got) != 0 || p.printerID() != "" {
		t.Fatalf("before the first read: %v %q", got, p.printerID())
	}
	if err := p.refreshService(ctx); err != nil {
		t.Fatal(err)
	}
	if reg.Key != printerKey || reg.Name != "LulzBot TAZ Workhorse" || reg.Process != "fdm" {
		t.Errorf("the registration %+v", reg)
	}
	if got := strings.Join(p.Projects(), ","); got != "Garden,Shop wall" || p.printerID() != "printer1" {
		t.Fatalf("after the read: %q %q", got, p.printerID())
	}

	// The print of "garden" goes to the project "Garden" of the service, with the printer.
	p.sendNew(ctx, now)
	mu.Lock()
	if len(prints) != 1 || prints[0].Note != "/api/v1/projects/shared1/prints" || prints[0].PrinterID != "printer1" || made != 0 {
		t.Fatalf("the print %+v, %d new projects", prints, made)
	}
	mu.Unlock()

	// A new deck program reads the answers from cloud-service.json while the service is away.
	if got := strings.Join(newLink().Projects(), ","); got != "Garden,Shop wall" {
		t.Errorf("from the file: %q", got)
	}
}

// A deck of a ChiTu resin printer registers its printer with the process "resin" and the name of
// its setup.
func TestServiceRegistersResinPrinter(t *testing.T) {
	var reg cloud.PrinterRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/printers":
			json.Unmarshal(body, &reg)
			json.NewEncoder(w).Encode(cloud.Printer{ID: "printer2", Name: reg.Name, Process: reg.Process})
		case r.Method == "GET" && r.URL.Path == "/api/v1/projects":
			w.Write([]byte("[]"))
		default:
			http.Error(w, "no such request", 404)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	id := cloud.Identity{Domain: srv.URL, Deck: "d2", Key: make([]byte, 32), Enrolled: time.Now()}
	b, _ := json.Marshal(id)
	os.WriteFile(filepath.Join(dir, "cloud.json"), b, 0o600)
	c := printer.NewChiTu("127.0.0.1:1", "Phenom", &printer.History{Path: filepath.Join(dir, "history.json")})
	p := &projectLink{Link: &cloud.Link{Path: filepath.Join(dir, "cloud.json")}, k: c, stateDir: dir, name: c.Name}
	if err := p.refreshService(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reg.Name != "Phenom" || reg.Process != "resin" || p.printerID() != "printer2" {
		t.Errorf("the registration %+v, printer %q", reg, p.printerID())
	}
}
