package wire

import "time"

// The plans and the builds of a project from its GitHub repository (docs/specs/github-builds.md).
//
//   - At each push, an Action of the repository uploads a plan with an OIDC token of GitHub: the
//     parts of printdeck.yml, their models, and the parts that changed. It slices nothing.
//   - On the plan page, the owner chooses the parts and puts them on plates. Slice makes a build
//     of the plan with the plates.
//   - The slice run of the Action gets the plates of the build, slices each plate, and uploads
//     the files. The owner checks the preview of each plate, and approves it or rejects it.

// PlanManifest is the body of POST /api/v1/plans: the parts of the commit. The token of GitHub
// gives the repository, the ref and the commit. Commit is the head commit of a pull request,
// which the token does not give, or "" for the commit of the token.
type PlanManifest struct {
	Commit string     `json:"commit,omitempty"`
	PR     int        `json:"pr,omitempty"`
	Parts  []PlanPart `json:"parts"`
}

// PlanPart is one part of printdeck.yml. Models are the names of its model files in the plan
// (STL), for the 3D views, in the order of Files. Size is the size of the part on the bed in mm:
// all its files, one copy each.
type PlanPart struct {
	Name     string   `json:"name"`
	Files    []string `json:"files"`
	Count    int      `json:"count"`
	Printer  string   `json:"printer"` // the printer of printdeck.yml, for example "taz"
	Process  string   `json:"process"` // "fdm" or "resin"
	Profile  string   `json:"profile,omitempty"`
	Material string   `json:"material,omitempty"`
	Prebuilt string   `json:"prebuilt,omitempty"` // a resin file of a person, in place of a slice
	Alone    bool     `json:"alone,omitempty"`
	Changed  bool     `json:"changed,omitempty"`
	Models   []string `json:"models,omitempty"`
	SizeX    float64  `json:"size_x,omitempty"`
	SizeY    float64  `json:"size_y,omitempty"`
	SizeZ    float64  `json:"size_z,omitempty"`
	// Bed is the size of the bed of the printer in mm (X, Y), and Margin the room around each
	// object for the brim or the skirt, from the profile. The plan page checks the plates with
	// them.
	BedX   float64 `json:"bed_x,omitempty"`
	BedY   float64 `json:"bed_y,omitempty"`
	Margin float64 `json:"margin,omitempty"`
}

// UploadReply is the reply to the upload of a plan or of the files of a build: the ID, the page,
// and the grant for the files.
type UploadReply struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Grant Grant  `json:"grant"`
}

// The kinds of plans.
const (
	BuildTest    = "test"    // a pull request
	BuildMain    = "main"    // the default branch
	BuildBranch  = "branch"  // another branch
	BuildRelease = "release" // a tag
)

// ProjectPlan is a plan on the plan page and on the project page.
type ProjectPlan struct {
	ID      string            `json:"id"`
	Repo    string            `json:"repo"`
	Commit  string            `json:"commit"`
	Ref     string            `json:"ref"`
	Kind    string            `json:"kind"`
	PR      int               `json:"pr,omitempty"`
	Tag     string            `json:"tag,omitempty"`
	Branch  string            `json:"branch,omitempty"`
	Run     string            `json:"run,omitempty"` // the page of the workflow run on GitHub
	Created time.Time         `json:"created"`
	Ready   bool              `json:"ready"` // all its models are up
	Parts   []ProjectPlanPart `json:"parts"`
	Builds  []ProjectBuild    `json:"builds,omitempty"`
}

// ProjectPlanPart is a part of a plan, with the addresses of its models.
type ProjectPlanPart struct {
	PlanPart
	ModelURLs []string `json:"model_urls,omitempty"`
}

// PlateRequest is one plate of a build request: the parts of the plan (by their index in the
// plan) with their counts on this plate.
type PlateRequest struct {
	Parts []PlatePart `json:"parts"`
}

// PlatePart is a part on a plate, and its count of copies.
type PlatePart struct {
	Part  int    `json:"part"`
	Count int    `json:"count"`
	Name  string `json:"name,omitempty"`
}

// BuildRequest is the body of POST /api/v1/projects/<id>/plans/<plan>/builds: the plates.
type BuildRequest struct {
	Plates []PlateRequest `json:"plates"`
}

// The states of a build.
const (
	BuildSlicing = "slicing" // waiting for the slice run, or slicing
	BuildReady   = "ready"   // the files of all its plates are up
	BuildFailed  = "failed"
)

// The states of a plate of a ready build.
const (
	PartWaiting  = "waiting" // for the approval of the owner
	PartApproved = "approved"
	PartRejected = "rejected"
	PartReplaced = "replaced" // a newer plan of the same pull request or branch came
)

// BuildPlate is a plate of a build: its parts, and after the slice its file and the facts of
// the slice. File is the name of the sliced file in the build, Preview the name of the file of
// the 3D preview (path.bin for FDM, part.bin for resin), and Picture a picture of the plate.
type BuildPlate struct {
	Printer     string      `json:"printer"`
	Process     string      `json:"process"`
	Profile     string      `json:"profile,omitempty"`
	Material    string      `json:"material,omitempty"`
	Parts       []PlatePart `json:"parts"`
	File        string      `json:"file,omitempty"`
	Size        int64       `json:"size,omitempty"`
	SHA256      string      `json:"sha256,omitempty"`
	Preview     string      `json:"preview,omitempty"`
	Picture     string      `json:"picture,omitempty"`
	Seconds     int         `json:"seconds,omitempty"`
	Grams       float64     `json:"grams,omitempty"`
	ML          float64     `json:"ml,omitempty"`
	Layers      int         `json:"layers,omitempty"`
	LayerHeight float64     `json:"layer_height,omitempty"`
	Slicer      string      `json:"slicer,omitempty"`
}

// BuildJob is the reply to GET /api/v1/builds/<id> for the slice run: the commit of the plan, and
// the plates with the names of their parts in printdeck.yml.
type BuildJob struct {
	ID     string       `json:"id"`
	Commit string       `json:"commit"`
	Plates []BuildPlate `json:"plates"`
}

// BuildFiles is the body of POST /api/v1/builds/<id>/files: the plates of the job, in the same
// order, with their files and the facts of the slice.
type BuildFiles struct {
	Plates []BuildPlate `json:"plates"`
}

// BuildStarted is the body of POST /api/v1/builds/<id>/started: the run of the slice on GitHub.
type BuildStarted struct {
	RunID string `json:"run_id"`
}

// BuildFailure is the body of POST /api/v1/builds/<id>/failed.
type BuildFailure struct {
	Error string `json:"error"`
}

// ProjectBuild is a build on the plan page, the build page and the project page.
type ProjectBuild struct {
	ID      string    `json:"id"`
	Plan    string    `json:"plan"`
	Commit  string    `json:"commit"`
	Created time.Time `json:"created"`
	State   string    `json:"state"`
	Error   string    `json:"error,omitempty"`
	// Start is the command that starts the slice run by hand, while the service cannot start it.
	Start string `json:"start,omitempty"`
	// Run is the page of the slice run on GitHub, after the run starts.
	Run    string              `json:"run,omitempty"`
	Plates []ProjectBuildPlate `json:"plates"`
}

// ProjectBuildPlate is a plate of a build, with its state and the addresses of its files.
type ProjectBuildPlate struct {
	BuildPlate
	State      string    `json:"state,omitempty"`
	By         string    `json:"by,omitempty"`
	At         time.Time `json:"at,omitzero"`
	Reason     string    `json:"reason,omitempty"`
	PreviewURL string    `json:"preview_url,omitempty"`
	PictureURL string    `json:"picture_url,omitempty"`
	// FileURL is the address of the sliced file, for the 3D view of the moves of a G-code.
	FileURL string `json:"file_url,omitempty"`
}

// BuildDecision is the body of POST .../builds/<build>/plates/<n>/approve and .../reject: the
// reason of a rejection.
type BuildDecision struct {
	Reason string `json:"reason,omitempty"`
}

// RepoRequest is the body of PUT /api/v1/projects/<id>/repo: the GitHub repository of the
// project, "owner/name", or "" for none.
type RepoRequest struct {
	Repo string `json:"repo"`
}
