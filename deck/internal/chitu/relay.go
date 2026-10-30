package chitu

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// Relay lets the ChituManager app of ChiTuBox reach a board that answers no broadcast, such as the
// board of the Phenom. ChituManager sends M99999 to the broadcast address of UDP port 3000 to find
// boards. The relay answers it with the line of the board and the address of the relay. Each other
// packet of a client goes to the board, and each answer of the board goes back to the client.
//
// The relay keeps the uploads: M28 <name>, the data packets, and M29. ChituManager sends the data
// packets with no wait for the answers and does not resend a packet, so the relay answers each
// packet itself and writes the file to Dir. OnUpload then sends the file to the board one packet
// at a time (Client.Upload).
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
	// Dir is the folder of the uploads while they come in. "" is the folder of temporary files.
	Dir string
	// OnReceive reports the bytes of an upload that came to the relay: at M28 (0), about each
	// MB, and at M29 (done).
	OnReceive func(name string, bytes int64, done bool)
	// OnUpload gets each complete upload: the name of the file and the path of the copy in Dir.
	// OnUpload owns the copy.
	OnUpload func(name, path string)
	// Busy returns the reason that the board can start no print now, for example an upload to the
	// board, or "". The relay answers M6030 with this error.
	Busy func() string
	// OnActive reports that a client talks to the board (true), or that no client talked to it for
	// relayIdle (false).
	OnActive func(active bool)

	mu       sync.Mutex
	sessions map[string]*relaySession
}

// m4001Answer is the answer of the Phenom to M4001 (the steps of the axes), from a capture of
// 2026-10-30. The relay gives it while the board is busy.
const m4001Answer = "ok X:0.011430 Y:0.011430 Z:0.001250 E:0.001340 T:0/0/0/400/1 U:'GBK' B:1"

// RelayChunk is the size of the data packets that the relay asks ChituManager for (BUFSIZE in
// the answer to M99999). With no BUFSIZE, ChituManager sends 1280 bytes in a packet, about 650
// packets a second. The relay writes each packet at its offset, so any size works. The deck sends
// the file to the board in packets of UploadChunk.
const RelayChunk = 32768

// relayIdle is the time with no packet after which a client is gone.
const relayIdle = 2 * time.Minute

// relaySession is a client of the relay: its own socket to the board, and its upload.
type relaySession struct {
	board net.Conn
	last  time.Time
	name  string        // the name of M28
	file  *os.File      // the copy of the upload, or nil with no upload
	parts map[int64]int // the offsets and the sizes of the data packets of the upload
	bad   bool          // a packet of the upload was not in the known form
	got   int64         // the bytes of the data packets of the upload
	told  int64         // got at the last OnReceive
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
	if a, ok := r.upload(s, p); ok {
		r.Conn.WriteTo([]byte(a+"\r\n"), from)
		return
	}
	if r.Busy != nil {
		if why := r.Busy(); why != "" {
			switch line := strings.TrimSpace(string(p)); {
			case strings.HasPrefix(line, "M6030"):
				r.Conn.WriteTo([]byte("Error:"+why+"\r\n"), from)
				return
			case line == "M4001":
				// ChituManager sends M4001 before M28. The board gives no answer during an upload.
				r.Conn.WriteTo([]byte(m4001Answer+"\r\n"), from)
				return
			}
		}
	}
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
	line := fmt.Sprintf("ok MAC:%s IP:%s VER:%s ID:%s-relay NAME:%s BUFSIZE:%d\r\n", in.MAC, ip, in.Version, in.ID, name, RelayChunk)
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
				s.close(true)
				delete(r.sessions, k)
				if len(r.sessions) == 0 && r.OnActive != nil {
					go r.OnActive(false)
				}
			}
		}
		r.mu.Unlock()
	}
}

// upload takes a packet of an upload of the session s, and returns the answer to the client. ok is
// false for another packet.
func (r *Relay) upload(s *relaySession, p []byte) (answer string, ok bool) {
	line := strings.TrimSpace(string(p))
	if name, ok := strings.CutPrefix(line, "M28 "); ok {
		s.close(true)
		name = strings.TrimSpace(name)
		f, err := os.CreateTemp(r.Dir, ".upload-*")
		if err != nil {
			log.Printf("chitu relay: %v", err)
			return "Error:" + err.Error(), true
		}
		s.name, s.file, s.parts, s.bad, s.got, s.told = name, f, map[int64]int{}, false, 0, 0
		r.receive(name, 0, false)
		return "ok N:0", true
	}
	if s.file == nil {
		return "", false
	}
	if line == "M29" {
		name, path := s.name, s.file.Name()
		size, whole := joinedSize(s.parts)
		bad := s.bad
		s.close(false)
		r.receive(name, size, true)
		if bad || !whole {
			os.Remove(path)
			log.Printf("chitu relay: the upload of %s lost packets", name)
			return "Error:The upload lost packets", true
		}
		if err := os.Truncate(path, size); err != nil || r.OnUpload == nil {
			os.Remove(path)
			return "ok", true
		}
		go r.OnUpload(name, path)
		return "ok", true
	}
	data, off, ok := DataPacket(p)
	if !ok {
		if strings.HasPrefix(line, "M") {
			return "", false // a command during the upload, for example M4000
		}
		s.bad = true
		return "Error:A bad data packet", true
	}
	if _, err := s.file.WriteAt(data, int64(off)); err != nil {
		s.bad = true
		return "Error:" + err.Error(), true
	}
	if _, again := s.parts[int64(off)]; !again {
		s.got += int64(len(data))
	}
	s.parts[int64(off)] = len(data)
	if s.got-s.told >= 1<<20 {
		s.told = s.got
		r.receive(s.name, s.got, false)
	}
	return "ok", true
}

// receive calls OnReceive.
func (r *Relay) receive(name string, n int64, done bool) {
	if r.OnReceive != nil {
		r.OnReceive(name, n, done)
	}
}

// close ends the upload of the session, and removes its copy when remove is true.
func (s *relaySession) close(remove bool) {
	if s.file == nil {
		return
	}
	s.file.Close()
	if remove {
		os.Remove(s.file.Name())
	}
	s.file, s.parts = nil, nil
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

// joinedSize returns the size of an upload from the offsets and the sizes of its packets. whole is
// false for a gap.
func joinedSize(parts map[int64]int) (size int64, whole bool) {
	offs := make([]int64, 0, len(parts))
	for o := range parts {
		offs = append(offs, o)
	}
	slices.Sort(offs)
	for _, o := range offs {
		if o > size {
			return size, false
		}
		size = max(size, o+int64(parts[o]))
	}
	return size, size > 0
}
