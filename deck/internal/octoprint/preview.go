package octoprint

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"speakerdeck/internal/gcode"
	"speakerdeck/internal/stl"
)

const (
	PreviewW, PreviewH = 400, 260
	maxDownload        = 80 << 20 // larger files are not drawn
)

// preview keeps the picture of the model that is printing.
//
// The G-code of the print is downloaded and read once. It tells how far the print has come.
// If there is an STL with a matching name, in OctoPrint or in ModelDir, the picture is that model,
// shaded, with the printed height in colour. Otherwise it is the toolpath from the G-code.
type preview struct {
	path      string
	gc        *gcode.Model
	mesh      *stl.Mesh
	loading   bool
	rendering bool
	img       image.Image
	imgPos    int64
}

// Preview returns the picture of the print, or nil.
func (c *Client) Preview() image.Image {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pv.img
}

func (c *Client) updatePreview(st State) {
	c.mu.Lock()
	defer c.mu.Unlock()
	pv := &c.pv
	if !st.Busy() || st.FilePath == "" {
		pv.img, pv.imgPos = nil, 0
		return
	}
	if pv.path != st.FilePath {
		if !pv.loading && st.FileSize <= maxDownload {
			pv.loading = true
			go c.loadModel(st.FilePath)
		}
		return
	}
	step := max(st.FileSize/200, 1) // draw again every 0.5 percent
	if pv.gc != nil && !pv.rendering && (pv.img == nil || st.FilePos-pv.imgPos >= step) {
		pv.rendering = true
		go c.render(pv.gc, pv.mesh, st.FilePos)
	}
}

func (c *Client) loadModel(path string) {
	var gc *gcode.Model
	if body, err := c.get("/downloads/files/local/" + escape(path)); err == nil {
		gc, _ = gcode.Parse(body)
		body.Close()
	}
	var mesh *stl.Mesh
	if gc != nil {
		mesh = c.findSTL(path)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pv.loading = false
	// On failure the path is still stored, so the same file is not tried again every 2 seconds.
	c.pv.path, c.pv.gc, c.pv.mesh, c.pv.img = path, gc, mesh, nil
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

// stem makes names comparable: "LTAZ_lower_panel.gcode" and "Lower Panel.STL" give a match.
func stem(name string) string {
	name = strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	return strings.NewReplacer(" ", "", "_", "", "-", "").Replace(strings.ToLower(name))
}

func matches(gcodeName, stlName string) bool {
	g, s := stem(gcodeName), stem(stlName)
	return len(s) >= 4 && len(g) >= 4 && (g == s || strings.Contains(g, s) || strings.Contains(s, g))
}

// findSTL looks for the model of a G-code file in OctoPrint and in ModelDir.
func (c *Client) findSTL(gcodePath string) *stl.Mesh {
	if c.ModelDir != "" {
		files, _ := filepath.Glob(filepath.Join(c.ModelDir, "*"))
		for _, f := range files {
			if strings.EqualFold(filepath.Ext(f), ".stl") && matches(gcodePath, f) {
				if r, err := os.Open(f); err == nil {
					m, _ := stl.Parse(r)
					r.Close()
					if m != nil {
						return m
					}
				}
			}
		}
	}
	body, err := c.get("/api/files/local?recursive=true")
	if err != nil {
		return nil
	}
	var list struct {
		Files []struct{ Name, Path, Type string }
	}
	json.NewDecoder(body).Decode(&list)
	body.Close()
	for _, f := range list.Files {
		if f.Type == "model" && matches(gcodePath, f.Name) {
			if r, err := c.get("/downloads/files/local/" + escape(f.Path)); err == nil {
				m, _ := stl.Parse(r)
				r.Close()
				return m
			}
		}
	}
	return nil
}

var (
	printedColor = color.RGBA{0xff, 0x9f, 0x43, 255}
	ghostColor   = color.RGBA{0x55, 0x5e, 0x6d, 255}
	pathWarm     = color.RGBA{0xff, 0xb3, 0x6b, 255} // toolpath, low layers
	pathCool     = color.RGBA{0xff, 0x7a, 0x2e, 255} // toolpath, high layers
	pathGhost    = color.RGBA{0x2c, 0x33, 0x3f, 255}
)

func (c *Client) render(gc *gcode.Model, mesh *stl.Mesh, pos int64) {
	var img *image.RGBA
	if mesh != nil {
		frac := float64(gc.ZAt(pos)) / float64(max(gc.MaxZ, 0.001))
		img = mesh.Render(PreviewW, PreviewH, frac, printedColor, ghostColor)
	} else {
		img = gc.Render(PreviewW, PreviewH, pos, pathWarm, pathCool, pathGhost)
	}
	c.mu.Lock()
	c.pv.img, c.pv.imgPos, c.pv.rendering = img, pos, false
	c.mu.Unlock()
}
