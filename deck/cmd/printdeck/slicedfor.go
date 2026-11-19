package main

import (
	"regexp"
	"strings"

	"printdeck/internal/gcodemeta"
)

// The check of the printer of a file: a file that is sliced for another printer does not print on
// this one. The deck reads the printer that the slicer wrote in the file (gcodemeta.SlicedFor),
// and refuses the file only when the name is clearly another printer: another printer of this
// deck, a Bambu Lab model on a printer that is not that model, or a printer with a Bambu Lab
// model and a file of another model. A file that names no printer, or a name that the deck does
// not know, goes through.

// deckPrinterName is a printer of the deck for the check: its name, and its model when the
// printer reports one (backendFormat).
type deckPrinterName struct {
	name, model string
	files       string // backendFormat: "gcode", "gcode.3mf", "ctb", or "" when not known
}

// names reports that the name s of a file is the printer p: one name has the other in it as whole
// words, with no case ("Bambu Lab X2D 0.4 nozzle" is "Bambu Lab X2D", "LulzBot TAZ Workhorse" is
// "TAZ Workhorse"). A short name of a printer ("A") does not match each name with an "a" in it.
func (p deckPrinterName) names(s string) bool {
	for _, n := range []string{p.name, p.model} {
		if hasWords(s, n) || hasWords(n, s) {
			return true
		}
	}
	return false
}

// hasWords reports that s has the words of w, at word edges, with no case.
func hasWords(s, w string) bool {
	w = strings.TrimSpace(w)
	if w == "" || strings.TrimSpace(s) == "" {
		return false
	}
	re, err := regexp.Compile(`(?i)(^|[^\pL\pN])` + regexp.QuoteMeta(w) + `($|[^\pL\pN])`)
	return err == nil && re.MatchString(s)
}

// isBambu reports a Bambu Lab model name.
func isBambu(s string) bool {
	s = strings.ToLower(s)
	return strings.HasPrefix(s, "bambu lab") || strings.HasPrefix(s, "bbl ")
}

// slicedForReason returns the reason to refuse a file that is sliced for the printer target on
// the printer self of a deck with the printers others, or "".
func slicedForReason(target string, self deckPrinterName, others []deckPrinterName) string {
	if strings.TrimSpace(target) == "" || self.names(target) {
		return ""
	}
	name := self.name
	if name == "" {
		name = "the printer of this deck"
	}
	refuse := "Sliced for " + target + "; this printer is " + name
	for _, o := range others {
		if o.names(target) {
			return refuse
		}
	}
	switch {
	case isBambu(target) && self.files == "gcode":
		return refuse // a Bambu Lab model on a printer of G-code
	case isBambu(self.model) && isBambu(target):
		return refuse // another Bambu Lab model
	}
	return ""
}

// printerCheck returns the check of the uploads of the printer id of the deck ("" for the first
// printer), for octoapi.Server.PrinterCheck.
func printerCheck(id string) func(tmpPath string) string {
	return func(tmp string) string {
		target := gcodemeta.SlicedFor(tmp)
		if target == "" {
			return ""
		}
		self, others := deckPrinterNames(id)
		return slicedForReason(target, self, others)
	}
}

// deckPrinterNames returns the printer id of the deck and the other printers, by their names and
// their models.
func deckPrinterNames(id string) (self deckPrinterName, others []deckPrinterName) {
	if deckPrinterList == nil {
		// A deck with one printer.
		if printerFormat != nil {
			self.files, self.model = printerFormat("")
		}
		return self, nil
	}
	for i, p := range deckPrinterList() {
		key := p.ID
		if i == 0 {
			key = ""
		}
		n := deckPrinterName{name: p.Name}
		if printerFormat != nil {
			n.files, n.model = printerFormat(key)
		}
		if key == id {
			self = n
		} else {
			others = append(others, n)
		}
	}
	return self, others
}
