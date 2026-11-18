package ui

import (
	"path/filepath"
	"testing"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/signing"
)

// Settings > Signed prints: the toggle needs a trusted slicer, and a tap on a slicer asks to
// remove it.
func TestSignedPrintsPage(t *testing.T) {
	a, _, _ := newPrintApp(idle)
	cv := gfx.New(a.Screen().W, a.Screen().H)
	a.Signing = &signing.Store{Path: filepath.Join(t.TempDir(), "signing.json")}
	a.open(pageSettings)
	tapLabel(t, a, cv, "Signed prints")
	if a.topPage() != pageSigning {
		t.Fatalf("page %v", a.topPage())
	}
	// With no trusted slicer, the toggle explains and stays off.
	tapLabel(t, a, cv, "Signed prints only")
	if a.Signing.SignedOnly() || a.confirm == nil {
		t.Fatal("the setting went on with no trusted slicer")
	}
	a.confirm = nil
	k, err := a.Signing.Add("zslice", "ed25519 6kpsY+KcUgq+9VB7Ey7F+ZVHdq6+vnuSQh7qaRRG0iw=", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	tapLabel(t, a, cv, "Signed prints only")
	if !a.Signing.SignedOnly() {
		t.Fatal("the setting is off")
	}
	tapLabel(t, a, cv, "zslice")
	if a.confirm == nil || a.confirm.yes != "Remove" {
		t.Fatalf("no question to remove %s", k.ID)
	}
	a.confirm.onYes()
	if len(a.Signing.Keys()) != 0 {
		t.Error("the key is still trusted")
	}
	if got := signingLine(true, 2); got != "Only signed prints · 2 trusted slicers" {
		t.Errorf("line %q", got)
	}
}
