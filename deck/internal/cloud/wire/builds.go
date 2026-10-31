package wire

import "time"

// The builds of a project from its GitHub repository (docs/specs/github-builds.md). An Action of
// the repository slices the parts, and uploads each build with an OIDC token of GitHub. The
// owner checks the preview of each part on the build page, and approves it or rejects it.

// BuildManifest is the body of POST /api/v1/builds: the parts of the build. The token of GitHub
// gives the repository, the ref and the commit. Commit is the head commit of a pull request,
// which the token does not give, or "" for the commit of the token.
type BuildManifest struct {
	Commit string      `json:"commit,omitempty"`
	PR     int         `json:"pr,omitempty"`
	Parts  []BuildPart `json:"parts"`
}

// BuildPart is one part of a build: one plate of one printer. File is the name of the sliced
// file in the build, Preview the name of the file of the 3D preview (path.bin for FDM, part.bin
// for resin), and Picture the name of a picture of the plate, or "".
type BuildPart struct {
	Name        string  `json:"name"`
	Printer     string  `json:"printer"` // the printer of printdeck.yml, for example "taz"
	Process     string  `json:"process"` // "fdm" or "resin"
	Material    string  `json:"material,omitempty"`
	Count       int     `json:"count,omitempty"`
	File        string  `json:"file"`
	Size        int64   `json:"size"`
	SHA256      string  `json:"sha256"`
	Preview     string  `json:"preview,omitempty"`
	Picture     string  `json:"picture,omitempty"`
	Seconds     int     `json:"seconds,omitempty"`
	Grams       float64 `json:"grams,omitempty"`
	ML          float64 `json:"ml,omitempty"`
	Layers      int     `json:"layers,omitempty"`
	LayerHeight float64 `json:"layer_height,omitempty"`
	Profile     string  `json:"profile,omitempty"`
	Slicer      string  `json:"slicer,omitempty"`
}

// BuildReply is the reply to POST /api/v1/builds: the ID of the build, and the grant for its
// files. The Action then uploads the files and sends POST /api/v1/builds/<id>/done.
type BuildReply struct {
	ID    string `json:"id"`
	URL   string `json:"url"` // the build page
	Grant Grant  `json:"grant"`
}

// The kinds of builds.
const (
	BuildTest    = "test"    // a pull request
	BuildMain    = "main"    // the default branch
	BuildBranch  = "branch"  // another branch
	BuildRelease = "release" // a tag
)

// The states of a part of a build.
const (
	PartWaiting  = "waiting" // for the approval of the owner
	PartApproved = "approved"
	PartRejected = "rejected"
	PartReplaced = "replaced" // a newer build of the same pull request or branch came
)

// ProjectBuild is a build on the project page and on the build page.
type ProjectBuild struct {
	ID      string             `json:"id"`
	Repo    string             `json:"repo"`
	Commit  string             `json:"commit"`
	Ref     string             `json:"ref"`
	Kind    string             `json:"kind"`
	PR      int                `json:"pr,omitempty"`
	Tag     string             `json:"tag,omitempty"`
	Branch  string             `json:"branch,omitempty"`
	Run     string             `json:"run,omitempty"` // the page of the workflow run on GitHub
	Created time.Time          `json:"created"`
	Ready   bool               `json:"ready"` // all its files are up
	Parts   []ProjectBuildPart `json:"parts"`
}

// ProjectBuildPart is a part of a build, with its state and the addresses of its files.
type ProjectBuildPart struct {
	BuildPart
	State      string    `json:"state"`
	By         string    `json:"by,omitempty"`
	At         time.Time `json:"at,omitzero"`
	Reason     string    `json:"reason,omitempty"`
	PreviewURL string    `json:"preview_url,omitempty"`
	PictureURL string    `json:"picture_url,omitempty"`
}

// BuildDecision is the body of POST /api/v1/projects/<id>/builds/<build>/parts/<n>/approve and
// .../reject: the reason of a rejection.
type BuildDecision struct {
	Reason string `json:"reason,omitempty"`
}

// RepoRequest is the body of PUT /api/v1/projects/<id>/repo: the GitHub repository of the
// project, "owner/name", or "" for none.
type RepoRequest struct {
	Repo string `json:"repo"`
}
