package bambu

import (
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The texts of the HMS codes and of the print errors. Bambu Lab publishes them at
// e.bambulab.com/query.php, as Bambu Studio and Bambu Handy show them. The list for one model
// (d=<the first 3 characters of the serial number>) has the codes of that model. The deck keeps
// the list of its printer in a file, and has the list for all models in its program
// (hms-en.json.gz) for a deck with no internet.

//go:embed hms-en.json.gz
var hmsEmbedded []byte

// HMSURL is the address of the list of Bambu Lab.
var HMSURL = "https://e.bambulab.com/query.php"

// HMSTexts are the texts of the HMS codes ("0500060000020070": attr and code in hex) and of the
// print errors ("0300400C").
type HMSTexts struct {
	Ver   int64             `json:"ver"`
	HMS   map[string]string `json:"hms"`
	Error map[string]string `json:"error"`
}

var (
	embeddedOnce  sync.Once
	embeddedTexts *HMSTexts
)

// EmbeddedHMS returns the texts in the program: the list of Bambu Lab for all models.
func EmbeddedHMS() *HMSTexts {
	embeddedOnce.Do(func() {
		t := &HMSTexts{}
		if zr, err := gzip.NewReader(bytes.NewReader(hmsEmbedded)); err == nil {
			json.NewDecoder(zr).Decode(t)
		}
		embeddedTexts = t
	})
	return embeddedTexts
}

// parseHMS reads the answer of query.php.
func parseHMS(r io.Reader) (*HMSTexts, error) {
	var q struct {
		Result int `json:"result"`
		Data   struct {
			HMS struct {
				Ver int64 `json:"ver"`
				En  []struct {
					Ecode string `json:"ecode"`
					Intro string `json:"intro"`
				} `json:"en"`
			} `json:"device_hms"`
			Error struct {
				En []struct {
					Ecode string `json:"ecode"`
					Intro string `json:"intro"`
				} `json:"en"`
			} `json:"device_error"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(r, 16<<20)).Decode(&q); err != nil {
		return nil, err
	}
	if q.Result != 0 || len(q.Data.HMS.En) == 0 {
		return nil, fmt.Errorf("hms: the list has no codes (result %d)", q.Result)
	}
	t := &HMSTexts{Ver: q.Data.HMS.Ver, HMS: map[string]string{}, Error: map[string]string{}}
	for _, e := range q.Data.HMS.En {
		t.HMS[strings.ToUpper(e.Ecode)] = strings.TrimSpace(e.Intro)
	}
	for _, e := range q.Data.Error.En {
		t.Error[strings.ToUpper(e.Ecode)] = strings.TrimSpace(e.Intro)
	}
	return t, nil
}

// FetchHMS gets the list of the model of the serial number from Bambu Lab. It sends the first 3
// characters of the serial number (the model), and no other data of the printer.
func FetchHMS(ctx context.Context, serial string) (*HMSTexts, error) {
	q := url.Values{"lang": {"en"}}
	if len(serial) >= 3 {
		q.Set("d", strings.ToUpper(serial[:3]))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, HMSURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hms: %s", resp.Status)
	}
	return parseHMS(resp.Body)
}

// LoadHMS reads a list that SaveHMS wrote.
func LoadHMS(path string) (*HMSTexts, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	t := &HMSTexts{}
	if err := json.Unmarshal(b, t); err != nil {
		return nil, err
	}
	return t, nil
}

// SaveHMS writes the list to path.
func SaveHMS(path string, t *HMSTexts) error {
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(path), 0o700)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// hmsModules are the parts of the printer by the first byte of the attr of an HMS code, as the
// Home Assistant integration of Bambu printers names them.
var hmsModules = map[int64]string{
	0x03: "Motion controller",
	0x05: "Mainboard",
	0x07: "AMS",
	0x08: "Toolhead",
	0x0C: "Camera",
	0x12: "AMS Lite",
}

// Key returns the code in the form of the list: "0500060000020070".
func (h HMS) Key() string { return fmt.Sprintf("%08X%08X", h.Attr&0xffffffff, h.Code&0xffffffff) }

// Text returns the text of the HMS code h: the text of t, then the text of the program, or else
// the part of the printer and "message". ok is false when no list has a text.
func (t *HMSTexts) Text(h HMS) (text string, ok bool) {
	for _, l := range []*HMSTexts{t, EmbeddedHMS()} {
		if l != nil {
			if s := l.HMS[h.Key()]; s != "" {
				return s, true
			}
		}
	}
	part := hmsModules[h.Attr>>24&0xff]
	if part == "" {
		part = "Printer"
	}
	return part + " message", false
}

// ErrorText returns the text of the print error code (print_error), or "".
func (t *HMSTexts) ErrorText(code int64) string {
	key := fmt.Sprintf("%08X", code&0xffffffff)
	for _, l := range []*HMSTexts{t, EmbeddedHMS()} {
		if l != nil {
			if s := l.Error[key]; s != "" {
				return s
			}
		}
	}
	return ""
}

// hmsRefresh is the age of the file of the list after which the deck gets it again.
const hmsRefresh = 7 * 24 * time.Hour

// HMSFile keeps the list of the model of a printer in a file, and gets it again from Bambu Lab
// when the file is old. The zero value has only the list of the program.
type HMSFile struct {
	Path string // the file, or "" for no file

	mu    sync.Mutex
	texts *HMSTexts
}

// Texts returns the list: the file, or nil before Refresh read one.
func (f *HMSFile) Texts() *HMSTexts {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.texts
}

// Refresh reads the file, and gets the list of serial from Bambu Lab when the file is missing or
// old. An error of the network keeps the old list.
func (f *HMSFile) Refresh(ctx context.Context, serial string) error {
	if f.Path == "" {
		return nil
	}
	if t, err := LoadHMS(f.Path); err == nil {
		f.mu.Lock()
		f.texts = t
		f.mu.Unlock()
		if fi, err := os.Stat(f.Path); err == nil && time.Since(fi.ModTime()) < hmsRefresh {
			return nil
		}
	}
	t, err := FetchHMS(ctx, serial)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.texts = t
	f.mu.Unlock()
	return SaveHMS(f.Path, t)
}
