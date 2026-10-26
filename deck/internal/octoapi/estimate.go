package octoapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"printdeck/internal/printer"
)

// The time of a print of a G-code file on this printer, for the Cura plugin. Cura shows it in
// place of the time of the slicer.
//
//	POST /api/plugin/printdeck/estimate
//	(the G-code file as the body, with Content-Encoding: gzip or no encoding)
//
//	{"moves": 5850, "prep": 690, "total": 6540, "planned": 5625, "plan_factor": 1.04,
//	 "plan_prints": 7, "steps": ["home", "heat-nozzle", "wipe", "cool", "probe", "heat-bed", "heat-nozzle", "prime"]}
//
// The fields are seconds and the parts of printer.FileEstimate. total is the time left that the
// deck shows at the start of the print. The deck keeps the file in memory only: the SD card gets
// no write. A file of more than printer.MaxEstimateSize bytes gets 413, and a deck with no
// motion limits of the printer yet gets 503.

// estimateLimit is the size of the largest file of a request, in bytes.
var estimateLimit = int64(printer.MaxEstimateSize)

// serveEstimate plans the G-code file of the request.
func (s *Server) serveEstimate(w http.ResponseWriter, r *http.Request) {
	if s.Estimate == nil {
		http.NotFound(w, r)
		return
	}
	limit := estimateLimit
	var src io.Reader = http.MaxBytesReader(w, r.Body, limit)
	var buf bytes.Buffer
	switch enc := strings.ToLower(r.Header.Get("Content-Encoding")); enc {
	case "gzip":
		zr, err := gzip.NewReader(src)
		if err != nil {
			apiError(w, http.StatusBadRequest, "bad gzip body")
			return
		}
		src = zr
	case "", "identity":
		if n := r.ContentLength; n > 0 && n <= limit {
			buf.Grow(int(n))
		}
	default:
		apiError(w, http.StatusUnsupportedMediaType, "unknown encoding "+enc)
		return
	}
	_, err := io.Copy(&buf, io.LimitReader(src, limit+1))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge) || int64(buf.Len()) > limit:
		apiError(w, http.StatusRequestEntityTooLarge, printer.ErrTooLarge.Error())
		return
	case err != nil:
		apiError(w, http.StatusBadRequest, "the file did not read")
		return
	}
	est, err := s.Estimate(r.Context(), buf.Bytes())
	switch {
	case errors.Is(err, printer.ErrTooLarge):
		apiError(w, http.StatusRequestEntityTooLarge, err.Error())
	case err != nil:
		apiError(w, http.StatusServiceUnavailable, err.Error())
	default:
		writeJSON(w, http.StatusOK, est)
	}
}

// estimateFields are the fields of the Server for the time of a file.
type estimateFields struct {
	// Estimate returns the time of a print of a G-code file (printer.Klippy.EstimateFile), or
	// is nil.
	Estimate func(ctx context.Context, data []byte) (printer.FileEstimate, error)
}
