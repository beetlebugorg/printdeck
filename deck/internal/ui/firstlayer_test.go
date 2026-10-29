package ui

import (
	"testing"
	"time"
)

// layerPrinter is a printer with the plan of the slicer for each layer.
type layerPrinter struct {
	*recPrinter
	toLayer, toEnd float64
}

func (p layerPrinter) LayerLeft(pos int64, layer int) (float64, float64, bool) {
	return p.toLayer, p.toEnd, layer >= 1
}

// The time left of the first layer is the plan to the second layer, scaled by the time left of the
// print over its plan.
func TestFirstLayerLeft(t *testing.T) {
	a, pr, _ := newPrintApp(idle)
	st := idle
	if _, ok := a.firstLayerLeft(st); ok {
		t.Error("a time with no plan")
	}
	a.Printer = layerPrinter{recPrinter: pr, toLayer: 120, toEnd: 3600}
	st.HasTimeLeft, st.TimeLeft = true, 90*time.Minute // the print runs 1.5 times slower than the plan
	left, ok := a.firstLayerLeft(st)
	if !ok || left != 3*time.Minute {
		t.Errorf("left %v %v, want 3m0s", left, ok)
	}
}
