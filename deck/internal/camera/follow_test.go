package camera

import (
	"image"
	"testing"
)

// Follow the part zooms a camera with the setting to the box of the part, and the end of the
// print goes back to the position of the settings. A change of the user stops the follow.
func TestFollowPart(t *testing.T) {
	h := ptzHub(t)
	box := Box{Min: [3]float64{120, 120, 0}, Max: [3]float64{160, 160, 30}}
	if ids := h.FollowPart(box, 0); len(ids) != 0 {
		t.Fatalf("follow with the setting off: %v", ids)
	}
	s := h.Settings()
	s.SetCamera(CamSettings{ID: "test", FollowPart: true})
	h.SetSettings(s)
	if ids := h.FollowPart(box, 0); len(ids) != 0 {
		t.Fatalf("follow with no map: %v", ids)
	}
	h.SetPTZMap("test", testMap())
	if ids := h.FollowPart(box, 0); len(ids) != 1 {
		t.Fatalf("no follow: %v", ids)
	}
	p, _ := h.PTZ("test")
	if p.Pos.Zoom <= 100 {
		t.Fatalf("no zoom: %+v", p.Pos)
	}
	// The time lapse crop of the frames now is most of the frame.
	cal, ok := h.Cam("test").Calibration()
	if !ok {
		t.Fatal("no calibration while the camera follows")
	}
	cr, ok := cal.PartCrop(box, 0, image.Pt(960, 540), 0)
	if !ok || (cr.Dx() < 960*8/10 && cr.Dy() < 540*8/10) {
		t.Errorf("time lapse crop %v at %+v", cr, p.Pos)
	}
	// The wide calibration has the crop of the time lapse in the crop of the camera.
	wide := synthCalibration(t, newSynthCamera())
	wr, _ := PartWindow(wide, box, 0)
	cu, cv, k := testMap().Window(p.Pos)
	if wr[0] < cu-0.5/k-1e-3 || wr[2] > cu+0.5/k+1e-3 || wr[1] < cv-0.5/k-1e-3 || wr[3] > cv+0.5/k+1e-3 {
		t.Errorf("the crop of the camera %.3f %.3f %.2f misses the part %v", cu, cv, k, wr)
	}
	if h.Settings().Camera("test").PTZ != nil {
		t.Error("the follow changed the settings")
	}
	h.EndFollow()
	if p, _ := h.PTZ("test"); p.Pos != p.Wide() {
		t.Errorf("after the print: %+v", p.Pos)
	}
	// A change of the user during the print stops the follow until the end of the print.
	h.FollowPart(box, 0)
	user := PTZPos{Zoom: 150}
	h.SetPTZ("test", &user)
	h.FollowPart(box, 0)
	if p, _ := h.PTZ("test"); p.Pos != user {
		t.Errorf("follow after a change of the user: %+v", p.Pos)
	}
	h.EndFollow()
	if ids := h.FollowPart(box, 0); len(ids) != 1 {
		t.Errorf("no follow in the next print: %v", ids)
	}
}
