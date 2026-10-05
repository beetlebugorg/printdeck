package ui

import (
	"image"

	"speakerdeck/internal/spotify"

	"speakerdeck/internal/gfx"
)

const pickerRows = 6

type pickMode int

const (
	pickAdd  pickMode = iota // add the current track to a playlist
	pickPlay                 // start a playlist
)

// picker is the list of playlists to add the current track to, or to play.
type picker struct {
	mode    pickMode
	loading bool
	err     string
	page    int
}

func (a *App) openPicker(mode pickMode) {
	a.picker = &picker{mode: mode, loading: true}
	go func() {
		err := a.Music.LoadPlaylists()
		a.picker2(func(p *picker) {
			p.loading = false
			if err != nil {
				p.err = errText(err)
			}
		})
	}()
}

// picker2 changes the picker, if it is still open.
func (a *App) picker2(f func(*picker)) {
	a.post(func() {
		if a.picker != nil {
			f(a.picker)
		}
	})
}

func (a *App) drawPicker(c *gfx.Canvas) {
	pk := a.picker
	st := a.Music.State()
	title, sub := "Add to playlist", st.Title+" · "+st.Artist
	if pk.mode == pickPlay {
		title, sub = "Play a playlist", "Tap a playlist to play it here"
	}
	c.Text(image.Rect(24, 64, W-150, 108), title, gfx.Face(true, 28), text, gfx.Left)
	a.button(c, image.Rect(W-130, 66, W-24, 110), "Close", raised, text, func() { a.picker = nil })
	c.Text(image.Rect(24, 108, W-24, 136), sub, gfx.Face(false, 18), dim, gfx.Left)

	var lists []spotify.Playlist
	for _, pl := range a.Music.Playlists() {
		if pk.mode == pickPlay || pl.Editable {
			lists = append(lists, pl)
		}
	}
	switch {
	case pk.loading:
		c.Text(image.Rect(30, 300, W-30, 350), "Loading…", gfx.Face(false, 22), dim, gfx.Center)
		return
	case pk.err != "":
		c.Text(image.Rect(30, 280, W-30, 330), pk.err, gfx.Face(false, 20), red, gfx.Center)
		return
	case len(lists) == 0:
		c.Text(image.Rect(30, 300, W-30, 350), "No playlists", gfx.Face(false, 22), dim, gfx.Center)
		return
	}
	pages := (len(lists) + pickerRows - 1) / pickerRows
	pk.page = max(0, min(pk.page, pages-1))
	for i := 0; i < pickerRows; i++ {
		n := pk.page*pickerRows + i
		if n >= len(lists) {
			break
		}
		pl := lists[n]
		r := image.Rect(24, 148+i*80, W-24, 148+i*80+72)
		fill := surface
		if a.isPressed(r) {
			fill = raised
		}
		c.RoundRect(r, 16, fill)
		c.Text(image.Rect(r.Min.X+18, r.Min.Y+6, r.Max.X-90, r.Min.Y+40), pl.Name, gfx.Face(true, 22), text, gfx.Left)
		sub := itoa(pl.Tracks) + " tracks"
		switch {
		case pl.Recent:
			sub = "Played here before"
		case pl.Tracks == 0:
			sub = ""
		}
		c.Text(image.Rect(r.Min.X+18, r.Min.Y+38, r.Max.X-90, r.Max.Y-6), sub, gfx.Face(false, 16), dim, gfx.Left)
		if pk.mode == pickPlay {
			drawPlay(c, image.Rect(r.Max.X-76, r.Min.Y+4, r.Max.X-12, r.Min.Y+68), green)
		} else {
			drawPlus(c, r.Max.X-44, r.Min.Y+36, 12, green)
		}
		mode := pk.mode
		a.addHit(hit{r: r, tap: func() {
			a.picker = nil
			go func() {
				if mode == pickPlay {
					if err := a.Music.PlayPlaylist(pl.URI); err != nil {
						a.Toast(errText(err))
					} else {
						a.Toast("Playing " + pl.Name)
					}
				} else if err := a.Music.AddToPlaylist(pl.ID); err != nil {
					a.Toast(errText(err))
				} else {
					a.Toast("Added to " + pl.Name)
				}
			}()
		}})
	}
	if pages > 1 {
		half := (W - 48 - 16) / 2
		a.button(c, image.Rect(24, 636, 24+half, 700), "Previous", raised, text, func() { pk.page-- })
		a.button(c, image.Rect(24+half+16, 636, W-24, 700), "Next", raised, text, func() { pk.page++ })
	}
}
