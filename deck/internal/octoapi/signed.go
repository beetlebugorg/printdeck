package octoapi

import (
	"net/http"
	"path"
	"strings"
)

// The setting "Signed prints only" (package signing). With it on, the API queues and starts only
// G-code files with a good signature of a trusted slicer. A file that fails the check does not go
// in the store or the queue: the reply is 422 with the reason, and the touch screen shows it. The
// API also refuses G-code commands (POST /api/printer/command), because those lines have no
// signature.

// signedOnly tells if "Signed prints only" is on.
func (s *Server) signedOnly() bool {
	return s.Signing != nil && s.Signing.SignedOnly()
}

// rejection is the error of uploadCheck: the reason for the rejection.
type rejection string

func (r rejection) Error() string { return string(r) }

// uploadCheck returns the check of an upload of the file name for Store.SaveChecked, or nil when
// "Signed prints only" is off or the file is an STL model.
func (s *Server) uploadCheck(name string) func(string) error {
	if !s.signedOnly() || strings.EqualFold(path.Ext(name), ".stl") {
		return nil
	}
	return func(tmp string) error {
		if reason := s.Signing.Allowed(tmp); reason != "" {
			return rejection(reason)
		}
		return nil
	}
}

// signatureCheck checks the G-code file p of the store. It returns "" when the deck can queue the
// file, or the reason for the rejection. An STL model has no signature, and is not checked: the
// API never prints it.
func (s *Server) signatureCheck(p string) string {
	if s.Signing == nil || strings.EqualFold(path.Ext(p), ".stl") {
		return ""
	}
	full, err := s.Store.FullPath(p)
	if err != nil {
		return "File not found"
	}
	return s.Signing.Allowed(full)
}

// reject answers a rejected file, and shows the reason on the touch screen.
func (s *Server) reject(w http.ResponseWriter, p, reason string) {
	if s.Rejected != nil {
		s.Rejected(path.Base(p), reason)
	}
	apiError(w, http.StatusUnprocessableEntity, reason)
}
