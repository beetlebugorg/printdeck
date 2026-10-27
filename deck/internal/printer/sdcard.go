package printer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
)

// SDFile is a file or a folder on the SD card of the printer.
type SDFile struct {
	Name     string
	Size     int64
	Dir      bool
	Modified time.Time
}

// SDCopy is the copy of a file from the SD card of the printer to the deck. ID is the number of the
// last copy that started. Path is the file on the card while the copy runs, else "". Done is the
// name of the last file that was copied, and Err the error of the last copy.
type SDCopy struct {
	ID           int
	Path         string
	Copied, Size int64
	Done, Err    string
}

// HasSDCard is true when the printer has [printdeck_sdcard]: the deck reads the SD card of the
// printer.
func (k *Klippy) HasSDCard() bool { return k.Machine().Has("printdeck_sdcard") }

// callResult calls an endpoint of Klipper and decodes its result into out.
func (k *Klippy) callResult(method string, params, out any) error {
	k.mu.Lock()
	conn := k.conn
	k.mu.Unlock()
	if conn == nil {
		return ErrNotReady
	}
	ctx, cancel := context.WithTimeout(context.Background(), controlTimeout)
	defer cancel()
	raw, err := conn.Call(ctx, method, params)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// SDFiles lists the folder dir of the SD card of the printer ("" is the top), folders first, then
// the G-code files by name. Other files are not in the list.
func (k *Klippy) SDFiles(dir string) ([]SDFile, error) {
	var res struct {
		Files []struct {
			Name     string `json:"name"`
			Size     int64  `json:"size"`
			Dir      bool   `json:"dir"`
			Modified int64  `json:"modified"`
		} `json:"files"`
	}
	if err := k.callResult("printdeck/sdcard/list", map[string]any{"path": dir}, &res); err != nil {
		return nil, err
	}
	var dirs, files []SDFile
	for _, f := range res.Files {
		e := SDFile{Name: f.Name, Size: f.Size, Dir: f.Dir}
		if f.Modified > 0 {
			e.Modified = time.Unix(f.Modified, 0)
		}
		switch {
		case f.Dir:
			dirs = append(dirs, e)
		case isGcode(f.Name):
			files = append(files, e)
		}
	}
	return append(dirs, files...), nil
}

func isGcode(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	return ext == ".gcode" || ext == ".gco" || ext == ".g"
}

// CopySDFile starts the copy of the file p of the SD card to the deck, and returns the name that it
// gets there and the ID of the copy (SDCopy). A file on the deck with the same name and size is
// that file: then no copy starts, and id is 0. A file of the same name and another size keeps its
// place: the copy gets a number at the end of its name.
func (k *Klippy) CopySDFile(p string, size int64) (name string, id int, err error) {
	base := path.Base(p)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for n := 1; ; n++ {
		name = base
		if n > 1 {
			name = fmt.Sprintf("%s-%d%s", stem, n, ext)
		}
		info, err := k.store.Stat(name)
		if err != nil {
			break // no file of this name
		}
		if info.Size() == size {
			return name, 0, nil
		}
		if n > 99 {
			return "", 0, errors.New("too many files of this name")
		}
	}
	var res struct {
		ID int `json:"id"`
	}
	err = k.callResult("printdeck/sdcard/copy", map[string]any{"path": p, "name": name}, &res)
	return name, res.ID, err
}

// CancelSDCopy stops the copy from the SD card.
func (k *Klippy) CancelSDCopy() {
	k.later(func() error { return k.call("printdeck/sdcard/cancel", nil) })
}
