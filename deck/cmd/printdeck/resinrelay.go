package main

import (
	"context"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"printdeck/internal/chitu"
	"printdeck/internal/printer"
)

// startChiTuRelay starts the relay of ChiTuBox (chitu.Relay) on UDP port 3000 for the ChiTu
// backend. ChiTuBox then lists the deck as a printer, and its uploads and starts go through the
// deck to the board. The deck keeps each upload, sends it to the board one packet at a time, and
// shows its preview. port is the UDP port,
// chitu.Port on a deck.
func startChiTuRelay(ctx context.Context, stateDir string, pr printerBackend, port int) {
	c, ok := pr.(*printer.ChiTu)
	if !ok {
		return
	}
	c.UploadDir = filepath.Join(stateDir, "resin-files")
	conn, err := net.ListenPacket("udp4", ":"+strconv.Itoa(port))
	if err != nil {
		log.Printf("chitu relay: %v", err)
		return
	}
	// ChituManager sends about 500 packets a second with no wait.
	if uc, ok := conn.(*net.UDPConn); ok {
		uc.SetReadBuffer(4 << 20)
	}
	if err := os.MkdirAll(c.UploadDir, 0o755); err != nil {
		log.Printf("chitu relay: %v", err)
	}
	name, _ := os.Hostname()
	r := &chitu.Relay{Conn: conn, Board: c.Client.Addr, Info: c.RelayInfo, Name: name,
		Dir: c.UploadDir, OnUpload: c.SaveUpload, OnReceive: c.Received, OnActive: c.Hold, Busy: c.Busy}
	go r.Run(ctx)
}
