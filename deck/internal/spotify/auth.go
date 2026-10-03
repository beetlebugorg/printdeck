package spotify

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Spotify accepts http only for the loopback IP address. Nothing listens there:
// the user copies the address of the failed redirect back to "speakerdeck auth".
const RedirectURI = "http://127.0.0.1:8888/callback"

const scopes = "user-read-playback-state user-modify-playback-state user-read-currently-playing"

type token struct {
	Access  string    `json:"access_token"`
	Refresh string    `json:"refresh_token"`
	Expires time.Time `json:"expires"`
}

func (c *Client) tokenPath() string { return filepath.Join(c.cfg.StateDir, "spotify.json") }

func (c *Client) loadToken() {
	b, err := os.ReadFile(c.tokenPath())
	if err != nil {
		return
	}
	json.Unmarshal(b, &c.tok)
}

func (c *Client) saveToken() {
	os.MkdirAll(c.cfg.StateDir, 0o700)
	b, _ := json.Marshal(c.tok)
	os.WriteFile(c.tokenPath(), b, 0o600)
}

// AuthURL starts the PKCE flow. Keep the verifier for Exchange.
func (c *Client) AuthURL() (authURL, verifier string) {
	raw := make([]byte, 48)
	rand.Read(raw)
	verifier = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"client_id":             {c.cfg.ClientID},
		"response_type":         {"code"},
		"redirect_uri":          {RedirectURI},
		"scope":                 {scopes},
		"code_challenge_method": {"S256"},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
	}
	return "https://accounts.spotify.com/authorize?" + q.Encode(), verifier
}

// Exchange takes the pasted redirect address (or only the code) and stores the tokens.
func (c *Client) Exchange(pasted, verifier string) error {
	code := strings.TrimSpace(pasted)
	if u, err := url.Parse(code); err == nil && u.Query().Get("code") != "" {
		code = u.Query().Get("code")
	} else if u != nil && u.Query().Get("error") != "" {
		return errors.New("Spotify refused: " + u.Query().Get("error"))
	}
	return c.tokenRequest(url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {RedirectURI},
		"code_verifier": {verifier},
	})
}

func (c *Client) tokenRequest(form url.Values) error {
	form.Set("client_id", c.cfg.ClientID)
	resp, err := c.http.PostForm("https://accounts.spotify.com/api/token", form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var r struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		Expires int    `json:"expires_in"`
		Error   string `json:"error_description"`
	}
	json.NewDecoder(resp.Body).Decode(&r)
	if resp.StatusCode != 200 {
		return errors.New("token request failed: " + resp.Status + " " + r.Error)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tok.Access = r.Access
	if r.Refresh != "" {
		c.tok.Refresh = r.Refresh
	}
	c.tok.Expires = time.Now().Add(time.Duration(r.Expires-30) * time.Second)
	c.saveToken()
	return nil
}

// accessToken returns a valid token and refreshes it when needed.
func (c *Client) accessToken() (string, error) {
	c.mu.Lock()
	tok := c.tok
	c.mu.Unlock()
	if tok.Refresh == "" {
		return "", errNotAuthorized
	}
	if time.Now().Before(tok.Expires) {
		return tok.Access, nil
	}
	if err := c.tokenRequest(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.Refresh}}); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tok.Access, nil
}

var errNotAuthorized = errors.New("not authorized")
