// Package simmcu is a simulated printer board. The Klipper host connects to it with "serial: sim"
// in the [mcu] section, and runs as with a real board: it configures the board, reads the
// thermistors, heats, homes and prints. The board speaks the Klipper protocol with the data
// dictionary of an ATmega2560 (a RAMBo), and runs in the process of the host on a pipe, so it needs
// no serial port.
//
// What the board simulates:
//
//   - The heaters: each heater output (an output with a max_duration) heats the thermistor that
//     was configured with it, in a thermal model. The thermistor reads as a "Generic 3950" with a
//     4.7k pullup.
//   - The steppers: the queued steps, with their times, give the position of each stepper.
//   - The endstops and the probe: the first homing of a switch triggers in the move (so the zero
//     of the simulated bed is not at Z 0),
//     and the board keeps that stepper position as the place of the switch. Later homings trigger
//     at that place, so they agree with each other, as on a real machine.
//
// The temperatures and the places of the axes and switches are in one World for the life of the
// process, so a firmware restart does not change them.
package simmcu

import (
	"bytes"
	"compress/zlib"
	_ "embed"
	"encoding/json"
	"io"
	"log"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"printdeck/internal/klipper/msgproto"
)

// Serial is the serial option of the [mcu] section for the simulated board.
const Serial = "sim"

//go:embed klipper.dict
var dictionary []byte

// The thermal model of a heater: the power at full duty in W, the heat capacity in J/K, and the
// loss to the room in W/K.
type heaterModel struct{ power, capacity, loss float64 }

var (
	nozzle = heaterModel{power: 40, capacity: 10, loss: 0.115} // 4 °/s at full power, up to about 370 °
	bed    = heaterModel{power: 250, capacity: 500, loss: 1.6} // 0.5 °/s at full power, up to about 180 °
	other  = heaterModel{power: 40, capacity: 50, loss: 0.5}
)

// heaterPins are the heaters of the RAMBo, by the pin of the heater. A heater on another pin is a
// nozzle when it is the first one, else a bed when it is the second one.
var heaterPins = map[string]heaterModel{"PH6": nozzle, "PE5": bed}

// modelOf is the thermal model of heater i on pin.
func modelOf(pin string, i int) heaterModel {
	if m, ok := heaterPins[pin]; ok {
		return m
	}
	switch i {
	case 0:
		return nozzle
	case 1:
		return bed
	}
	return other
}

// room is the temperature of the room.
const room = 22.0

// World is the physical state of the simulated printer.
type World struct {
	mu       sync.Mutex
	start    time.Time
	temps    []float64          // the temperature of each heater, in config order
	physical map[string]float64 // the steps of each stepper (by step pin) before the board started
	switches map[string]float64 // the place of each switch (by pin), in steps of its stepper
	last     time.Time
}

var world = &World{start: time.Now(), physical: map[string]float64{}, switches: map[string]float64{}}

// Open starts a simulated board, and returns the host end of its link.
func Open() (io.ReadWriteCloser, error) {
	host, board := net.Pipe()
	b, err := newBoard(board)
	if err != nil {
		host.Close()
		board.Close()
		return nil, err
	}
	go b.serve()
	go b.run()
	return host, nil
}

// Board is one session of the simulated board: from a start to a reset.
type Board struct {
	conn     net.Conn
	parser   *msgproto.Parser
	identify []byte
	freq     float64
	adcMax   float64
	pwmMax   float64
	statics  map[string]int64 // the static strings of the dictionary, for a shutdown

	mu        sync.Mutex
	writeMu   sync.Mutex
	seq       byte
	crc       string // the CRC of finalize_config, or ""
	shutdown  string // the reason of a shutdown, or ""
	steppers  map[int64]*stepper
	endstops  map[int64]*endstop
	trsyncs   map[int64]*trsync
	adcs      []*adc
	adcByOID  map[int64]*adc
	outs      map[int64]*output
	heaters   []*output
	closed    chan struct{}
	closeOnce sync.Once
}

func newBoard(conn net.Conn) (*Board, error) {
	p := msgproto.NewParser()
	if err := p.ProcessIdentify(dictionary, false); err != nil {
		return nil, err
	}
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	w.Write(dictionary)
	w.Close()
	var d struct {
		Config        map[string]any   `json:"config"`
		StaticStrings map[string]int64 `json:"static_strings"`
	}
	if err := json.Unmarshal(dictionary, &d); err != nil {
		return nil, err
	}
	num := func(k string) float64 {
		v, _ := d.Config[k].(float64)
		return v
	}
	return &Board{conn: conn, parser: p, identify: z.Bytes(), freq: num("CLOCK_FREQ"), adcMax: num("ADC_MAX"),
		pwmMax: num("PWM_MAX"), statics: d.StaticStrings, steppers: map[int64]*stepper{}, endstops: map[int64]*endstop{},
		trsyncs: map[int64]*trsync{}, adcByOID: map[int64]*adc{}, outs: map[int64]*output{}, closed: make(chan struct{})}, nil
}

// now is the clock of the board: CLOCK_FREQ ticks a second, since the start of the World.
func (b *Board) now() uint64 { return uint64(time.Since(world.start).Seconds() * b.freq) }

// clock64 turns a 32-bit clock of a command into the 64-bit clock that is nearest to now.
func (b *Board) clock64(c32 int64) uint64 {
	n := b.now()
	return uint64(int64(n) - int64(int32(uint32(n)-uint32(c32))))
}

func (b *Board) close() {
	b.closeOnce.Do(func() {
		close(b.closed)
		b.conn.Close()
	})
}

// send writes one response to the host, in its own message block.
func (b *Board) send(name string, args map[string]msgproto.Value) {
	f := b.parser.LookupResponse(name)
	if f == nil {
		return
	}
	msg, err := f.EncodeByName(args)
	if err != nil {
		log.Printf("simmcu: %s: %v", name, err)
		return
	}
	b.mu.Lock()
	seq := b.seq
	b.mu.Unlock()
	b.write(msgproto.EncodeMsgblock(int(seq), msg))
}

func (b *Board) write(p []byte) {
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	if _, err := b.conn.Write(p); err != nil {
		b.close()
	}
}

// serve reads the message blocks of the host and answers them, until the link closes.
func (b *Board) serve() {
	defer b.close()
	var buf []byte
	tmp := make([]byte, 4096)
	for {
		n, err := b.conn.Read(tmp)
		if err != nil {
			return
		}
		buf = append(buf, tmp[:n]...)
		for {
			l := b.parser.CheckPacket(buf)
			if l == 0 {
				break
			}
			if l < 0 {
				buf = buf[1:]
				continue
			}
			block := append([]byte(nil), buf[:l]...)
			buf = buf[l:]
			b.handleBlock(block)
		}
	}
}

// handleBlock runs the commands of one block, and acks it with an empty block.
func (b *Board) handleBlock(block []byte) {
	b.mu.Lock()
	b.seq = (block[msgproto.MessagePosSeq] + 1) & msgproto.MessageSeqMask
	b.mu.Unlock()
	msgs, err := b.parser.Dump(block)
	if err != nil {
		log.Printf("simmcu: %v", err)
		return
	}
	for _, text := range msgs[1:] {
		b.command(text)
	}
	b.mu.Lock()
	seq := b.seq
	b.mu.Unlock()
	b.write(msgproto.EncodeMsgblock(int(seq), nil))
}

// args reads the parameters of a command in its text form: "name a=1 b=PH6".
func args(text string) (string, map[string]string) {
	f := strings.Fields(text)
	m := map[string]string{}
	for _, kv := range f[1:] {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return f[0], m
}

func num(m map[string]string, k string) int64 {
	v, _ := strconv.ParseInt(m[k], 10, 64)
	return v
}

// command runs one command of the host.
func (b *Board) command(text string) {
	name, a := args(text)
	oid := num(a, "oid")
	switch name {
	case "identify":
		off, count := num(a, "offset"), num(a, "count")
		end := min(int(off+count), len(b.identify))
		data := []byte{}
		if int(off) < end {
			data = b.identify[off:end]
		}
		b.send("identify_response", map[string]msgproto.Value{"offset": off, "data": data})
	case "get_clock":
		b.send("clock", map[string]msgproto.Value{"clock": int64(uint32(b.now()))})
	case "get_uptime":
		c := b.now()
		b.send("uptime", map[string]msgproto.Value{"high": int64(c >> 32), "clock": int64(uint32(c))})
	case "get_config":
		b.mu.Lock()
		isConfig, crc, isShutdown := 0, int64(0), 0
		if b.crc != "" {
			isConfig = 1
			crc, _ = strconv.ParseInt(b.crc, 10, 64)
		}
		if b.shutdown != "" {
			isShutdown = 1
		}
		b.mu.Unlock()
		b.send("config", map[string]msgproto.Value{"is_config": int64(isConfig), "crc": crc,
			"is_shutdown": int64(isShutdown), "move_count": int64(500)})
	case "finalize_config":
		b.mu.Lock()
		b.crc = a["crc"]
		b.mu.Unlock()
	case "emergency_stop":
		b.doShutdown("Command request")
	case "clear_shutdown":
		b.mu.Lock()
		b.shutdown = ""
		b.mu.Unlock()
	case "reset":
		// The firmware restarts: this session ends, and the host connects again.
		go func() {
			time.Sleep(50 * time.Millisecond)
			b.close()
		}()
	case "config_stepper":
		b.mu.Lock()
		b.steppers[oid] = newStepper(a["step_pin"])
		b.mu.Unlock()
	case "queue_step":
		b.mu.Lock()
		if s := b.steppers[oid]; s != nil {
			s.queue(num(a, "interval"), num(a, "count"), num(a, "add"), b.clock64)
		}
		b.mu.Unlock()
	case "set_next_step_dir":
		b.mu.Lock()
		if s := b.steppers[oid]; s != nil {
			s.nextDir = num(a, "dir") != 0
		}
		b.mu.Unlock()
	case "reset_step_clock":
		b.mu.Lock()
		if s := b.steppers[oid]; s != nil {
			s.advance(b.now())
			s.last, s.needReset = b.clock64(num(a, "clock")), false
		}
		b.mu.Unlock()
	case "stepper_get_position":
		b.mu.Lock()
		var pos int64
		if s := b.steppers[oid]; s != nil {
			pos = s.at(b.now())
		}
		b.mu.Unlock()
		b.send("stepper_position", map[string]msgproto.Value{"oid": oid, "pos": pos})
	case "stepper_stop_on_trigger":
		b.mu.Lock()
		if s := b.steppers[oid]; s != nil {
			s.trsync = num(a, "trsync_oid")
		}
		b.mu.Unlock()
	case "config_endstop":
		b.mu.Lock()
		b.endstops[oid] = &endstop{pin: a["pin"]}
		b.mu.Unlock()
	case "endstop_home":
		b.mu.Lock()
		if e := b.endstops[oid]; e != nil {
			e.home(b, num(a, "sample_count") != 0, num(a, "pin_value"), num(a, "trsync_oid"), num(a, "trigger_reason"), b.clock64(num(a, "clock")))
		}
		b.mu.Unlock()
	case "endstop_query_state":
		b.mu.Lock()
		var homing, pin int64
		if e := b.endstops[oid]; e != nil {
			if e.homing {
				homing = 1
			}
			pin = 1 - e.pinValue // the switch is open
		}
		b.mu.Unlock()
		b.send("endstop_state", map[string]msgproto.Value{"oid": oid, "homing": homing,
			"next_clock": int64(uint32(b.now())), "pin_value": pin})
	case "config_trsync":
		b.mu.Lock()
		b.trsyncs[oid] = &trsync{}
		b.mu.Unlock()
	case "trsync_start":
		b.mu.Lock()
		if t := b.trsyncs[oid]; t != nil {
			*t = trsync{active: true, reportAt: b.clock64(num(a, "report_clock")), reportTicks: uint64(num(a, "report_ticks")),
				expireReason: num(a, "expire_reason")}
		}
		b.mu.Unlock()
	case "trsync_set_timeout":
		b.mu.Lock()
		if t := b.trsyncs[oid]; t != nil && t.active {
			t.expireAt = b.clock64(num(a, "clock"))
		}
		b.mu.Unlock()
	case "trsync_trigger":
		// The firmware answers with the reason of the trigger: the first one, which can be a
		// switch before this command.
		b.mu.Lock()
		b.trigger(oid, num(a, "reason"), b.now(), false)
		var reason int64
		if t := b.trsyncs[oid]; t != nil {
			reason = t.reason
		}
		b.mu.Unlock()
		b.send("trsync_state", map[string]msgproto.Value{"oid": oid, "can_trigger": int64(0),
			"trigger_reason": reason, "clock": int64(0)})
	case "config_analog_in":
		b.mu.Lock()
		ad := &adc{pin: a["pin"], heater: len(b.adcs)}
		b.adcs = append(b.adcs, ad)
		b.adcByOID[oid] = ad
		b.mu.Unlock()
	case "query_analog_in":
		b.mu.Lock()
		if ad := b.adcByOID[oid]; ad != nil {
			ad.oid, ad.next, ad.count, ad.rest = oid, b.clock64(num(a, "clock")), num(a, "sample_count"), uint64(num(a, "rest_ticks"))
			ad.minV, ad.maxV, ad.checks = num(a, "min_value"), num(a, "max_value"), num(a, "range_check_count")
			ad.bytes = max(2, num(a, "bytes_per_report"))
		}
		b.mu.Unlock()
	case "config_digital_out":
		b.mu.Lock()
		o := &output{pin: a["pin"], value: float64(num(a, "value")), max: 1}
		b.outs[oid] = o
		if num(a, "max_duration") > 0 {
			b.heaters = append(b.heaters, o)
		}
		b.mu.Unlock()
	case "set_digital_out_pwm_cycle":
		b.mu.Lock()
		if o := b.outs[oid]; o != nil {
			o.max = float64(max(1, num(a, "cycle_ticks")))
		}
		b.mu.Unlock()
	case "queue_digital_out":
		b.mu.Lock()
		if o := b.outs[oid]; o != nil {
			o.pending = append(o.pending, change{b.clock64(num(a, "clock")), float64(num(a, "on_ticks"))})
		}
		b.mu.Unlock()
	case "update_digital_out":
		b.mu.Lock()
		if o := b.outs[oid]; o != nil {
			o.value, o.max = float64(num(a, "value")), 1
		}
		b.mu.Unlock()
	case "config_pwm_out":
		b.mu.Lock()
		o := &output{pin: a["pin"], value: float64(num(a, "value")), max: b.pwmMax}
		b.outs[oid] = o
		if num(a, "max_duration") > 0 {
			b.heaters = append(b.heaters, o)
		}
		b.mu.Unlock()
	case "queue_pwm_out":
		b.mu.Lock()
		if o := b.outs[oid]; o != nil {
			o.pending = append(o.pending, change{b.clock64(num(a, "clock")), float64(num(a, "value"))})
		}
		b.mu.Unlock()
	}
	// Other commands (the display, SPI, buttons, static outputs) need no answer.
}

// doShutdown stops the board, as the firmware does after an error.
func (b *Board) doShutdown(reason string) {
	b.mu.Lock()
	if b.shutdown != "" {
		b.mu.Unlock()
		return
	}
	b.shutdown = reason
	for _, o := range b.outs {
		o.value, o.pending = 0, nil
	}
	b.mu.Unlock()
	b.send("shutdown", map[string]msgproto.Value{"clock": int64(uint32(b.now())), "static_string_id": b.statics[reason]})
}

// run is the time of the board: it applies the scheduled outputs, heats, moves the steppers,
// checks the switches, and sends the reports, until the session ends.
func (b *Board) run() {
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-b.closed:
			b.saveWorld()
			return
		case <-tick.C:
		}
		now := b.now()
		b.mu.Lock()
		for _, o := range b.outs {
			o.apply(now)
		}
		b.heat()
		var reports []func()
		for oid, t := range b.trsyncs {
			if !t.active {
				continue
			}
			if t.reportTicks > 0 && now >= t.reportAt {
				clock, oid := t.reportAt, oid
				reports = append(reports, func() {
					b.send("trsync_state", map[string]msgproto.Value{"oid": oid, "can_trigger": int64(1),
						"trigger_reason": int64(0), "clock": int64(uint32(clock))})
				})
				for t.reportAt <= now {
					t.reportAt += t.reportTicks
				}
			}
		}
		for _, e := range b.endstops {
			e.check(b, now)
		}
		for oid, t := range b.trsyncs {
			if t.active && t.expireAt != 0 && now >= t.expireAt {
				b.trigger(oid, t.expireReason, t.expireAt, false)
			}
		}
		shutdown := b.shutdown != ""
		for _, ad := range b.adcs {
			for ad.rest > 0 && ad.count > 0 && now >= ad.next {
				ad.next += ad.rest
				v := b.adcValue(ad)
				if !shutdown && (v < ad.minV || v > ad.maxV) {
					ad.bad++
					if ad.bad >= ad.checks && ad.checks > 0 {
						reports = append(reports, func() { b.doShutdown("ADC out of range") })
					}
				} else {
					ad.bad = 0
				}
				oid, next := ad.oid, ad.next
				reports = append(reports, func() {
					b.send("analog_in_state", map[string]msgproto.Value{"oid": oid, "next_clock": int64(uint32(next)),
						"values": []byte{byte(v), byte(v >> 8)}})
				})
			}
		}
		b.mu.Unlock()
		for _, r := range reports {
			r()
		}
	}
}

// heat moves the temperature of each heater with its duty, in its thermal model. b.mu is locked.
func (b *Board) heat() {
	world.mu.Lock()
	defer world.mu.Unlock()
	t := time.Now()
	dt := 0.0
	if !world.last.IsZero() {
		dt = min(0.1, t.Sub(world.last).Seconds())
	}
	world.last = t
	for i := range b.adcs {
		for len(world.temps) <= i {
			world.temps = append(world.temps, room)
		}
		m, duty := other, 0.0
		if i < len(b.heaters) {
			m = modelOf(b.heaters[i].pin, i)
			if b.shutdown == "" {
				duty = b.heaters[i].duty()
			}
		}
		temp := world.temps[i]
		world.temps[i] = temp + dt*(m.power*duty-m.loss*(temp-room))/m.capacity
	}
}

// adcValue is the reading of a thermistor: the sum of its samples, as the firmware reports it.
// The thermistor is a "Generic 3950" (100k at 25 °) with a 4.7k pullup. b.mu is locked.
func (b *Board) adcValue(ad *adc) int64 {
	world.mu.Lock()
	temp := room
	if ad.heater < len(world.temps) {
		temp = world.temps[ad.heater]
	}
	world.mu.Unlock()
	r := 100000 * math.Exp(3950*(1/(temp+273.15)-1/298.15))
	frac := r / (r + 4700)
	return int64(math.Round(frac*b.adcMax)) * ad.count
}

// debug logs the homing of the simulated board, with PRINTDECK_SIM_DEBUG=1.
var debug = os.Getenv("PRINTDECK_SIM_DEBUG") != ""

func debugf(format string, args ...any) {
	if debug {
		log.Printf("simmcu: "+format, args...)
	}
}

// trigger triggers a trsync at clock: its steppers stop there, and the host gets the report.
// fromSwitch is true when an endstop or a probe triggered it. b.mu is locked.
func (b *Board) trigger(oid, reason int64, clock uint64, fromSwitch bool) {
	t := b.trsyncs[oid]
	if t == nil || !t.active {
		return
	}
	t.active, t.reason = false, reason
	debugf("trsync %d triggered: reason %d at %d (switch %v)", oid, reason, clock, fromSwitch)
	for _, s := range b.steppers {
		if s.trsync == oid {
			s.stop(clock)
			s.trsync = -1
		}
	}
	for _, e := range b.endstops {
		if e.homing && e.trsync == oid {
			e.homing = false
		}
	}
	go b.send("trsync_state", map[string]msgproto.Value{"oid": oid, "can_trigger": int64(0),
		"trigger_reason": reason, "clock": int64(uint32(clock))})
}

// saveWorld keeps the place of each stepper for the next session, as the axes do not move when the
// firmware restarts.
func (b *Board) saveWorld() {
	b.mu.Lock()
	defer b.mu.Unlock()
	world.mu.Lock()
	defer world.mu.Unlock()
	for _, s := range b.steppers {
		world.physical[s.pin] = s.physical(b.now())
	}
}

func (w *World) physicalSteps(pin string) float64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.physical[pin]
}
