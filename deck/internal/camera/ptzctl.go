package camera

// The PTZ controls of a camera (V4L2_CID_ZOOM_ABSOLUTE, V4L2_CID_PAN_ABSOLUTE and
// V4L2_CID_TILT_ABSOLUTE).
const (
	cidZoomAbsolute = 0x009a090d
	cidPanAbsolute  = 0x009a0908
	cidTiltAbsolute = 0x009a0909
)

// ctrlDev reads and sets the controls of a device (v4l2_linux.go).
type ctrlDev interface {
	// query returns the range of the control id, and false for a camera without it.
	query(id uint32) (PTZRange, bool)
	set(id uint32, v int) error
}

// resetPTZ sets the PTZ controls of a camera to the wide position: the smallest zoom, and pan and
// tilt at their defaults. The deck crops the frames itself (DeckPTZ), so the camera must send its
// whole picture. A camera can keep a zoom of an older deck or of another program.
func resetPTZ(d ctrlDev) {
	if r, ok := d.query(cidPanAbsolute); ok && r.Valid() {
		d.set(cidPanAbsolute, r.Clamp(r.Default))
	}
	if r, ok := d.query(cidTiltAbsolute); ok && r.Valid() {
		d.set(cidTiltAbsolute, r.Clamp(r.Default))
	}
	if r, ok := d.query(cidZoomAbsolute); ok && r.Valid() {
		d.set(cidZoomAbsolute, r.Min)
	}
}
