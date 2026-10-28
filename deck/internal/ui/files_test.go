package ui

import (
	"testing"

	"printdeck/internal/gcodemeta"
)

// The Files page shows the cleaned file name, and the title of a file that has one. The page of
// the file has the name as its heading and the file name in the slicer data.
func TestFileNames(t *testing.T) {
	a, pr, cv := newPrintApp(idle)
	a.pages = []page{pageFiles}
	a.Draw(cv)
	if !hasKey(a, "Benchy") || !hasKey(a, "Case body") {
		t.Fatalf("no cleaned names in the list")
	}
	pr.Meta_ = gcodemeta.Meta{Title: "Hex planter"}
	a.Draw(cv)
	if !hasKey(a, "Hex planter") {
		t.Fatalf("no title in the list")
	}
	pr.Meta_ = gcodemeta.Meta{}
	tapLabel(t, a, cv, "Case body")
	if a.topPage() != pageFile || a.file.Path != "case_body.gcode" {
		t.Fatalf("page %v, file %q", a.topPage(), a.file.Path)
	}
	a.Draw(cv)
	if a.lcdTitle != "Case body" {
		t.Errorf("heading %q", a.lcdTitle)
	}
	tapLabel(t, a, cv, "Print")
	if a.confirm == nil || a.confirm.question != "Print Case body?" {
		t.Errorf("question %+v", a.confirm)
	}
}
