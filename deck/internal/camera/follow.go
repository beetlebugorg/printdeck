package camera

// Follow the part: during a print, a camera with the setting FollowPart zooms to the box of the
// part. The deck sets the PTZ position that shows the box with the margin of the time lapse crop
// (partMargin), and at the end of the print the camera goes back to the position of its settings.
// The stream of the camera is one stream, so each viewer sees the zoom.

// FollowPart sets the PTZ position of each camera with FollowPart on to show the box b of the
// part (the toolhead coordinates), with the bed at the toolhead Y bedY. A camera needs a zoom
// control, a valid PTZ map and a valid calibration. A camera with a PTZ change of the user during
// the print (SetPTZ) stays at that position. The deck calls it again while the print runs: a
// position that does not change has no effect. It returns the IDs of the cameras that follow.
func (h *Hub) FollowPart(b Box, bedY float64) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var ids []string
	for _, cs := range h.settings.Cameras {
		id := cs.ID
		pos, ok := h.partPosLocked(cs, b, bedY)
		if !ok || h.userMoved[id] {
			if h.byPart[id] {
				delete(h.follow, id)
				delete(h.byPart, id)
				h.calGen++
			}
			continue
		}
		ids = append(ids, id)
		if old, had := h.follow[id]; had && old == pos && h.byPart[id] {
			continue
		}
		if h.follow == nil {
			h.follow = map[string]PTZPos{}
		}
		if h.byPart == nil {
			h.byPart = map[string]bool{}
		}
		h.follow[id], h.byPart[id] = pos, true
		h.calGen++
	}
	return ids
}

// EndFollow ends the follow of the part of each camera: the cameras go back to the positions of
// their settings.
func (h *Hub) EndFollow() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id := range h.byPart {
		delete(h.follow, id)
		h.calGen++
	}
	h.byPart, h.userMoved = nil, nil
}

// partPosLocked returns the PTZ position of the camera cs that shows the box b, with the bed at
// bedY.
func (h *Hub) partPosLocked(cs CamSettings, b Box, bedY float64) (PTZPos, bool) {
	ctl, ok := h.ptzs[cs.ID]
	cal := h.cals[cs.ID]
	m := cs.PTZMap
	if !cs.FollowPart || cs.Off || !ok || cal == nil || !m.ValidAt(cs.Rotate) {
		return PTZPos{}, false
	}
	var d Device
	for _, dev := range h.devicesLocked() {
		if dev.ID == cs.ID {
			d = dev
		}
	}
	if !cal.ValidFor(d, h.formats[cs.ID]) {
		return PTZPos{}, false
	}
	r, ok := PartWindow(cal.ToWide(m), b, bedY)
	if !ok {
		return PTZPos{}, false
	}
	return m.PosFor(r, ctl), true
}

// PartWindow returns the rectangle of the wide frame that shows the box b with the bed at bedY,
// in normalized coordinates of the frames of the calibration wide (u0, v0, u1, v1): the projection
// of the box with the margin of the time lapse crop at each side. It is false when a corner of the
// box is behind the camera.
func PartWindow(wide *Calibration, b Box, bedY float64) ([4]float64, bool) {
	pts, ok := projectAll(func(p [3]float64) (float64, float64, bool) {
		return wide.ProjectAt(p, bedY, 1, 1, wide.Rotate)
	}, boxCorners(b))
	if !ok {
		return [4]float64{}, false
	}
	r := bounds(pts)
	if r[2] <= 0 || r[3] <= 0 || r[0] >= 1 || r[1] >= 1 {
		// The box is out of the picture.
		return [4]float64{}, false
	}
	mx, my := partMargin*(r[2]-r[0]), partMargin*(r[3]-r[1])
	return [4]float64{max(0, r[0]-mx), max(0, r[1]-my), min(1, r[2]+mx), min(1, r[3]+my)}, true
}
