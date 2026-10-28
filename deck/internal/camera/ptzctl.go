package camera

// The PTZ controls of a camera (V4L2_CID_ZOOM_ABSOLUTE, V4L2_CID_PAN_ABSOLUTE and
// V4L2_CID_TILT_ABSOLUTE).
const (
	cidZoomAbsolute = 0x009a090d
	cidPanAbsolute  = 0x009a0908
	cidTiltAbsolute = 0x009a0909
)

// ctrlDev reads and sets the controls of a device: the V4L2 device (v4l2_linux.go), or the test
// camera.
type ctrlDev interface {
	// query returns the range of the control id, and false for a camera without it.
	query(id uint32) (PTZRange, bool)
	get(id uint32) (int, error)
	set(id uint32, v int) error
}

// ptzCtl sets the PTZ position of a camera with a zoom control.
type ptzCtl struct {
	d    ctrlDev
	p    PTZ
	want PTZPos // the position of the last apply
	set  bool   // apply has set want
}

// newPTZCtl returns the PTZ control of a camera, or nil for a camera with no zoom control. Pan and
// tilt are optional.
func newPTZCtl(d ctrlDev) *ptzCtl {
	z, ok := d.query(cidZoomAbsolute)
	if !ok || !z.Valid() {
		return nil
	}
	c := &ptzCtl{d: d, p: PTZ{Zoom: z}}
	if r, ok := d.query(cidPanAbsolute); ok && r.Valid() {
		c.p.Pan = r
	}
	if r, ok := d.query(cidTiltAbsolute); ok && r.Valid() {
		c.p.Tilt = r
	}
	c.p.Pos = c.p.Wide()
	c.read()
	return c
}

// apply sets the position of the settings s in the camera, and reports that the position changed.
// A camera can refuse a pan or a tilt at the smallest zoom: the position is then the position that
// the camera reports, and the next change of the setting tries again.
func (c *ptzCtl) apply(s CamSettings) bool {
	t := c.p.Target(s.PTZ)
	if c.set && t == c.want {
		return false
	}
	c.want, c.set = t, true
	old := c.p.Pos
	zoom := func() { c.d.set(cidZoomAbsolute, t.Zoom) }
	pan := func() {
		if c.p.Pan.Valid() {
			c.d.set(cidPanAbsolute, t.Pan)
		}
		if c.p.Tilt.Valid() {
			c.d.set(cidTiltAbsolute, t.Tilt)
		}
	}
	// A zoom out moves the crop to its place first, and a zoom in zooms first: the crop stays in
	// the sensor at each step.
	if t.Zoom < old.Zoom {
		pan()
		zoom()
	} else {
		zoom()
		pan()
	}
	c.read()
	return c.p.Pos != old
}

// read reads the position of the camera. A control that the camera does not read keeps the value
// of the last apply.
func (c *ptzCtl) read() {
	if v, err := c.d.get(cidZoomAbsolute); err == nil {
		c.p.Pos.Zoom = v
	} else if c.set {
		c.p.Pos.Zoom = c.want.Zoom
	}
	if c.p.Pan.Valid() {
		if v, err := c.d.get(cidPanAbsolute); err == nil {
			c.p.Pos.Pan = v
		} else if c.set {
			c.p.Pos.Pan = c.want.Pan
		}
	}
	if c.p.Tilt.Valid() {
		if v, err := c.d.get(cidTiltAbsolute); err == nil {
			c.p.Pos.Tilt = v
		} else if c.set {
			c.p.Pos.Tilt = c.want.Tilt
		}
	}
}
