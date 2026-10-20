// Package motion_report reports the stepper and toolhead positions: the live position for the
// status, the API dumps of the step queues and move queues, and the log of them at a shutdown.
//
// Ported to Go from klippy/extras/motion_report.py of Klipper
// (https://github.com/Klipper3d/klipper), commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f.
//
//	Copyright (C) 2021  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package motion_report

import (
	"fmt"
	"log"
	"math"
	"sort"
	"strings"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/extras/bulk_sensor"
	"printdeck/internal/klipper/extras/motion_queuing"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/host"
	"printdeck/internal/klipper/mcu"
	"printdeck/internal/klipper/motion"
	"printdeck/internal/klipper/stepper"
)

// DumpStepper extracts the queue_step messages of a stepper.
type DumpStepper struct {
	printer        *host.Printer
	mcuStepper     *stepper.MCUStepper
	lastBatchClock uint64
	batchBulk      *bulk_sensor.BatchBulkHelper
}

func newDumpStepper(printer *host.Printer, s *stepper.MCUStepper) *DumpStepper {
	ds := &DumpStepper{printer: printer, mcuStepper: s}
	ds.batchBulk = bulk_sensor.NewBatchBulkHelper(printer, ds.processBatch, nil, nil, bulk_sensor.BatchInterval)
	apiResp := map[string]any{"header": []string{"interval", "count", "add"}}
	ds.batchBulk.AddMuxEndpoint("motion_report/dump_stepper", "name", s.Name(), apiResp)
	return ds
}

// StepQueue returns the queue_step commands between two clocks, the oldest first.
func (ds *DumpStepper) StepQueue(startClock, endClock uint64) []motion.HistorySteps {
	var res [][]motion.HistorySteps
	for {
		data := ds.mcuStepper.DumpSteps(128, startClock, endClock)
		if len(data) == 0 {
			break
		}
		res = append(res, data)
		if len(data) < 128 {
			break
		}
		endClock = data[len(data)-1].FirstClock
	}
	var out []motion.HistorySteps
	for i := len(res) - 1; i >= 0; i-- {
		for j := len(res[i]) - 1; j >= 0; j-- {
			out = append(out, res[i][j])
		}
	}
	return out
}

func (ds *DumpStepper) logSteps(data []motion.HistorySteps) {
	if len(data) == 0 {
		return
	}
	out := []string{fmt.Sprintf("Dumping stepper '%s' (%s) %d queue_step:", ds.mcuStepper.Name(),
		ds.mcuStepper.MCU().Name(), len(data))}
	for i, s := range data {
		out = append(out, fmt.Sprintf("queue_step %d: t=%d p=%d i=%d c=%d a=%d", i, s.FirstClock,
			s.StartPosition, s.Interval, s.StepCount, s.Add))
	}
	log.Print(strings.Join(out, "\n"))
}

func (ds *DumpStepper) processBatch(eventtime float64) map[string]any {
	data := ds.StepQueue(ds.lastBatchClock, 1<<63)
	if len(data) == 0 {
		return map[string]any{}
	}
	for i, d := range data {
		if d.StepCount == 0 {
			// End block on a set_position marker to simplify timing
			data = data[:i+1]
			break
		}
	}
	ctpt := ds.mcuStepper.MCU().ClockToPrintTime
	first := data[0]
	firstClock := first.FirstClock
	firstTime := ctpt(float64(firstClock))
	lastClock := data[len(data)-1].LastClock
	ds.lastBatchClock = lastClock
	lastTime := ctpt(float64(lastClock))
	mcuPos := first.StartPosition
	startPosition := ds.mcuStepper.MCUToCommandedPosition(mcuPos)
	stepDist := ds.mcuStepper.StepDist()
	tdata := make([][3]int, len(data))
	for i, s := range data {
		tdata[i] = [3]int{s.Interval, s.StepCount, s.Add}
	}
	return map[string]any{"data": tdata, "start_position": startPosition, "start_mcu_position": mcuPos,
		"step_distance": stepDist, "first_clock": firstClock, "first_step_time": firstTime,
		"last_clock": lastClock, "last_step_time": lastTime}
}

const neverTime = 9999999999999999.

// trapqMove is a move of a dump: time, duration, start velocity, acceleration, start position,
// direction.
type trapqMove struct {
	time, duration, startVelocity, acceleration float64
	startPosition, direction                    [3]float64
}

func (m trapqMove) list() []any {
	return []any{m.time, m.duration, m.startVelocity, m.acceleration, m.startPosition[:], m.direction[:]}
}

// DumpTrapQ extracts the moves of a move queue.
type DumpTrapQ struct {
	printer       *host.Printer
	name          string
	trapq         *motion.TrapQ
	lastBatchMsg  trapqMove
	motionQueuing *motion_queuing.PrinterMotionQueuing
	batchBulk     *bulk_sensor.BatchBulkHelper
}

func newDumpTrapQ(printer *host.Printer, name string, tq *motion.TrapQ) *DumpTrapQ {
	dt := &DumpTrapQ{printer: printer, name: name, trapq: tq,
		motionQueuing: printer.LookupObject("motion_queuing").(*motion_queuing.PrinterMotionQueuing)}
	dt.batchBulk = bulk_sensor.NewBatchBulkHelper(printer, dt.processBatch, nil, nil, bulk_sensor.BatchInterval)
	apiResp := map[string]any{"header": []string{"time", "duration", "start_velocity", "acceleration",
		"start_position", "direction"}}
	dt.batchBulk.AddMuxEndpoint("motion_report/dump_trapq", "name", name, apiResp)
	return dt
}

// ExtractTrapQ returns the moves between two print times, the oldest first.
func (dt *DumpTrapQ) ExtractTrapQ(startTime, endTime float64) []motion.PullMove {
	var res [][]motion.PullMove
	for {
		data := dt.trapq.ExtractOld(128, startTime, endTime)
		if len(data) == 0 {
			break
		}
		res = append(res, data)
		if len(data) < 128 {
			break
		}
		endTime = data[len(data)-1].PrintTime
	}
	var out []motion.PullMove
	for i := len(res) - 1; i >= 0; i-- {
		for j := len(res[i]) - 1; j >= 0; j-- {
			out = append(out, res[i][j])
		}
	}
	return out
}

func (dt *DumpTrapQ) logTrapQ(data []motion.PullMove) {
	if len(data) == 0 {
		return
	}
	out := []string{fmt.Sprintf("Dumping trapq '%s' %d moves:", dt.name, len(data))}
	for i, m := range data {
		out = append(out, fmt.Sprintf("move %d: pt=%.6f mt=%.6f sv=%.6f a=%.6f sp=(%.6f,%.6f,%.6f) ar=(%.6f,%.6f,%.6f)",
			i, m.PrintTime, m.MoveT, m.StartV, m.Accel, m.StartX, m.StartY, m.StartZ, m.XR, m.YR, m.ZR))
	}
	log.Print(strings.Join(out, "\n"))
}

// TrapQPosition returns the position and velocity at a print time, and false for no move.
func (dt *DumpTrapQ) TrapQPosition(printTime float64) ([3]float64, float64, bool) {
	data := dt.trapq.ExtractOld(1, 0., printTime)
	if len(data) == 0 {
		return [3]float64{}, 0, false
	}
	move := data[0]
	relTime := printTime - move.PrintTime
	moveTime := math.Max(0., math.Min(move.MoveT, relTime))
	dist := (move.StartV + .5*move.Accel*moveTime) * moveTime
	pos := [3]float64{move.StartX + move.XR*dist, move.StartY + move.YR*dist, move.StartZ + move.ZR*dist}
	velocity := 0.
	if relTime >= 0. && relTime <= move.MoveT {
		velocity = move.StartV + move.Accel*moveTime
	}
	return pos, velocity, true
}

func (dt *DumpTrapQ) processBatch(eventtime float64) map[string]any {
	qtime := dt.lastBatchMsg.time + math.Min(dt.lastBatchMsg.duration, 0.100)
	data := dt.ExtractTrapQ(qtime, neverTime)
	d := make([]trapqMove, len(data))
	for i, m := range data {
		d[i] = trapqMove{m.PrintTime, m.MoveT, m.StartV, m.Accel, [3]float64{m.StartX, m.StartY, m.StartZ},
			[3]float64{m.XR, m.YR, m.ZR}}
	}
	if len(d) > 0 {
		if startDripTime, ok := dt.motionQueuing.CheckDripTiming(); ok {
			// If homing, delay sending trapq entries that may change
			for len(d) > 0 && d[len(d)-1].time+d[len(d)-1].duration >= startDripTime {
				d = d[:len(d)-1]
			}
		}
	}
	if len(d) > 0 && d[0] == dt.lastBatchMsg {
		d = d[1:]
	}
	if len(d) == 0 {
		return map[string]any{}
	}
	dt.lastBatchMsg = d[len(d)-1]
	out := make([][]any, len(d))
	for i, m := range d {
		out[i] = m.list()
	}
	return map[string]any{"data": out}
}

const statusRefreshTime = 0.250

// toolhead is the part of the toolhead that the reports use.
type toolhead interface {
	TrapQ() *motion.TrapQ
	ExtraAxes() []any
}

type trapqOwner interface{ TrapQ() *motion.TrapQ }

type namer interface{ Name() string }

// PrinterMotionReport holds the dumps of the steppers and move queues.
type PrinterMotionReport struct {
	printer        *host.Printer
	steppers       map[string]*DumpStepper
	dtrapqs        map[string]*DumpTrapQ
	nextStatusTime float64
	lastStatus     map[string]any
}

func load(config *configfile.Wrapper) any {
	mr := &PrinterMotionReport{printer: host.Of(config), steppers: map[string]*DumpStepper{},
		dtrapqs: map[string]*DumpTrapQ{}}
	mr.lastStatus = map[string]any{"live_position": gcode.Coord{}, "live_velocity": 0.,
		"live_extruder_velocity": 0., "steppers": []string{}, "trapq": []string{}}
	// Register handlers
	mr.printer.RegisterEventHandler("klippy:connect", func(...any) { mr.connect() })
	mr.printer.RegisterEventHandler("klippy:analyze_shutdown", func(args ...any) {
		details, _ := args[1].(map[string]any)
		mr.handleAnalyzeShutdown(args[0].(string), details)
	})
	return mr
}

func init() { host.RegisterModule("motion_report", load, nil) }

// RegisterStepper adds the dump of a stepper.
func (mr *PrinterMotionReport) RegisterStepper(config *configfile.Wrapper, s *stepper.MCUStepper) {
	mr.steppers[s.Name()] = newDumpStepper(mr.printer, s)
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (mr *PrinterMotionReport) connect() {
	// Lookup toolhead trapq
	th := mr.printer.LookupObject("toolhead").(toolhead)
	mr.dtrapqs["toolhead"] = newDumpTrapQ(mr.printer, "toolhead", th.TrapQ())
	// Lookup extruder trapqs
	for i := 0; i < 99; i++ {
		ename := fmt.Sprintf("extruder%d", i)
		if ename == "extruder0" {
			ename = "extruder"
		}
		extruder, ok := mr.printer.TryLookupObject(ename).(trapqOwner)
		if !ok {
			break
		}
		mr.dtrapqs[ename] = newDumpTrapQ(mr.printer, ename, extruder.TrapQ())
	}
	// Lookup manual_stepper trapqs
	for _, o := range mr.printer.LookupObjects("manual_stepper") {
		mr.dtrapqs[o.Name] = newDumpTrapQ(mr.printer, o.Name, o.Obj.(trapqOwner).TrapQ())
	}
	// Populate 'trapq' and 'steppers' in get_status result
	mr.lastStatus["steppers"] = sortedKeys(mr.steppers)
	mr.lastStatus["trapq"] = sortedKeys(mr.dtrapqs)
}

// Shutdown handling

func (mr *PrinterMotionReport) dumpShutdown(dsteppers []*DumpStepper, shutdownTime, duration float64) {
	// Log stepper queue_steps on mcu that started shutdown (if any)
	startTime := shutdownTime - duration
	endTime := shutdownTime + duration
	for _, ds := range dsteppers {
		m := ds.mcuStepper.MCU()
		startClock := max(0, m.PrintTimeToClock(startTime))
		endClock := m.PrintTimeToClock(endTime)
		ds.logSteps(ds.StepQueue(uint64(startClock), uint64(endClock)))
	}
	// Log trapqs around time of shutdown
	for _, name := range sortedKeys(mr.dtrapqs) {
		dt := mr.dtrapqs[name]
		dt.logTrapQ(dt.ExtractTrapQ(startTime, endTime))
	}
	// Log estimated toolhead position at time of shutdown
	dt, ok := mr.dtrapqs["toolhead"]
	if !ok {
		return
	}
	if pos, _, ok := dt.TrapQPosition(shutdownTime); ok {
		log.Printf("Requested toolhead position at shutdown time %.6f: %v", shutdownTime, pos)
	}
}

func (mr *PrinterMotionReport) handleAnalyzeShutdown(msg string, details map[string]any) {
	switch msg {
	case "MCU shutdown":
		name, _ := details["mcu"].(string)
		m, ok := mr.printer.TryLookupObject(name).(*mcu.MCU)
		shutdownClock, hasClock := details["shutdown_clock"].(int64)
		if !ok || !hasClock {
			return
		}
		shutdownTime := m.ClockToPrintTime(float64(shutdownClock))
		var dsteppers []*DumpStepper
		for _, name := range sortedKeys(mr.steppers) {
			if ds := mr.steppers[name]; ds.mcuStepper.MCU() == m {
				dsteppers = append(dsteppers, ds)
			}
		}
		mr.dumpShutdown(dsteppers, shutdownTime, 0.100)
	case "Internal error in stepcompress":
		log.Printf("stepcompress error info: %v", details)
		stepGenTime, ok := details["step_gen_time"].(float64)
		if !ok {
			return
		}
		var dsteppers []*DumpStepper
		if name, _ := details["queue_name"].(string); mr.steppers[name] != nil {
			dsteppers = []*DumpStepper{mr.steppers[name]}
		}
		mr.dumpShutdown(dsteppers, stepGenTime, 1.0)
	}
}

// Status reporting

// Status returns the live position and velocities, refreshed each 0.25 seconds.
func (mr *PrinterMotionReport) Status(eventtime float64) map[string]any {
	if eventtime < mr.nextStatusTime || len(mr.dtrapqs) == 0 {
		return mr.lastStatus
	}
	mr.nextStatusTime = eventtime + statusRefreshTime
	xyzvelocity, evelocity := 0., 0.
	// Calculate current requested toolhead position
	th := mr.printer.LookupObject("toolhead").(toolhead)
	extraAxes := th.ExtraAxes()
	livePos := make([]float64, len(extraAxes))
	m := mr.printer.LookupObject("mcu").(*mcu.MCU)
	printTime := m.EstimatedPrintTime(eventtime)
	if pos, velocity, ok := mr.dtrapqs["toolhead"].TrapQPosition(printTime); ok {
		copy(livePos[:3], pos[:])
		xyzvelocity = velocity
	}
	// Calculate requested position of extra axes
	for i, ea := range extraAxes {
		n, ok := ea.(namer)
		if ea == nil || !ok {
			continue
		}
		if eh, ok := mr.dtrapqs[n.Name()]; ok {
			if pos, velocity, ok := eh.TrapQPosition(printTime); ok {
				livePos[i] = pos[0]
				if i == 3 {
					evelocity = velocity
				}
			}
		}
	}
	// Report status
	status := map[string]any{}
	for k, v := range mr.lastStatus {
		status[k] = v
	}
	var coord gcode.Coord
	copy(coord[:], livePos)
	status["live_position"] = coord
	status["live_velocity"] = xyzvelocity
	status["live_extruder_velocity"] = evelocity
	mr.lastStatus = status
	return status
}
