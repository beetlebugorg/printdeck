package cloud

import "printdeck/internal/cloud/wire"

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
	FramesReply    = wire.FramesReply
	InviteRequest  = wire.InviteRequest
	InviteReply    = wire.InviteReply
	InviteView     = wire.InviteView
	PublicRequest  = wire.PublicRequest
	PublicReply    = wire.PublicReply
	PrintRequest   = wire.PrintRequest
	PrintReply     = wire.PrintReply
	PrintDone      = wire.PrintDone
)
