package bambu

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// This file is a small MQTT 3.1.1 client: CONNECT, SUBSCRIBE, PUBLISH at QoS 0 and 1, and
// PINGREQ. A Bambu printer needs only these.

// The packet types of MQTT 3.1.1.
const (
	pktConnect   = 1
	pktConnack   = 2
	pktPublish   = 3
	pktPuback    = 4
	pktSubscribe = 8
	pktSuback    = 9
	pktPingreq   = 12
	pktPingresp  = 13
	pktDisconn   = 14
)

// maxPacket is the largest packet that the client reads. A full report of an X2D is about 10 KB.
const maxPacket = 4 << 20

// ErrRefused is a CONNECT that the printer refused, for example for a wrong access code.
var ErrRefused = errors.New("the printer refused the login: check the access code")

// mqttConn is one MQTT session on a connection.
type mqttConn struct {
	c  net.Conn
	r  *bufio.Reader
	mu sync.Mutex // held for each write
	id uint16     // the last packet ID
}

// writePacket writes one packet of type typ with the flags and the body.
func (m *mqttConn) writePacket(typ, flags byte, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b := []byte{typ<<4 | flags}
	b = appendLen(b, len(body))
	b = append(b, body...)
	m.c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := m.c.Write(b)
	return err
}

// readPacket reads one packet. It returns the type, the flags and the body.
func (m *mqttConn) readPacket() (typ, flags byte, body []byte, err error) {
	h, err := m.r.ReadByte()
	if err != nil {
		return 0, 0, nil, err
	}
	n, err := readLen(m.r)
	if err != nil {
		return 0, 0, nil, err
	}
	if n > maxPacket {
		return 0, 0, nil, fmt.Errorf("mqtt: a packet of %d bytes", n)
	}
	body = make([]byte, n)
	if _, err := io.ReadFull(m.r, body); err != nil {
		return 0, 0, nil, err
	}
	return h >> 4, h & 0x0f, body, nil
}

// appendLen appends the remaining length n in the variable length code of MQTT.
func appendLen(b []byte, n int) []byte {
	for {
		d := byte(n % 128)
		n /= 128
		if n > 0 {
			d |= 0x80
		}
		b = append(b, d)
		if n == 0 {
			return b
		}
	}
}

// readLen reads a remaining length.
func readLen(r io.ByteReader) (int, error) {
	n, mul := 0, 1
	for i := 0; i < 4; i++ {
		d, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		n += int(d&0x7f) * mul
		if d&0x80 == 0 {
			return n, nil
		}
		mul *= 128
	}
	return 0, errors.New("mqtt: a bad length")
}

// appendStr appends a string with its 2-byte length.
func appendStr(b []byte, s string) []byte {
	b = binary.BigEndian.AppendUint16(b, uint16(len(s)))
	return append(b, s...)
}

// readStr reads a string with its 2-byte length from the start of b, and returns the rest of b.
func readStr(b []byte) (string, []byte, error) {
	if len(b) < 2 {
		return "", nil, errors.New("mqtt: a short string")
	}
	n := int(binary.BigEndian.Uint16(b))
	if len(b) < 2+n {
		return "", nil, errors.New("mqtt: a short string")
	}
	return string(b[2 : 2+n]), b[2+n:], nil
}

// connect sends CONNECT with the login, and reads CONNACK.
func (m *mqttConn) connect(clientID, user, pass string, keepAlive time.Duration) error {
	var b []byte
	b = appendStr(b, "MQTT")
	b = append(b, 4, 0xc2) // level 4; user name, password, clean session
	b = binary.BigEndian.AppendUint16(b, uint16(keepAlive/time.Second))
	b = appendStr(b, clientID)
	b = appendStr(b, user)
	b = appendStr(b, pass)
	if err := m.writePacket(pktConnect, 0, b); err != nil {
		return err
	}
	typ, _, body, err := m.readPacket()
	if err != nil {
		return err
	}
	if typ != pktConnack || len(body) < 2 {
		return fmt.Errorf("mqtt: packet %d before CONNACK", typ)
	}
	switch body[1] {
	case 0:
		return nil
	case 4, 5:
		return ErrRefused
	default:
		return fmt.Errorf("mqtt: CONNACK code %d", body[1])
	}
}

// nextID returns a new packet ID.
func (m *mqttConn) nextID() uint16 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.id++
	if m.id == 0 {
		m.id = 1
	}
	return m.id
}

// subscribe sends SUBSCRIBE for topic at QoS 0. The SUBACK comes to the read loop.
func (m *mqttConn) subscribe(topic string) error {
	b := binary.BigEndian.AppendUint16(nil, m.nextID())
	b = appendStr(b, topic)
	b = append(b, 0)
	return m.writePacket(pktSubscribe, 2, b)
}

// publish sends payload to topic at QoS 0.
func (m *mqttConn) publish(topic string, payload []byte) error {
	b := appendStr(nil, topic)
	return m.writePacket(pktPublish, 0, append(b, payload...))
}

// parsePublish returns the topic and the payload of a PUBLISH body, and the packet ID for
// QoS 1 and 2 (0 for QoS 0).
func parsePublish(flags byte, body []byte) (topic string, id uint16, payload []byte, err error) {
	topic, rest, err := readStr(body)
	if err != nil {
		return "", 0, nil, err
	}
	if (flags>>1)&3 > 0 {
		if len(rest) < 2 {
			return "", 0, nil, errors.New("mqtt: no packet ID")
		}
		id, rest = binary.BigEndian.Uint16(rest), rest[2:]
	}
	return topic, id, rest, nil
}
