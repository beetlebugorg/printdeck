package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"printdeck/internal/cloud"
)

// People sign in with passkeys (WebAuthn). The service stores no passwords and no email
// addresses.
//
//   - The owner: the deck asks for an owner code (POST /api/v1/owner/code, signed), and shows the
//     page /owner#code=<code> as a QR code. The page makes a passkey with the code. The passkeys of
//     the owner are owners/<credential id>.json.
//   - A friend: the owner makes an invite of a project. The page /j#<code> makes a passkey with the
//     invite. The passkey of a friend is members/<credential id>.json, and it works for that
//     project only.
//
// A sign-in gives a session token: the claims and an HMAC of them with the session key of the
// service (keys/session in the bucket, made at the first use). The pages send it in the header
// Authorization: Bearer <token>. The state of a passkey ceremony goes to the page in the same
// way, so the API keeps no state between the begin and the finish.

// SessionTTL is how long a session works, and OwnerCodeTTL how long an owner code works.
const (
	SessionTTL   = 12 * time.Hour
	OwnerCodeTTL = 10 * time.Minute
	stateTTL     = 5 * time.Minute
)

// The kinds of the people of a session.
const (
	KindOwner  = "owner"
	KindMember = "member"
)

// Session is the claims of a session token.
type Session struct {
	Kind    string `json:"k"`
	Project string `json:"p,omitempty"` // a member: the project
	Member  string `json:"m,omitempty"` // a member: the member in the project
	Expires int64  `json:"e"`           // Unix seconds
}

// sessionKeyFile is the key of the session key in the bucket.
const sessionKeyFile = "keys/session"

// sessionKey returns the key of the session tokens: from the bucket, or a new random key at the
// first use.
func (s *Server) sessionKey(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	k := s.key
	s.mu.Unlock()
	if k != nil {
		return k, nil
	}
	b, _, err := s.Store.Get(ctx, sessionKeyFile)
	if errors.Is(err, ErrNotFound) {
		n := make([]byte, 32)
		rand.Read(n)
		_, err = s.Store.Put(ctx, sessionKeyFile, n, PutOptions{ContentType: "application/octet-stream", IfNoneMatch: true})
		if errors.Is(err, ErrConflict) {
			b, _, err = s.Store.Get(ctx, sessionKeyFile) // another instance made it first
		} else {
			b = n
		}
	}
	if err != nil {
		return nil, err
	}
	if len(b) < 32 {
		return nil, errors.New("the session key is too short")
	}
	s.mu.Lock()
	s.key = b
	s.mu.Unlock()
	return b, nil
}

// seal returns v as a token: the JSON of v and its HMAC with the session key and the purpose, in
// base64url, joined with ".".
func (s *Server) seal(ctx context.Context, purpose string, v any) (string, error) {
	key, err := s.sessionKey(ctx)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(v)
	m := hmac.New(sha256.New, key)
	m.Write([]byte(purpose + "\x00"))
	m.Write(b)
	enc := base64.RawURLEncoding
	return enc.EncodeToString(b) + "." + enc.EncodeToString(m.Sum(nil)), nil
}

// open checks a token of seal with the purpose, and reads it into v.
func (s *Server) open(ctx context.Context, purpose, tok string, v any) bool {
	key, err := s.sessionKey(ctx)
	if err != nil {
		return false
	}
	body, sum, ok := strings.Cut(tok, ".")
	if !ok {
		return false
	}
	enc := base64.RawURLEncoding
	b, err1 := enc.DecodeString(body)
	got, err2 := enc.DecodeString(sum)
	if err1 != nil || err2 != nil {
		return false
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(purpose + "\x00"))
	m.Write(b)
	return hmac.Equal(got, m.Sum(nil)) && json.Unmarshal(b, v) == nil
}

// newSession returns a session token for the person.
func (s *Server) newSession(ctx context.Context, ss Session) (cloud.SignInReply, error) {
	ss.Expires = s.now().Add(SessionTTL).Unix()
	tok, err := s.seal(ctx, "session", ss)
	if err != nil {
		return cloud.SignInReply{}, err
	}
	out := cloud.SignInReply{Token: tok, Kind: ss.Kind, Project: ss.Project, Expires: time.Unix(ss.Expires, 0).UTC()}
	if ss.Kind == KindOwner {
		out.Name = s.ownerName(ctx)
	} else if p, _, err := s.loadProject(ctx, ss.Project); err == nil {
		if m := p.member(ss.Member); m != nil {
			out.Name = m.Name
		}
	}
	return out, nil
}

// newSessionInfo is the reply to GET /api/v1/me: the person of the session, with no new token.
func (s *Server) newSessionInfo(ctx context.Context, ss *Session) (any, error) {
	out, err := s.newSession(ctx, *ss)
	out.Token, out.Expires = "", time.Unix(ss.Expires, 0).UTC()
	return out, err
}

// session returns the session of a request, or 401.
func (s *Server) session(ctx context.Context, r *http.Request) (*Session, error) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	var ss Session
	if !ok || !s.open(ctx, "session", tok, &ss) {
		return nil, fail(http.StatusUnauthorized, "sign in again")
	}
	if s.now().Unix() > ss.Expires {
		return nil, fail(http.StatusUnauthorized, "the session ended: sign in again")
	}
	if ss.Kind == KindMember {
		// A member that the owner removed has no access, even with a session.
		p, _, err := s.loadProject(ctx, ss.Project)
		if err != nil || p.member(ss.Member) == nil {
			return nil, fail(http.StatusUnauthorized, "you are not a member of this project")
		}
	}
	return &ss, nil
}

// webAuthn returns the WebAuthn settings of the service: the domain of the links is the ID of the
// passkeys.
func (s *Server) webAuthn() (*webauthn.WebAuthn, error) {
	u, err := url.Parse(s.base())
	if err != nil {
		return nil, err
	}
	return webauthn.New(&webauthn.Config{RPID: u.Hostname(), RPDisplayName: "Printdeck", RPOrigins: []string{s.base()}})
}

// ceremony is the state of a passkey ceremony between its begin and its finish.
type ceremony struct {
	Purpose string               `json:"purpose"` // "owner", "invite" or "login"
	Code    string               `json:"code,omitempty"`
	Data    webauthn.SessionData `json:"data"`
	Expires int64                `json:"exp"`
}

func (s *Server) sealCeremony(ctx context.Context, c ceremony) (string, error) {
	c.Expires = s.now().Add(stateTTL).Unix()
	return s.seal(ctx, "ceremony", c)
}

func (s *Server) openCeremony(ctx context.Context, tok, purpose string) (*ceremony, error) {
	var c ceremony
	if !s.open(ctx, "ceremony", tok, &c) || c.Purpose != purpose {
		return nil, fail(http.StatusBadRequest, "bad state: start again")
	}
	if s.now().Unix() > c.Expires {
		return nil, fail(http.StatusBadRequest, "the passkey took too long: start again")
	}
	return &c, nil
}

// passUser is a person for the WebAuthn library.
type passUser struct {
	id          []byte
	name        string
	credentials []webauthn.Credential
}

func (u *passUser) WebAuthnID() []byte                         { return u.id }
func (u *passUser) WebAuthnName() string                       { return u.name }
func (u *passUser) WebAuthnDisplayName() string                { return u.name }
func (u *passUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

// The user handles of the passkeys: the owner, or a member of a project.
const ownerHandle = "owner"

func memberHandle(project, member string) string { return "m:" + project + ":" + member }

// Passkey is the file of a passkey: owners/<id>.json or members/<id>.json, where <id> is the
// credential ID in base64url.
type Passkey struct {
	Credential webauthn.Credential `json:"credential"`
	Device     string              `json:"device,omitempty"`
	Added      time.Time           `json:"added"`
	Project    string              `json:"project,omitempty"` // a member
	Member     string              `json:"member,omitempty"`
}

func ownerKeyFile(id []byte) string {
	return "owners/" + base64.RawURLEncoding.EncodeToString(id) + ".json"
}
func memberKeyFile(id []byte) string {
	return "members/" + base64.RawURLEncoding.EncodeToString(id) + ".json"
}

// Owner is owner.json: the name of the owner, which invites show.
type Owner struct {
	Name string `json:"name"`
}

func (s *Server) ownerName(ctx context.Context) string {
	var o Owner
	if b, _, err := s.Store.Get(ctx, "owner.json"); err == nil {
		json.Unmarshal(b, &o)
	}
	if o.Name == "" {
		return "The owner"
	}
	return o.Name
}

// ownerPasskeys returns the passkeys of the owner.
func (s *Server) ownerPasskeys(ctx context.Context) ([]Passkey, error) {
	keys, err := s.Store.List(ctx, "owners/")
	if err != nil {
		return nil, err
	}
	var out []Passkey
	for _, k := range keys {
		b, _, err := s.Store.Get(ctx, k)
		if err != nil {
			continue
		}
		var p Passkey
		if json.Unmarshal(b, &p) == nil {
			out = append(out, p)
		}
	}
	return out, nil
}

// routeAuth serves /api/v1/auth/... and /api/v1/owner/code.
func (s *Server) routeAuth(ctx context.Context, r *http.Request, body []byte, rest string) (any, error) {
	if r.Method != http.MethodPost {
		return nil, fail(http.StatusMethodNotAllowed, "no such request")
	}
	var in cloud.PasskeyRequest
	if len(body) > 0 && json.Unmarshal(body, &in) != nil {
		return nil, fail(http.StatusBadRequest, "bad JSON")
	}
	wa, err := s.webAuthn()
	if err != nil {
		return nil, err
	}
	resident := webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
		ResidentKey: protocol.ResidentKeyRequirementRequired, RequireResidentKey: protocol.ResidentKeyRequired(),
		UserVerification: protocol.VerificationPreferred})
	switch rest {
	case "owner/begin":
		if ok, err := s.peekCode(ctx, in.Code, KindOwner); err != nil || !ok {
			return nil, codeError(err)
		}
		keys, err := s.ownerPasskeys(ctx)
		if err != nil {
			return nil, err
		}
		var excl []protocol.CredentialDescriptor
		for _, k := range keys {
			excl = append(excl, k.Credential.Descriptor())
		}
		u := &passUser{id: []byte(ownerHandle), name: "Printdeck owner"}
		opts, sd, err := wa.BeginRegistration(u, resident, webauthn.WithExclusions(excl))
		if err != nil {
			return nil, err
		}
		st, err := s.sealCeremony(ctx, ceremony{Purpose: KindOwner, Data: *sd})
		return cloud.PasskeyBegin{Options: mustJSON(opts), State: st}, err
	case "owner/finish":
		c, err := s.openCeremony(ctx, in.State, KindOwner)
		if err != nil {
			return nil, err
		}
		parsed, err := protocol.ParseCredentialCreationResponseBytes(in.Credential)
		if err != nil {
			return nil, fail(http.StatusBadRequest, "the passkey did not read")
		}
		cred, err := wa.CreateCredential(&passUser{id: []byte(ownerHandle), name: "Printdeck owner"}, c.Data, parsed)
		if err != nil {
			return nil, fail(http.StatusUnauthorized, "the passkey did not check")
		}
		ok, err := s.useCode(ctx, in.Code, KindOwner)
		if err != nil || !ok {
			return nil, codeError(err)
		}
		if err := s.putJSON(ctx, ownerKeyFile(cred.ID), Passkey{Credential: *cred, Device: clip(in.Device, 64), Added: s.now().UTC()}); err != nil {
			return nil, err
		}
		if name := clip(in.Name, 40); name != "" {
			if err := s.putJSON(ctx, "owner.json", Owner{Name: name}); err != nil {
				return nil, err
			}
		}
		return s.newSession(ctx, Session{Kind: KindOwner})
	case "invite/begin":
		inv, err := s.loadInvite(ctx, in.Code)
		if err != nil {
			return nil, err
		}
		u := &passUser{id: []byte(memberHandle(inv.Project, inv.Member)), name: inv.Name}
		opts, sd, err := wa.BeginRegistration(u, resident)
		if err != nil {
			return nil, err
		}
		st, err := s.sealCeremony(ctx, ceremony{Purpose: "invite", Code: inviteHash(in.Code), Data: *sd})
		return cloud.PasskeyBegin{Options: mustJSON(opts), State: st}, err
	case "invite/finish":
		c, err := s.openCeremony(ctx, in.State, "invite")
		if err != nil {
			return nil, err
		}
		if c.Code != inviteHash(in.Code) {
			return nil, fail(http.StatusBadRequest, "bad state: start again")
		}
		inv, err := s.loadInvite(ctx, in.Code)
		if err != nil {
			return nil, err
		}
		parsed, err := protocol.ParseCredentialCreationResponseBytes(in.Credential)
		if err != nil {
			return nil, fail(http.StatusBadRequest, "the passkey did not read")
		}
		u := &passUser{id: []byte(memberHandle(inv.Project, inv.Member)), name: inv.Name}
		cred, err := wa.CreateCredential(u, c.Data, parsed)
		if err != nil {
			return nil, fail(http.StatusUnauthorized, "the passkey did not check")
		}
		if err := s.join(ctx, in.Code, inv, cred, clip(in.Name, 40)); err != nil {
			return nil, err
		}
		return s.newSession(ctx, Session{Kind: KindMember, Project: inv.Project, Member: inv.Member})
	case "login/begin":
		opts, sd, err := wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationPreferred))
		if err != nil {
			return nil, err
		}
		st, err := s.sealCeremony(ctx, ceremony{Purpose: "login", Data: *sd})
		return cloud.PasskeyBegin{Options: mustJSON(opts), State: st}, err
	case "login/finish":
		c, err := s.openCeremony(ctx, in.State, "login")
		if err != nil {
			return nil, err
		}
		parsed, err := protocol.ParseCredentialRequestResponseBytes(in.Credential)
		if err != nil {
			return nil, fail(http.StatusBadRequest, "the passkey did not read")
		}
		var found *Passkey
		var file string
		handler := func(rawID, handle []byte) (webauthn.User, error) {
			file = ownerKeyFile(rawID)
			if string(handle) != ownerHandle {
				file = memberKeyFile(rawID)
			}
			b, _, err := s.Store.Get(ctx, file)
			if err != nil {
				return nil, errors.New("no such passkey")
			}
			var p Passkey
			if json.Unmarshal(b, &p) != nil {
				return nil, errors.New("bad passkey")
			}
			id := ownerHandle
			if p.Project != "" {
				id = memberHandle(p.Project, p.Member)
			}
			if string(handle) != id {
				return nil, errors.New("the passkey is of another person")
			}
			found = &p
			return &passUser{id: []byte(id), name: id, credentials: []webauthn.Credential{p.Credential}}, nil
		}
		cred, err := wa.ValidateDiscoverableLogin(handler, c.Data, parsed)
		if err != nil || found == nil {
			return nil, fail(http.StatusUnauthorized, "this passkey does not work here: it was removed, or it is of another service")
		}
		// The counter of the passkey, for a check of a copied key at the next sign-in.
		found.Credential.Authenticator = cred.Authenticator
		if err := s.putJSON(ctx, file, found); err != nil {
			return nil, err
		}
		if found.Project != "" {
			return s.newSession(ctx, Session{Kind: KindMember, Project: found.Project, Member: found.Member})
		}
		return s.newSession(ctx, Session{Kind: KindOwner})
	}
	return nil, fail(http.StatusMethodNotAllowed, "no such request")
}

// ownerCode serves POST /api/v1/owner/code: a deck asks for an owner code, and shows the page of
// the code as a QR code.
func (s *Server) ownerCode(ctx context.Context, r *http.Request, body []byte) (any, error) {
	if _, err := s.deck(ctx, r, body); err != nil {
		return nil, err
	}
	code, err := newCodeTTL(ctx, s.Store, KindOwner, s.now(), OwnerCodeTTL)
	if err != nil {
		return nil, err
	}
	return cloud.OwnerCodeReply{Code: code, URL: s.base() + "/owner#code=" + code,
		Expires: s.now().Add(OwnerCodeTTL).UTC()}, nil
}

// peekCode reports that a code of the kind works, and keeps it.
func (s *Server) peekCode(ctx context.Context, code, kind string) (bool, error) {
	n := normalCode(code)
	if len(n) != 12 {
		return false, nil
	}
	b, _, err := s.Store.Get(ctx, codeKey(n))
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var c Code
	return json.Unmarshal(b, &c) == nil && c.Kind == kind && !s.now().After(c.Expires), nil
}

func codeError(err error) error {
	if err != nil {
		return err
	}
	return fail(http.StatusUnauthorized, "the code is wrong, used or expired: make a new code on the deck")
}

// mustJSON is the JSON of v, for the options of a passkey ceremony.
func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// inviteHash is the SHA-256 of an invite code, in hex: the name of its file.
func inviteHash(code string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(code)))
	return hex.EncodeToString(sum[:])
}
