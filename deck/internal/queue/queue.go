// Package queue keeps the print queue of a deck: the files that wait for their turn on the
// printer, in order. The queue never starts a print. The deck asks at the end of each print, and a
// person starts the next file with a press.
package queue

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"
)

// MaxItems is the most files in the queue.
const MaxItems = 50

// Item is a file in the queue.
type Item struct {
	ID string `json:"id"`
	// Path is the file on the deck: the path in the G-code store, or the name of a resin file.
	Path string `json:"path"`
	// Name is the name of the file for the screen, for example "Pod door".
	Name string `json:"name"`
	// Project is the name of the project of the print at the service, or "".
	Project string `json:"project,omitempty"`
	// Material is the material of the file, for example "PLA", and MaterialName its name, for
	// example "MatterHackers PLA Black". Color is #rrggbb, or "".
	Material     string `json:"material,omitempty"`
	MaterialName string `json:"material_name,omitempty"`
	Color        string `json:"color,omitempty"`
	// Seconds is the print time of the slicer, or 0.
	Seconds int `json:"seconds,omitempty"`
	// From is where the file came from: "deck" (the screen), "slicer" (an upload), or "build"
	// (an approved plate of the service).
	From  string    `json:"from,omitempty"`
	Added time.Time `json:"added"`
}

// ErrFull is the error of Add when the queue has MaxItems files.
var ErrFull = errors.New("the queue is full")

// Queue is the queue of the deck, kept in the JSON file Path. The zero Path keeps it in memory.
type Queue struct {
	Path string
	// OnChange is called after each change, or is nil.
	OnChange func()

	mu     sync.Mutex
	loaded bool
	items  []Item
}

func (q *Queue) load() {
	if q.loaded {
		return
	}
	q.loaded = true
	if q.Path == "" {
		return
	}
	if b, err := os.ReadFile(q.Path); err == nil {
		json.Unmarshal(b, &q.items)
	}
}

// save writes the queue: to a new file, then a rename, so a power loss keeps the old file or
// the new one.
func (q *Queue) save() error {
	if q.Path == "" {
		return nil
	}
	b, err := json.Marshal(q.items)
	if err != nil {
		return err
	}
	tmp := q.Path + ".new"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, q.Path)
}

func (q *Queue) changed() error {
	err := q.save()
	if q.OnChange != nil {
		go q.OnChange()
	}
	return err
}

// List returns the files of the queue, the next first.
func (q *Queue) List() []Item {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.load()
	return append([]Item(nil), q.items...)
}

// Len is the count of files in the queue.
func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.load()
	return len(q.items)
}

// Next returns the next file, if any.
func (q *Queue) Next() (Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.load()
	if len(q.items) == 0 {
		return Item{}, false
	}
	return q.items[0], true
}

// Add puts the file it at the end of the queue, and returns it with its ID and time.
func (q *Queue) Add(it Item) (Item, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.load()
	if len(q.items) >= MaxItems {
		return Item{}, ErrFull
	}
	b := make([]byte, 6)
	rand.Read(b)
	it.ID = hex.EncodeToString(b)
	if it.Added.IsZero() {
		it.Added = time.Now().UTC()
	}
	q.items = append(q.items, it)
	return it, q.changed()
}

// Remove takes the file id out of the queue. It reports false when the queue has no such file.
func (q *Queue) Remove(id string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.load()
	for i, it := range q.items {
		if it.ID == id {
			q.items = append(q.items[:i], q.items[i+1:]...)
			q.changed()
			return true
		}
	}
	return false
}

// Move moves the file id by d places: -1 is one place nearer the front.
func (q *Queue) Move(id string, d int) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.load()
	for i, it := range q.items {
		if it.ID != id {
			continue
		}
		j := max(0, min(len(q.items)-1, i+d))
		if j == i {
			return false
		}
		q.items = append(q.items[:i], q.items[i+1:]...)
		q.items = append(q.items[:j], append([]Item{it}, q.items[j:]...)...)
		q.changed()
		return true
	}
	return false
}

// Seconds is the print time of all the files of the queue, from the times of the slicer.
func (q *Queue) Seconds() int {
	n := 0
	for _, it := range q.List() {
		n += it.Seconds
	}
	return n
}
