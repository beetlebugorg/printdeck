// Package deploy has the files that pwf deploy sends to AWS: the CloudFormation template and the
// static pages of the service.
package deploy

import "embed"

// Template is the CloudFormation template of the service.
//
//go:embed template.yaml
var Template string

// Web is the static pages, at the root of the data bucket.
//
//go:embed web
var Web embed.FS
