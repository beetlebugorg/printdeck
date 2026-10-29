// Package api is the API of the Printdeck cloud service: the enrollment of the decks, and the
// signed requests of the decks. The Lambda function (cmd/pwf-api) serves it behind CloudFront at
// /api/v1/.
//
// The API never replies 403 or 404. CloudFront replaces those statuses with the "not found" page
// of the service, for the shares that do not exist. The API replies 401 for a request with no
// valid auth, and 410 for a thing that does not exist.
package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-lambda-go/events"

	"printdeck/cloud/relay"
	"printdeck/internal/cloud"
)

// Server is the API.
type Server struct {
	Store  Store
	Grants Granter
	// Domain is the domain of the service, for the links. Base is the whole base URL of the
	// links instead, for pwf dev, or "".
	Domain string
	Base   string
	// Relay is the push relay of the iPhone app at /api/v1/relay/, or nil.
	Relay *relay.Relay
	// Now is the clock, or nil for time.Now.
	Now func() time.Time

	mu   sync.Mutex
	seen map[string]time.Time // the request IDs of the signed requests of the last 2*MaxSkew
	key  []byte               // the session key (auth.go)
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// maxBody is the largest body of a request to the API.
const maxBody = 64 << 10

// Error is an error with the status of its reply.
type Error struct {
	Status int
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

func fail(status int, msg string) error { return &Error{Status: status, Msg: msg} }

// ServeHTTP serves the API.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		reply(w, http.StatusRequestEntityTooLarge, cloud.ErrorReply{Error: "the body is too large"})
		return
	}
	out, err := s.route(r.Context(), r, body)
	var rr *relayReply
	if errors.As(err, &rr) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(rr.status)
		w.Write([]byte(rr.body))
		return
	}
	if err != nil {
		var e *Error
		if !errors.As(err, &e) {
			log.Printf("api: %s %s: internal error", r.Method, routeName(r.URL.Path))
			e = &Error{Status: http.StatusInternalServerError, Msg: "internal error"}
		}
		reply(w, e.Status, cloud.ErrorReply{Error: e.Msg})
		return
	}
	if out == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	reply(w, http.StatusOK, out)
}

// routeName is the path of a request with no IDs, for the logs.
func routeName(p string) string {
	if i := strings.Index(p, "/api/v1/"); i >= 0 {
		p = p[i+len("/api/v1/"):]
	}
	if i := strings.IndexByte(p, '/'); i >= 0 {
		p = p[:i]
	}
	return p
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// route runs the request, and returns the value of the reply, or nil for 204.
func (s *Server) route(ctx context.Context, r *http.Request, body []byte) (any, error) {
	p := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case p == "/api/v1/health" && r.Method == http.MethodGet:
		return map[string]bool{"ok": true}, nil
	case (p == "/api/v1/relay/register" || p == "/api/v1/relay/push") && s.Relay != nil:
		return nil, s.serveRelay(r, body, strings.TrimPrefix(p, "/api/v1/relay"))
	case p == "/api/v1/decks" && r.Method == http.MethodPost:
		return s.enroll(ctx, body)
	case strings.HasPrefix(p, "/api/v1/auth/"):
		return s.routeAuth(ctx, r, body, strings.TrimPrefix(p, "/api/v1/auth/"))
	case p == "/api/v1/owner/code" && r.Method == http.MethodPost:
		return s.ownerCode(ctx, r, body)
	case p == "/api/v1/invites/view" && r.Method == http.MethodPost:
		return s.viewInvite(ctx, body)
	case p == "/api/v1/me" && r.Method == http.MethodGet:
		ss, err := s.session(ctx, r)
		if err != nil {
			return nil, err
		}
		return s.newSessionInfo(ctx, ss)
	case p == "/api/v1/projects" || strings.HasPrefix(p, "/api/v1/projects/"):
		return s.routeProjects(ctx, r, body, strings.TrimPrefix(strings.TrimPrefix(p, "/api/v1/projects"), "/"))
	case p == "/api/v1/shares" || strings.HasPrefix(p, "/api/v1/shares/"):
		return s.routeShares(ctx, r, body, strings.TrimPrefix(strings.TrimPrefix(p, "/api/v1/shares"), "/"))
	case p == "/api/v1/decks/self":
		d, err := s.deck(ctx, r, body)
		if err != nil {
			return nil, err
		}
		switch r.Method {
		case http.MethodGet:
			return cloud.DeckInfo{Deck: d.ID, Name: d.Name, Enrolled: d.Enrolled}, nil
		case http.MethodDelete:
			return nil, s.Store.Delete(ctx, deckKey(d.ID))
		}
	}
	return nil, fail(http.StatusMethodNotAllowed, "no such request")
}

// newID returns a random ID of n bytes, in lowercase base32 with no padding.
func newID(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
}

// Deck is the file of an enrolled deck, decks/<id>.json.
type Deck struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	PublicKey []byte    `json:"public_key"`
	Enrolled  time.Time `json:"enrolled"`
}

func deckKey(id string) string { return "decks/" + id + ".json" }

// deck checks the signature of a request, and returns its deck. A request ID that the server saw
// in the last 2*MaxSkew fails: a replay. A new instance of the function has no IDs, so this check
// is partial, and TLS is the main guard.
func (s *Server) deck(ctx context.Context, r *http.Request, body []byte) (*Deck, error) {
	sg, err := cloud.ReadSigned(r.Header)
	if err != nil {
		return nil, fail(http.StatusUnauthorized, err.Error())
	}
	if !validID(sg.Deck) {
		return nil, fail(http.StatusUnauthorized, "no such deck")
	}
	b, _, err := s.Store.Get(ctx, deckKey(sg.Deck))
	if errors.Is(err, ErrNotFound) {
		return nil, fail(http.StatusUnauthorized, "no such deck: connect the deck again")
	}
	if err != nil {
		return nil, err
	}
	var d Deck
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, err
	}
	now := s.now()
	if err := cloud.Verify(ed25519.PublicKey(d.PublicKey), r.Header, r.Method, r.URL.RequestURI(), body, now); err != nil {
		return nil, fail(http.StatusUnauthorized, err.Error())
	}
	if !s.firstUse(sg.Deck+"/"+sg.Request, now) {
		return nil, fail(http.StatusUnauthorized, "the request ID was used before")
	}
	return &d, nil
}

// firstUse records a request ID, and reports that the server did not see it in the last
// 2*MaxSkew.
func (s *Server) firstUse(id string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == nil {
		s.seen = map[string]time.Time{}
	}
	for k, t := range s.seen {
		if now.Sub(t) > 2*cloud.MaxSkew {
			delete(s.seen, k)
		}
	}
	if _, ok := s.seen[id]; ok {
		return false
	}
	s.seen[id] = now
	return true
}

// validID reports that id has only the characters of newID, and a sane length.
func validID(id string) bool {
	if len(id) < 8 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= '2' && c <= '7') {
			return false
		}
	}
	return true
}

// relayReply is a reply of the push relay, as it is: the relay has its own JSON bodies.
type relayReply struct {
	status int
	body   string
}

func (e *relayReply) Error() string { return e.body }

// serveRelay runs a request of the push relay. The relay replies 403 for a bad send token or code,
// and 404 for no such path. CloudFront shows its page for those statuses, so they go out as 401
// and 410.
func (s *Server) serveRelay(r *http.Request, body []byte, path string) error {
	resp := relay.Handle(s.Relay, events.LambdaFunctionURLRequest{RawPath: path, Body: string(body),
		RequestContext: events.LambdaFunctionURLRequestContext{HTTP: events.LambdaFunctionURLRequestContextHTTPDescription{Method: r.Method}}})
	switch resp.StatusCode {
	case http.StatusForbidden:
		resp.StatusCode = http.StatusUnauthorized
	case http.StatusNotFound:
		resp.StatusCode = http.StatusGone
	}
	return &relayReply{status: resp.StatusCode, body: resp.Body}
}
