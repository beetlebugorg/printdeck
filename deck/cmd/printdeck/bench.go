package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"printdeck/internal/gcodestore"
	"printdeck/internal/gfx"
	"printdeck/internal/printer"
	"printdeck/internal/ui"
)

// bench runs the deck UI with no screen on a Klipper host, for a test bench such as
// printer/cr10: the display of the printer shows the deck, and the knob works. The frames of the
// touch screen go to OUT.png, and each new screen of the display goes to the output.
//
//	printdeck bench SOCKET OUT.png
func bench(args []string) {
	if len(args) != 2 {
		log.Fatal("usage: printdeck bench SOCKET OUT.png")
	}
	dir, _ := os.MkdirTemp("", "printdeck-bench")
	defer os.RemoveAll(dir)
	k := printer.NewKlippy(args[0], &gcodestore.Store{Dir: dir})
	app := ui.New()
	app.Printer, app.Music, app.Vol = k, &ui.DemoMusic{}, ui.DemoVolume(0) // no music on a bench
	app.SetTab(ui.TabPrint)
	if f, ok := os.LookupEnv("LCD_FONT"); ok {
		app.LCDFont = f
	}
	app.PreheatTool, app.PreheatBed = 200, 60
	ctx := context.Background()
	go k.Run(ctx, app.Redraw)
	go lcdLoop(ctx, k, app)
	go func() {
		last := ""
		for range time.Tick(250 * time.Millisecond) {
			b, _ := json.Marshal(app.LCD())
			if s := string(b); s != last {
				fmt.Println(s)
				last = s
			}
		}
	}()
	// The frames of the deck, for a browser at ADDR (see benchPage).
	var mu sync.Mutex
	var frame []byte
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, benchPage) })
		mux.HandleFunc("/frame.png", func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			b := frame
			mu.Unlock()
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "no-store")
			w.Write(b)
		})
		log.Printf("bench: the deck is at http://localhost%s", benchAddr)
		log.Print(http.ListenAndServe(benchAddr, mux))
	}()
	var last time.Time
	app.Run(ctx, nil, func(c *gfx.Canvas) {
		if time.Since(last) < 150*time.Millisecond {
			return
		}
		last = time.Now()
		var b bytes.Buffer
		png.Encode(&b, c.RGBA)
		mu.Lock()
		frame = b.Bytes()
		mu.Unlock()
		os.WriteFile(args[1], b.Bytes(), 0o644)
	})
}

// benchAddr is the address of the page of the bench.
const benchAddr = ":8091"

// benchPage shows the frames of the deck, 5 times a second.
const benchPage = `<!doctype html><title>Printdeck bench</title>
<body style="margin:0;background:#222;display:grid;place-items:center;height:100vh">
<img id=f src="/frame.png" style="height:96vh;image-rendering:pixelated">
<script>setInterval(()=>{const i=new Image();i.onload=()=>f.src=i.src;i.src="/frame.png?"+Date.now()},200)</script>`
