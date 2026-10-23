package main

import (
	_ "embed"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strconv"
	"strings"

	"rsc.io/qr"

	"printdeck/internal/printer"
	"printdeck/internal/remote"
	"printdeck/internal/ui"
)

// The pages of the rolls in the browser: the page of one roll, which the QR code of its label
// opens, with Load; and the sheet of labels, for round labels of 2 inches, 12 on a letter sheet
// (Avery 22807). The login of the remote screen protects them.

//go:embed roll.html
var rollHTML string

//go:embed labels.html
var labelsHTML string

var (
	rollPage   = template.Must(template.New("roll").Parse(rollHTML))
	labelsPage = template.Must(template.New("labels").Parse(labelsHTML))
)

// rollView is the page of one roll.
type rollView struct {
	ID, Number, Name, Line, Color string
	In                            bool   // the roll is in the printer
	Busy                          bool   // the printer prints: Load waits for the end
	Message                       string // after Load
	Failed                        bool
}

// labelView is one label of the sheet; an empty label keeps its place on the sheet.
type labelView struct {
	Number, Name, Line string
	QR                 template.HTML // an SVG
	Left, Top          float64       // inches from the corner of the sheet
}

// The places of the labels on the sheet: Avery 22807, round, 2 inches, 3 across and 4 down, with
// margins of 0.625 inches.
const (
	labelD, labelCols, labelRows = 2.0, 3, 4
	labelMarginX, labelMarginY   = 0.625, 0.625
)

func labelPlace(i int) (left, top float64) {
	gapX := (8.5 - 2*labelMarginX - labelCols*labelD) / (labelCols - 1)
	gapY := (11 - 2*labelMarginY - labelRows*labelD) / (labelRows - 1)
	c, r := i%labelCols, i/labelCols
	return labelMarginX + float64(c)*(labelD+gapX), labelMarginY + float64(r)*(labelD+gapY)
}

func registerRollPages(mux *http.ServeMux, rs *remote.Server, pr printerBackend, app *ui.App) {
	view := func(sp printer.Spool) rollView {
		v := rollView{ID: sp.ID, Number: sp.Number(), Name: sp.Label(), Color: sp.Color,
			Line: sp.Material + " · " + printer.Weight(sp.Left()) + " left"}
		if in, ok := pr.SpoolStore().In(); ok && in.ID == sp.ID {
			v.In = true
		}
		st := pr.State()
		v.Busy = st.Busy() && !st.Paused
		return v
	}
	roll := func(w http.ResponseWriter, r *http.Request) (printer.Spool, bool) {
		rolls := pr.SpoolStore()
		if rolls == nil {
			http.NotFound(w, r)
			return printer.Spool{}, false
		}
		sp, ok := rolls.Get(r.PathValue("id"))
		if !ok {
			http.Error(w, "The deck has no roll "+r.PathValue("id")+".", http.StatusNotFound)
		}
		return sp, ok
	}
	mux.HandleFunc("GET /roll/{id}", rs.Auth(func(w http.ResponseWriter, r *http.Request) {
		if sp, ok := roll(w, r); ok {
			rollPage.Execute(w, view(sp))
		}
	}))
	mux.HandleFunc("POST /roll/{id}/load", rs.Auth(func(w http.ResponseWriter, r *http.Request) {
		sp, ok := roll(w, r)
		if !ok {
			return
		}
		v := view(sp)
		switch {
		case v.Busy:
			v.Message, v.Failed = "The printer prints now. Load the roll after the print.", true
		case !app.LoadRoll(sp.ID):
			v.Message, v.Failed = "The deck has a filament change open. Finish it on the deck first.", true
		default:
			v.Message = fmt.Sprintf("The deck heats the nozzle for %s. Go to the printer, and follow the steps on its screen.", sp.Material)
		}
		rollPage.Execute(w, v)
	}))
	mux.HandleFunc("GET /rolls/labels", rs.Auth(func(w http.ResponseWriter, r *http.Request) {
		rolls := pr.SpoolStore()
		if rolls == nil {
			http.NotFound(w, r)
			return
		}
		host := r.Host
		if h, err := os.Hostname(); err == nil && h != "" && h != "localhost" {
			host = h + ".local"
		}
		// The rolls to print: the rolls with a tick, or all of them. The first label is the place
		// "from" on the sheet (1 to 12), so that a sheet with used labels prints on the rest.
		pick := map[string]bool{}
		for _, id := range r.URL.Query()["roll"] {
			pick[id] = true
		}
		from, _ := strconv.Atoi(r.URL.Query().Get("from"))
		from = max(1, min(labelCols*labelRows, from))
		type choice struct {
			ID, Number, Name string
			On               bool
		}
		var choices []choice
		labels := make([]labelView, from-1)
		for _, sp := range rolls.List() {
			on := len(pick) == 0 || pick[sp.ID]
			choices = append(choices, choice{sp.ID, sp.Number(), sp.Label(), on})
			if on && len(labels) < labelCols*labelRows {
				labels = append(labels, labelView{Number: sp.Number(), Name: sp.Label(), Line: sp.Material,
					QR: qrSVG("http://" + host + "/roll/" + sp.ID)})
			}
		}
		for i := range labels {
			labels[i].Left, labels[i].Top = labelPlace(i)
		}
		var places []int
		for i := 1; i <= labelCols*labelRows; i++ {
			places = append(places, i)
		}
		labelsPage.Execute(w, map[string]any{"Labels": labels, "Choices": choices, "From": from, "Places": places,
			"Host": host})
	}))
}

// qrSVG is a QR code of text as an SVG, with a quiet zone of 4 modules.
func qrSVG(text string) template.HTML {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return ""
	}
	n := code.Size + 8
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`, n, n)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, n, n)
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+4, y+4)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return template.HTML(b.String())
}
