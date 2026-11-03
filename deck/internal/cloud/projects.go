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
	PasskeyRequest    = wire.PasskeyRequest
	PasskeyBegin      = wire.PasskeyBegin
	SignInReply       = wire.SignInReply
	PasskeyInfo       = wire.PasskeyInfo
	OwnerCodeReply    = wire.OwnerCodeReply
	ProjectRequest    = wire.ProjectRequest
	ProjectSummary    = wire.ProjectSummary
	PublicProject     = wire.PublicProject
	ProjectView       = wire.ProjectView
	ProjectMember     = wire.ProjectMember
	MemberEdit        = wire.MemberEdit
	ProjectPrint      = wire.ProjectPrint
	PrintEdit         = wire.PrintEdit
	PlanManifest      = wire.PlanManifest
	PlanPart          = wire.PlanPart
	PlanDoc           = wire.PlanDoc
	PlanAssembly      = wire.PlanAssembly
	PlanAssemblyItem  = wire.PlanAssemblyItem
	MaterialLink      = wire.MaterialLink
	ProjectAssembly   = wire.ProjectAssembly
	QueueRemove       = wire.QueueRemove
	QueueRemoved      = wire.QueueRemoved
	ProjectDoc        = wire.ProjectDoc
	UploadReply       = wire.UploadReply
	ProjectPlan       = wire.ProjectPlan
	ProjectPlanPart   = wire.ProjectPlanPart
	PlateRequest      = wire.PlateRequest
	PlatePart         = wire.PlatePart
	BuildRequest      = wire.BuildRequest
	BuildPlate        = wire.BuildPlate
	BuildJob          = wire.BuildJob
	BuildFiles        = wire.BuildFiles
	BuildFailure      = wire.BuildFailure
	BuildStarted      = wire.BuildStarted
	BuildProgress     = wire.BuildProgress
	ProjectBuild      = wire.ProjectBuild
	ProjectBuildPlate = wire.ProjectBuildPlate
	BuildDecision     = wire.BuildDecision
	RepoRequest       = wire.RepoRequest
	NotesEdit         = wire.NotesEdit
	ProjectPhoto      = wire.ProjectPhoto
	ProjectLive       = wire.ProjectLive
	FramesReply       = wire.FramesReply
	InviteRequest     = wire.InviteRequest
	InviteReply       = wire.InviteReply
	InviteView        = wire.InviteView
	PublicRequest     = wire.PublicRequest
	PublicReply       = wire.PublicReply
	NameReply         = wire.NameReply
	Printer           = wire.Printer
	PrinterRequest    = wire.PrinterRequest
	QueueReport       = wire.QueueReport
	PrinterLive       = wire.PrinterLive
	PlateSend         = wire.PlateSend
	SentPlate         = wire.SentPlate
	PlateTaken        = wire.PlateTaken
	QueueEntry        = wire.QueueEntry
	ProjectQueued     = wire.ProjectQueued
	PrintRequest      = wire.PrintRequest
	PrintReply        = wire.PrintReply
	PrintDone         = wire.PrintDone
	LiveRequest       = wire.LiveRequest
	LiveReply         = wire.LiveReply
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
	Gcode   []byte // the G-code with gzip, for the 3D view of the print page, or nil
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
	if len(up.Gcode) > 0 {
		if err := PostFile(ctx, c.HTTP, out.Grant, "print.gcode.gz", "application/gzip", cache, up.Gcode); err != nil {
			return "", err
		}
		done.Gcode = true
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

// EndLive tells the project that the deck has no print in it now.
func (c *Client) EndLive(ctx context.Context, id *Identity, project string) error {
	return c.Do(ctx, id, http.MethodDelete, "/api/v1/projects/"+project+"/live", nil, nil)
}

// EndPrinterLive tells the project that the printer (an ID of RegisterPrinter) has no print in it
// now.
func (c *Client) EndPrinterLive(ctx context.Context, id *Identity, project, printer string) error {
	return c.Do(ctx, id, http.MethodDelete, "/api/v1/projects/"+project+"/live/"+printer, nil, nil)
}

// RegisterPrinter adds a printer of the deck at the service, or changes the printer with the same
// key. The reply has the ID of the printer.
// SentPlates returns the approved plates that the owner sent to the printer.
func (c *Client) SentPlates(ctx context.Context, id *Identity, printer string) ([]SentPlate, error) {
	var out []SentPlate
	err := c.Do(ctx, id, http.MethodGet, "/api/v1/printers/"+printer+"/plates", nil, &out)
	return out, err
}

// TakePlate tells the service that a sent plate is in the queue of the deck.
func (c *Client) TakePlate(ctx context.Context, id *Identity, printer string, in PlateTaken) error {
	return c.Do(ctx, id, http.MethodPost, "/api/v1/printers/"+printer+"/plates/taken", in, nil)
}

// SendPrinterLive sends what the printer does now to the service.
func (c *Client) SendPrinterLive(ctx context.Context, id *Identity, printer string, in PrinterLive) error {
	return c.Do(ctx, id, http.MethodPost, "/api/v1/printers/"+printer+"/live", in, nil)
}

// QueueRemoves returns the removals of files of the queue of the printer that wait for the deck.
func (c *Client) QueueRemoves(ctx context.Context, id *Identity, printer string) ([]QueueRemove, error) {
	var out []QueueRemove
	err := c.Do(ctx, id, http.MethodGet, "/api/v1/printers/"+printer+"/removes", nil, &out)
	return out, err
}

// QueueRemovesDone tells the service that the deck did the removals of the items.
func (c *Client) QueueRemovesDone(ctx context.Context, id *Identity, printer string, items []string) error {
	return c.Do(ctx, id, http.MethodPost, "/api/v1/printers/"+printer+"/removes/done", QueueRemoved{Items: items}, nil)
}

// SendQueue reports the files of the queue of the printer to the service.
func (c *Client) SendQueue(ctx context.Context, id *Identity, printer string, in QueueReport) error {
	return c.Do(ctx, id, http.MethodPost, "/api/v1/printers/"+printer+"/queue", in, nil)
}

func (c *Client) RegisterPrinter(ctx context.Context, id *Identity, in PrinterRequest) (Printer, error) {
	var out Printer
	err := c.Do(ctx, id, http.MethodPost, "/api/v1/printers", in, &out)
	return out, err
}

// Projects returns the projects at the service: the projects of all the printers of the owner.
func (c *Client) Projects(ctx context.Context, id *Identity) ([]ProjectSummary, error) {
	var out []ProjectSummary
	err := c.Do(ctx, id, http.MethodGet, "/api/v1/projects", nil, &out)
	return out, err
}

// The kinds of builds, and the states of their parts.
const (
	BuildTest    = wire.BuildTest
	BuildMain    = wire.BuildMain
	BuildBranch  = wire.BuildBranch
	BuildRelease = wire.BuildRelease
	BuildSlicing = wire.BuildSlicing
	BuildReady   = wire.BuildReady
	BuildFailed  = wire.BuildFailed
	PartWaiting  = wire.PartWaiting
	PartApproved = wire.PartApproved
	PartRejected = wire.PartRejected
	PartReplaced = wire.PartReplaced
	PhaseSetup   = wire.PhaseSetup
	PhaseSlice   = wire.PhaseSlice
	PhaseUpload  = wire.PhaseUpload
)
