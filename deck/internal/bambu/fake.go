package bambu

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net"
	"sync"
	"time"
)

// Fake is a printer for the tests: an MQTT broker on TLS that checks the login, sends the report
// that the test sets, and keeps the requests.
type Fake struct {
	Serial, Code string
	Cert         *x509.Certificate // the certificate of the broker

	ln       net.Listener
	mu       sync.Mutex
	report   map[string]any
	requests []map[string]any
	conns    []*mqttConn
}

// NewFake starts a fake printer on a free port of 127.0.0.1.
func NewFake(serial, code string) (*Fake, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: serial},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, _ := x509.ParseCertificate(der)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}})
	if err != nil {
		return nil, err
	}
	f := &Fake{Serial: serial, Code: code, Cert: cert, ln: ln, report: map[string]any{}}
	go f.accept()
	return f, nil
}

// Addr returns the address of the broker.
func (f *Fake) Addr() string { return f.ln.Addr().String() }

// Close stops the broker.
func (f *Fake) Close() {
	f.ln.Close()
	f.mu.Lock()
	for _, m := range f.conns {
		m.c.Close()
	}
	f.mu.Unlock()
}

// Send sends the "print" object p to the clients, and merges it into the report of the next
// pushall.
func (f *Fake) Send(p map[string]any) {
	f.mu.Lock()
	mergeMap(f.report, p)
	conns := append([]*mqttConn(nil), f.conns...)
	f.mu.Unlock()
	b, _ := json.Marshal(map[string]any{"print": p})
	for _, m := range conns {
		m.writePacket(pktPublish, 0, append(appendStr(nil, "device/"+f.Serial+"/report"), b...))
	}
}

// Requests returns the requests that the clients sent.
func (f *Fake) Requests() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.requests...)
}

func (f *Fake) accept() {
	for {
		c, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.serve(c)
	}
}

func (f *Fake) serve(c net.Conn) {
	defer c.Close()
	m := &mqttConn{c: c, r: bufio.NewReader(c)}
	typ, _, body, err := m.readPacket()
	if err != nil || typ != pktConnect {
		return
	}
	// The body: protocol name, level, flags, keep alive, client ID, user, password.
	_, rest, _ := readStr(body)
	if len(rest) < 4 {
		return
	}
	_, rest, _ = readStr(rest[4:])
	user, rest, _ := readStr(rest)
	pass, _, _ := readStr(rest)
	if user != User || pass != f.Code {
		m.writePacket(pktConnack, 0, []byte{0, 5})
		return
	}
	m.writePacket(pktConnack, 0, []byte{0, 0})
	for {
		typ, flags, body, err := m.readPacket()
		if err != nil {
			return
		}
		switch typ {
		case pktSubscribe:
			m.writePacket(pktSuback, 0, []byte{body[0], body[1], 0})
			f.mu.Lock()
			f.conns = append(f.conns, m)
			f.mu.Unlock()
		case pktPingreq:
			m.writePacket(pktPingresp, 0, nil)
		case pktPublish:
			_, _, payload, err := parsePublish(flags, body)
			if err != nil {
				return
			}
			var req map[string]any
			json.Unmarshal(payload, &req)
			f.mu.Lock()
			f.requests = append(f.requests, req)
			full, _ := json.Marshal(map[string]any{"print": f.report})
			f.mu.Unlock()
			if p, ok := req["pushing"].(map[string]any); ok && p["command"] == "pushall" {
				m.writePacket(pktPublish, 0, append(appendStr(nil, "device/"+f.Serial+"/report"), full...))
			}
			if p, ok := req["info"].(map[string]any); ok && p["command"] == "get_version" {
				v := `{"info":{"command":"get_version","module":[{"name":"ota","product_name":"Bambu Lab X2D","sw_ver":"01.01.00.80"}]}}`
				m.writePacket(pktPublish, 0, append(appendStr(nil, "device/"+f.Serial+"/report"), v...))
			}
		}
	}
}
