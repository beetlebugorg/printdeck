package ui

import (
	"fmt"
	"image"

	"printdeck/internal/gfx"
	"printdeck/kit"
)

// Settings, App keys: the API keys that apps got with App Keys (the Allow on the deck), with
// their app and their printer. A key of the deck works for each printer ("All printers"). A tap
// removes a key: the app then needs a new Allow.

// AppKey is an app key in the list. ID names the key for RemoveKey; the list never shows the key.
type AppKey struct {
	ID    string
	App   string // "zslice", "Cura"
	Scope string // "All printers", or the name of the printer of the key
}

// KeyAdmin lists and removes the app keys of the deck.
type KeyAdmin interface {
	Keys() []AppKey
	RemoveKey(id string) error
}

// appKeysLine is the line of the App keys row of Settings.
func appKeysLine(keys []AppKey) string {
	switch len(keys) {
	case 0:
		return "No app has a key"
	case 1:
		return "1 app key · " + keys[0].App
	}
	return fmt.Sprintf("%d app keys", len(keys))
}

// drawAppKeys is the page Settings, App keys.
func (a *App) drawAppKeys(c *gfx.Canvas) {
	f := a.navPage(c, "App keys")
	keys := a.Keys.Keys()
	area := a.listArea(f)
	if len(keys) == 0 {
		noItems(c, area, "No app keys", "An app gets a key when you", "tap Allow on the deck.")
		return
	}
	l := kit.List{Area: area, N: len(keys), RowH: 84, Gap: 8, Scroll: &a.keysScroll}
	a.list(c, l, func(i int) string { return keys[i].App }, func(c *gfx.Canvas, i int, r image.Rectangle) {
		a.menuRow(c, r, keys[i].App, keys[i].Scope+" · key "+lastPart(keys[i].ID), dim, nil)
	}, func(i int) {
		k := keys[i]
		a.confirm = &confirmBox{question: "Remove the key of " + k.App + " (" + k.Scope + ")? The app then needs a new Allow.",
			yes: "Remove", danger: true, onYes: func() {
				if err := a.Keys.RemoveKey(k.ID); err != nil {
					a.Toast("The key was not removed: " + err.Error())
				}
			}}
	})
}

// lastPart is the part of an ID after its last ":".
func lastPart(id string) string {
	for i := len(id) - 1; i >= 0; i-- {
		if id[i] == ':' {
			return id[i+1:]
		}
	}
	return id
}
