package ui

import (
	"image"
	"image/color"
	"time"

	"speakerdeck/internal/octoprint"
	"speakerdeck/internal/spotify"
)

// Demo sources give the UI fixed data, to look at it without a Spotify account or a printer.

type DemoMusic struct{ State_ spotify.State }

func (d *DemoMusic) State() spotify.State { return d.State_ }
func (d *DemoMusic) Cover() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 400, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 400; x++ {
			img.Set(x, y, color.RGBA{uint8(40 + x/3), uint8(80 + y/5), uint8(200 - x/4), 255})
		}
	}
	return img
}
func (d *DemoMusic) Toggle()            {}
func (d *DemoMusic) Next()              {}
func (d *DemoMusic) Previous()          {}
func (d *DemoMusic) Seek(time.Duration) {}
func (d *DemoMusic) SetShuffle(bool)    {}
func (d *DemoMusic) CycleRepeat()       {}

type DemoPrinter struct {
	State_ octoprint.State
	Img    image.Image
}

func (d *DemoPrinter) State() octoprint.State { return d.State_ }
func (d *DemoPrinter) Files() []octoprint.File {
	return []octoprint.File{{Path: "benchy.gcode", Name: "benchy.gcode"}, {Path: "case_body.gcode", Name: "case_body.gcode"}}
}
func (d *DemoPrinter) Preview() image.Image { return d.Img }
func (d *DemoPrinter) LoadFiles()           {}
func (d *DemoPrinter) Pause()               {}
func (d *DemoPrinter) Resume()              {}
func (d *DemoPrinter) Cancel()              {}
func (d *DemoPrinter) Connect()             {}
func (d *DemoPrinter) Heat(int, int)        {}
func (d *DemoPrinter) PrintFile(string)     {}

type DemoVolume int

func (d DemoVolume) Volume() int { return int(d) }
func (d DemoVolume) Max() int    { return 100 }
func (d DemoVolume) Set(int)     {}

// SetTab selects a tab; for the demo and for tests.
func (a *App) SetTab(t Tab) { a.tab = t }

// OpenPicker opens the playlist picker; for the demo.
func (a *App) OpenPicker() { a.picker = &picker{} }

func (d *DemoMusic) ToggleLike() error    { return nil }
func (d *DemoMusic) LoadPlaylists() error { return nil }
func (d *DemoMusic) Playlists() []spotify.Playlist {
	return []spotify.Playlist{{ID: "1", Name: "Workshop", Tracks: 212}, {ID: "2", Name: "Road trip", Tracks: 48}, {ID: "3", Name: "Quiet", Tracks: 17}}
}
func (d *DemoMusic) AddToPlaylist(string) error { return nil }
