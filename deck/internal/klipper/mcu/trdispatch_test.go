package mcu

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"printdeck/internal/klipper/msgproto"
	"printdeck/internal/klipper/serialqueue"
)

type recordPort struct {
	mu      sync.Mutex
	written []byte
}

func (p *recordPort) Write(b []byte) (int, error) {
	p.mu.Lock()
	p.written = append(p.written, b...)
	p.mu.Unlock()
	return len(b), nil
}

func (p *recordPort) Read(b []byte) (int, error) { select {} }

// messages returns the written messages, as groups of three integers.
func (p *recordPort) messages() [][]uint32 {
	p.mu.Lock()
	data := append([]byte(nil), p.written...)
	p.mu.Unlock()
	var out [][]uint32
	for len(data) > 0 && int(data[0]) <= len(data) {
		block := data[:data[0]]
		data = data[data[0]:]
		k := 1
		for {
			if _, err := msgproto.DecodeInts(block, 3*k); err != nil {
				break
			}
			k++
		}
		vals, _ := msgproto.DecodeInts(block, 3*(k-1))
		for i := 0; i+3 <= len(vals); i += 3 {
			out = append(out, vals[i:i+3])
		}
	}
	return out
}

func stateMsg(oid, canTrigger, reason, clock uint32) []byte {
	return msgproto.EncodeMsgblock(0, msgproto.EncodeInts(12, oid, canTrigger, reason, clock))
}

// One board reports that it can trigger: the dispatcher extends the timeout of the other board.
// Then the other board reports a trigger: the dispatcher sends trsync_trigger to both boards.
func TestTrdispatch(t *testing.T) {
	const setTimeoutTag, triggerTag, stateTag = 10, 11, 12
	p1, p2 := &recordPort{}, &recordPort{}
	sq1 := serialqueue.New(p1, serialqueue.DebugFile, "a")
	sq2 := serialqueue.New(p2, serialqueue.DebugFile, "b")
	defer sq1.Exit()
	defer sq2.Exit()
	sq1.SetClockEst(1e6, 0, 0)
	sq2.SetClockEst(1e6, 0, 0)
	td := newTrdispatch()
	m1 := td.newTrdispatchMCU(sq1, sq1.AllocCommandQueue(), 5, setTimeoutTag, triggerTag, stateTag)
	m2 := td.newTrdispatchMCU(sq2, sq2.AllocCommandQueue(), 7, setTimeoutTag, triggerTag, stateTag)
	m1.setup(1000, 5000, 4000, 1000)
	m2.setup(1000, 5000, 4000, 1000)
	td.start(ReasonHostRequest)
	// The prefix of the fast reader is the tag and the oid.
	if !reflect.DeepEqual(m1.fr.Prefix, msgproto.EncodeInts(stateTag, 5)) {
		t.Fatalf("prefix % x", m1.fr.Prefix)
	}
	m1.handleTrsyncState(0.01, stateMsg(5, 1, 0, 10000))
	// A message of another oid does nothing.
	m1.handleTrsyncState(0.01, stateMsg(6, 0, 1, 10000))
	m2.handleTrsyncState(0.02, stateMsg(7, 0, ReasonEndstopHit, 20000))
	// After the trigger, the dispatcher ignores the reports.
	m1.handleTrsyncState(0.03, stateMsg(5, 0, 1, 30000))
	td.stop()
	want1 := [][]uint32{{triggerTag, 5, ReasonHostRequest}}
	want2 := [][]uint32{{setTimeoutTag, 7, 14000}, {triggerTag, 7, ReasonHostRequest}}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if reflect.DeepEqual(p1.messages(), want1) && reflect.DeepEqual(p2.messages(), want2) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("board 1 got %v, want %v; board 2 got %v, want %v", p1.messages(), want1, p2.messages(), want2)
}
