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
// deck to the board. The deck keeps a copy of each upload for its preview. port is the UDP port,
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
	name, _ := os.Hostname()
	r := &chitu.Relay{Conn: conn, Board: c.Client.Addr, Info: c.RelayInfo, Name: name,
		OnUpload: c.SaveUpload, OnActive: c.Hold}
	go r.Run(ctx)
}
