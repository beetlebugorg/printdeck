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
	Roots        *x509.CertPool    // the CA of the certificate, for Client.Roots
	// StartAnswer gives the answer to a project_file with the url: the result ("success" or
	// "fail") and the reason. answer false gives no answer. nil answers each start with success.
	StartAnswer func(url string) (result, reason string, answer bool)

	ln       net.Listener
	mu       sync.Mutex
	report   map[string]any
	requests []map[string]any
	conns    []*mqttConn
}

// NewFake starts a fake printer on a free port of 127.0.0.1, with a certificate for the serial
// number from a CA of its own: a client of the test sets Roots to f.Roots.
func NewFake(serial, code string) (*Fake, error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	caTmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test BBL CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, err
	}
	ca, _ := x509.ParseCertificate(caDER)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: serial},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return nil, err
	}
	cert, _ := x509.ParseCertificate(der)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}})
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	f := &Fake{Serial: serial, Code: code, Cert: cert, Roots: roots, ln: ln, report: map[string]any{}}
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
			if p, ok := req["print"].(map[string]any); ok && p["command"] == "project_file" {
				url, _ := p["url"].(string)
				result, reason, answer := "success", "", true
				if f.StartAnswer != nil {
					result, reason, answer = f.StartAnswer(url)
				}
				if answer {
					a, _ := json.Marshal(map[string]any{"print": map[string]any{"command": "project_file",
						"sequence_id": p["sequence_id"], "result": result, "reason": reason}})
					m.writePacket(pktPublish, 0, append(appendStr(nil, "device/"+f.Serial+"/report"), a...))
				}
			}
			if p, ok := req["info"].(map[string]any); ok && p["command"] == "get_version" {
				v := `{"info":{"command":"get_version","module":[{"name":"ota","product_name":"Bambu Lab X2D","sw_ver":"01.01.00.80"}]}}`
				m.writePacket(pktPublish, 0, append(appendStr(nil, "device/"+f.Serial+"/report"), v...))
			}
		}
	}
}
