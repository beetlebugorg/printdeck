package printer

import (
	"bytes"
	"compress/gzip"
	"io"
)

// MaxGcodeGz is the largest G-code file after gzip that goes to the service for the 3D view of a
// share and of a print page. The service takes files of up to 16 MB.
const MaxGcodeGz = 15 << 20

// GcodeGz returns the G-code file at path, compressed with gzip, for the 3D view of the page of
// the print. It returns nil for a file that it cannot read, a file larger than maxModelSize, and
// a file that is larger than MaxGcodeGz after gzip.
func (v *View) GcodeGz(path string) []byte {
	if path == "" {
		return nil
	}
	body, err := v.src.Open(path)
	if err != nil {
		return nil
	}
	defer body.Close()
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	n, err := io.Copy(zw, io.LimitReader(body, maxModelSize+1))
	if err != nil || n > maxModelSize || zw.Close() != nil || buf.Len() > MaxGcodeGz {
		return nil
	}
	return buf.Bytes()
}
