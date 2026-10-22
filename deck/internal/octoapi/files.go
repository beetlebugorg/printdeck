package octoapi

import (
	"errors"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strings"

	"printdeck/internal/gcodestore"
	"printdeck/internal/printer"
)

// maxUpload is the largest file that the API takes.
const maxUpload = 2 << 30

// fileEntry is one file in the form of the OctoPrint file list.
func (s *Server) fileEntry(r *http.Request, f printer.File, size int64) map[string]any {
	kind, typePath := "machinecode", []string{"machinecode", "gcode"}
	if strings.EqualFold(path.Ext(f.Name), ".stl") {
		kind, typePath = "model", []string{"model", "stl"}
	}
	return map[string]any{
		"name": f.Name, "display": f.Name, "path": f.Path, "type": kind, "typePath": typePath,
		"origin": "local", "size": size, "date": f.Date.Unix(),
		"refs": map[string]string{
			"resource": baseURL(r) + "/api/files/local/" + escapePath(f.Path),
			"download": baseURL(r) + "/downloads/files/local/" + escapePath(f.Path),
		},
	}
}

func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

func (s *Server) serveFiles(w http.ResponseWriter, r *http.Request) {
	var list []map[string]any
	for _, files := range [][]printer.File{s.Store.Files(), s.Store.Models()} {
		for _, f := range files {
			var size int64
			if fi, err := s.Store.Stat(f.Path); err == nil {
				size = fi.Size()
			}
			list = append(list, s.fileEntry(r, f, size))
		}
	}
	if list == nil {
		list = []map[string]any{}
	}
	free, total := diskSpace(s.Store.Dir)
	writeJSON(w, http.StatusOK, map[string]any{"files": list, "free": free, "total": total})
}

// serveUpload takes a file from a slicer: a multipart form with "file", and optional "path" (a
// folder), "select" and "print". The parts can come in any order, so the file goes to the store
// first, and select and print are done after the last part.
func (s *Server) serveUpload(w http.ResponseWriter, r *http.Request) {
	mr, err := r.MultipartReader()
	if err != nil {
		apiError(w, http.StatusBadRequest, "No file included")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	var (
		name, folder, saved string
		doSelect, doPrint   bool
	)
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			apiError(w, http.StatusBadRequest, "Bad upload")
			return
		}
		switch part.FormName() {
		case "file":
			name = path.Base(part.FileName())
			saved, err = s.Store.Save(path.Join(folder, name), part)
			if errors.Is(err, gcodestore.ErrBadName) {
				apiError(w, http.StatusUnsupportedMediaType, "Not a G-code or STL file")
				return
			}
			if err != nil {
				apiError(w, http.StatusInternalServerError, "Could not save the file")
				return
			}
		case "path":
			folder = strings.Trim(formValue(part), "/")
		case "select":
			doSelect = isTrue(formValue(part))
		case "print":
			doPrint = isTrue(formValue(part))
		}
		part.Close()
	}
	if saved == "" {
		apiError(w, http.StatusBadRequest, "No file included")
		return
	}
	isGcode := !strings.EqualFold(path.Ext(saved), ".stl")
	effectivePrint := false
	if doPrint && isGcode {
		if !s.Printer.State().Connected || s.Printer.State().Busy() {
			apiError(w, http.StatusConflict, "The file is saved, but the printer is busy or not operational")
			return
		}
		s.startPrint(saved)
		effectivePrint = true
	}
	ref := baseURL(r) + "/api/files/local/" + escapePath(saved)
	w.Header().Set("Location", ref)
	writeJSON(w, http.StatusCreated, map[string]any{
		"done": true, "effectiveSelect": doSelect || effectivePrint, "effectivePrint": effectivePrint,
		"files": map[string]any{"local": map[string]any{"name": path.Base(saved), "path": saved, "origin": "local",
			"refs": map[string]string{"resource": ref, "download": baseURL(r) + "/downloads/files/local/" + escapePath(saved)}}},
	})
}

func formValue(p *multipart.Part) string {
	b, _ := io.ReadAll(io.LimitReader(p, 1024))
	return strings.TrimSpace(string(b))
}

func isTrue(v string) bool {
	switch strings.ToLower(v) {
	case "true", "yes", "1", "on":
		return true
	}
	return false
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request) {
	p := r.PathValue("path")
	fi, err := s.Store.Stat(p)
	if err != nil {
		apiError(w, http.StatusNotFound, "File not found")
		return
	}
	f := printer.File{Path: strings.TrimPrefix(p, "/"), Name: path.Base(p), Date: fi.ModTime()}
	writeJSON(w, http.StatusOK, s.fileEntry(r, f, fi.Size()))
}

// serveFileCommand selects a file, and prints it with "print": true. The Klipper backend prints the
// file when it is selected with print, so "select" alone only checks that the file is there.
func (s *Server) serveFileCommand(w http.ResponseWriter, r *http.Request) {
	p := r.PathValue("path")
	var req struct {
		Command string `json:"command"`
		Print   bool   `json:"print"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Command != "select" {
		apiError(w, http.StatusBadRequest, "Unknown command")
		return
	}
	if _, err := s.Store.Stat(p); err != nil {
		apiError(w, http.StatusNotFound, "File not found")
		return
	}
	if req.Print {
		if !s.operational(w) {
			return
		}
		if s.Printer.State().Busy() {
			apiError(w, http.StatusConflict, "Printer is already printing")
			return
		}
		s.startPrint(strings.TrimPrefix(p, "/"))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) serveDelete(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.PathValue("path"), "/")
	if st := s.Printer.State(); st.Busy() && st.FilePath == p {
		apiError(w, http.StatusConflict, "The file is printing")
		return
	}
	err := s.Store.Delete(p)
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, gcodestore.ErrBadName):
		apiError(w, http.StatusNotFound, "File not found")
	case err != nil:
		apiError(w, http.StatusInternalServerError, "Could not delete the file")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) serveDownload(w http.ResponseWriter, r *http.Request) {
	f, err := s.Store.Open(r.PathValue("path"))
	if err != nil {
		apiError(w, http.StatusNotFound, "File not found")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	io.Copy(w, f)
}
