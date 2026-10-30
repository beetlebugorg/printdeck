package chitu

import (
	"bytes"
	"encoding/binary"
	"image/color"
	"math"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// dataPacket makes a data packet of an upload, in the form of DataPacket.
func dataPacket(data []byte, off int) []byte {
	p := append([]byte(nil), data...)
	p = binary.LittleEndian.AppendUint32(p, uint32(off))
	var x byte
	for _, b := range p {
		x ^= b
	}
	return append(p, x, 0x83)
}

// The relay answers M99999 with the board and the address of the relay, relays the other queries
// to the board and the answers back, and reads an upload from its packets.
func TestRelay(t *testing.T) {
	board, err := NewFakeBoard("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer board.Close()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got []byte
	var gotName string
	busy := ""
	active := make(chan bool, 4)
	r := &Relay{Conn: conn, Board: board.Addr(),
		Info: func() (Info, bool) {
			return Info{MAC: "00:e0:4c:18:00:29", Version: "V1.4.1", ID: "29", Name: "CBD"}, true
		},
		OnUpload: func(name, path string) {
			data, _ := os.ReadFile(path)
			os.Remove(path)
			mu.Lock()
			gotName, got = name, data
			mu.Unlock()
		},
		Dir:      t.TempDir(),
		Busy:     func() string { mu.Lock(); defer mu.Unlock(); return busy },
		OnActive: func(a bool) { active <- a }, Name: "resin deck", AnswerLocal: true}
	ctx := t.Context()
	go r.Run(ctx)

	c := &Client{Addr: conn.LocalAddr().String(), Timeout: 500 * time.Millisecond}
	in, err := c.Info(ctx)
	if err != nil || in.Name != "resin-deck" || in.ID != "29-relay" || in.IP != "127.0.0.1" {
		t.Fatalf("the answer of the relay to M99999: %+v %v", in, err)
	}
	board.Set(func(b *FakeBoard) { b.Read, b.Size, b.Z = 10, 100, 1.5 })
	st, err := c.Status(ctx)
	if err != nil || st.Z != 1.5 || st.Size != 100 {
		t.Fatalf("a query through the relay: %+v %v", st, err)
	}
	select {
	case a := <-active:
		if !a {
			t.Error("not active")
		}
	case <-time.After(time.Second):
		t.Error("no OnActive")
	}

	// An upload, as ChituManager sends it: M28, the data packets with no wait (one out of order
	// and one sent twice), M29. The relay answers each packet, and the board gets none of them.
	up, err := net.DialUDP("udp4", nil, conn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer up.Close()
	file := []byte("0123456789abcdefghij")
	up.Write([]byte("M28 part.cbddlp"))
	up.Write(dataPacket(file[0:8], 0))
	up.Write(dataPacket(file[16:], 16))
	up.Write(dataPacket(file[8:16], 8))
	up.Write(dataPacket(file[8:16], 8))
	up.Write([]byte("M29"))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := got != nil
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	if gotName != "part.cbddlp" || string(got) != string(file) {
		t.Fatalf("the upload %q %q", gotName, got)
	}
	busy = "The deck sends a file to the printer"
	mu.Unlock()
	for _, q := range board.Log() {
		if strings.HasPrefix(q, "M2") {
			t.Errorf("the board got %q", q)
		}
	}

	// A start while the deck is busy gets an error, and the board gets no M6030.
	if a, err := c.Query(ctx, "M6030 ':part.cbddlp'"); err != nil || !strings.HasPrefix(a, "Error:The deck") {
		t.Errorf("M6030 when busy: %q %v", a, err)
	}
	for _, q := range board.Log() {
		if strings.HasPrefix(q, "M6030") {
			t.Errorf("the board got %q", q)
		}
	}

	// An upload with a gap fails.
	got = nil
	up.Write([]byte("M28 gap.cbddlp"))
	up.Write(dataPacket(file[0:8], 0))
	up.Write(dataPacket(file[16:], 16))
	up.Write([]byte("M29"))
	ans := make([]byte, 256)
	var last string
	up.SetReadDeadline(time.Now().Add(time.Second))
	for {
		n, err := up.Read(ans)
		if err != nil {
			break
		}
		last = string(ans[:n])
	}
	if !strings.HasPrefix(last, "Error:") {
		t.Errorf("the answer to M29 with a gap: %q", last)
	}
}

// A data packet with a bad checksum is no data packet.
func TestDataPacket(t *testing.T) {
	p := dataPacket([]byte("hello"), 1280)
	if d, off, ok := DataPacket(p); !ok || string(d) != "hello" || off != 1280 {
		t.Fatalf("%q %d %v", d, off, ok)
	}
	p[0] ^= 1
	if _, _, ok := DataPacket(p); ok {
		t.Error("a bad checksum")
	}
	if _, _, ok := DataPacket([]byte("M4000")); ok {
		t.Error("a command as data")
	}
}

// The header and the preview of a sliced file that the test builds in the layout of ReadSliced.
func TestPreview(t *testing.T) {
	data := make([]byte, 112)
	put := func(off int, v uint32) { binary.LittleEndian.PutUint32(data[off:], v) }
	putF := func(off int, v float32) { put(off, math.Float32bits(v)) }
	put(0, MagicCBDDLP)
	put(4, 2)
	putF(8, 192)
	putF(32, 0.05)
	put(52, 3840)
	put(56, 2400)
	put(68, 1200)
	put(76, 36000)
	// The large preview: 3 x 2 pixels, a run of 4 red pixels and 2 blue ones.
	head := len(data)
	put(60, uint32(head))
	data = append(data, make([]byte, 32)...)
	red := uint16(0x1F<<11) | 0x0020
	blue := uint16(0x1F)
	img := binary.LittleEndian.AppendUint16(nil, red)
	img = binary.LittleEndian.AppendUint16(img, 0x3000|3)
	img = binary.LittleEndian.AppendUint16(img, blue)
	img = binary.LittleEndian.AppendUint16(img, blue)
	off := len(data)
	data = append(data, img...)
	binary.LittleEndian.PutUint32(data[head:], 3)
	binary.LittleEndian.PutUint32(data[head+4:], 2)
	binary.LittleEndian.PutUint32(data[head+8:], uint32(off))
	binary.LittleEndian.PutUint32(data[head+12:], uint32(len(img)))

	s, err := ReadSliced(data)
	if err != nil || s.Layers != 1200 || s.LayerHeight != 0.05 || s.ResX != 3840 || s.PrintSeconds != 36000 {
		t.Fatalf("header %+v %v", s, err)
	}
	p, err := Preview(data, true)
	if err != nil {
		t.Fatal(err)
	}
	if c := color.RGBAModel.Convert(p.At(0, 1)).(color.RGBA); c.R != 248 || c.B != 0 {
		t.Errorf("the 4th pixel %v, want red", c)
	}
	if c := color.RGBAModel.Convert(p.At(2, 1)).(color.RGBA); c.B != 248 || c.R != 0 {
		t.Errorf("the last pixel %v, want blue", c)
	}
	if _, err := Preview(data, false); err == nil {
		t.Error("a small preview with no offset")
	}
	if _, err := ReadSliced([]byte("not a sliced file at all, just some text to fill the bytes up to the header size of one hundred and twelve")); err == nil {
		t.Error("a file with no magic")
	}
}

// Upload sends a file one packet at a time, sends a lost packet again, and follows a resend of
// the board.
func TestUpload(t *testing.T) {
	board, err := NewFakeBoard("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer board.Close()
	file := make([]byte, 5*UploadChunk+100)
	for i := range file {
		file[i] = byte(i * 7)
	}
	board.Set(func(b *FakeBoard) { b.LoseData, b.SaveNoOK, b.WriteErrors = 2, true, 1 })
	c := &Client{Addr: board.Addr()}
	var last int64
	err = c.Upload(t.Context(), "part.ctb", bytes.NewReader(file), int64(len(file)), func(n int64) { last = n })
	if err != nil {
		t.Fatal(err)
	}
	board.Set(func(b *FakeBoard) {
		if !bytes.Equal(b.Uploaded["part.ctb"], file) {
			t.Errorf("the board saved %d bytes, want %d", len(b.Uploaded["part.ctb"]), len(file))
		}
	})
	if last != int64(len(file)) {
		t.Errorf("progress %d", last)
	}
	if m := resendRE.FindStringSubmatch("ok\rend 2560,offset error:3840"); m == nil || m[1] != "2560" {
		t.Errorf("a joined answer: %v", m)
	}
}

// A layer of a .cbddlp: a lit square in the middle of a screen of 16 x 8 pixels, read 4 pixels
// wide.
func TestLayerImage(t *testing.T) {
	const resX, resY = 16, 8
	var code []byte
	run := func(lit bool, n int) {
		for n > 0 {
			k := min(n, 0x7F)
			b := byte(k)
			if lit {
				b |= 0x80
			}
			code = append(code, b)
			n -= k
		}
	}
	// Rows 0 to 3 dark; rows 4 to 7 lit in the columns 8 to 15.
	run(false, 4*resX)
	for range 4 {
		run(false, 8)
		run(true, 8)
	}
	data := make([]byte, 200)
	put := func(off int, v uint32) { binary.LittleEndian.PutUint32(data[off:], v) }
	put(0, MagicCBDDLP)
	put(52, resX)
	put(56, resY)
	put(64, 120) // the table of the layers
	put(68, 1)
	put(120+12, 160) // the data of layer 0
	put(120+16, uint32(len(code)))
	copy(data[160:], code)
	h, err := ReadSliced(data)
	if err != nil {
		t.Fatal(err)
	}
	img, err := LayerImage(bytes.NewReader(data), h, 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 4 || b.Dy() != 2 {
		t.Fatalf("size %v", b)
	}
	want := []uint8{0, 0, 0, 0, 0, 0, 255, 255}
	for i, v := range want {
		if img.Pix[i] != v {
			t.Errorf("pixel %d: %d, want %d (%v)", i, img.Pix[i], v, img.Pix)
			break
		}
	}
	if _, err := LayerImage(bytes.NewReader(data), h, 1, 4); err == nil {
		t.Error("a layer past the end")
	}
}

// A board that answers the start after 3 s starts the print: StartPrint waits for it.
func TestStartPrintSlow(t *testing.T) {
	board, err := NewFakeBoard("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer board.Close()
	board.Set(func(b *FakeBoard) { b.Files, b.SlowStart = []string{"plate.cbddlp 1000"}, true })
	c := &Client{Addr: board.Addr()}
	if err := c.StartPrint(t.Context(), "plate.cbddlp"); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, q := range board.Log() {
		if strings.HasPrefix(q, "M6030") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d starts, want 1", n)
	}
}
