package chitu

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// Relay lets the ChituManager app of ChiTuBox reach a board that answers no broadcast, such as the
// board of the Phenom. ChituManager sends M99999 to the broadcast address of UDP port 3000 to find
// boards. The relay answers it with the line of the board and the address of the relay. Each other
// packet of a client goes to the board, and each answer of the board goes back to the client. So
// the uploads and the starts of ChiTuBox go through the relay with no change.
//
// The relay also reads the upload of a file from the packets: M28 <name>, the data packets, and
// M29. Other programs for ChiTu boards describe a data packet as the data, the offset of the data in
// the file (4 bytes, little-endian), the XOR of the bytes before it, and the byte 0x83. A packet in
// another form goes to the board as it is, and the relay keeps no copy of that file.
type Relay struct {
	Conn  net.PacketConn // the socket of UDP port 3000
	Board string         // the address of the board, "192.168.76.58:3000"
	// Info returns the board for the answer to M99999, false before the board answered.
	Info func() (Info, bool)
	// Name is the name in the answer to M99999 in place of the name of the board, with no space.
	// The ID in the answer is the ID of the board with "-relay" after it: ChituManager keeps one
	// printer for each ID, and the board itself answers the broadcast too.
	Name string
	// AnswerLocal answers M99999 from an address of this computer too, for the tests. Else the
	// relay is silent to the discovery of the deck itself.
	AnswerLocal bool
	// OnUpload gets each file that a client sent to the board through the relay, when the relay
	// could read all its packets.
	OnUpload func(name string, data []byte)
	// OnActive reports that a client talks to the board (true), or that no client talked to it for
	// relayIdle (false).
	OnActive func(active bool)

	mu       sync.Mutex
	sessions map[string]*relaySession
}

// relayIdle is the time with no packet after which a client is gone.
const relayIdle = 2 * time.Minute

// relaySession is a client of the relay: its own socket to the board, and its upload.
type relaySession struct {
	board net.Conn
	last  time.Time
	file  string         // the name of M28, or "" with no upload
	parts map[int][]byte // the data of the upload by offset
	bad   bool           // a packet of the upload was not in the known form
}

// Run serves the clients until ctx ends.
func (r *Relay) Run(ctx context.Context) {
	go func() {
		<-ctx.Done()
		r.Conn.Close()
	}()
	go r.sweep(ctx)
	buf := make([]byte, 65536)
	for {
		n, from, err := r.Conn.ReadFrom(buf)
		if err != nil {
			return
		}
		r.packet(ctx, append([]byte(nil), buf[:n]...), from)
	}
}

// packet handles one packet of the client from.
func (r *Relay) packet(ctx context.Context, p []byte, from net.Addr) {
	if strings.TrimSpace(string(p)) == "M99999" {
		r.answerInfo(from)
		return
	}
	s, err := r.session(ctx, from)
	if err != nil {
		log.Printf("chitu relay: %v", err)
		return
	}
	r.watch(s, p)
	s.board.Write(p)
}

// answerInfo answers M99999 with the line of the board and the address of the relay that the
// client reaches.
func (r *Relay) answerInfo(to net.Addr) {
	if r.Info == nil {
		return
	}
	in, ok := r.Info()
	if !ok {
		return
	}
	ip := localIP(to)
	if ip == "" || (!r.AnswerLocal && isLocal(to)) {
		return
	}
	name := in.Name
	if r.Name != "" {
		name = strings.ReplaceAll(r.Name, " ", "-")
	}
	line := fmt.Sprintf("ok MAC:%s IP:%s VER:%s ID:%s-relay NAME:%s\r\n", in.MAC, ip, in.Version, in.ID, name)
	r.Conn.WriteTo([]byte(line), to)
}

// localIP is the address of this computer on the way to the address to.
func localIP(to net.Addr) string {
	ua, ok := to.(*net.UDPAddr)
	if !ok {
		return ""
	}
	c, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: ua.IP, Port: ua.Port})
	if err != nil {
		return ""
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP.String()
}

// isLocal reports whether the address a is an address of this computer.
func isLocal(a net.Addr) bool {
	ua, ok := a.(*net.UDPAddr)
	if !ok {
		return false
	}
	if ua.IP.IsLoopback() {
		return true
	}
	addrs, _ := net.InterfaceAddrs()
	for _, x := range addrs {
		if n, ok := x.(*net.IPNet); ok && n.IP.Equal(ua.IP) {
			return true
		}
	}
	return false
}

// session returns the session of the client from, and opens it with its socket to the board.
func (r *Relay) session(ctx context.Context, from net.Addr) (*relaySession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := from.String()
	if s := r.sessions[k]; s != nil {
		s.last = time.Now()
		return s, nil
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", r.Board)
	if err != nil {
		return nil, err
	}
	s := &relaySession{board: conn, last: time.Now()}
	if r.sessions == nil {
		r.sessions = map[string]*relaySession{}
	}
	if len(r.sessions) == 0 && r.OnActive != nil {
		go r.OnActive(true)
	}
	r.sessions[k] = s
	// The answers of the board go back to the client.
	go func() {
		buf := make([]byte, 65536)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			r.Conn.WriteTo(buf[:n], from)
		}
	}()
	return s, nil
}

// sweep closes the sessions with no packet for relayIdle.
func (r *Relay) sweep(ctx context.Context) {
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		r.mu.Lock()
		for k, s := range r.sessions {
			if time.Since(s.last) > relayIdle {
				s.board.Close()
				delete(r.sessions, k)
				if len(r.sessions) == 0 && r.OnActive != nil {
					go r.OnActive(false)
				}
			}
		}
		r.mu.Unlock()
	}
}

// watch reads the upload of a file from the packet p of the session s.
func (r *Relay) watch(s *relaySession, p []byte) {
	line := strings.TrimSpace(string(p))
	switch {
	case strings.HasPrefix(line, "M28 "):
		s.file, s.parts, s.bad = strings.TrimSpace(strings.TrimPrefix(line, "M28 ")), map[int][]byte{}, false
	case line == "M29" && s.file != "":
		name, parts, bad := s.file, s.parts, s.bad
		s.file, s.parts = "", nil
		if data, ok := joinParts(parts); ok && !bad && r.OnUpload != nil {
			go r.OnUpload(name, data)
		}
	case s.file != "":
		data, off, ok := DataPacket(p)
		if !ok {
			s.bad = true
			return
		}
		s.parts[off] = data
	}
}

// DataPacket reads a data packet of an upload: the data, its offset in the file, the XOR of the
// data and the offset, and the byte 0x83. ok is false for another packet.
func DataPacket(p []byte) (data []byte, offset int, ok bool) {
	n := len(p)
	if n < 7 || p[n-1] != 0x83 {
		return nil, 0, false
	}
	var x byte
	for _, b := range p[:n-2] {
		x ^= b
	}
	if x != p[n-2] {
		return nil, 0, false
	}
	return p[:n-6], int(binary.LittleEndian.Uint32(p[n-6 : n-2])), true
}

// joinParts puts the data of an upload together by offset. ok is false for a gap.
func joinParts(parts map[int][]byte) ([]byte, bool) {
	offs := make([]int, 0, len(parts))
	for o := range parts {
		offs = append(offs, o)
	}
	sort.Ints(offs)
	var out []byte
	for _, o := range offs {
		if o != len(out) {
			if o < len(out) {
				continue // a packet sent again
			}
			return nil, false
		}
		out = append(out, parts[o]...)
	}
	return out, len(out) > 0
}
