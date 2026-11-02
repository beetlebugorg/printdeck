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
	Docs   []PlanDoc  `json:"docs,omitempty"`
	// Assemblies are the assemblies of printdeck.yml, for the 3D views of the Parts tab.
	Assemblies []PlanAssembly `json:"assemblies,omitempty"`
}

// PlanAssembly is an assembly of printdeck.yml: the parts and the hardware in their places. The
// Parts tab shows it in 3D, and moves its items apart for the exploded view.
type PlanAssembly struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Items       []PlanAssemblyItem `json:"items"`
}

// PlanAssemblyItem is one model of an assembly: a part of the plan, or hardware (a display, a
// board, the screws).
type PlanAssemblyItem struct {
	Name string `json:"name"`
	// Part is the label of the part of the plan, "Name" or "Name (variant)", or "" for hardware.
	Part string `json:"part,omitempty"`
	// Model is the file name of the model in the plan, for example "asm1-2.stl": mm, Z up, in
	// its place in the assembly.
	Model string `json:"model"`
	Color string `json:"color,omitempty"` // "#rrggbb", or "" for the color of the part
	// Explode is the direction (x, y, z) that the item moves in the exploded view, or empty:
	// then it moves away from the middle of the assembly.
	Explode []float64 `json:"explode,omitempty"`
}

// PlanDoc is a PDF of the docs of printdeck.yml: a guide or a spec of the project. The plan run
// uploads it as PDF in the plan, and a picture of its first page as Thumb (PNG), or "" when it
// has no picture. File is the path of the PDF in the repository.
type PlanDoc struct {
	Name   string `json:"name"`
	File   string `json:"file"`
	PDF    string `json:"pdf"`
	Thumb  string `json:"thumb,omitempty"`
	Size   int64  `json:"size"`
	Pages  int    `json:"pages,omitempty"`
	SHA256 string `json:"sha256"`
}

// ProjectDoc is a PDF of a plan on the pages, with the addresses of the PDF and of its picture,
// and the link to the file in the repository at the commit of the plan.
type ProjectDoc struct {
	PlanDoc
	URL      string `json:"url,omitempty"`
	ThumbURL string `json:"thumb_url,omitempty"`
	RepoURL  string `json:"repo_url,omitempty"`
	Plan     string `json:"plan,omitempty"`
	Commit   string `json:"commit,omitempty"`
}

// PlanPart is one part of printdeck.yml. Models are the names of its model files in the plan
// (STL), for the 3D views, in the order of Files. Size is the size of the part on the bed in mm:
// all its files, one copy each.
type PlanPart struct {
	Name string `json:"name"`
	// Variant is the variant of the part, for example "fine", or "" for a part with no variants.
	// Each variant of a part is a part of its own in the plan, with its own files and profile.
	Variant  string   `json:"variant,omitempty"`
	Files    []string `json:"files"`
	Count    int      `json:"count"`
	Printer  string   `json:"printer"` // the printer of printdeck.yml, for example "taz"
	Process  string   `json:"process"` // "fdm" or "resin"
	Profile  string   `json:"profile,omitempty"`
	Material string   `json:"material,omitempty"`
	Prebuilt string   `json:"prebuilt,omitempty"` // a resin file of a person, in place of a slice
	// MaterialName and MaterialColor describe the material for the pages: "MatterHackers PLA
	// Black" and "#1a1a1a" for a roll of a profile, or the material itself.
	MaterialName  string   `json:"material_name,omitempty"`
	MaterialColor string   `json:"material_color,omitempty"`
	Alone         bool     `json:"alone,omitempty"`
	Changed       bool     `json:"changed,omitempty"`
	// Digest is the SHA-256 of what makes the plate of the part (its entry, its files and its
	// profile), or "" from an older Action. A waiting plate stays when a newer plan has its parts
	// with the same digests.
	Digest string `json:"digest,omitempty"`
	// Description is a few sentences about the part, and Group the heading of the part on the
	// Parts tab, from printdeck.yml. A change of them changes no plate.
	Description   string   `json:"description,omitempty"`
	Group         string   `json:"group,omitempty"`
	Models        []string `json:"models,omitempty"`
	SizeX         float64  `json:"size_x,omitempty"`
	SizeY         float64  `json:"size_y,omitempty"`
	SizeZ         float64  `json:"size_z,omitempty"`
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
	ID     string `json:"id"`
	Repo   string `json:"repo"`
	Commit string `json:"commit"`
	Ref    string `json:"ref"`
	Kind   string `json:"kind"`
	PR     int    `json:"pr,omitempty"`
	// PRState is the state of the pull request PR on GitHub: "open", "closed" or "merged", or ""
	// when the service does not know it.
	PRState string `json:"pr_state,omitempty"`
	Tag    string `json:"tag,omitempty"`
	Branch string `json:"branch,omitempty"`
	Run    string `json:"run,omitempty"` // the page of the workflow run on GitHub
	// Fork is the fork of the plan of a member's pull request, for example "sam/bittlex", and
	// Author the GitHub username of the member; both "" for a plan of the repository.
	Fork    string            `json:"fork,omitempty"`
	Author  string            `json:"author,omitempty"`
	Created time.Time         `json:"created"`
	Ready   bool              `json:"ready"` // all its models are up
	Parts   []ProjectPlanPart `json:"parts"`
	Docs    []ProjectDoc      `json:"docs,omitempty"`
	// Assemblies are the assemblies of the plan, with the addresses of their models.
	Assemblies []ProjectAssembly `json:"assemblies,omitempty"`
	Builds  []ProjectBuild    `json:"builds,omitempty"`
}

// ProjectAssembly is an assembly of a plan, with the address of the model of each item, in the
// order of the items.
type ProjectAssembly struct {
	PlanAssembly
	ModelURLs []string `json:"model_urls,omitempty"`
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
	Part    int    `json:"part"`
	Count   int    `json:"count"`
	Name    string `json:"name,omitempty"`
	Variant string `json:"variant,omitempty"`
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
	Printer       string      `json:"printer"`
	Process       string      `json:"process"`
	Profile       string      `json:"profile,omitempty"`
	Material      string      `json:"material,omitempty"`
	MaterialName  string      `json:"material_name,omitempty"`
	MaterialColor string      `json:"material_color,omitempty"`
	Parts         []PlatePart `json:"parts"`
	File          string      `json:"file,omitempty"`
	Size          int64       `json:"size,omitempty"`
	SHA256        string      `json:"sha256,omitempty"`
	Preview       string      `json:"preview,omitempty"`
	Picture       string      `json:"picture,omitempty"`
	Seconds       int         `json:"seconds,omitempty"`
	Grams         float64     `json:"grams,omitempty"`
	ML            float64     `json:"ml,omitempty"`
	Layers        int         `json:"layers,omitempty"`
	LayerHeight   float64     `json:"layer_height,omitempty"`
	Slicer        string      `json:"slicer,omitempty"`
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

// BuildProgress is the body of POST /api/v1/builds/<id>/progress: what the slice run does now,
// for example "Slicing plate 2 of 3: Pod door (fine) x2".
type BuildProgress struct {
	Text string `json:"text"`
	// Phase is the step of the run: "setup" (the checkout and the slicer), "slice" or "upload".
	// An older Action sends no phase.
	Phase string `json:"phase,omitempty"`
	// Plate is the number of the plate that slices now, from 1, in the phase "slice".
	Plate int `json:"plate,omitempty"`
}

// The phases of BuildProgress.
const (
	PhaseSetup  = "setup"
	PhaseSlice  = "slice"
	PhaseUpload = "upload"
)

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
	// Progress is what the slice run does now, while the build slices.
	Progress string `json:"progress,omitempty"`
	// Phase and Plate are the step of the slice run, as BuildProgress sends them.
	Phase string `json:"phase,omitempty"`
	Plate int    `json:"plate,omitempty"`
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
	// SentTo is the name of the printer that the owner sent the plate to, SentAt the time, and
	// TakenAt the time when the deck of the printer put it in its queue.
	SentTo  string    `json:"sent_to,omitempty"`
	SentAt  time.Time `json:"sent_at,omitzero"`
	TakenAt time.Time `json:"taken_at,omitzero"`
	// Queued is the file of the plate in the queue of a printer, from the last report of the
	// deck, or nil when the plate is in no queue (for example after a removal on the deck).
	Queued *ProjectQueued `json:"queued,omitempty"`
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

// PlateSend is the body of POST .../builds/<build>/plates/<n>/send: the printer at the service
// that gets the approved plate in its queue.
type PlateSend struct {
	Printer string `json:"printer"`
}

// SentPlate is an approved plate that the owner sent to the printer of a deck (GET
// /api/v1/printers/<id>/plates): the deck downloads the file, adds it to its queue, and says so
// with POST /api/v1/printers/<id>/plates/taken.
type SentPlate struct {
	Project     string `json:"project"`      // the ID of the project
	ProjectName string `json:"project_name"` // its name, for the queue of the deck
	Build       string `json:"build"`
	Plate       int    `json:"plate"` // the index of the plate in the build
	Name        string `json:"name"`  // the parts of the plate, for the queue
	File        string `json:"file"`  // the name of the file, for example "plate-1.gcode"
	URL         string `json:"url"`   // a signed address of the file
	Size        int64  `json:"size,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Seconds     int    `json:"seconds,omitempty"`
}

// PlateTaken is the body of POST /api/v1/printers/<id>/plates/taken.
type PlateTaken struct {
	Project string `json:"project"`
	Build   string `json:"build"`
	Plate   int    `json:"plate"`
}
