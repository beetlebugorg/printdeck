package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"printdeck/internal/cloud/wire"
)

// The types of the projects and of the sign-in with passkeys (package wire).
type (
	PasskeyRequest = wire.PasskeyRequest
	PasskeyBegin   = wire.PasskeyBegin
	SignInReply    = wire.SignInReply
	OwnerCodeReply = wire.OwnerCodeReply
	ProjectRequest = wire.ProjectRequest
	ProjectSummary = wire.ProjectSummary
	ProjectView    = wire.ProjectView
	ProjectMember  = wire.ProjectMember
	ProjectPrint   = wire.ProjectPrint
	ProjectPhoto   = wire.ProjectPhoto
	ProjectLive    = wire.ProjectLive
	FramesReply    = wire.FramesReply
	InviteRequest  = wire.InviteRequest
	InviteReply    = wire.InviteReply
	InviteView     = wire.InviteView
	PublicRequest  = wire.PublicRequest
	PublicReply    = wire.PublicReply
	PrintRequest   = wire.PrintRequest
	PrintReply     = wire.PrintReply
	PrintDone      = wire.PrintDone
	LiveRequest    = wire.LiveRequest
	LiveReply      = wire.LiveReply
)

// OwnerCode asks the service for an owner code: the page that makes the passkey of the owner.
func (c *Client) OwnerCode(ctx context.Context, id *Identity) (OwnerCodeReply, error) {
	var out OwnerCodeReply
	err := c.Do(ctx, id, http.MethodPost, "/api/v1/owner/code", nil, &out)
	return out, err
}

// NewProject makes a project at the service.
func (c *Client) NewProject(ctx context.Context, id *Identity, name string) (ProjectSummary, error) {
	var out ProjectSummary
	err := c.Do(ctx, id, http.MethodPost, "/api/v1/projects", ProjectRequest{Name: name}, &out)
	return out, err
}

// ProjectExists reports that the project is at the service: the owner can delete a project.
func (c *Client) ProjectExists(ctx context.Context, id *Identity, project string) (bool, error) {
	var v ProjectView
	err := c.Do(ctx, id, http.MethodGet, "/api/v1/projects/"+project, nil, &v)
	var ae *APIError
	if errors.As(err, &ae) && ae.Status == http.StatusGone {
		return false, nil
	}
	return err == nil, err
}

// PrintUpload is a print for a project: the record, the picture of the model as a JPEG (or nil),
// the seconds of each layer from the slicer, and the frames of the time lapse. Frame returns the
// JPEG of frame i of Frames, as the video has it.
type PrintUpload struct {
	Request PrintRequest
	Model   []byte
	Plan    []float64
	Frames  int
	FPS     int
	Frame   func(i int) ([]byte, error)
}

// SendPrint uploads a print to the project, and returns its ID at the service. A print with the
// same key replaces the one that is there.
func (c *Client) SendPrint(ctx context.Context, id *Identity, project string, up PrintUpload) (string, error) {
	var out PrintReply
	if err := c.Do(ctx, id, http.MethodPost, "/api/v1/projects/"+project+"/prints", up.Request, &out); err != nil {
		return "", err
	}
	const cache = "private, max-age=31536000, immutable"
	done := PrintDone{FPS: max(1, up.FPS)}
	if up.Model != nil {
		if err := PostFile(ctx, c.HTTP, out.Grant, "model.jpg", "image/jpeg", cache, up.Model); err != nil {
			return "", err
		}
		done.Model = true
	}
	if len(up.Plan) > 0 {
		b, _ := json.Marshal(wire.SharePlan{Layers: up.Plan})
		if err := PostFile(ctx, c.HTTP, out.Grant, "plan.json", "application/json", cache, b); err != nil {
			return "", err
		}
		done.Plan = true
	}
	for i := 0; i < up.Frames && up.Frame != nil; i++ {
		if c.now().After(out.Grant.Expires.Add(-time.Minute)) {
			break // a very long upload stops at the end of the grant
		}
		b, err := up.Frame(i)
		if err != nil {
			break
		}
		if err := PostFile(ctx, c.HTTP, out.Grant, "lapse/"+strconv.Itoa(done.Frames)+".jpg", "image/jpeg", cache, b); err != nil {
			return "", err
		}
		done.Frames++
	}
	if err := c.Do(ctx, id, http.MethodPost, "/api/v1/projects/"+project+"/prints/"+out.Print+"/done", done, nil); err != nil {
		return "", err
	}
	return out.Print, nil
}

// ProjectPage is the page of a project at the service of the domain.
func ProjectPage(domain, project string) string { return baseURL(domain) + "/app#x=" + project }

// SendLive sends the progress of the print now to its project. A reply with a grant asks for the
// picture of the model.
func (c *Client) SendLive(ctx context.Context, id *Identity, project string, in LiveRequest) (LiveReply, error) {
	var out LiveReply
	err := c.Do(ctx, id, http.MethodPost, "/api/v1/projects/"+project+"/live", in, &out)
	return out, err
}

// EndLive tells the project that the deck prints nothing in it now.
func (c *Client) EndLive(ctx context.Context, id *Identity, project string) error {
	return c.Do(ctx, id, http.MethodDelete, "/api/v1/projects/"+project+"/live", nil, nil)
}
