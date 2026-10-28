package cloud

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultDomain is the domain of the service of a new deck.
const DefaultDomain = "printdeck.beetlebug.org"

// Identity is the link of the deck to its service. The deck writes it once, at the enrollment.
type Identity struct {
	// Domain is the domain of the service, for example printdeck.beetlebug.org. A value with
	// "://" is a whole base URL, for the tests.
	Domain string `json:"domain"`
	// Deck is the ID that the service gave the deck, and Name the name of the deck there.
	Deck string `json:"deck"`
	Name string `json:"name"`
	// Key is the seed of the Ed25519 key of the deck (32 bytes).
	Key      []byte    `json:"key"`
	Enrolled time.Time `json:"enrolled"`
}

// PrivateKey returns the Ed25519 key of the identity.
func (id *Identity) PrivateKey() ed25519.PrivateKey { return ed25519.NewKeyFromSeed(id.Key) }

// LoadIdentity reads the identity from path. It returns nil and no error when the file does not
// exist: the deck is not connected.
func LoadIdentity(path string) (*Identity, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var id Identity
	if err := json.Unmarshal(b, &id); err != nil {
		return nil, fmt.Errorf("cloud identity: %w", err)
	}
	if len(id.Key) != ed25519.SeedSize || id.Deck == "" || id.Domain == "" {
		return nil, errors.New("cloud identity: incomplete file")
	}
	return &id, nil
}

// Save writes the identity to path. Only the owner of the file can read it: it has the private key.
func (id *Identity) Save(path string) error {
	b, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// baseURL is the URL of the service of a domain.
func baseURL(domain string) string {
	if strings.Contains(domain, "://") {
		return strings.TrimSuffix(domain, "/")
	}
	return "https://" + domain
}

// EnrollRequest is the body of POST /api/v1/decks.
type EnrollRequest struct {
	Code      string `json:"code"`
	PublicKey string `json:"public_key"` // the Ed25519 public key, standard base64
	Name      string `json:"name"`
}

// EnrollReply is the reply to POST /api/v1/decks.
type EnrollReply struct {
	Deck string `json:"deck"`
}

// DeckInfo is the reply to GET /api/v1/decks/self.
type DeckInfo struct {
	Deck     string    `json:"deck"`
	Name     string    `json:"name"`
	Enrolled time.Time `json:"enrolled"`
}

// ErrorReply is the body of a reply with an error.
type ErrorReply struct {
	Error string `json:"error"`
}

// APIError is an error reply of the service.
type APIError struct {
	Status int
	Msg    string
}

func (e *APIError) Error() string {
	if e.Msg == "" {
		return fmt.Sprintf("the service replied %d", e.Status)
	}
	return e.Msg
}

// Client sends the requests of the deck to its service.
type Client struct {
	HTTP *http.Client
	// Now is the clock of the signatures, or nil for time.Now.
	Now func() time.Time
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Enroll makes the key of the deck and enrolls it at the service of domain with the code from
// `pwf enroll-code`. The caller saves the identity.
func (c *Client) Enroll(ctx context.Context, domain, code, name string) (*Identity, error) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	in := EnrollRequest{Code: strings.TrimSpace(code), PublicKey: base64.StdEncoding.EncodeToString(pub), Name: name}
	var out EnrollReply
	if err := c.send(ctx, nil, domain, http.MethodPost, "/api/v1/decks", in, &out); err != nil {
		return nil, err
	}
	if out.Deck == "" {
		return nil, errors.New("the service sent no deck ID")
	}
	return &Identity{Domain: domain, Deck: out.Deck, Name: name, Key: key.Seed(), Enrolled: c.now().UTC()}, nil
}

// Do sends a signed request of the deck with the JSON body in (or none for nil), and reads the
// JSON reply into out (or no reply for nil).
func (c *Client) Do(ctx context.Context, id *Identity, method, path string, in, out any) error {
	return c.send(ctx, id, id.Domain, method, path, in, out)
}

// Self returns the deck as the service knows it. A deck that the owner removed gets an
// APIError with the status 401.
func (c *Client) Self(ctx context.Context, id *Identity) (DeckInfo, error) {
	var out DeckInfo
	err := c.Do(ctx, id, http.MethodGet, "/api/v1/decks/self", nil, &out)
	return out, err
}

// Disconnect removes the deck from its service.
func (c *Client) Disconnect(ctx context.Context, id *Identity) error {
	return c.Do(ctx, id, http.MethodDelete, "/api/v1/decks/self", nil, nil)
}

// send sends a request, signed when id is not nil.
func (c *Client) send(ctx context.Context, id *Identity, domain, method, path string, in, out any) error {
	var body []byte
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = b
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL(domain)+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if id != nil {
		Sign(req.Header, id.PrivateKey(), id.Deck, method, path, body, c.now())
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		var e ErrorReply
		json.Unmarshal(b, &e)
		return &APIError{Status: resp.StatusCode, Msg: e.Error}
	}
	if out == nil || len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, out)
}
