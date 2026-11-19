package bambu

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestHMSTexts(t *testing.T) {
	// The list of the program has the codes of all models.
	if s, ok := EmbeddedHMS().Text(HMS{Attr: 0x03009300, Code: 0x00010008}); !ok || s == "" {
		t.Errorf("embedded %q %v", s, ok)
	}
	// The list of a model from Bambu Lab, with its own text, kept in a file.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("d") != "20P" || r.URL.Query().Get("lang") != "en" {
			t.Errorf("query %v", r.URL.Query())
		}
		w.Write([]byte(`{"result":0,"data":{"device_hms":{"ver":7,"en":[{"ecode":"0500060000020070","intro":"A test text."}]},` +
			`"device_error":{"en":[{"ecode":"0300400C","intro":"The print was cancelled."}]}}}`))
	}))
	defer srv.Close()
	old := HMSURL
	HMSURL = srv.URL
	defer func() { HMSURL = old }()
	f := &HMSFile{Path: filepath.Join(t.TempDir(), "hms-texts.json")}
	if err := f.Refresh(context.Background(), "20P5AJ000000000"); err != nil {
		t.Fatal(err)
	}
	texts := f.Texts()
	if s, ok := texts.Text(HMS{Attr: 0x05000600, Code: 0x00020070}); !ok || s != "A test text." {
		t.Errorf("model %q %v", s, ok)
	}
	if s := texts.ErrorText(0x0300400C); s != "The print was cancelled." {
		t.Errorf("error %q", s)
	}
	// A code that no list has: the part of the printer.
	if s, ok := texts.Text(HMS{Attr: 0x07000600, Code: 0x00020999}); ok || s != "AMS message" {
		t.Errorf("unknown %q %v", s, ok)
	}
	// The file has the list for the next start, with no network.
	HMSURL = "http://127.0.0.1:1"
	g := &HMSFile{Path: f.Path}
	if err := g.Refresh(context.Background(), "20P5AJ000000000"); err != nil || g.Texts().Ver != 7 {
		t.Errorf("from the file: %v %+v", err, g.Texts())
	}
}
