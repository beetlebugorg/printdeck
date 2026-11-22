package printer

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"strings"
	"testing"
)

func liveViewBambu(t *testing.T, ipcam string) *Bambu {
	t.Helper()
	b := NewBambu("127.0.0.1:1", "S", "12345678", "", "X2D", nil)
	if err := json.Unmarshal([]byte(`{"ipcam":`+ipcam+`}`), &b.last); err != nil {
		t.Fatal(err)
	}
	b.has = true
	return b
}

func TestLiveViewsKeepRawValues(t *testing.T) {
	b := liveViewBambu(t, `{"support_liveview_cams":[{"id":4},{"id":5}],"current_liveview":4}`)
	v, ok := b.LiveViews()
	if !ok || string(v.Supported) != `[{"id":4},{"id":5}]` || string(v.Current) != "4" {
		t.Fatalf("got %s %s %v", v.Supported, v.Current, ok)
	}
}

func TestLiveViewsAbsentAreNull(t *testing.T) {
	v, ok := liveViewBambu(t, `{"resolution":"1080p"}`).LiveViews()
	out, err := json.Marshal(v)
	if !ok || err != nil || string(out) != `{"support_liveview_cams":null,"current_liveview":null}` {
		t.Fatalf("got %s %v %v", out, ok, err)
	}
}

func TestLiveViewsHideCodeAndLogOnce(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	b := liveViewBambu(t, `{"support_liveview_cams":"rtsps://bblp:12345678@x","current_liveview":12345678}`)
	b.logLiveViews()
	b.logLiveViews()
	if strings.Contains(buf.String(), "12345678") {
		t.Fatalf("the log shows the code: %s", buf.String())
	}
	if n := strings.Count(buf.String(), "camera views"); n != 1 {
		t.Fatalf("%d log lines", n)
	}
}
