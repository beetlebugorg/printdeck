package cloud

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"
)

// Link is the connection of the deck to its service, for the screens: the identity in its file,
// and the requests of Settings, Cloud. The requests run in the background. OnChange gets a call
// at the end of each one.
type Link struct {
	// Path is the file of the identity.
	Path string
	// Name is the name of the deck at the service.
	Name     string
	Client   Client
	OnChange func()

	mu      sync.Mutex
	loaded  bool
	id      *Identity
	busy    bool
	err     string
	checked time.Time
}

// LinkState is the state of a Link.
type LinkState struct {
	Connected bool
	Domain    string
	Deck      string
	Name      string
	Enrolled  time.Time
	// Busy reports that a request runs. Err is the error of the last request, or "".
	Busy bool
	Err  string
	// Checked is the time of the last good check of the connection.
	Checked time.Time
}

// load reads the identity file once.
func (l *Link) load() {
	if l.loaded {
		return
	}
	l.loaded = true
	id, err := LoadIdentity(l.Path)
	if err != nil {
		l.err = err.Error()
	}
	l.id = id
}

// Identity returns the identity, or nil for a deck that is not connected.
func (l *Link) Identity() *Identity {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.load()
	return l.id
}

// State returns the state of the link.
func (l *Link) State() LinkState {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.load()
	s := LinkState{Busy: l.busy, Err: l.err, Checked: l.checked}
	if l.id != nil {
		s.Connected, s.Domain, s.Deck, s.Name, s.Enrolled = true, l.id.Domain, l.id.Deck, l.id.Name, l.id.Enrolled
	}
	return s
}

// run runs a request in the background, one at a time.
func (l *Link) run(f func(ctx context.Context) error) {
	l.mu.Lock()
	if l.busy {
		l.mu.Unlock()
		return
	}
	l.busy, l.err = true, ""
	l.mu.Unlock()
	l.changed()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		err := f(ctx)
		cancel()
		l.mu.Lock()
		l.busy = false
		if err != nil {
			l.err = err.Error()
		}
		l.mu.Unlock()
		l.changed()
	}()
}

func (l *Link) changed() {
	if l.OnChange != nil {
		l.OnChange()
	}
}

// Connect enrolls the deck at the service of domain with a code from `pwf enroll-code`, and
// saves the identity.
func (l *Link) Connect(domain, code string) {
	l.run(func(ctx context.Context) error {
		id, err := l.Client.Enroll(ctx, domain, code, l.Name)
		if err != nil {
			return err
		}
		if err := id.Save(l.Path); err != nil {
			return errors.New("the deck connected, but its key was not saved: " + err.Error())
		}
		l.mu.Lock()
		l.id, l.checked = id, time.Now()
		l.mu.Unlock()
		return nil
	})
}

// Check asks the service for the deck, and records the time of a good reply.
func (l *Link) Check() {
	id := l.Identity()
	if id == nil {
		return
	}
	l.run(func(ctx context.Context) error {
		if _, err := l.Client.Self(ctx, id); err != nil {
			return err
		}
		l.mu.Lock()
		l.checked = time.Now()
		l.mu.Unlock()
		return nil
	})
}

// Disconnect removes the deck from the service, and deletes the identity file. The file goes also
// when the service does not reply, so a deck can always disconnect. The owner then removes the
// deck with `pwf decks remove`.
func (l *Link) Disconnect() {
	id := l.Identity()
	if id == nil {
		return
	}
	l.run(func(ctx context.Context) error {
		err := l.Client.Disconnect(ctx, id)
		var ae *APIError
		if errors.As(err, &ae) && ae.Status == 401 {
			err = nil // the service has no such deck already
		}
		if rmErr := os.Remove(l.Path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			return rmErr
		}
		l.mu.Lock()
		l.id, l.checked = nil, time.Time{}
		l.mu.Unlock()
		if err != nil {
			return errors.New("the deck is disconnected, but the service did not reply: remove it with pwf decks remove " + id.Deck)
		}
		return nil
	})
}
