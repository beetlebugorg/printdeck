package octoprint

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"printdeck/internal/printer"
)

// source gives the print view the files of OctoPrint.
type source struct{ c *Client }

func (s source) Open(path string) (io.ReadCloser, error) {
	return s.c.get("/downloads/files/local/" + escape(path))
}

func (s source) Models() []printer.File {
	body, err := s.c.get("/api/files/local?recursive=true")
	if err != nil {
		return nil
	}
	defer body.Close()
	var list struct {
		Files []struct{ Name, Path, Type string }
	}
	json.NewDecoder(body).Decode(&list)
	var models []printer.File
	for _, f := range list.Files {
		if f.Type == "model" {
			models = append(models, printer.File{Path: f.Path, Name: f.Name})
		}
	}
	return models
}

func escape(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func (c *Client) get(path string) (io.ReadCloser, error) {
	req, _ := http.NewRequest("GET", c.url+path, nil)
	req.Header.Set("X-Api-Key", c.key)
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", path, resp.Status)
	}
	return resp.Body, nil
}
