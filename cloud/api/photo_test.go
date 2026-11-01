package api

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

// exifCommentJPEG returns a JPEG of w x h pixels with an EXIF block (the orientation o, and the text of
// a GPS tag) and a comment after its SOI marker.
func exifCommentJPEG(t *testing.T, w, h, o int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	img.Set(0, 0, color.RGBA{0xff, 0, 0, 0xff})
	var enc bytes.Buffer
	if err := jpeg.Encode(&enc, img, nil); err != nil {
		t.Fatal(err)
	}
	// TIFF, big-endian: one IFD with the orientation tag (0x0112, SHORT, 1 value).
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08")
	tiff = binary.BigEndian.AppendUint16(tiff, 1)
	tiff = append(tiff, 0x01, 0x12, 0x00, 0x03, 0, 0, 0, 1)
	tiff = binary.BigEndian.AppendUint16(tiff, uint16(o))
	tiff = append(tiff, 0, 0, 0, 0, 0, 0)
	tiff = append(tiff, "GPSLatitude 40.0 N"...)
	segment := func(marker byte, body []byte) []byte {
		s := []byte{0xff, marker}
		s = binary.BigEndian.AppendUint16(s, uint16(len(body)+2))
		return append(s, body...)
	}
	out := append([]byte{}, enc.Bytes()[:2]...)
	out = append(out, segment(0xe1, append([]byte("Exif\x00\x00"), tiff...))...)
	out = append(out, segment(0xfe, []byte("taken at home"))...)
	return append(out, enc.Bytes()[2:]...)
}

// metaSegments returns the markers of the APP1 to APP15 and COM segments of a JPEG.
func metaSegments(b []byte) []byte {
	var out []byte
	for i := 2; i+4 <= len(b) && b[i] == 0xff; {
		m := b[i+1]
		if m == 0xda || m == 0xd9 {
			break
		}
		if m >= 0xe1 && m <= 0xef || m == 0xfe {
			out = append(out, m)
		}
		i += 2 + int(binary.BigEndian.Uint16(b[i+2:]))
	}
	return out
}

func TestCleanPhotoStripsMetadata(t *testing.T) {
	in := exifCommentJPEG(t, 40, 20, 6)
	if exifOrientation(in) != 6 || len(metaSegments(in)) != 2 {
		t.Fatalf("the test photo: orientation %d, segments %x", exifOrientation(in), metaSegments(in))
	}
	full, small, w, h, err := CleanPhoto(in)
	if err != nil {
		t.Fatal(err)
	}
	// Orientation 6 turns the photo: 40 x 20 becomes 20 x 40.
	if w != 20 || h != 40 {
		t.Errorf("size %d x %d, not 20 x 40", w, h)
	}
	for name, b := range map[string][]byte{"full": full, "small": small} {
		if m := metaSegments(b); len(m) > 0 {
			t.Errorf("%s: metadata segments %x", name, m)
		}
		for _, s := range []string{"Exif", "GPS", "taken at home"} {
			if bytes.Contains(b, []byte(s)) {
				t.Errorf("%s: has %q", name, s)
			}
		}
	}
}
