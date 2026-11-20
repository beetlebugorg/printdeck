package main

import "testing"

// A Bambu Lab printer reports its model after the first link: the service gets it then, and the
// name of the printer at the service is the model when the printer has no name.
func TestProjectLinkModelOf(t *testing.T) {
	model := ""
	p := &projectLink{modelOf: func() string { return model }}
	if p.printerModel() != "" {
		t.Fatal("a model before the link")
	}
	model = "Bambu Lab X2D"
	if p.printerModel() != "Bambu Lab X2D" || p.printerName() != "Bambu Lab X2D" {
		t.Errorf("model %q, name %q", p.printerModel(), p.printerName())
	}
	p.name = "X2D"
	if p.printerName() != "X2D" {
		t.Errorf("name %q", p.printerName())
	}
}
