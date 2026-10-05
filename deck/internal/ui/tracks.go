package ui

import (
	"image"

	"speakerdeck/internal/gfx"
	"speakerdeck/internal/spotify"
)

const (
	trackRowH  = 64
	listTop    = 150
	listBottom = contentY1 - 8
	scrubW     = 52 // width of the strip on the right that jumps through the list
)

// trackList shows the songs of one playlist, album, or Liked Songs. The list is long, so Spotify is asked
// for one page at a time, when the rows come into view.
type trackList struct {
	pl     spotify.Playlist
	items  []spotify.Track // all places of the list, empty ones are not loaded yet
	total  int
	pages  map[int]int // 1 while loading, 2 when loaded
	err    string
	scroll int // pixels
}

func (a *App) openTracks(pl spotify.Playlist) {
	a.tracks = &trackList{pl: pl, pages: map[int]int{}}
}

func (t *trackList) maxScroll() int { return max(0, t.total*trackRowH-(listBottom-listTop)) }

// ensure asks for the pages of the rows that are in view, and the next one.
func (a *App) ensure(tl *trackList) {
	first := tl.scroll / trackRowH
	last := first + (listBottom-listTop)/trackRowH + 1
	for _, i := range []int{first, last, last + spotify.TracksPage/2} {
		if tl.total > 0 && i >= tl.total {
			i = tl.total - 1
		}
		page := max(i, 0) / spotify.TracksPage
		if tl.pages[page] != 0 || tl.err != "" {
			continue
		}
		tl.pages[page] = 1
		go func() {
			tracks, total, err := a.Music.LoadTracks(tl.pl.URI, page*spotify.TracksPage)
			a.post(func() {
				if err != nil {
					tl.err = errText(err)
					return
				}
				if tl.items == nil {
					tl.items, tl.total = make([]spotify.Track, total), total
				}
				copy(tl.items[min(page*spotify.TracksPage, len(tl.items)):], tracks)
				tl.pages[page] = 2
			})
		}()
	}
}

func (a *App) playTrackAt(tl *trackList, idx int) {
	if idx < 0 || idx >= len(tl.items) || tl.items[idx].URI == "" {
		return
	}
	t := tl.items[idx]
	var follow []string
	for i := idx + 1; i < len(tl.items) && len(follow) < 99 && tl.items[i].URI != ""; i++ {
		follow = append(follow, tl.items[i].URI)
	}
	ctx := tl.pl.URI
	a.tracks, a.picker = nil, nil
	go func() {
		if err := a.Music.PlayTrack(ctx, t.URI, follow); err != nil {
			a.Toast(errText(err))
		} else {
			a.Toast("Playing " + t.Name)
		}
	}()
}

func (a *App) drawTracks(c *gfx.Canvas) {
	tl := a.tracks
	a.ensure(tl)
	playing := a.Music.State().URI
	tl.scroll = max(0, min(tl.scroll, tl.maxScroll()))
	viewH := listBottom - listTop

	// The rows go first. The header and the bottom edge are drawn over the ones that stick out.
	if tl.err == "" {
		first := tl.scroll / trackRowH
		for i := first; i < len(tl.items) && listTop+i*trackRowH-tl.scroll < listBottom; i++ {
			y := listTop + i*trackRowH - tl.scroll
			r := image.Rect(20, y, W-scrubW-8, y+trackRowH-6)
			c.RoundRect(r, 12, surface)
			t := tl.items[i]
			if t.URI == "" {
				continue
			}
			col := text
			if t.URI == playing {
				col = green
				c.RoundRect(image.Rect(r.Min.X, r.Min.Y+10, r.Min.X+5, r.Max.Y-10), 2, green)
			}
			c.Text(image.Rect(r.Min.X+16, r.Min.Y+5, r.Max.X-12, r.Min.Y+34), t.Name, gfx.Face(true, 20), col, gfx.Left)
			c.Text(image.Rect(r.Min.X+16, r.Min.Y+32, r.Max.X-12, r.Max.Y-4), t.Artist, gfx.Face(false, 16), dim, gfx.Left)
		}
	}
	c.Fill(image.Rect(0, contentY0, W, listTop), bg)
	c.Fill(image.Rect(0, listBottom, W, contentY1), bg)

	switch {
	case tl.err != "":
		c.Text(image.Rect(30, 280, W-30, 330), "This list cannot be shown here.", gfx.Face(false, 20), dim, gfx.Center)
		c.Text(image.Rect(30, 330, W-30, 370), "Play it with the button above.", gfx.Face(false, 20), dim, gfx.Center)
	case tl.total == 0:
		c.Text(image.Rect(30, 300, W-30, 350), "Loading…", gfx.Face(false, 22), dim, gfx.Center)
	}

	// Header
	a.button(c, image.Rect(24, 64, 124, 108), "Back", raised, text, func() { a.tracks = nil })
	play := image.Rect(W-134, 64, W-24, 108)
	a.button(c, play, "Play", green, onAcc, func() {
		uri, name := tl.pl.URI, tl.pl.Name
		a.tracks, a.picker = nil, nil
		go func() {
			if err := a.Music.PlayPlaylist(uri); err != nil {
				a.Toast(errText(err))
			} else {
				a.Toast("Playing " + name)
			}
		}()
	})
	c.Text(image.Rect(132, 62, W-142, 90), tl.pl.Name, gfx.Face(true, 22), text, gfx.Center)
	sub := ""
	if tl.total > 0 {
		sub = itoa(tl.total) + " songs"
	}
	c.Text(image.Rect(132, 88, W-142, 112), sub, gfx.Face(false, 16), dim, gfx.Center)

	if tl.err != "" || tl.total == 0 {
		return
	}

	// Drag the list, or tap a song.
	a.addHit(hit{r: image.Rect(0, listTop, W-scrubW-8, listBottom),
		scroll: func(dy int) { tl.scroll = max(0, min(tl.scroll+dy, tl.maxScroll())) },
		tapAt:  func(p image.Point) { a.playTrackAt(tl, (p.Y-listTop+tl.scroll)/trackRowH) },
	})

	// The strip on the right jumps through the whole list.
	strip := image.Rect(W-scrubW-4, listTop, W-4, listBottom)
	track := image.Rect(strip.Max.X-26, strip.Min.Y, strip.Max.X-14, strip.Max.Y)
	c.RoundRect(track, 6, surface)
	frac := 0.0
	if m := tl.maxScroll(); m > 0 {
		frac = float64(tl.scroll) / float64(m)
	}
	thumbH := 72
	ty := strip.Min.Y + int(frac*float64(viewH-thumbH))
	thumb := image.Rect(strip.Max.X-34, ty, strip.Max.X-6, ty+thumbH)
	col := dim
	if a.dragging != nil && a.dragging.r == strip {
		col = green
	}
	c.RoundRect(thumb, 12, col)
	// The handle is centred on the finger: its centre can go from half a handle below the top to half above the bottom.
	jump := func(f float64) {
		g := (f*float64(viewH) - float64(thumbH)/2) / float64(viewH-thumbH)
		tl.scroll = int(max(0, min(1, g)) * float64(tl.maxScroll()))
	}
	a.addHit(hit{r: strip, vertical: true, drag: jump, end: jump})
}
