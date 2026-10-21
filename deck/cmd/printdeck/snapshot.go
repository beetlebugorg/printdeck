package main

import (
	"bufio"
	"bytes"
	"fmt"
	"image"
	"image/png"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"printdeck/internal/gfx"
	"printdeck/internal/ui"
)

// snapshot logs in to a Printdeck, opens a session of its UI, taps the screen, and writes the
// frame as a PNG. It draws the frame from the drawing commands, as a browser does. It is for the
// manual and for checks of a Printdeck with no SSH.
//
//	printdeck snapshot URL PASSWORD OUT.png [h=HEIGHT] [tap=X,Y ...] [wait=MS]
//
// X and Y are in the units of the UI: 480 wide.
func snapshot(args []string) {
	if len(args) < 3 {
		log.Fatal("usage: printdeck snapshot URL PASSWORD OUT.png [h=HEIGHT] [tap=X,Y ...] [wait=MS]")
	}
	base, password, out := strings.TrimSuffix(args[0], "/"), args[1], args[2]
	height, wait := ui.BaseH, 1500*time.Millisecond
	var taps []image.Point
	for _, a := range args[3:] {
		k, v, _ := strings.Cut(a, "=")
		switch k {
		case "h":
			height, _ = strconv.Atoi(v)
		case "wait":
			ms, _ := strconv.Atoi(v)
			wait = time.Duration(ms) * time.Millisecond
		case "tap":
			x, y, _ := strings.Cut(v, ",")
			xi, _ := strconv.Atoi(x)
			yi, _ := strconv.Atoi(y)
			taps = append(taps, image.Pt(xi, yi))
		}
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	if r, err := client.PostForm(base+"/login", url.Values{"password": {password}}); err != nil {
		log.Fatal(err)
	} else {
		r.Body.Close()
		if r.Request.URL.Path == "/login" {
			log.Fatal("the password is not correct")
		}
	}

	stream := &http.Client{Jar: jar}
	resp, err := stream.Get(fmt.Sprintf("%s/ui/events?h=%d", base, height))
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	var mu sync.Mutex
	var frame []byte
	session := make(chan string, 1)
	go func() {
		s := bufio.NewScanner(resp.Body)
		s.Buffer(make([]byte, 1<<20), 16<<20)
		event := ""
		for s.Scan() {
			l := s.Text()
			switch {
			case strings.HasPrefix(l, "event: "):
				event = l[7:]
			case strings.HasPrefix(l, "data: ") && event == "session":
				id, h, _ := strings.Cut(l[6:], " ")
				if n, err := strconv.Atoi(h); err == nil {
					height = n
				}
				session <- id
				event = ""
			case strings.HasPrefix(l, "data: "):
				mu.Lock()
				frame = []byte(l[6:])
				mu.Unlock()
			}
		}
	}()
	id := <-session
	time.Sleep(500 * time.Millisecond)
	touch := func(kind string, p image.Point) {
		body := fmt.Sprintf(`{"kind":%q,"x":%f,"y":%f}`, kind, float64(p.X)/ui.W, float64(p.Y)/float64(height))
		if r, err := client.Post(base+"/ui/touch?s="+id, "application/json", strings.NewReader(body)); err == nil {
			r.Body.Close()
		}
	}
	for _, p := range taps {
		touch("down", p)
		time.Sleep(80 * time.Millisecond)
		touch("up", p)
		time.Sleep(700 * time.Millisecond)
	}
	time.Sleep(wait)
	mu.Lock()
	f := frame
	mu.Unlock()
	if f == nil {
		log.Fatal("no frame")
	}
	images := map[int]image.Image{}
	c := gfx.New(ui.W, height)
	err = gfx.Replay(c, f, func(n int) image.Image {
		if img, ok := images[n]; ok {
			return img
		}
		r, err := client.Get(fmt.Sprintf("%s/ui/img/%d", base, n))
		if err != nil {
			return nil
		}
		defer r.Body.Close()
		img, _ := png.Decode(r.Body)
		images[n] = img
		return img
	})
	if err != nil {
		log.Fatal(err)
	}
	var b bytes.Buffer
	png.Encode(&b, c.RGBA)
	if err := os.WriteFile(out, b.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
}
