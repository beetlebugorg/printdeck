// Package clocksync keeps an estimate of the clock of the board against host time: a linear
// regression of get_clock samples, about one each second. It converts print times to board clocks
// and back, and gives the serial queue the clock estimate for the release times of the commands.
//
// Ported to Go from klippy/clocksync.py of Klipper (https://github.com/Klipper3d/klipper), commit
// 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f. The SecondarySync class, for a second board, is not
// ported.
//
//	Copyright (C) 2016-2018  Kevin O'Connor <kevin@koconnor.net>
//	Go port: Copyright (C) 2026  the Printdeck authors
//
// This file may be distributed under the terms of the GNU GPLv3 license.
package clocksync

import (
	"fmt"
	"log"
	"math"
	"sync"

	"printdeck/internal/klipper/reactor"
	"printdeck/internal/klipper/serialhdl"
	"printdeck/internal/klipper/serialqueue"
)

const (
	rttAge        = .000010 / (60. * 60.)
	decay         = 1. / 30.
	transmitExtra = .001
)

// Estimate is a sample time, the board clock at that time, and the board frequency.
type Estimate struct {
	SampleTime float64
	Clock      float64
	Freq       float64
}

// ClockSync is the clock estimate of one board.
type ClockSync struct {
	reactor       *reactor.Reactor
	serial        *serialhdl.SerialReader
	getClockTimer *reactor.Timer
	getClockCmd   []byte
	cmdQueue      *serialqueue.CommandQueue
	mcuFreq       float64

	mu sync.Mutex // protects the fields below: _handle_clock runs on the reading goroutine
	// Tracking of background requests
	queriesPending int
	// 32bit to 64bit clock conversions
	lastClock uint64
	// Linear regression of mcu clock and system sent_time
	timeAvg, timeVariance     float64
	clockAvg, clockCovariance float64
	predictionVariance        float64
	lastPredictionTime        float64
	minHalfRTT, minRTTTime    float64
	clockEst                  Estimate
}

func New(r *reactor.Reactor) *ClockSync {
	cs := &ClockSync{reactor: r, mcuFreq: 1., minHalfRTT: 999999999.9}
	cs.getClockTimer = r.RegisterTimer(cs.getClockEvent, reactor.Never)
	return cs
}

// Connect loads the first clock and frequency, takes 8 clock samples, and starts the periodic
// get_clock queries. It runs in reactor code.
func (cs *ClockSync) Connect(serial *serialhdl.SerialReader) error {
	cs.serial = serial
	freq, err := serial.MsgParser().ConstantFloat("CLOCK_FREQ")
	if err != nil {
		return err
	}
	cs.mcuFreq = freq
	// Load initial clock and frequency
	params, err := serial.SendWithResponse("get_uptime", "uptime")
	if err != nil {
		return err
	}
	cs.mu.Lock()
	cs.lastClock = uint64(params.Int("high"))<<32 | uint64(params.Int("clock"))
	cs.clockAvg = float64(cs.lastClock)
	cs.timeAvg = params.Float("#sent_time")
	cs.clockEst = Estimate{cs.timeAvg, cs.clockAvg, cs.mcuFreq}
	cs.predictionVariance = math.Pow(.001*cs.mcuFreq, 2)
	cs.mu.Unlock()
	// Enable periodic get_clock timer
	for i := 0; i < 8; i++ {
		cs.reactor.Pause(cs.reactor.Monotonic() + 0.050)
		cs.mu.Lock()
		cs.lastPredictionTime = -9999.
		cs.mu.Unlock()
		params, err := serial.SendWithResponse("get_clock", "clock")
		if err != nil {
			return err
		}
		cs.handleClock(params)
	}
	cs.getClockCmd, err = serial.MsgParser().CreateCommand("get_clock")
	if err != nil {
		return err
	}
	cs.cmdQueue = serial.AllocCommandQueue()
	serial.RegisterResponse(cs.handleClock, "clock", serialhdl.NoOID)
	cs.reactor.UpdateTimer(cs.getClockTimer, reactor.Now)
	return nil
}

// ConnectFile sets the estimate for klippy's batch mode: with pace, the real board frequency;
// without, a very fast clock, so all commands go out at once.
func (cs *ClockSync) ConnectFile(serial *serialhdl.SerialReader, pace bool) error {
	cs.serial = serial
	freq, err := serial.MsgParser().ConstantFloat("CLOCK_FREQ")
	if err != nil {
		return err
	}
	cs.mcuFreq = freq
	cs.mu.Lock()
	cs.clockEst = Estimate{0., 0., cs.mcuFreq}
	cs.mu.Unlock()
	f := 1000000000000.
	if pace {
		f = cs.mcuFreq
	}
	serial.SetClockEst(f, cs.reactor.Monotonic(), 0)
	return nil
}

// getClockEvent sends a get_clock query. The next one is at an unusual time, so the clock messages
// do not line up with other periodic events.
func (cs *ClockSync) getClockEvent(eventtime float64) float64 {
	cs.serial.RawSend(cs.getClockCmd, 0, 0, cs.cmdQueue)
	cs.mu.Lock()
	cs.queriesPending++
	cs.mu.Unlock()
	return eventtime + .9839
}

// updateRegression adds a sample to the regression. It returns false for an outlier. cs.mu must
// be locked.
func (cs *ClockSync) updateRegression(sentTime, clock float64) bool {
	// Calculate expected clock using existing time/clock regression
	oldFreq := cs.clockEst.Freq
	expClock := (sentTime-cs.timeAvg)*oldFreq + cs.clockAvg
	// Track prediction accuracy and filter out extreme outliers
	clockDiff2 := math.Pow(clock-expClock, 2)
	if clockDiff2 > 25.*cs.predictionVariance && clockDiff2 > math.Pow(.000500*cs.mcuFreq, 2) {
		if clock > expClock && sentTime < cs.lastPredictionTime+10. {
			log.Printf("Ignoring clock sample %.3f: freq=%d diff=%d stddev=%.3f",
				sentTime, int64(oldFreq), int64(clock-expClock), math.Sqrt(cs.predictionVariance))
			return false
		}
		log.Printf("Resetting prediction variance %.3f: freq=%d diff=%d stddev=%.3f",
			sentTime, int64(oldFreq), int64(clock-expClock), math.Sqrt(cs.predictionVariance))
		cs.predictionVariance = math.Pow(.001*cs.mcuFreq, 2)
	} else {
		cs.lastPredictionTime = sentTime
		cs.predictionVariance = (1. - decay) * (cs.predictionVariance + clockDiff2*decay)
	}
	// Add clock and sent_time to linear regression
	diffSentTime := sentTime - cs.timeAvg
	cs.timeAvg += decay * diffSentTime
	cs.timeVariance = (1. - decay) * (cs.timeVariance + diffSentTime*diffSentTime*decay)
	diffClock := clock - cs.clockAvg
	cs.clockAvg += decay * diffClock
	cs.clockCovariance = (1. - decay) * (cs.clockCovariance + diffSentTime*diffClock*decay)
	return true
}

// updateBestRTT keeps the smallest round-trip time, aged over time. cs.mu must be locked.
func (cs *ClockSync) updateBestRTT(sentTime, receiveTime float64) {
	halfRTT := .5 * (receiveTime - sentTime)
	agedRTT := (sentTime - cs.minRTTTime) * rttAge
	if halfRTT < cs.minHalfRTT+agedRTT {
		cs.minHalfRTT = halfRTT
		cs.minRTTTime = sentTime
	}
}

// handleClock adds a clock response to the estimate. It runs on the reading goroutine.
func (cs *ClockSync) handleClock(params serialhdl.Params) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.queriesPending = 0
	// Extend clock to 64bit
	clockDelta := uint64(uint32(uint64(params.Int("clock")) - cs.lastClock))
	cs.lastClock += clockDelta
	clock := float64(cs.lastClock)
	// Determine message timing
	sentTime := params.Float("#sent_time")
	receiveTime := params.Float("#receive_time")
	if sentTime == 0 {
		// sent_time unknown because of 'get_clock' retransmit
		return
	}
	// Update sent_time to clock regression
	if !cs.updateRegression(sentTime, clock) {
		// Message is an "outlier" and should be discarded
		return
	}
	// Update serialqueue message release timing
	newFreq := cs.clockCovariance / cs.timeVariance
	predStddev := math.Sqrt(cs.predictionVariance)
	cs.serial.SetClockEst(newFreq, cs.timeAvg+transmitExtra, uint64(int64(cs.clockAvg-3.*predStddev)))
	// Update time translation (mainly for multi-mcu synchronization)
	cs.updateBestRTT(sentTime, receiveTime)
	cs.clockEst = Estimate{cs.timeAvg + cs.minHalfRTT, cs.clockAvg, newFreq}
}

// MCUFreq is the nominal clock frequency of the board.
func (cs *ClockSync) MCUFreq() float64 { return cs.mcuFreq }

// PrintTimeToClock converts a print time to a board clock.
func (cs *ClockSync) PrintTimeToClock(printTime float64) int64 {
	return int64(printTime * cs.mcuFreq)
}

// ClockToPrintTime converts a board clock to a print time.
func (cs *ClockSync) ClockToPrintTime(clock float64) float64 { return clock / cs.mcuFreq }

// Estimate returns the current estimate.
func (cs *ClockSync) Estimate() Estimate {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.clockEst
}

// GetClock returns the estimated board clock at a host time.
func (cs *ClockSync) GetClock(eventtime float64) int64 {
	e := cs.Estimate()
	return int64(e.Clock + (eventtime-e.SampleTime)*e.Freq)
}

// EstimateClockSystime returns the host time of a board clock.
func (cs *ClockSync) EstimateClockSystime(reqclock int64) float64 {
	e := cs.Estimate()
	return (float64(reqclock)-e.Clock)/e.Freq + e.SampleTime
}

// EstimatedPrintTime returns the print time at a host time.
func (cs *ClockSync) EstimatedPrintTime(eventtime float64) float64 {
	return cs.ClockToPrintTime(float64(cs.GetClock(eventtime)))
}

// Clock32ToClock64 extends a 32-bit board clock, with the last clock sample.
func (cs *ClockSync) Clock32ToClock64(clock32 uint32) uint64 {
	cs.mu.Lock()
	lastClock := cs.lastClock
	cs.mu.Unlock()
	return serialqueue.ClockFromClock32(lastClock, clock32)
}

// IsActive reports that the board answers the clock queries: at most 4 are without an answer.
func (cs *ClockSync) IsActive() bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.queriesPending <= 4
}

// DumpDebug returns the state, for the log after an error.
func (cs *ClockSync) DumpDebug() string {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	e := cs.clockEst
	return fmt.Sprintf("clocksync state: mcu_freq=%d last_clock=%d"+
		" clock_est=(%.3f %d %.3f) min_half_rtt=%.6f min_rtt_time=%.3f"+
		" time_avg=%.3f(%.3f) clock_avg=%.3f(%.3f) pred_variance=%.3f",
		int64(cs.mcuFreq), cs.lastClock, e.SampleTime, int64(e.Clock), e.Freq,
		cs.minHalfRTT, cs.minRTTTime, cs.timeAvg, cs.timeVariance,
		cs.clockAvg, cs.clockCovariance, cs.predictionVariance)
}

// Stats returns the frequency estimate.
func (cs *ClockSync) Stats(eventtime float64) string {
	return fmt.Sprintf("freq=%d", int64(cs.Estimate().Freq))
}

// CalibrateClock returns the print time offset and frequency of the main board.
func (cs *ClockSync) CalibrateClock(printTime, eventtime float64) (float64, float64) {
	return 0., cs.mcuFreq
}
