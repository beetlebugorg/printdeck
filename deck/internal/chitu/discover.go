package chitu

import (
	"context"
	"net"
	"strconv"
	"time"
)

// Discover asks for the boards on the networks of the computer, and returns the boards that answer
// in wait, one for each MAC address. It sends M99999 to port 3000 of the broadcast address of each
// IPv4 network and of 255.255.255.255, and to each address of a network of 254 addresses or fewer:
// the board of a Phenom answered no broadcast on 2026-10-30. targets are the addresses to ask in
// place of these ("host:port"), for the tests.
func Discover(ctx context.Context, wait time.Duration, targets ...string) ([]Info, error) {
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var to []*net.UDPAddr
	for _, t := range targets {
		if a, err := net.ResolveUDPAddr("udp4", t); err == nil {
			to = append(to, a)
		}
	}
	if len(targets) == 0 {
		to = append(to, &net.UDPAddr{IP: net.IPv4bcast, Port: Port})
		for _, b := range broadcasts() {
			to = append(to, &net.UDPAddr{IP: b, Port: Port})
		}
		for _, ip := range sweep() {
			to = append(to, &net.UDPAddr{IP: ip, Port: Port})
		}
	}
	for _, a := range to {
		conn.WriteTo([]byte("M99999"), a)
	}
	deadline := time.Now().Add(wait)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	conn.SetReadDeadline(deadline)
	var out []Info
	seen := map[string]bool{}
	buf := make([]byte, 4096)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			break // the end of the wait
		}
		in, err := ParseInfo(string(buf[:n]))
		if err != nil {
			continue
		}
		// The address of the answer: a board can give an old IP in its text.
		if ua, ok := from.(*net.UDPAddr); ok {
			in.IP = ua.IP.String()
			if ua.Port != Port {
				in.IP = net.JoinHostPort(in.IP, strconv.Itoa(ua.Port))
			}
		}
		k := in.MAC
		if k == "" {
			k = in.IP
		}
		if !seen[k] {
			seen[k] = true
			out = append(out, in)
		}
	}
	return out, nil
}

// sweepBits is the most host bits of a network that Discover asks address by address: a /24.
const sweepBits = 8

// sweep returns each address of the IPv4 networks of the computer with sweepBits or fewer host
// bits, with no network address, no broadcast address and no address of the computer.
func sweep() []net.IP {
	var out []net.IP
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok || n.IP.IsLoopback() {
			continue
		}
		ip4 := n.IP.To4()
		ones, bits := n.Mask.Size()
		if ip4 == nil || bits != 32 || bits-ones > sweepBits || bits-ones < 2 {
			continue
		}
		base := ip4.Mask(n.Mask)
		hosts := 1<<(bits-ones) - 1
		for h := 1; h < hosts; h++ {
			ip := make(net.IP, net.IPv4len)
			copy(ip, base)
			ip[2] |= byte(h >> 8)
			ip[3] |= byte(h)
			if !ip.Equal(ip4) {
				out = append(out, ip)
			}
		}
	}
	return out
}

// broadcasts returns the broadcast address of each IPv4 network of the computer, with no loopback.
func broadcasts() []net.IP {
	var out []net.IP
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok || n.IP.IsLoopback() {
			continue
		}
		ip4 := n.IP.To4()
		if ip4 == nil || len(n.Mask) != net.IPv4len {
			continue
		}
		b := make(net.IP, net.IPv4len)
		for i := range b {
			b[i] = ip4[i] | ^n.Mask[i]
		}
		out = append(out, b)
	}
	return out
}
