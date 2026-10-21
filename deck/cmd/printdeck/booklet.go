package main

import (
	_ "embed"
	"html/template"
	"image/color"
	"log"
	"os"
	"path/filepath"

	"printdeck/internal/lcd"
)

// The booklet: the manual as the instruction booklet of an old game. The pages are half letter,
// 5.5 x 8.5 inches. Each guide has a page of text, and each picture a page with its callouts. The
// page count is a multiple of 4: two pages go on each side of a letter sheet, and the sheets are
// folded and stapled in the middle (booklet.html?sheets).

//go:embed booklet.html
var bookletHTML string

// bookPage is one page: the cover, the contents, a page of a guide, a notes page or the back.
type bookPage struct {
	Kind     string // "cover", "contents", "guide", "notes", "back"
	N        int    // the page number
	Chapter  string
	Blocks   []bookBlock // the parts of a page of a guide, from the top
	Contents []bookEntry
	Lines    []struct{} // the ruled lines of a notes page
}

// bookBlock is a part of a page of a guide.
type bookBlock struct {
	Kind     string // "title", "para", "note", "steps", "figure", "lcd", "table"
	Text     string
	Steps    []string
	StepFrom int // the number of the first step
	Fig      *figure
	From     int // the number of the first callout of the block
	Callouts []callout
	Notes    []string
	Rows     [][2]string
}

type bookEntry struct {
	Chapter, Title string
	Page           int
}

// The estimated heights on a page, in inches. A page of a guide has 7.4 inches for its blocks;
// the estimates use 7 inches, because they are not exact.
// The width of a line of text is about 82 characters, and of a column of callouts about 40.
const (
	bookPageH     = 7.0
	bookLine      = 0.19 // a line of text
	bookPartLine  = 0.157
	bookTitleH    = 0.5
	bookFigTitleH = 0.32
	bookFigTall   = 4.6 // a tall picture with its margins
	bookFigWide   = 2.3 // a wide picture over the width of the page
)

// lines is the number of lines of a text in a column of cols characters.
func lines(n, cols int) float64 { return float64((n + cols - 1) / cols) }

// bookPages makes the pages of the booklet from the guides. Each guide starts on a new page. Its
// text, steps and pictures then fill the pages in order. A picture stays with its callouts; the
// callouts that do not fit go on the next page, under the picture again. wide is for the pictures
// of a wide screen.
func bookPages(secs []section, wide bool) []bookPage {
	figH := bookFigTall
	if wide {
		figH = bookFigWide
	}
	pages := []bookPage{{Kind: "cover"}, {Kind: "contents"}}
	var entries []bookEntry
	for _, s := range secs {
		entries = append(entries, bookEntry{Chapter: s.Chapter, Title: s.Title, Page: len(pages) + 1})
		p := bookPage{Kind: "guide", Chapter: s.Chapter}
		used := 0.0
		add := func(b bookBlock, h float64) {
			if used+h > bookPageH && len(p.Blocks) > 0 {
				pages = append(pages, p)
				p, used = bookPage{Kind: "guide", Chapter: s.Chapter}, 0
			}
			p.Blocks, used = append(p.Blocks, b), used+h
		}
		add(bookBlock{Kind: "title", Text: s.Title}, bookTitleH)
		for _, t := range s.Intro {
			add(bookBlock{Kind: "para", Text: t}, lines(len(t), 82)*bookLine+0.1)
		}
		for i, t := range s.Steps {
			add(bookBlock{Kind: "steps", Steps: []string{t}, StepFrom: i + 1}, lines(len(t), 74)*bookLine+0.08)
		}
		if s.ID == "lcd" { // the pictures of the display
			add(bookBlock{Kind: "lcd", Rows: s.Table}, lines(len(s.Table), 2)*1.45)
		} else if len(s.Table) > 0 {
			for _, r := range s.Table {
				add(bookBlock{Kind: "table", Rows: [][2]string{r}}, lines(len(r[1]), 44)*0.17+0.12)
			}
		}
		for i := range s.Figures {
			f := &s.Figures[i]
			// The callouts in two columns, and the notes under them.
			partH := func(cs []callout) float64 {
				h := 0.0
				for _, c := range cs {
					h += lines(len(c.Title)+len(c.Text)+6, 40)*bookPartLine + 0.07
				}
				return h/2 + 0.12
			}
			notesH := 0.0
			for _, n := range f.Notes {
				notesH += lines(len(n), 80)*0.17 + 0.12
			}
			b := bookBlock{Kind: "figure", Fig: f, From: 1}
			title := f.Caption
			for j := 0; j < len(f.Callouts); j++ {
				next := append(b.Callouts[:len(b.Callouts):len(b.Callouts)], f.Callouts[j])
				// The callouts that do not fit on a page under the picture go to the next page.
				if bookFigTitleH+figH+partH(next) > bookPageH && len(b.Callouts) > 0 {
					b.Text = title
					add(b, bookFigTitleH+figH+partH(b.Callouts))
					b, title = bookBlock{Kind: "figure", Fig: f, From: j + 1}, f.Caption+" (more)"
					next = []callout{f.Callouts[j]}
				}
				b.Callouts = next
			}
			b.Text, b.Notes = title, f.Notes
			add(b, bookFigTitleH+figH+partH(b.Callouts)+notesH)
		}
		pages = append(pages, p)
	}
	pages[1].Contents = entries
	// The back cover is the last page, and the count is a multiple of 4.
	for (len(pages)+1)%4 != 0 {
		pages = append(pages, bookPage{Kind: "notes", Lines: make([]struct{}, 18)})
	}
	pages = append(pages, bookPage{Kind: "back"})
	for i := range pages {
		pages[i].N = i + 1
	}
	return pages
}

// writeBooklet writes DIR/booklet.html and the pictures of its cover.
func writeBooklet(dir string, secs []section, w, h int) {
	// The title of the cover, in the font of the printer display.
	writePNG(filepath.Join(dir, "img", "cover-title.png"),
		lcd.DotLines([]string{"PRINTDECK"}, 9, "8x16", 7, rgb(0x1f4bd1), rgb(0x2a58e0), rgb(0xf2f6ff)))
	status := &lcd.Screen{Status: &lcd.Status{State: "READY PLA", Clock: "2:24PM", Message: "XYZ HOMED", Tool: 210, ToolT: 210,
		Bed: 60, BedT: 60, Fan: 1, Position: "X110 Y110 Z5.00"}}
	writePNG(filepath.Join(dir, "img", "cover-status.png"), lcd.DotImage(status.Bits(3), 4, rgb(0x1f4bd1), rgb(0x2a58e0), rgb(0xf2f6ff)))

	tpl := template.Must(template.New("booklet").Funcs(template.FuncMap{
		"inc":  func(i int) int { return i + 1 },
		"add":  func(a, b int) int { return a + b },
		"even": func(n int) bool { return n%2 == 0 },
		"W":    func() int { return w },
		"G":    func() int { return gutter },
		"VW":   func() int { return w + 2*gutter },
		"H":    func() int { return h },
		"shown": func(b bookBlock, n int) bool { // a badge of a callout of this block
			return n >= b.From && n < b.From+len(b.Callouts)
		},
	}).Parse(bookletHTML))
	out, err := os.Create(filepath.Join(dir, "booklet.html"))
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()
	if err := tpl.Execute(out, map[string]any{"Pages": bookPages(secs, w > h), "Version": osVersion(), "Wide": w > h}); err != nil {
		log.Fatal(err)
	}
}

func rgb(v uint32) color.RGBA { return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255} }
