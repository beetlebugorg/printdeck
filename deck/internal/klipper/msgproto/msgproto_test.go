package msgproto

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func loadParser(t *testing.T) *Parser {
	t.Helper()
	dict, err := os.ReadFile("../testdata/klipper.dict")
	if err != nil {
		t.Fatal(err)
	}
	p := NewParser()
	if err := p.ProcessIdentify(dict, false); err != nil {
		t.Fatal(err)
	}
	return p
}

// dumpStream decodes a command stream like klippy/parsedump.py: each block, without its "seq" line.
func dumpStream(t *testing.T, p *Parser, data []byte) string {
	t.Helper()
	var out strings.Builder
	for len(data) > 0 {
		l := p.CheckPacket(data)
		if l == 0 {
			break
		}
		if l < 0 {
			t.Fatalf("invalid data at %q", data[:min(8, len(data))])
		}
		msgs, err := p.Dump(data[:l])
		if err != nil {
			t.Fatal(err)
		}
		out.WriteString(strings.Join(msgs[1:], "\n") + "\n")
		data = data[l:]
	}
	return out.String()
}

// The Go parser decodes the reference stream of klippy exactly as Klipper's parsedump.py does.
func TestDumpMatchesKlipper(t *testing.T) {
	p := loadParser(t)
	serial, err := os.ReadFile("../testdata/basic.serial")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../testdata/basic.txt")
	if err != nil {
		t.Fatal(err)
	}
	got := dumpStream(t, p, serial)
	if got == string(want) {
		return
	}
	g, w := strings.Split(got, "\n"), strings.Split(string(want), "\n")
	for i := 0; i < len(g) && i < len(w); i++ {
		if g[i] != w[i] {
			t.Fatalf("line %d:\n got %q\nwant %q", i+1, g[i], w[i])
		}
	}
	t.Fatalf("got %d lines, want %d", len(g), len(w))
}

// Encoding each decoded command again gives the same bytes: the encoder matches the stream.
func TestEncodeMatchesKlipper(t *testing.T) {
	p := loadParser(t)
	serial, err := os.ReadFile("../testdata/basic.serial")
	if err != nil {
		t.Fatal(err)
	}
	blocks := 0
	for data := serial; len(data) > 0; blocks++ {
		l := p.CheckPacket(data)
		if l <= 0 {
			t.Fatalf("invalid block %d", blocks)
		}
		block := data[:l]
		var payload []byte
		for pos := MessageHeaderSize; pos < l-MessageTrailerSize; {
			f, err := p.lookup(block, pos)
			if err != nil {
				t.Fatal(err)
			}
			m, ok := f.(*MessageFormat)
			if !ok {
				t.Fatalf("block %d: %s is not a command", blocks, f.Name())
			}
			params, next, err := m.parse(block, pos)
			if err != nil {
				t.Fatal(err)
			}
			cmd, err := m.EncodeByName(params)
			if err != nil {
				t.Fatalf("block %d: %s: %v", blocks, m.Name(), err)
			}
			payload = append(payload, cmd...)
			pos = next
		}
		if again := EncodeMsgblock(int(block[MessagePosSeq]), payload); !bytes.Equal(again, block) {
			t.Fatalf("block %d:\n got % x\nwant % x", blocks, again, block)
		}
		data = data[l:]
	}
	if blocks < 100 {
		t.Errorf("only %d blocks", blocks)
	}
}

func TestVLQ(t *testing.T) {
	for _, v := range []int64{0, 1, 0x5f, 0x60, -0x20, -0x21, 0x2fff, 0x3000, 0x17ffff, 0x180000, 0xbffffff, 0xc000000, -0x4000000, 0x7fffffff, -0x80000000} {
		b := encodeVLQ(nil, v)
		got, n, err := parseVLQ(b, 0, true)
		if err != nil || got != v || n != len(b) {
			t.Errorf("%#x: encoded % x, parsed %#x (%d bytes, %v)", v, b, got, n, err)
		}
	}
	// Unsigned parameters read negative numbers as 32-bit values.
	if got, _, _ := parseVLQ(encodeVLQ(nil, -1), 0, false); got != 0xffffffff {
		t.Errorf("unsigned -1 parsed as %#x", got)
	}
}

func TestPyBytesRepr(t *testing.T) {
	for in, want := range map[string]string{
		"":         `b''`,
		"ab":       `b'ab'`,
		"a'b":      `b"a'b"`,
		"a'\"b":    `b'a\'"b'`,
		"\x00\n\\": `b'\x00\n\\'`,
	} {
		if got := PyBytesRepr([]byte(in)); got != want {
			t.Errorf("%q: %s, want %s", in, got, want)
		}
	}
}

// A command text gives the same bytes as Klipper. Buffers are in hex, as in msgproto.py.
func TestCreateCommand(t *testing.T) {
	p := loadParser(t)
	for _, c := range []struct{ text, want string }{
		{"set_digital_out pin=PG1 value=1", "set_digital_out pin=PG1 value=1"},
		{"spi_send oid=0 data=00b2", `spi_send oid=0 data=b'\x00\xb2'`},
		{"config_analog_in oid=14 pin=PF2", "config_analog_in oid=14 pin=PF2"},
	} {
		cmd, err := p.CreateCommand(c.text)
		if err != nil {
			t.Fatalf("%s: %v", c.text, err)
		}
		msgs, err := p.Dump(EncodeMsgblock(0, cmd))
		if err != nil || msgs[1] != c.want {
			t.Errorf("%s: %v %v, want %s", c.text, msgs, err, c.want)
		}
	}
	if _, err := p.CreateCommand("set_digital_out pin=PZ9 value=1"); err == nil {
		t.Error("a pin that the board does not have was accepted")
	}
}
