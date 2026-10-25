package mcu

import (
	"encoding/binary"
	"fmt"
	"math"

	"printdeck/internal/klipper/configfile"
	"printdeck/internal/klipper/gcode"
	"printdeck/internal/klipper/pins"
	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/serialhdl"
	"printdeck/internal/klipper/serialqueue"
)

// Wrapper classes for MCU pins

// Stepper is the part of a stepper that the endstops need.
type Stepper interface {
	MCU() *MCU
	OID() int
	Name() string
	NoteHomingEnd()
}

// The reasons of a trsync trigger.
const (
	ReasonEndstopHit   = 1
	ReasonHostRequest  = 2
	ReasonPastEndTime  = 3
	ReasonCommsTimeout = 4
)

// Trsync is the trigger synchronization of a board (MCU_trsync): it stops the steppers of the
// board when an endstop triggers.
type Trsync struct {
	mcu               *MCU
	trdispatch        *trdispatch
	trdispatchMCU     *trdispatchMCU
	reactor           *reactor.Reactor
	steppers          []Stepper
	oid               int
	cmdQueue          *serialqueue.CommandQueue
	responseTrsync    *AsyncResponseWrapper
	trsyncStartCmd    *CommandWrapper
	trsyncTimeoutCmd  *CommandWrapper
	trsyncTriggerCmd  *CommandWrapper
	trsyncQueryCmd    *CommandQueryWrapper
	stepperStopCmd    *CommandWrapper
	triggerCompletion *reactor.Completion
	homeEndClock      int64
	hasHomeEndClock   bool
}

func newTrsync(m *MCU, td *trdispatch) *Trsync {
	t := &Trsync{mcu: m, trdispatch: td, reactor: m.printer.Reactor()}
	t.oid = m.CreateOID()
	t.cmdQueue = m.AllocCommandQueue()
	m.RegisterConfigCallback(t.buildConfig)
	m.printer.RegisterEventHandler("klippy:shutdown", func(...any) { t.shutdown() })
	return t
}

func (t *Trsync) MCU() *MCU                               { return t.mcu }
func (t *Trsync) OID() int                                { return t.oid }
func (t *Trsync) CommandQueue() *serialqueue.CommandQueue { return t.cmdQueue }

// AddStepper adds a stepper to stop at a trigger.
func (t *Trsync) AddStepper(s Stepper) {
	for _, o := range t.steppers {
		if o == s {
			return
		}
	}
	t.steppers = append(t.steppers, s)
}

// Steppers lists the steppers.
func (t *Trsync) Steppers() []Stepper { return append([]Stepper(nil), t.steppers...) }

func (t *Trsync) buildConfig() {
	m := t.mcu
	// Setup config
	m.AddConfigCmd(fmtf("config_trsync oid=%d", t.oid), false, false)
	m.AddConfigCmd(fmtf("trsync_start oid=%d report_clock=0 report_ticks=0 expire_reason=0", t.oid), false, true)
	// Lookup commands
	t.trsyncStartCmd = m.LookupCommand("trsync_start oid=%c report_clock=%u report_ticks=%u expire_reason=%c", t.cmdQueue)
	t.trsyncTimeoutCmd = m.LookupCommand("trsync_set_timeout oid=%c clock=%u", t.cmdQueue)
	t.trsyncTriggerCmd = m.LookupCommand("trsync_trigger oid=%c reason=%c", t.cmdQueue)
	t.trsyncQueryCmd = m.LookupQueryCommand("trsync_trigger oid=%c reason=%c",
		"trsync_state oid=%c can_trigger=%c trigger_reason=%c clock=%u", int64(t.oid), t.cmdQueue, false)
	t.stepperStopCmd = m.LookupCommand("stepper_stop_on_trigger oid=%c trsync_oid=%c", t.cmdQueue)
	// Create trdispatch_mcu object
	setTimeoutTag := m.LookupCommand("trsync_set_timeout oid=%c clock=%u", nil).CommandTag()
	triggerTag := m.LookupCommand("trsync_trigger oid=%c reason=%c", nil).CommandTag()
	stateTag := m.LookupCommand("trsync_state oid=%c can_trigger=%c trigger_reason=%c clock=%u", nil).CommandTag()
	t.trdispatchMCU = t.trdispatch.newTrdispatchMCU(m.serial.SerialQueue(), t.cmdQueue, uint32(t.oid),
		setTimeoutTag, triggerTag, stateTag)
}

func (t *Trsync) shutdown() {
	if tc := t.triggerCompletion; tc != nil {
		t.triggerCompletion = nil
		tc.Complete(false)
	}
}

func (t *Trsync) handleTrsyncState(params serialhdl.Params) {
	if params.Int("can_trigger") == 0 {
		if tc := t.triggerCompletion; tc != nil {
			t.triggerCompletion = nil
			reason := params.Int("trigger_reason")
			isFailure := reason >= ReasonCommsTimeout
			t.reactor.AsyncComplete(tc, isFailure)
		}
	} else if t.hasHomeEndClock {
		clock := t.mcu.Clock32ToClock64(uint32(params.Int("clock")))
		if clock >= t.homeEndClock {
			t.hasHomeEndClock = false
			t.trsyncTriggerCmd.Send([]any{t.oid, ReasonPastEndTime}, 0, 0)
		}
	}
}

// Start starts the trigger synchronization at a print time.
func (t *Trsync) Start(printTime, reportOffset float64, triggerCompletion *reactor.Completion, expireTimeout float64) {
	t.triggerCompletion = triggerCompletion
	t.hasHomeEndClock = false
	clock := t.mcu.PrintTimeToClock(printTime)
	expireTicks := t.mcu.SecondsToClock(expireTimeout)
	expireClock := clock + expireTicks
	reportTicks := t.mcu.SecondsToClock(expireTimeout * .3)
	reportClock := clock + int64(float64(reportTicks)*reportOffset+.5)
	minExtendTicks := int64(float64(reportTicks)*.8 + .5)
	t.trdispatchMCU.setup(uint64(clock), uint64(expireClock), uint64(expireTicks), uint64(minExtendTicks))
	t.responseTrsync = t.mcu.RegisterSerialResponse(t.handleTrsyncState,
		"trsync_state oid=%c can_trigger=%c trigger_reason=%c clock=%u", int64(t.oid))
	t.trsyncStartCmd.Send([]any{t.oid, reportClock, reportTicks, ReasonCommsTimeout}, 0, clock)
	for _, s := range t.steppers {
		t.stepperStopCmd.Send([]any{s.OID(), t.oid}, 0, 0)
	}
	t.trsyncTimeoutCmd.Send([]any{t.oid, expireClock}, 0, clock)
}

// SetHomeEndTime sets the end time of the homing move.
func (t *Trsync) SetHomeEndTime(homeEndTime float64) {
	t.homeEndClock = t.mcu.PrintTimeToClock(homeEndTime)
	t.hasHomeEndClock = true
}

// Stop stops the trigger synchronization, and returns the trigger reason.
func (t *Trsync) Stop() int {
	t.responseTrsync.Unregister()
	t.responseTrsync = nil
	t.triggerCompletion = nil
	if t.mcu.IsFileoutput() {
		return ReasonEndstopHit
	}
	params := t.trsyncQueryCmd.Send([]any{t.oid, ReasonHostRequest}, 0, 0, true)
	for _, s := range t.steppers {
		s.NoteHomingEnd()
	}
	return int(params.Int("trigger_reason"))
}

const (
	trsyncTimeout          = 0.025
	trsyncSingleMCUTimeout = 0.250
)

// TriggerDispatch is the trigger synchronization of the boards of an endstop.
type TriggerDispatch struct {
	mcu               *MCU
	triggerCompletion *reactor.Completion
	trdispatch        *trdispatch
	trsyncs           []*Trsync
}

func NewTriggerDispatch(m *MCU) *TriggerDispatch {
	td := newTrdispatch()
	return &TriggerDispatch{mcu: m, trdispatch: td, trsyncs: []*Trsync{newTrsync(m, td)}}
}

func (d *TriggerDispatch) OID() int { return d.trsyncs[0].OID() }
func (d *TriggerDispatch) CommandQueue() *serialqueue.CommandQueue {
	return d.trsyncs[0].CommandQueue()
}

// AddStepper adds a stepper, with a trsync on its board.
func (d *TriggerDispatch) AddStepper(s Stepper) {
	var trsync *Trsync
	for _, t := range d.trsyncs {
		if t.MCU() == s.MCU() {
			trsync = t
			break
		}
	}
	if trsync == nil {
		trsync = newTrsync(s.MCU(), d.trdispatch)
		d.trsyncs = append(d.trsyncs, trsync)
	}
	trsync.AddStepper(s)
	// Check for unsupported multi-mcu shared stepper rails
	sname := s.Name()
	if len(sname) >= 8 && sname[:8] == "stepper_" {
		prefix := sname
		if len(prefix) > 9 {
			prefix = prefix[:9]
		}
		for _, ot := range d.trsyncs {
			for _, o := range ot.Steppers() {
				if ot != trsync && len(o.Name()) >= len(prefix) && o.Name()[:len(prefix)] == prefix {
					configfile.Raise("Multi-mcu homing not supported on multi-mcu shared axis")
				}
			}
		}
	}
}

// Steppers lists the steppers of all boards.
func (d *TriggerDispatch) Steppers() []Stepper {
	var out []Stepper
	for _, t := range d.trsyncs {
		out = append(out, t.Steppers()...)
	}
	return out
}

// Start starts the trigger synchronization of all boards.
func (d *TriggerDispatch) Start(printTime float64) *reactor.Completion {
	d.triggerCompletion = d.mcu.printer.Reactor().NewCompletion()
	expireTimeout := trsyncTimeout
	if len(d.trsyncs) == 1 {
		expireTimeout = trsyncSingleMCUTimeout
	}
	for i, t := range d.trsyncs {
		reportOffset := float64(i) / float64(len(d.trsyncs))
		t.Start(printTime, reportOffset, d.triggerCompletion, expireTimeout)
	}
	d.trdispatch.start(ReasonHostRequest)
	return d.triggerCompletion
}

// WaitEnd waits until the trigger, or the end of the homing move.
func (d *TriggerDispatch) WaitEnd(endTime float64) {
	d.trsyncs[0].SetHomeEndTime(endTime)
	if d.mcu.IsFileoutput() {
		d.triggerCompletion.Complete(true)
	}
	d.triggerCompletion.Wait(reactor.Never, nil)
}

// Stop stops the trigger synchronization, and returns the first error reason, or the reason of
// the first board.
func (d *TriggerDispatch) Stop() int {
	d.trdispatch.stop()
	res := make([]int, len(d.trsyncs))
	for i, t := range d.trsyncs {
		res[i] = t.Stop()
	}
	for _, r := range res {
		if r >= ReasonCommsTimeout {
			return r
		}
	}
	return res[0]
}

// Endstop is an endstop pin (MCU_endstop).
type Endstop struct {
	mcu       *MCU
	pin       string
	pullup    int
	invert    int
	oid       int
	homeCmd   *CommandWrapper
	queryCmd  *CommandQueryWrapper
	restTicks int64
	dispatch  *TriggerDispatch
}

func newEndstop(m *MCU, params *pins.Params) *Endstop {
	e := &Endstop{mcu: m, pin: params.Pin, pullup: params.Pullup, invert: params.Invert}
	e.oid = m.CreateOID()
	m.RegisterConfigCallback(e.buildConfig)
	e.dispatch = NewTriggerDispatch(m)
	return e
}

func (e *Endstop) MCU() *MCU            { return e.mcu }
func (e *Endstop) AddStepper(s Stepper) { e.dispatch.AddStepper(s) }
func (e *Endstop) Steppers() []Stepper  { return e.dispatch.Steppers() }

func (e *Endstop) buildConfig() {
	// Setup config
	e.mcu.AddConfigCmd(fmtf("config_endstop oid=%d pin=%s pull_up=%d", e.oid, e.pin, e.pullup), false, false)
	e.mcu.AddConfigCmd(fmtf("endstop_home oid=%d clock=0 sample_ticks=0 sample_count=0"+
		" rest_ticks=0 pin_value=0 trsync_oid=0 trigger_reason=0", e.oid), false, true)
	// Lookup commands
	cmdQueue := e.dispatch.CommandQueue()
	e.homeCmd = e.mcu.LookupCommand("endstop_home oid=%c clock=%u sample_ticks=%u sample_count=%c"+
		" rest_ticks=%u pin_value=%c trsync_oid=%c trigger_reason=%c", cmdQueue)
	e.queryCmd = e.mcu.LookupQueryCommand("endstop_query_state oid=%c",
		"endstop_state oid=%c homing=%c next_clock=%u pin_value=%c", int64(e.oid), cmdQueue, false)
}

// HomeStart starts the homing of the endstop.
func (e *Endstop) HomeStart(printTime, sampleTime float64, sampleCount int, restTime float64, triggered bool) *reactor.Completion {
	clock := e.mcu.PrintTimeToClock(printTime)
	restTicks := e.mcu.PrintTimeToClock(printTime+restTime) - clock
	e.restTicks = restTicks
	triggerCompletion := e.dispatch.Start(printTime)
	trig := 0
	if triggered {
		trig = 1
	}
	e.homeCmd.Send([]any{e.oid, clock, e.mcu.SecondsToClock(sampleTime), sampleCount, restTicks,
		trig ^ e.invert, e.dispatch.OID(), ReasonEndstopHit}, 0, clock)
	return triggerCompletion
}

// HomeWait waits for the end of the homing, and returns the trigger time (0 for no trigger).
func (e *Endstop) HomeWait(homeEndTime float64) float64 {
	e.dispatch.WaitEnd(homeEndTime)
	e.homeCmd.Send([]any{e.oid, 0, 0, 0, 0, 0, 0, 0}, 0, 0)
	res := e.dispatch.Stop()
	if res >= ReasonCommsTimeout {
		panic(gcode.Errorf("Communication timeout during homing"))
	}
	if res != ReasonEndstopHit {
		return 0.
	}
	if e.mcu.IsFileoutput() {
		return homeEndTime
	}
	params := e.queryCmd.Send([]any{e.oid}, 0, 0, true)
	nextClock := e.mcu.Clock32ToClock64(uint32(params.Int("next_clock")))
	return e.mcu.ClockToPrintTime(float64(nextClock - e.restTicks))
}

// QueryEndstop returns the state of the endstop at a print time.
func (e *Endstop) QueryEndstop(printTime float64) int {
	clock := e.mcu.PrintTimeToClock(printTime)
	if e.mcu.IsFileoutput() {
		return 0
	}
	params := e.queryCmd.Send([]any{e.oid}, clock, 0, true)
	return int(params.Int("pin_value")) ^ e.invert
}

// DigitalOut is a digital output pin (MCU_digital_out).
type DigitalOut struct {
	mcu           *MCU
	oid           int
	pin           string
	invert        int
	startValue    int
	shutdownValue int
	maxDuration   float64
	lastClock     int64
	setCmd        *CommandWrapper
}

func newDigitalOut(m *MCU, params *pins.Params) *DigitalOut {
	d := &DigitalOut{mcu: m, pin: params.Pin, invert: params.Invert, maxDuration: 2.}
	m.RegisterConfigCallback(d.buildConfig)
	d.startValue, d.shutdownValue = d.invert, d.invert
	return d
}

func (d *DigitalOut) MCU() *MCU { return d.mcu }

// SetupMaxDuration sets the time after which the board turns the pin to its shutdown value.
func (d *DigitalOut) SetupMaxDuration(maxDuration float64) { d.maxDuration = maxDuration }

// SetupStartValue sets the start and shutdown values.
func (d *DigitalOut) SetupStartValue(startValue, shutdownValue float64) {
	d.startValue = boolInt(startValue != 0) ^ d.invert
	d.shutdownValue = boolInt(shutdownValue != 0) ^ d.invert
}

func (d *DigitalOut) buildConfig() {
	if d.maxDuration != 0 && d.startValue != d.shutdownValue {
		pinError("Pin with max duration must have start value equal to shutdown value")
	}
	mdurTicks := d.mcu.SecondsToClock(d.maxDuration)
	if mdurTicks > MaxScheduleTicks {
		pinError("Digital pin max duration too large")
	}
	d.mcu.RequestMoveQueueSlot()
	d.oid = d.mcu.CreateOID()
	d.mcu.AddConfigCmd(fmtf("config_digital_out oid=%d pin=%s value=%d default_value=%d max_duration=%d",
		d.oid, d.pin, d.startValue, d.shutdownValue, mdurTicks), false, false)
	d.mcu.AddConfigCmd(fmtf("update_digital_out oid=%d value=%d", d.oid, d.startValue), false, true)
	cmdQueue := d.mcu.AllocCommandQueue()
	d.setCmd = d.mcu.LookupCommand("queue_digital_out oid=%c clock=%u on_ticks=%u", cmdQueue)
}

// SetDigital sets the pin at a print time.
func (d *DigitalOut) SetDigital(printTime float64, value bool) {
	clock := d.mcu.PrintTimeToClock(printTime)
	d.setCmd.Send([]any{d.oid, clock, boolInt(value) ^ d.invert}, d.lastClock, clock)
	d.lastClock = clock
}

// PWM is a pwm output pin (MCU_pwm), in hardware or in software.
type PWM struct {
	mcu           *MCU
	hardwarePWM   bool
	cycleTime     float64
	maxDuration   float64
	oid           int
	pin           string
	invert        int
	startValue    float64
	shutdownValue float64
	lastClock     int64
	lastValue     float64
	pwmMax        float64
	setCmd        *CommandWrapper
}

func newPWM(m *MCU, params *pins.Params) *PWM {
	p := &PWM{mcu: m, cycleTime: 0.100, maxDuration: 2., pin: params.Pin, invert: params.Invert}
	m.RegisterConfigCallback(p.buildConfig)
	p.startValue, p.shutdownValue = float64(p.invert), float64(p.invert)
	return p
}

func (p *PWM) MCU() *MCU { return p.mcu }

// SetupMaxDuration sets the time after which the board turns the pin to its shutdown value.
func (p *PWM) SetupMaxDuration(maxDuration float64) { p.maxDuration = maxDuration }

// SetupCycleTime sets the pwm cycle time.
func (p *PWM) SetupCycleTime(cycleTime float64, hardwarePWM bool) {
	p.cycleTime, p.hardwarePWM = cycleTime, hardwarePWM
}

// SetupStartValue sets the start and shutdown values, from 0 to 1.
func (p *PWM) SetupStartValue(startValue, shutdownValue float64) {
	if p.invert != 0 {
		startValue = 1. - startValue
		shutdownValue = 1. - shutdownValue
	}
	p.startValue = math.Max(0., math.Min(1., startValue))
	p.shutdownValue = math.Max(0., math.Min(1., shutdownValue))
	p.lastValue = p.startValue
}

func (p *PWM) buildConfig() {
	if p.maxDuration != 0 && p.startValue != p.shutdownValue {
		pinError("Pin with max duration must have start value equal to shutdown value")
	}
	cmdQueue := p.mcu.AllocCommandQueue()
	curtime := p.mcu.printer.Reactor().Monotonic()
	printtime := p.mcu.EstimatedPrintTime(curtime)
	p.lastClock = p.mcu.PrintTimeToClock(printtime + 0.200)
	cycleTicks := p.mcu.SecondsToClock(p.cycleTime)
	mdurTicks := p.mcu.SecondsToClock(p.maxDuration)
	if mdurTicks > MaxScheduleTicks {
		pinError("PWM pin max duration too large")
	}
	if p.hardwarePWM {
		p.pwmMax = p.mcu.ConstantFloat("PWM_MAX")
		p.mcu.RequestMoveQueueSlot()
		p.oid = p.mcu.CreateOID()
		p.mcu.AddConfigCmd(fmtf("config_pwm_out oid=%d pin=%s cycle_ticks=%d value=%d default_value=%d max_duration=%d",
			p.oid, p.pin, cycleTicks, int64(p.startValue*p.pwmMax), int64(p.shutdownValue*p.pwmMax), mdurTicks), false, false)
		svalue := int64(p.startValue*p.pwmMax + 0.5)
		p.mcu.AddConfigCmd(fmtf("queue_pwm_out oid=%d clock=%d value=%d", p.oid, p.lastClock, svalue), false, true)
		p.setCmd = p.mcu.LookupCommand("queue_pwm_out oid=%c clock=%u value=%hu", cmdQueue)
		return
	}
	// Software PWM
	if p.shutdownValue != 0. && p.shutdownValue != 1. {
		pinError("shutdown value must be 0.0 or 1.0 on soft pwm")
	}
	if cycleTicks > MaxScheduleTicks {
		pinError("PWM pin cycle time too large")
	}
	p.mcu.RequestMoveQueueSlot()
	p.oid = p.mcu.CreateOID()
	p.mcu.AddConfigCmd(fmtf("config_digital_out oid=%d pin=%s value=%d default_value=%d max_duration=%d",
		p.oid, p.pin, boolInt(p.startValue >= 1.0), boolInt(p.shutdownValue >= 0.5), mdurTicks), false, false)
	p.mcu.AddConfigCmd(fmtf("set_digital_out_pwm_cycle oid=%d cycle_ticks=%d", p.oid, cycleTicks), false, false)
	p.pwmMax = float64(cycleTicks)
	svalue := int64(p.startValue*float64(cycleTicks) + 0.5)
	p.mcu.AddConfigCmd(fmtf("queue_digital_out oid=%d clock=%d on_ticks=%d", p.oid, p.lastClock, svalue), true, false)
	p.setCmd = p.mcu.LookupCommand("queue_digital_out oid=%c clock=%u on_ticks=%u", cmdQueue)
}

// NextAlignedPrintTime returns the print time of the next pwm cycle start, for a software pwm
// with a value between 0 and 1.
func (p *PWM) NextAlignedPrintTime(printTime, allowEarly float64) float64 {
	// Filter cases where there is no need to sync anything
	if p.hardwarePWM {
		return printTime
	}
	if p.lastValue == 1. || p.lastValue == .0 {
		return printTime
	}
	// Simplify the calling and allow scheduling slightly earlier
	reqPtime := printTime - math.Min(allowEarly, 0.5*p.cycleTime)
	cycleTicks := p.mcu.SecondsToClock(p.cycleTime)
	reqClock := p.mcu.PrintTimeToClock(reqPtime)
	pulses := floorDiv(reqClock-p.lastClock+cycleTicks-1, cycleTicks)
	nextClock := p.lastClock + pulses*cycleTicks
	return p.mcu.ClockToPrintTime(float64(nextClock))
}

// SetPWM sets the pin at a print time, from 0 to 1.
func (p *PWM) SetPWM(printTime, value float64) {
	if p.invert != 0 {
		value = 1. - value
	}
	v := int64(math.Max(0., math.Min(1., value))*p.pwmMax + 0.5)
	clock := p.mcu.PrintTimeToClock(printTime)
	p.setCmd.Send([]any{p.oid, clock, v}, p.lastClock, clock)
	p.lastClock = clock
	p.lastValue = value
}

// Sample is an adc value at a print time.
type Sample struct {
	Time, Value float64
}

// ADC is an analog input pin (MCU_adc).
type ADC struct {
	mcu             *MCU
	pin             string
	minSample       float64
	maxSample       float64
	sampleTime      float64
	reportTime      float64
	sampleCount     int
	batchNum        int
	rangeCheckCount int
	reportClock     int64
	lastState       Sample
	oid             int
	callback        func(samples []Sample)
	invMaxADC       float64
}

func newADC(m *MCU, params *pins.Params) *ADC {
	a := &ADC{mcu: m, pin: params.Pin}
	m.RegisterConfigCallback(a.buildConfig)
	return a
}

func (a *ADC) MCU() *MCU { return a.mcu }

// SetupADCSample sets the sampling: the report time, the time and count of the samples of each
// report, the reports in a batch, and the range check.
func (a *ADC) SetupADCSample(reportTime, sampleTime float64, sampleCount, batchNum int, minval, maxval float64, rangeCheckCount int) {
	a.reportTime = reportTime
	a.sampleTime = sampleTime
	a.sampleCount = sampleCount
	a.batchNum = max(1, min(48/2, batchNum))
	a.minSample = minval
	a.maxSample = maxval
	a.rangeCheckCount = rangeCheckCount
}

// SetupADCCallback sets the function that gets the samples.
func (a *ADC) SetupADCCallback(callback func(samples []Sample)) { a.callback = callback }

// LastValue returns the last sample.
func (a *ADC) LastValue() Sample { return a.lastState }

func (a *ADC) buildConfig() {
	if a.sampleCount == 0 {
		return
	}
	a.oid = a.mcu.CreateOID()
	a.mcu.AddConfigCmd(fmtf("config_analog_in oid=%d pin=%s", a.oid, a.pin), false, false)
	clock := a.mcu.GetQuerySlot(a.oid)
	sampleTicks := a.mcu.SecondsToClock(a.sampleTime)
	mcuADCMax := a.mcu.ConstantFloat("ADC_MAX")
	maxADC := float64(a.sampleCount) * mcuADCMax
	if maxADC >= 1<<16 {
		configfile.Raise("ADC sample_count=%d too large for MCU", a.sampleCount)
	}
	a.invMaxADC = 1.0 / maxADC
	a.reportClock = a.mcu.SecondsToClock(a.reportTime)
	minSample := max(0, min(0xffff, int64(a.minSample*maxADC)))
	maxSample := max(0, min(0xffff, int64(math.Ceil(a.maxSample*maxADC))))
	// Setup periodic query and register response handler
	oldcmd := "query_analog_in oid=%c clock=%u sample_ticks=%u sample_count=%c" +
		" rest_ticks=%u min_value=%hu max_value=%hu range_check_count=%c"
	if a.batchNum == 1 && a.mcu.TryLookupCommand(oldcmd) != nil {
		a.mcu.AddConfigCmd(fmtf("query_analog_in oid=%d clock=%d sample_ticks=%d sample_count=%d rest_ticks=%d"+
			" min_value=%d max_value=%d range_check_count=%d", a.oid, clock, sampleTicks, a.sampleCount,
			a.reportClock, minSample, maxSample, a.rangeCheckCount), true, false)
		a.mcu.RegisterSerialResponse(a.oldHandleAnalogInState, "analog_in_state oid=%c next_clock=%u value=%hu", int64(a.oid))
		return
	}
	const bytesPerSample = 2
	bytesPerReport := a.batchNum * bytesPerSample
	a.mcu.AddConfigCmd(fmtf("query_analog_in oid=%d clock=%d sample_ticks=%d sample_count=%d rest_ticks=%d"+
		" bytes_per_report=%d min_value=%d max_value=%d range_check_count=%d", a.oid, clock, sampleTicks,
		a.sampleCount, a.reportClock, bytesPerReport, minSample, maxSample, a.rangeCheckCount), true, false)
	a.mcu.RegisterSerialResponse(a.handleAnalogInState, "analog_in_state oid=%c next_clock=%u values=%*s", int64(a.oid))
}

func (a *ADC) oldHandleAnalogInState(params serialhdl.Params) {
	lastValue := float64(params.Int("value")) * a.invMaxADC
	nextClock := a.mcu.Clock32ToClock64(uint32(params.Int("next_clock")))
	lastReadClock := nextClock - a.reportClock
	lastReadTime := a.mcu.ClockToPrintTime(float64(lastReadClock))
	a.lastState = Sample{lastReadTime, lastValue}
	if a.callback != nil {
		a.callback([]Sample{a.lastState})
	}
}

func (a *ADC) handleAnalogInState(params serialhdl.Params) {
	// Klipper reads the first value of the report (struct "<H").
	buf := params.Bytes("values")
	if len(buf) < 2 {
		return
	}
	values := []uint16{binary.LittleEndian.Uint16(buf)}
	nextClock := a.mcu.Clock32ToClock64(uint32(params.Int("next_clock")))
	num := len(values)
	samples := make([]Sample, num)
	for i, v := range values {
		samples[i] = Sample{a.mcu.ClockToPrintTime(float64(nextClock - int64(num-i)*a.reportClock)), float64(v) * a.invMaxADC}
	}
	a.lastState = samples[num-1]
	if a.callback != nil {
		a.callback(samples)
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// floorDiv divides as Python's // operator.
func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func fmtf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// PWMPin is a pwm output pin: an MCU pin, or a virtual pin of a chip such as multi_pin.
type PWMPin interface {
	MCU() *MCU
	SetupMaxDuration(maxDuration float64)
	SetupCycleTime(cycleTime float64, hardwarePWM bool)
	SetupStartValue(startValue, shutdownValue float64)
	NextAlignedPrintTime(printTime, allowEarly float64) float64
	SetPWM(printTime, value float64)
}

// DigitalOutPin is a digital output pin: an MCU pin, or a virtual pin of a chip such as
// multi_pin.
type DigitalOutPin interface {
	MCU() *MCU
	SetupMaxDuration(maxDuration float64)
	SetupStartValue(startValue, shutdownValue float64)
	SetDigital(printTime float64, value bool)
}
