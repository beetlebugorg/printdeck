package serialqueue

import (
	"bytes"
	"sync"
	"testing"
	"time"

	"printdeck/internal/klipper/msgproto"
)

// fakePort records the bytes that the queue writes, and gives it the bytes of a fake board.
type fakePort struct {
	mu      sync.Mutex
	written []byte
	in      chan []byte
}

func newFakePort() *fakePort { return &fakePort{in: make(chan []byte, 16)} }

func (p *fakePort) Write(b []byte) (int, error) {
	p.mu.Lock()
	p.written = append(p.written, b...)
	p.mu.Unlock()
	return len(b), nil
}

func (p *fakePort) Read(b []byte) (int, error) {
	data := <-p.in
	return copy(b, data), nil
}

func (p *fakePort) bytes() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]byte(nil), p.written...)
}

// blocks splits written bytes into message blocks. It skips the sync bytes of retransmits.
func blocks(t *testing.T, data []byte) [][]byte {
	t.Helper()
	var out [][]byte
	needSync := false
	for len(data) > 0 {
		l := msgblockCheck(&needSync, data)
		if l == 0 {
			t.Fatalf("a partial block at the end: % x", data)
		}
		if l > 0 {
			out = append(out, data[:l])
		} else {
			l = -l
		}
		data = data[l:]
	}
	return out
}

// payloads returns the message bytes of each block.
func payloads(b []byte) []byte {
	return b[msgproto.MessageHeaderSize : len(b)-msgproto.MessageTrailerSize]
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// In klippy's batch mode (a debug file and a very fast clock), all commands go out, and each block
// has at most 64 bytes.
func TestDebugFileSendsAll(t *testing.T) {
	port := newFakePort()
	sq := New(port, DebugFile, "test")
	defer sq.Exit()
	sq.SetClockEst(1e12, sq.Now(), 0)
	a, b := sq.AllocCommandQueue(), sq.AllocCommandQueue()
	var want int
	for i := 0; i < 50; i++ {
		msg := bytes.Repeat([]byte{byte(i)}, 1+i%7)
		q := a
		if i%2 == 1 {
			q = b
		}
		sq.Send(q, msg, 0, uint64(i), 0)
		want += len(msg)
	}
	waitFor(t, "all bytes", func() bool {
		got := 0
		for _, bl := range blocks(t, port.bytes()) {
			got += len(payloads(bl))
		}
		return got == want
	})
	for i, bl := range blocks(t, port.bytes()) {
		if len(bl) > msgproto.MessageMax {
			t.Errorf("block %d has %d bytes", i, len(bl))
		}
		if seq := bl[msgproto.MessagePosSeq] & msgproto.MessageSeqMask; int(seq) != (i+1)&msgproto.MessageSeqMask {
			t.Errorf("block %d has sequence %d", i, seq)
		}
	}
}

// Commands that are ready at the same time go out lowest request clock first. Within one command
// queue, the order of the sends stays.
func TestPriorityAndQueueOrder(t *testing.T) {
	port := newFakePort()
	sq := New(port, DebugFile, "test")
	defer sq.Exit()
	a, b := sq.AllocCommandQueue(), sq.AllocCommandQueue()
	// With no clock estimate yet, the queue waits; the commands collect, then go out together.
	sq.SendBatch(a, []Message{{Data: []byte{0xa1}, ReqClock: 30}, {Data: []byte{0xa2}, ReqClock: 10}})
	sq.SendBatch(b, []Message{{Data: []byte{0xb1}, ReqClock: 20}})
	sq.SetClockEst(1e12, sq.Now(), 0)
	sq.Send(b, []byte{0xb2}, 0, 40, 0) // a send wakes the loop
	waitFor(t, "4 commands", func() bool {
		n := 0
		for _, bl := range blocks(t, port.bytes()) {
			n += len(payloads(bl))
		}
		return n == 4
	})
	var order []byte
	for _, bl := range blocks(t, port.bytes()) {
		order = append(order, payloads(bl)...)
	}
	// a1 is first in queue a, so a2 (clock 10) cannot pass it: a1 (30) competes with b1 (20).
	if want := []byte{0xb1, 0xa1, 0xa2, 0xb2}; !bytes.Equal(order, want) {
		t.Errorf("order % x, want % x", order, want)
	}
}

// A command waits until the clock of the board reaches its min_clock.
func TestMinClockWaits(t *testing.T) {
	port := newFakePort()
	sq := New(port, DebugFile, "test")
	defer sq.Exit()
	start := sq.Now()
	sq.SetClockEst(1000, start, 0) // 1000 ticks per second
	q := sq.AllocCommandQueue()
	sq.Send(q, []byte{0x42}, 300, 400, 0) // not before 0.3 s
	time.Sleep(100 * time.Millisecond)
	if len(port.bytes()) != 0 {
		t.Fatal("the command went out before its min_clock")
	}
	waitFor(t, "the command", func() bool { return len(port.bytes()) > 0 })
	if d := sq.Now() - start; d < 0.25 {
		t.Errorf("the command went out after %.3f s, want about 0.3 s", d)
	}
}

// ackBlock is the 5-byte ack of the board for a sequence number.
func ackBlock(seq uint64) []byte { return msgproto.EncodeMsgblock(int(seq), nil) }

// The board acknowledges blocks; a notify command comes back from Pull, and a data message from the
// board too.
func TestAckNotifyAndReceive(t *testing.T) {
	port := newFakePort()
	sq := New(port, UART, "test")
	defer sq.Exit()
	sq.SetWireFrequency(250000)
	sq.SetClockEst(1e12, sq.Now(), 0)
	q := sq.AllocCommandQueue()
	sq.Send(q, []byte{0x11}, 0, 0, 7)
	waitFor(t, "the block", func() bool { return len(port.bytes()) > 0 })

	// The board acks block 1 with sequence 2, and sends a data message.
	port.in <- ackBlock(2)
	port.in <- msgproto.EncodeMsgblock(2, []byte{0x55, 0x01})
	r, ok := sq.Pull()
	if !ok || r.NotifyID != 7 || len(r.Data) != 0 {
		t.Fatalf("first pull %+v %v, want the notification of 7", r, ok)
	}
	r, ok = sq.Pull()
	if !ok || !bytes.Equal(payloads(r.Data), []byte{0x55, 0x01}) {
		t.Fatalf("second pull %+v %v, want the data message", r, ok)
	}
	if s := sq.Stats(); !bytes.Contains([]byte(s), []byte("receive_seq=2")) {
		t.Errorf("stats %q", s)
	}
}

// With no ack, the queue sends the block again after its timeout.
func TestRetransmit(t *testing.T) {
	port := newFakePort()
	sq := New(port, UART, "test")
	defer sq.Exit()
	sq.SetWireFrequency(250000)
	sq.SetClockEst(1e12, sq.Now(), 0)
	q := sq.AllocCommandQueue()
	sq.Send(q, []byte{0x22}, 0, 0, 0)
	waitFor(t, "the first block", func() bool { return len(port.bytes()) > 0 })
	first := port.bytes()
	waitFor(t, "a retransmit", func() bool { return len(port.bytes()) > len(first) })
	again := port.bytes()[len(first):]
	if again[0] != msgproto.MessageSync || !bytes.Equal(again[1:1+len(first)], first) {
		t.Errorf("retransmit % x, want a sync byte and % x", again, first)
	}
	// After the ack, nothing more is sent.
	port.in <- ackBlock(2)
	waitFor(t, "the ack", func() bool { return bytes.Contains([]byte(sq.Stats()), []byte("receive_seq=2")) })
	n := len(port.bytes())
	time.Sleep(150 * time.Millisecond)
	if len(port.bytes()) != n {
		t.Error("the queue sent again after the ack")
	}
}

func TestMsgblockCheckSkipsBadData(t *testing.T) {
	good := msgproto.EncodeMsgblock(1, []byte{0x01, 0x02})
	data := append([]byte{0x00, 0x13, msgproto.MessageSync}, good...)
	needSync := false
	if l := msgblockCheck(&needSync, data); l != -3 {
		t.Fatalf("bad data: %d, want -3 (up to and with the sync byte)", l)
	}
	if l := msgblockCheck(&needSync, data[3:]); l != len(good) {
		t.Fatalf("good block: %d, want %d", l, len(good))
	}
	bad := append([]byte(nil), good...)
	bad[3] ^= 0xff // break the CRC
	if l := msgblockCheck(&needSync, bad); l >= 0 {
		t.Errorf("a block with a bad CRC: %d", l)
	}
}

func TestClockConversions(t *testing.T) {
	ce := ClockEstimate{ConvClock: 1000, ConvTime: 10, EstFreq: 16e6}
	if c := ce.ClockFromTime(11); c != 16001000 {
		t.Errorf("clock at 11 s: %d", c)
	}
	if tm := ce.ClockToTime(16001000); tm != 11 {
		t.Errorf("time of clock 16001000: %v", tm)
	}
	// A 32-bit clock just after a wrap extends to the next 2^32 period.
	if c := ClockFromClock32(0x1fffffff0, 0x00000010); c != 0x200000010 {
		t.Errorf("clock32 after wrap: %#x", c)
	}
}
