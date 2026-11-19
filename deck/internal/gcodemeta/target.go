package gcodemeta

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"regexp"
)

// SlicedFor returns the printer that the print file at path is sliced for (Meta.Printer), or "".
// A sliced 3MF (a zip) has it in the G-code of its first plate.
func SlicedFor(path string) string {
	if isZip(path) {
		return slicedFor3MF(path)
	}
	m, err := Read(path)
	if err != nil {
		return ""
	}
	return m.Printer
}

// isZip reports that the file at path starts as a zip file.
func isZip(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var b [4]byte
	n, _ := io.ReadFull(f, b[:])
	return n == 4 && string(b[:]) == "PK\x03\x04"
}

// platePath is the G-code of a plate in a sliced 3MF.
var platePath = regexp.MustCompile(`^Metadata/plate_[0-9]+\.gcode$`)

// slicedFor3MF reads the head and the tail of the G-code of the first plate of the 3MF at path.
// The entry is compressed, so it reads the whole entry, and keeps only its head and its tail.
func slicedFor3MF(path string) string {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return ""
	}
	defer zr.Close()
	for _, f := range zr.File {
		if !platePath.MatchString(f.Name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return ""
		}
		data := headTail(rc)
		rc.Close()
		m, err := Parse(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return ""
		}
		return m.Printer
	}
	return ""
}

// headTail reads r and returns its first headSize bytes and its last tailSize bytes, which Parse
// reads of a file.
func headTail(r io.Reader) []byte {
	head := make([]byte, headSize)
	n, _ := io.ReadFull(r, head)
	head = head[:n]
	if n < headSize {
		return head
	}
	tail := make([]byte, 0, 2*tailSize)
	buf := make([]byte, 64<<10)
	for {
		k, err := r.Read(buf)
		tail = append(tail, buf[:k]...)
		if len(tail) > tailSize {
			tail = append(tail[:0], tail[len(tail)-tailSize:]...)
		}
		if err != nil {
			break
		}
	}
	return append(head, tail...)
}
