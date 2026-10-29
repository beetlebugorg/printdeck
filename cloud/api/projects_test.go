package api

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"

	"printdeck/internal/cloud"
)

// softKey is a passkey in software, for the tests: an ES256 key and its credential ID.
type softKey struct {
	key    *ecdsa.PrivateKey
	id     []byte
	handle []byte // the user handle, from the registration
	count  uint32
}

func newSoftKey(t *testing.T) *softKey {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	rand.Read(id)
	return &softKey{key: k, id: id}
}

var b64 = base64.RawURLEncoding

// options reads the challenge, the RP ID and the user handle of the options of a ceremony.
func options(t *testing.T, raw json.RawMessage) (challenge, rpID string, user []byte) {
	t.Helper()
	var o struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RPID      string `json:"rpId"`
			RP        struct {
				ID string `json:"id"`
			} `json:"rp"`
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(raw, &o); err != nil {
		t.Fatal(err)
	}
	rpID = o.PublicKey.RPID
	if rpID == "" {
		rpID = o.PublicKey.RP.ID
	}
	user, _ = b64.DecodeString(o.PublicKey.User.ID)
	return o.PublicKey.Challenge, rpID, user
}

func (k *softKey) clientData(kind, challenge, origin string) []byte {
	b, _ := json.Marshal(map[string]any{"type": kind, "challenge": challenge, "origin": origin, "crossOrigin": false})
	return b
}

// authData is the authenticator data: the hash of the RP ID, the flags (user present and
// verified), the counter, and with cred the attested credential.
func (k *softKey) authData(rpID string, cred bool) []byte {
	h := sha256.Sum256([]byte(rpID))
	out := append([]byte(nil), h[:]...)
	flags := byte(0x05)
	if cred {
		flags |= 0x40
	}
	k.count++
	out = append(out, flags)
	out = binary.BigEndian.AppendUint32(out, k.count)
	if cred {
		out = append(out, make([]byte, 16)...) // the AAGUID
		out = binary.BigEndian.AppendUint16(out, uint16(len(k.id)))
		out = append(out, k.id...)
		x, y := make([]byte, 32), make([]byte, 32)
		k.key.X.FillBytes(x)
		k.key.Y.FillBytes(y)
		cose, _ := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
		out = append(out, cose...)
	}
	return out
}

// create is the credential of navigator.credentials.create for the options.
func (k *softKey) create(t *testing.T, opts json.RawMessage, origin string) json.RawMessage {
	challenge, rpID, user := options(t, opts)
	k.handle = user
	att, _ := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": k.authData(rpID, true)})
	b, _ := json.Marshal(map[string]any{"id": b64.EncodeToString(k.id), "rawId": b64.EncodeToString(k.id), "type": "public-key",
		"response": map[string]any{"clientDataJSON": b64.EncodeToString(k.clientData("webauthn.create", challenge, origin)),
			"attestationObject": b64.EncodeToString(att), "transports": []string{"internal"}},
		"clientExtensionResults": map[string]any{}})
	return b
}

// get is the credential of navigator.credentials.get for the options.
func (k *softKey) get(t *testing.T, opts json.RawMessage, origin string) json.RawMessage {
	challenge, _, _ := options(t, opts)
	u, _ := url.Parse(origin)
	ad := k.authData(u.Hostname(), false)
	cd := k.clientData("webauthn.get", challenge, origin)
	sum := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte(nil), ad...), sum[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, k.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"id": b64.EncodeToString(k.id), "rawId": b64.EncodeToString(k.id), "type": "public-key",
		"response": map[string]any{"clientDataJSON": b64.EncodeToString(cd), "authenticatorData": b64.EncodeToString(ad),
			"signature": b64.EncodeToString(sig), "userHandle": b64.EncodeToString(k.handle)},
		"clientExtensionResults": map[string]any{}})
	return b
}

// call sends a request of a page to the API, with the session token, and reads the reply into
// out. It returns the status.
func call(t *testing.T, base, method, path, token string, in, out any) int {
	t.Helper()
	var body []byte
	if in != nil {
		body, _ = json.Marshal(in)
	}
	req, _ := http.NewRequest(method, base+path, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode == 200 {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

// register runs a registration ceremony at /api/v1/auth/<kind>/... with the code.
func register(t *testing.T, base string, k *softKey, kind, code, name string) cloud.SignInReply {
	t.Helper()
	var begin cloud.PasskeyBegin
	if st := call(t, base, "POST", "/api/v1/auth/"+kind+"/begin", "", cloud.PasskeyRequest{Code: code}, &begin); st != 200 {
		t.Fatalf("%s begin: %d", kind, st)
	}
	var out cloud.SignInReply
	req := cloud.PasskeyRequest{Code: code, State: begin.State, Credential: k.create(t, begin.Options, base), Name: name, Device: "Test"}
	if st := call(t, base, "POST", "/api/v1/auth/"+kind+"/finish", "", req, &out); st != 200 {
		t.Fatalf("%s finish: %d", kind, st)
	}
	return out
}

// login runs a sign-in with the passkey k, and returns the status and the reply.
func login(t *testing.T, base string, k *softKey) (int, cloud.SignInReply) {
	t.Helper()
	var begin cloud.PasskeyBegin
	if st := call(t, base, "POST", "/api/v1/auth/login/begin", "", nil, &begin); st != 200 {
		t.Fatalf("login begin: %d", st)
	}
	var out cloud.SignInReply
	st := call(t, base, "POST", "/api/v1/auth/login/finish", "", cloud.PasskeyRequest{State: begin.State, Credential: k.get(t, begin.Options, base)}, &out)
	return st, out
}

// The whole flow of a project: the owner passkey from a code of the deck, a project, an invite,
// a friend who joins, a print of the deck, a photo, the public address, and the removal of the
// friend.
func TestProjectFlow(t *testing.T) {
	d, base := devService(t)
	// A passkey needs a domain: localhost, and no IP address.
	base = strings.Replace(base, "127.0.0.1", "localhost", 1)
	d.API.Base, d.Grants.URL = base, base+"/upload"
	ctx := context.Background()
	d.Store.Put(ctx, PublicPage, []byte("<!doctype html><title>Project · Printdeck</title><!-- preview -->"), PutOptions{ContentType: "text/html"})
	l := connectedLink(t, d, base)
	id := l.Identity()
	var c cloud.Client

	// The owner passkey, with a code of the deck. The code works once.
	var oc cloud.OwnerCodeReply
	if err := c.Do(ctx, id, "POST", "/api/v1/owner/code", nil, &oc); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(oc.URL, base+"/owner#code=") {
		t.Fatalf("owner URL %q", oc.URL)
	}
	owner := newSoftKey(t)
	in := register(t, base, owner, "owner", oc.Code, "Jeremy")
	if in.Kind != KindOwner || in.Token == "" || in.Name != "Jeremy" {
		t.Fatalf("owner sign-in %+v", in)
	}
	if st := call(t, base, "POST", "/api/v1/auth/owner/begin", "", cloud.PasskeyRequest{Code: oc.Code}, nil); st != 401 {
		t.Errorf("a used owner code: %d", st)
	}
	st, again := login(t, base, owner)
	if st != 200 || again.Kind != KindOwner {
		t.Fatalf("owner login: %d %+v", st, again)
	}
	tok := again.Token

	// A project, from the browser of the owner.
	var sum cloud.ProjectSummary
	if st := call(t, base, "POST", "/api/v1/projects", tok, cloud.ProjectRequest{Name: "Garden planters"}, &sum); st != 200 {
		t.Fatalf("new project: %d", st)
	}
	if sum.URL != base+"/app#x="+sum.ID {
		t.Errorf("project URL %q", sum.URL)
	}
	if st := call(t, base, "POST", "/api/v1/projects", "", cloud.ProjectRequest{Name: "No"}, nil); st != 401 {
		t.Errorf("a project with no sign-in: %d", st)
	}

	// An invite, and a friend who joins with a passkey.
	var inv cloud.InviteReply
	if st := call(t, base, "POST", "/api/v1/projects/"+sum.ID+"/invites", tok, cloud.InviteRequest{Name: "Sam"}, &inv); st != 200 {
		t.Fatalf("invite: %d", st)
	}
	code := strings.TrimPrefix(inv.URL, base+"/j#")
	var iv cloud.InviteView
	if st := call(t, base, "POST", "/api/v1/invites/view", "", cloud.PasskeyRequest{Code: code}, &iv); st != 200 || iv.Project != "Garden planters" || iv.Owner != "Jeremy" || iv.Name != "Sam" {
		t.Fatalf("invite view %d %+v", st, iv)
	}
	sam := newSoftKey(t)
	joined := register(t, base, sam, "invite", code, "Sam")
	if joined.Kind != KindMember || joined.Project != sum.ID || joined.Name != "Sam" {
		t.Fatalf("join %+v", joined)
	}
	if st := call(t, base, "POST", "/api/v1/invites/view", "", cloud.PasskeyRequest{Code: code}, nil); st != 410 {
		t.Errorf("a used invite: %d", st)
	}
	st, samIn := login(t, base, sam)
	if st != 200 || samIn.Kind != KindMember || samIn.Project != sum.ID {
		t.Fatalf("member login %d %+v", st, samIn)
	}
	var list []cloud.ProjectSummary
	if call(t, base, "GET", "/api/v1/projects", samIn.Token, nil, &list); len(list) != 1 || list[0].ID != sum.ID || list[0].Members != 1 {
		t.Errorf("the projects of a member: %+v", list)
	}
	if st := call(t, base, "POST", "/api/v1/projects/"+sum.ID+"/invites", samIn.Token, cloud.InviteRequest{Name: "Max"}, nil); st != 401 {
		t.Errorf("an invite of a member: %d", st)
	}

	// A print of the deck, with the picture of the model and two frames of the time lapse.
	var pr cloud.PrintReply
	req := cloud.PrintRequest{Name: "Planter", File: "planter.gcode", Result: "complete", Ended: time.Now().UTC(), Seconds: 3600,
		Material: "PLA", Grams: 42, Key: "planter.gcode@1"}
	if err := c.Do(ctx, id, "POST", "/api/v1/projects/"+sum.ID+"/prints", req, &pr); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"model.jpg", "lapse/0.jpg", "lapse/1.jpg"} {
		if err := cloud.PostFile(ctx, nil, pr.Grant, f, "image/jpeg", "private, max-age=31536000", []byte("jpeg "+f)); err != nil {
			t.Fatal(err)
		}
	}
	var view cloud.ProjectView
	if err := c.Do(ctx, id, "POST", "/api/v1/projects/"+sum.ID+"/prints/"+pr.Print+"/done", cloud.PrintDone{Frames: 3, FPS: 24}, &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Prints) != 1 || view.Prints[0].Frames != 2 || view.Prints[0].Model == "" || view.Prints[0].Grams != 42 {
		t.Fatalf("prints %+v", view.Prints)
	}
	if code, body := get(t, view.Prints[0].Model); code != 200 || string(body) != "jpeg model.jpg" {
		t.Errorf("the model picture: %d %q", code, body)
	}
	if code, _ := get(t, base+"/private/x/"+sum.ID+"/prints/"+pr.Print+"/model.jpg"); code == 200 {
		t.Error("a private file with no token")
	}
	var fr cloud.FramesReply
	if st := call(t, base, "GET", "/api/v1/projects/"+sum.ID+"/prints/"+pr.Print+"/frames", samIn.Token, nil, &fr); st != 200 || len(fr.URLs) != 2 || fr.FPS != 24 {
		t.Fatalf("frames %d %+v", st, fr)
	}
	// The same print again replaces it.
	var pr2 cloud.PrintReply
	c.Do(ctx, id, "POST", "/api/v1/projects/"+sum.ID+"/prints", req, &pr2)
	if pr2.Print != pr.Print {
		t.Errorf("a second upload of a print made a new print")
	}
	for _, f := range []string{"model.jpg", "lapse/0.jpg", "lapse/1.jpg"} {
		cloud.PostFile(ctx, nil, pr2.Grant, f, "image/jpeg", "private, max-age=31536000", []byte("jpeg "+f))
	}
	c.Do(ctx, id, "POST", "/api/v1/projects/"+sum.ID+"/prints/"+pr.Print+"/done", cloud.PrintDone{Frames: 2, FPS: 24}, nil)

	// A photo of the friend.
	var ph cloud.PhotoReply
	if st := call(t, base, "POST", "/api/v1/projects/"+sum.ID+"/photos", samIn.Token, nil, &ph); st != 200 {
		t.Fatalf("photo grant: %d", st)
	}
	if err := cloud.PostFile(ctx, nil, ph.Grant, "photo", "image/jpeg", "no-store", exifJPEG(t, 64, 48, 1)); err != nil {
		t.Fatal(err)
	}
	if st := call(t, base, "POST", "/api/v1/projects/"+sum.ID+"/photos/"+ph.Photo+"/done", samIn.Token, map[string]string{"print": pr.Print}, &view); st != 200 {
		t.Fatalf("photo done: %d", st)
	}
	if len(view.Photos) != 1 || view.Photos[0].By != "Sam" || !view.Photos[0].Mine || view.Photos[0].Print != pr.Print {
		t.Errorf("photos %+v", view.Photos)
	}

	// The public address: a page, page.json and the copies, with no file names; off deletes them.
	var pub cloud.PublicReply
	if st := call(t, base, "POST", "/api/v1/projects/"+sum.ID+"/public", tok, cloud.PublicRequest{On: true}, &pub); st != 200 || !strings.HasPrefix(pub.URL, base+"/p/") {
		t.Fatalf("public on: %d %+v", st, pub)
	}
	if st := call(t, base, "POST", "/api/v1/projects/"+sum.ID+"/public", samIn.Token, cloud.PublicRequest{On: false}, nil); st != 401 {
		t.Errorf("a member turns the public address off: %d", st)
	}
	code2, page := get(t, pub.URL)
	if code2 != 200 || !strings.Contains(string(page), "<title>Garden planters · Printdeck</title>") {
		t.Errorf("public page %d %s", code2, page)
	}
	_, pj := get(t, pub.URL+"/page.json")
	var pv cloud.ProjectView
	json.Unmarshal(pj, &pv)
	if len(pv.Prints) != 1 || pv.Prints[0].File != "" || pv.Members != nil || len(pv.Photos) != 1 {
		t.Errorf("public page.json %s", pj)
	}
	if code, _ := get(t, pub.URL+"/"+pv.Prints[0].Model); code != 200 {
		t.Errorf("public model picture: %d", code)
	}
	if code, _ := get(t, pub.URL+"/prints/"+pv.Prints[0].ID+"/lapse/1.jpg"); code != 200 {
		t.Errorf("public frame: %d", code)
	}
	call(t, base, "POST", "/api/v1/projects/"+sum.ID+"/public", tok, cloud.PublicRequest{On: false}, nil)
	if code, _ := get(t, pub.URL); code != 404 {
		t.Errorf("the public page after off: %d", code)
	}

	// The owner removes the friend: the session and the passkey of the friend stop working.
	if st := call(t, base, "DELETE", "/api/v1/projects/"+sum.ID+"/members/"+joined.Project, tok, nil, nil); st != 410 {
		t.Errorf("remove a member that does not exist: %d", st)
	}
	call(t, base, "GET", "/api/v1/projects/"+sum.ID, tok, nil, &view)
	if len(view.Members) != 1 || !view.Members[0].Joined {
		t.Fatalf("members %+v", view.Members)
	}
	if st := call(t, base, "DELETE", "/api/v1/projects/"+sum.ID+"/members/"+view.Members[0].ID, tok, nil, nil); st != 200 {
		t.Fatalf("remove member: %d", st)
	}
	if st := call(t, base, "GET", "/api/v1/projects/"+sum.ID, samIn.Token, nil, nil); st != 401 {
		t.Errorf("a removed member: %d", st)
	}
	if st, _ := login(t, base, sam); st != 401 {
		t.Errorf("the passkey of a removed member: %d", st)
	}

	// The owner deletes the project.
	if st := call(t, base, "DELETE", "/api/v1/projects/"+sum.ID, tok, nil, nil); st != 204 {
		t.Fatalf("delete: %d", st)
	}
	if keys, _ := d.Store.List(ctx, "x/"); len(keys) != 0 {
		t.Errorf("files after the delete: %v", keys)
	}
}
