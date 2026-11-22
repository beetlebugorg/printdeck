package printer

import (
	"log"
	"time"

	"printdeck/internal/bambu"
)

// The detection of a filament change from the reports of a Bambu Lab printer.
//
// A change starts at the first report during a print with stg_cur 4 (change), 22 (unload) or 24
// (load), or with no spool on the active extruder (snow, or tray_now on a printer with one
// extruder). It ends at the first report with a normal stg_cur and a spool other than the spool
// before the change. That state must stay for changeHold, because the printer can show a normal
// stage between two steps of a change. The resolution of a sample is the time between two reports.
//
// A pause, a report gap, the PREPARE state, or the end of the print drops the change. The first
// change of a print on layer 0 or 1 is the first load of the print and gives no sample: the
// G-code of an X2D file can load its first filament after "M73 L1".

const (
	changeHold     = 3 * time.Second  // the time of normal reports at the end of a change
	changeSettle   = 15 * time.Second // the longest wait for the report of the new spool
	changeGap      = 30 * time.Second // a longer time between two reports drops a change
	changeLongest  = 10 * time.Minute
	changeShortest = time.Second
)

// changeWatch follows the filament changes of the print now.
type changeWatch struct {
	last    time.Time // the last report
	hasSlot bool      // slot and nozzle are from a report of normal printing
	slot    int
	nozzle  int
	changed bool // the print had a change
	run     *changeRun
}

// changeRun is a change that runs. normal is the first report of normal printing, or zero.
type changeRun struct {
	start, normal time.Time
	from, nozzle  int
	material      string
	firstLoad     bool
}

// changeStage reports that the stg_cur value n is a step of a filament change.
func changeStage(n int) bool { return n == 4 || n == 22 || n == 24 }

// normalStage reports that the stg_cur value n is normal printing.
func normalStage(n int) bool { return n <= 0 || n == 255 }

// pauseStage reports that the stg_cur value n is a pause of the printer.
func pauseStage(n int) bool {
	switch n {
	case 5, 6, 16, 17, 20, 21, 23, 26, 27, 28, 30, 32, 33, 34, 35:
		return true
	}
	return false
}

// changeMaterial returns the material of the tray slot in r, or "".
func changeMaterial(r bambu.Report, slot int) string {
	t, _ := trayOf(r, slot)
	return t.Type
}

// observe reads the report r at now, and returns the sample of a change that ended.
func (w *changeWatch) observe(r bambu.Report, now time.Time) (FilamentChange, bool) {
	gap := !w.last.IsZero() && now.Sub(w.last) > changeGap
	w.last = now
	stage := r.Stage.Int()
	if gap || r.State != "RUNNING" || r.Preparing() || pauseStage(stage) {
		w.run, w.hasSlot = nil, false
		return FilamentChange{}, false
	}
	slot := r.ActiveSlot()
	normal := normalStage(stage) && slot >= 0
	if run := w.run; run != nil {
		return w.follow(run, r, now, slot, normal)
	}
	if normal {
		if w.hasSlot && slot != w.slot {
			log.Printf("bambu: tray %d to tray %d with no report of a change: no sample", w.slot, slot)
		}
		w.slot, w.nozzle, w.hasSlot = slot, r.ActiveExtruder(), true
		return FilamentChange{}, false
	}
	if w.hasSlot && (changeStage(stage) || slot < 0) {
		w.run = &changeRun{start: now, from: w.slot, nozzle: w.nozzle, material: changeMaterial(r, w.slot),
			firstLoad: !w.changed && r.Layer.Int() <= 1}
		w.changed = true
	}
	return FilamentChange{}, false
}

// follow reads a report during the change run.
func (w *changeWatch) follow(run *changeRun, r bambu.Report, now time.Time, slot int, normal bool) (FilamentChange, bool) {
	if now.Sub(run.start) > changeLongest {
		w.run, w.hasSlot = nil, false
		return FilamentChange{}, false
	}
	if !normal {
		run.normal = time.Time{}
		return FilamentChange{}, false
	}
	if run.normal.IsZero() {
		run.normal = now
	}
	held := now.Sub(run.normal)
	if slot != run.from && held >= changeHold {
		w.run = nil
		w.slot, w.nozzle, w.hasSlot = slot, r.ActiveExtruder(), true
		secs := run.normal.Sub(run.start)
		if run.firstLoad || secs < changeShortest {
			return FilamentChange{}, false
		}
		return FilamentChange{At: run.start, FromTray: run.from, ToTray: slot, NozzleChange: run.nozzle != r.ActiveExtruder(),
			FromMaterial: run.material, ToMaterial: changeMaterial(r, slot), Seconds: secs.Seconds()}, true
	}
	if held > changeSettle {
		w.run = nil
		w.slot, w.nozzle, w.hasSlot = slot, r.ActiveExtruder(), true
	}
	return FilamentChange{}, false
}

// watchChange feeds the report r to the change watch, and keeps a sample of a change that ended.
// The caller holds mu.
func (b *Bambu) watchChange(r bambu.Report, now time.Time) {
	if b.system {
		return
	}
	s, ok := b.change.observe(r, now)
	if !ok {
		return
	}
	if v, ok := plannedFlush(b.cur, s.FromTray, s.ToTray); ok {
		s.PlannedFlush = &v
	}
	b.HeatLog.AddFilamentChange(s)
}

// plannedFlush returns the flush that the file of the print p plans for a change from one tray to
// another. The filaments of a tray come from the mapping of the print. false means that the file
// gives no value, or more than one value for the two trays.
func plannedFlush(p bambuPrint, from, to int) (float64, bool) {
	if p.use == nil || p.use.Flush == nil {
		return 0, false
	}
	var found []float64
	for i, a := range p.mapping {
		if a != from {
			continue
		}
		for j, c := range p.mapping {
			if c != to {
				continue
			}
			if v, ok := p.use.Flush[[2]int{i + 1, j + 1}]; ok {
				found = append(found, v)
			}
		}
	}
	if len(found) != 1 {
		return 0, false
	}
	return found[0], true
}
