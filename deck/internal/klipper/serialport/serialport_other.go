//go:build !linux

package serialport

import (
	"time"

	"go.bug.st/serial"
)

// Port is an open serial port.
type Port struct {
	p serial.Port
}

func mode(baud int) *serial.Mode {
	return &serial.Mode{BaudRate: baud, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit}
}

// Open opens a serial port at a baud rate. The port is exclusive: on macOS with TIOCEXCL, and on
// Windows always. rts sets the RTS line.
func Open(path string, baud int, rts bool) (*Port, error) {
	m := mode(baud)
	m.InitialStatusBits = &serial.ModemOutputBits{RTS: rts, DTR: true}
	p, err := serial.Open(path, m)
	if err != nil {
		return nil, err
	}
	return &Port{p: p}, nil
}

func (p *Port) Read(b []byte) (int, error)  { return p.p.Read(b) }
func (p *Port) Write(b []byte) (int, error) { return p.p.Write(b) }
func (p *Port) Close() error                { return p.p.Close() }

// SetBaud changes the baud rate.
func (p *Port) SetBaud(baud int) error { return p.p.SetMode(mode(baud)) }

// SetDTR sets the DTR line. On an Arduino style board, a change of DTR resets the board.
func (p *Port) SetDTR(on bool) error { return p.p.SetDTR(on) }

// SetRTS sets the RTS line.
func (p *Port) SetRTS(on bool) error { return p.p.SetRTS(on) }

// ClearHUPCL does nothing here: go.bug.st/serial has no control of HUPCL. The close of the port
// can reset an Arduino style board.
func (p *Port) ClearHUPCL() error { return nil }

// FlushOutput drops the bytes that are not sent yet.
func (p *Port) FlushOutput() error { return p.p.ResetOutputBuffer() }

// ReadFor reads what arrives within d, as the reads of pyserial with a timeout of 0 after a pause.
func (p *Port) ReadFor(d time.Duration) ([]byte, error) {
	defer p.p.SetReadTimeout(serial.NoTimeout)
	var out []byte
	buf := make([]byte, 4096)
	end := time.Now().Add(d)
	for {
		// A read with a timeout returns 0 bytes and no error at the timeout.
		if err := p.p.SetReadTimeout(max(time.Until(end), time.Millisecond)); err != nil {
			return out, err
		}
		n, err := p.p.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			return out, err
		}
		if n == 0 {
			return out, nil
		}
	}
}
