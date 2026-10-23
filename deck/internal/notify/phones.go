package notify

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// The phones of the Printdeck app that get the push messages of the deck. A phone pairs with the
// deck over the API of the deck: the app and the deck agree on a message key with X25519, and the
// deck encrypts each message with it (AES-256-GCM). The relay (relay/ in the repository) gives the
// encrypted message to APNs, and the app decrypts it. The relay and Apple cannot read it.

// Phone is a paired phone.
type Phone struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`       // the name of the phone, for the list on the deck
	Device    string    `json:"device"`     // the APNs device token, in hex
	Env       string    `json:"env"`        // "production" or "sandbox"
	SendToken string    `json:"send_token"` // from the relay
	Relay     string    `json:"relay"`      // the URL of the relay
	Key       []byte    `json:"key"`        // the message key, 32 bytes
	Added     time.Time `json:"added"`
}

// PairRequest is the request of the app to pair.
type PairRequest struct {
	Name      string `json:"name"`
	Device    string `json:"device"`
	Env       string `json:"env"`
	SendToken string `json:"send_token"`
	Relay     string `json:"relay"`
	PublicKey string `json:"public_key"` // the X25519 public key of the app, 32 bytes in base64
}

// keyInfo is the info of the HKDF of the message key. Its salt is the device token.
const keyInfo = "printdeck push v1"

// Phones are the paired phones, in a JSON file that only root can read.
type Phones struct {
	Path string

	mu     sync.Mutex
	loaded bool
	list   []Phone
}

func (p *Phones) load() {
	if p.loaded {
		return
	}
	p.loaded = true
	if b, err := os.ReadFile(p.Path); err == nil {
		json.Unmarshal(b, &p.list)
	}
}

func (p *Phones) save() error {
	b, _ := json.MarshalIndent(p.list, "", "  ")
	tmp := p.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p.Path)
}

// List returns the paired phones.
func (p *Phones) List() []Phone {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	return append([]Phone(nil), p.list...)
}

// Remove removes a phone.
func (p *Phones) Remove(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	for i, ph := range p.list {
		if ph.ID == id {
			p.list = append(p.list[:i], p.list[i+1:]...)
			return p.save()
		}
	}
	return errors.New("no such phone")
}

// Pair adds a phone, or makes a new key for a phone with the same device token. It returns the
// phone and the X25519 public key of the deck for the app, in base64.
func (p *Phones) Pair(req PairRequest, now time.Time) (Phone, string, error) {
	if req.Env != "production" && req.Env != "sandbox" {
		return Phone{}, "", errors.New("env must be production or sandbox")
	}
	dev, err := hex.DecodeString(req.Device)
	if err != nil || len(dev) < 32 {
		return Phone{}, "", errors.New("device must be an APNs device token in hex")
	}
	if req.SendToken == "" {
		return Phone{}, "", errors.New("no send token")
	}
	u, err := url.Parse(req.Relay)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return Phone{}, "", errors.New("relay must be an https URL")
	}
	raw, err := base64.StdEncoding.DecodeString(req.PublicKey)
	if err != nil {
		return Phone{}, "", errors.New("public_key must be base64")
	}
	theirs, err := ecdh.X25519().NewPublicKey(raw)
	if err != nil {
		return Phone{}, "", errors.New("public_key must be an X25519 key")
	}
	ours, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return Phone{}, "", err
	}
	shared, err := ours.ECDH(theirs)
	if err != nil {
		return Phone{}, "", errors.New("the key exchange failed")
	}
	key, err := hkdf.Key(sha256.New, shared, dev, keyInfo, 32)
	if err != nil {
		return Phone{}, "", err
	}
	id := make([]byte, 8)
	rand.Read(id)
	ph := Phone{ID: hex.EncodeToString(id), Name: strings.TrimSpace(req.Name), Device: strings.ToLower(req.Device),
		Env: req.Env, SendToken: req.SendToken, Relay: strings.TrimRight(req.Relay, "/"), Key: key, Added: now}
	if ph.Name == "" {
		ph.Name = "Phone"
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	for i, old := range p.list {
		if old.Device == ph.Device {
			ph.ID = old.ID
			p.list = append(p.list[:i], p.list[i+1:]...)
			break
		}
	}
	p.list = append(p.list, ph)
	if err := p.save(); err != nil {
		return Phone{}, "", err
	}
	return ph, base64.StdEncoding.EncodeToString(ours.PublicKey().Bytes()), nil
}

// plain is the plaintext of a message, as the app reads it.
type plain struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Tags  string `json:"tags,omitempty"`
	At    int64  `json:"at"`
}

// Seal encrypts a message with a key: a 12-byte nonce, then the ciphertext and the tag, in base64.
func Seal(key []byte, m Message, at time.Time) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	pt, _ := json.Marshal(plain{Title: m.Title, Body: m.Body, Tags: m.Tags, At: at.Unix()})
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, pt, nil)), nil
}

// open decrypts a message of Seal; for the tests, as the app does it.
func open(key []byte, payload string) (plain, error) {
	var p plain
	b, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return p, err
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	if len(b) < gcm.NonceSize() {
		return p, errors.New("short")
	}
	pt, err := gcm.Open(nil, b[:gcm.NonceSize()], b[gcm.NonceSize():], nil)
	if err != nil {
		return p, err
	}
	return p, json.Unmarshal(pt, &p)
}

// ErrGone is the answer of the relay for a phone that removed the app.
var ErrGone = errors.New("the app is gone from the phone")

// SendPhone encrypts a message for a phone, and posts it to the relay.
func SendPhone(ctx context.Context, ph Phone, m Message, at time.Time) error {
	payload, err := Seal(ph.Key, m, at)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"device": ph.Device, "env": ph.Env, "send_token": ph.SendToken,
		"payload": payload, "priority": m.Priority})
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ph.Relay+"/push", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.New("the relay did not answer")
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode == http.StatusGone:
		return ErrGone
	}
	return fmt.Errorf("the relay answered %d", resp.StatusCode)
}
