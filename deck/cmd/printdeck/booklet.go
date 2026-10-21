package main

import (
	_ "embed"
	"html/template"
	"image/color"
	"log"
	"os"
	"path/filepath"

	"printdeck/internal/lcd"
	"printdeck/internal/ui"
)

// The booklet: the manual as the instruction booklet of an old game. The pages are 5.5 x 4.25
// inches (a quarter of a letter sheet), in landscape, with a bleed of 1/8 inch and crop marks.
// Each guide has a page of text, and each picture a page with its callouts. The page count is a
// multiple of 4, for a booklet with staples.

//go:embed booklet.html
var bookletHTML string

// bookPage is one page of the booklet.
type bookPage struct {
	Kind     string // "cover", "contents", "text", "figure", "lcd", "table", "notes", "back"
	N        int    // the page number
	Title    string // the heading, or "" on a page that continues a guide
	Chapter  string
	Paras    []string
	Steps    []string
	StepFrom int // the number of the first step
	Fig      *figure
	From     int // the number of the first callout on the page
	Callouts []callout
	Notes    []string
	Rows     [][2]string
	Contents []bookEntry
}

type bookEntry struct {
	Chapter, Title string
	Page           int
}

// The capacity of the pages, in characters: a page of text, and the column of the callouts.
const (
	bookText     = 900
	bookCallouts = 700
)

// bookPages makes the pages of the booklet from the guides.
func bookPages(secs []section) []bookPage {
	pages := []bookPage{{Kind: "cover"}, {Kind: "contents"}}
	var entries []bookEntry
	for _, s := range secs {
		entries = append(entries, bookEntry{Chapter: s.Chapter, Title: s.Title, Page: len(pages) + 1})
		// The text of the guide: the intro, then the steps, on as many pages as they need.
		p := bookPage{Kind: "text", Title: s.Title, Chapter: s.Chapter, StepFrom: 1}
		used := 0
		flush := func() {
			if len(p.Paras) > 0 || len(p.Steps) > 0 {
				pages = append(pages, p)
				p = bookPage{Kind: "text", Chapter: s.Chapter, StepFrom: p.StepFrom + len(p.Steps)}
				used = 0
			}
		}
		for _, t := range s.Intro {
			if used+len(t) > bookText {
				flush()
			}
			p.Paras, used = append(p.Paras, t), used+len(t)+40
		}
		for _, t := range s.Steps {
			if used+len(t) > bookText {
				flush()
			}
			p.Steps, used = append(p.Steps, t), used+len(t)+40
		}
		if s.ID == "lcd" { // the pictures of the display
			flush()
			pages = append(pages, bookPage{Kind: "lcd", Title: "The display of the printer", Chapter: s.Chapter, Rows: s.Table})
		} else if len(s.Table) > 0 {
			flush()
			pages = append(pages, bookPage{Kind: "table", Title: s.Title, Chapter: s.Chapter, Rows: s.Table})
		}
		flush()
		for i := range s.Figures {
			f := &s.Figures[i]
			fp := bookPage{Kind: "figure", Title: f.Caption, Chapter: s.Chapter, Fig: f, From: 1}
			used := 0
			for j, c := range f.Callouts {
				n := len(c.Title) + len(c.Text) + 30
				if used+n > bookCallouts && len(fp.Callouts) > 0 {
					pages = append(pages, fp)
					fp = bookPage{Kind: "figure", Title: f.Caption + " (more)", Chapter: s.Chapter, Fig: f, From: j + 1}
					used = 0
				}
				fp.Callouts, used = append(fp.Callouts, c), used+n
			}
			for _, nt := range f.Notes {
				if used+len(nt) > bookCallouts {
					pages = append(pages, fp)
					fp = bookPage{Kind: "text", Chapter: s.Chapter}
					used = 0
				}
				fp.Notes, used = append(fp.Notes, nt), used+len(nt)
			}
			pages = append(pages, fp)
		}
	}
	pages[1].Contents = entries
	// The back cover is the last page, and the count is a multiple of 4.
	for (len(pages)+1)%4 != 0 {
		pages = append(pages, bookPage{Kind: "notes", Title: "Notes"})
	}
	pages = append(pages, bookPage{Kind: "back"})
	for i := range pages {
		pages[i].N = i + 1
	}
	return pages
}

// writeBooklet writes DIR/booklet.html and the pictures of its cover.
func writeBooklet(dir string, secs []section) {
	// The title of the cover, in the font of the printer display.
	writePNG(filepath.Join(dir, "img", "cover-title.png"),
		lcd.DotLines([]string{"PRINTDECK"}, 9, "8x16", 7, rgb(0x1f4bd1), rgb(0x2a58e0), rgb(0xf2f6ff)))
	status := &lcd.Screen{Status: &lcd.Status{State: "READY PLA", Clock: "14:24", Message: "XYZ HOMED", Tool: 210, ToolT: 210,
		Bed: 60, BedT: 60, Fan: 1, Position: "X110 Y110 Z5.00"}}
	writePNG(filepath.Join(dir, "img", "cover-status.png"), lcd.DotImage(status.Bits(3), 4, rgb(0x1f4bd1), rgb(0x2a58e0), rgb(0xf2f6ff)))

	tpl := template.Must(template.New("booklet").Funcs(template.FuncMap{
		"inc":  func(i int) int { return i + 1 },
		"add":  func(a, b int) int { return a + b },
		"even": func(n int) bool { return n%2 == 0 },
		"W":    func() int { return ui.W },
		"G":    func() int { return gutter },
		"VW":   func() int { return ui.W + 2*gutter },
		"H":    func() int { return ui.BaseH },
		"shown": func(p bookPage, n int) bool { // a badge of a callout on this page
			return n >= p.From && n < p.From+len(p.Callouts)
		},
	}).Parse(bookletHTML))
	out, err := os.Create(filepath.Join(dir, "booklet.html"))
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()
	if err := tpl.Execute(out, map[string]any{"Pages": bookPages(secs), "Version": osVersion()}); err != nil {
		log.Fatal(err)
	}
}

func rgb(v uint32) color.RGBA { return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255} }
