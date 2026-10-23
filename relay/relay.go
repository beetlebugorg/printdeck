// Package main is the push relay of the Printdeck app: an AWS Lambda function behind a function
// URL. It has the APNs key of the app, so a deck does not. The iPhone app registers its device
// token, and gets a send token. The deck posts the device token, the send token, and a message that
// the deck and the app encrypted with their own key at the pairing. The relay checks the send token
// and gives the message to APNs. It cannot read the message.
package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Secret is the secret of the relay, in AWS Secrets Manager.
type Secret struct {
	KeyID  string `json:"key_id"`  // the ID of the APNs key
	TeamID string `json:"team_id"` // the team of the developer account
	P8     string `json:"p8"`      // the APNs key, the text of the .p8 file
	// RelaySecret signs the send tokens. A new value makes all send tokens invalid.
	RelaySecret string `json:"relay_secret"`
}

// The hosts of APNs.
const (
	apnsProduction = "https://api.push.apple.com"
	apnsSandbox    = "https://api.sandbox.push.apple.com"
)

// maxPayload is the most bytes of an encrypted message. APNs takes a payload of 4096 bytes, and
// the payload has the alert and the keys too.
const maxPayload = 3000

// Relay sends the pushes.
type Relay struct {
	Secret Secret
	Topic  string       // the bundle ID of the app
	Client *http.Client // to APNs; HTTP/2
	// Hosts of APNs by environment, so that a test can use a fake server.
	Hosts map[string]string
	Now   func() time.Time

	mu      sync.Mutex
	jwt     string
	jwtTime time.Time
}

// NewRelay is a relay for the secret and the topic.
func NewRelay(s Secret, topic string) *Relay {
	return &Relay{Secret: s, Topic: topic, Client: &http.Client{Timeout: 15 * time.Second},
		Hosts: map[string]string{"production": apnsProduction, "sandbox": apnsSandbox}, Now: time.Now}
}

// Error is an error with an HTTP status for the reply.
type Error struct {
	Status int
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

func fail(status int, format string, a ...any) error {
	return &Error{Status: status, Msg: fmt.Sprintf(format, a...)}
}

// RegisterRequest is the body of POST /register, from the app.
type RegisterRequest struct {
	Device string `json:"device"` // the device token of APNs, in hex
	Env    string `json:"env"`    // "production" or "sandbox" (a build from Xcode)
}

// PushRequest is the body of POST /push, from the deck.
type PushRequest struct {
	Device    string `json:"device"`
	Env       string `json:"env"`
	SendToken string `json:"send_token"`
	Payload   string `json:"payload"` // the encrypted message, in base64
	Priority  int    `json:"priority,omitempty"`
}

// SendToken is the send token of a device: an HMAC of the environment and the device token with
// the relay secret, in base64url.
func (r *Relay) SendToken(env, device string) string {
	m := hmac.New(sha256.New, []byte(r.Secret.RelaySecret))
	m.Write([]byte(env + ":" + strings.ToLower(device)))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func checkDevice(env, device string) error {
	if env != "production" && env != "sandbox" {
		return fail(http.StatusBadRequest, "env must be production or sandbox")
	}
	if b, err := hex.DecodeString(device); err != nil || len(b) < 32 || len(b) > 100 {
		return fail(http.StatusBadRequest, "device must be a device token in hex")
	}
	return nil
}

// Register returns the send token of a device.
func (r *Relay) Register(req RegisterRequest) (string, error) {
	if err := checkDevice(req.Env, req.Device); err != nil {
		return "", err
	}
	return r.SendToken(req.Env, req.Device), nil
}

// Push checks the send token, and gives the message to APNs.
func (r *Relay) Push(req PushRequest) error {
	if err := checkDevice(req.Env, req.Device); err != nil {
		return err
	}
	want := r.SendToken(req.Env, req.Device)
	if !hmac.Equal([]byte(want), []byte(req.SendToken)) {
		return fail(http.StatusForbidden, "the send token is not valid for this device")
	}
	if req.Payload == "" || len(req.Payload) > maxPayload {
		return fail(http.StatusBadRequest, "payload must be 1 to %d bytes", maxPayload)
	}
	if _, err := base64.StdEncoding.DecodeString(req.Payload); err != nil {
		return fail(http.StatusBadRequest, "payload must be base64")
	}
	// The alert is a placeholder: the notification service extension of the app decrypts "e"
	// and shows the message. If the extension fails, iOS shows the placeholder.
	body, _ := json.Marshal(map[string]any{
		"aps": map[string]any{"alert": map[string]string{"title": "Printdeck", "body": "A message from your printer"},
			"mutable-content": 1, "sound": "default"},
		"e": req.Payload,
	})
	token, err := r.token()
	if err != nil {
		return err
	}
	hr, _ := http.NewRequest(http.MethodPost, r.Hosts[req.Env]+"/3/device/"+strings.ToLower(req.Device), bytes.NewReader(body))
	hr.Header.Set("authorization", "bearer "+token)
	hr.Header.Set("apns-topic", r.Topic)
	hr.Header.Set("apns-push-type", "alert")
	prio := "10"
	if req.Priority > 0 && req.Priority < 4 {
		prio = "5"
	}
	hr.Header.Set("apns-priority", prio)
	resp, err := r.Client.Do(hr)
	if err != nil {
		return fail(http.StatusBadGateway, "APNs did not answer")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	var reason struct {
		Reason string `json:"reason"`
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	json.Unmarshal(b, &reason)
	// 410: the app is gone from the device. The deck can stop sending to it.
	status := http.StatusBadGateway
	if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusBadRequest {
		status = resp.StatusCode
	}
	return fail(status, "APNs %d: %s", resp.StatusCode, reason.Reason)
}

// token is the JWT of the provider token of APNs, made again after 50 minutes. APNs takes a token
// for up to an hour, and refuses a new one more often than every 20 minutes.
func (r *Relay) token() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.Now()
	if r.jwt != "" && now.Sub(r.jwtTime) < 50*time.Minute {
		return r.jwt, nil
	}
	key, err := parseP8(r.Secret.P8)
	if err != nil {
		return "", fail(http.StatusInternalServerError, "%s", err.Error())
	}
	enc := base64.RawURLEncoding.EncodeToString
	head, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": r.Secret.KeyID})
	claims, _ := json.Marshal(map[string]any{"iss": r.Secret.TeamID, "iat": now.Unix()})
	signing := enc(head) + "." + enc(claims)
	sum := sha256.Sum256([]byte(signing))
	rs, ss, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		return "", fail(http.StatusInternalServerError, "the APNs token did not sign")
	}
	// ES256 is r and s, 32 bytes each.
	sig := make([]byte, 64)
	rs.FillBytes(sig[:32])
	ss.FillBytes(sig[32:])
	r.jwt, r.jwtTime = signing+"."+enc(sig), now
	return r.jwt, nil
}

func parseP8(p8 string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(p8))
	if block == nil {
		return nil, errors.New("the APNs key is not set: run set-apple-key.sh")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("the APNs key is not a PKCS#8 key")
	}
	ek, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("the APNs key is not an EC key")
	}
	return ek, nil
}

// verifyES256 checks a JWT signature with the public key; for the tests.
func verifyES256(pub *ecdsa.PublicKey, jwt string) bool {
	i := strings.LastIndexByte(jwt, '.')
	sig, err := base64.RawURLEncoding.DecodeString(jwt[i+1:])
	if err != nil || len(sig) != 64 {
		return false
	}
	sum := sha256.Sum256([]byte(jwt[:i]))
	return ecdsa.Verify(pub, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:]))
}
