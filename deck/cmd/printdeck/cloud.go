package main

import (
	"os"
	"path/filepath"

	"printdeck/internal/cloud"
)

// newCloudLink returns the link of the deck to its cloud service (Settings > Cloud). The identity
// of the deck is the file cloud.json of the state folder: the deck writes it once, when it
// connects. The name of the deck at the service is the host name.
func newCloudLink(stateDir string, redraw func()) *cloud.Link {
	name, err := os.Hostname()
	if err != nil || name == "" {
		name = "Printdeck"
	}
	return &cloud.Link{Path: filepath.Join(stateDir, "cloud.json"), Name: name, OnChange: redraw}
}
