package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"printdeck/cloud/api"
)

// runDev serves the whole service on one local address, with the data in memory: the API, the
// uploads and the pages, as CloudFront and S3 serve them. It prints an enrollment code for a
// deck. A simulated deck connects with the service set to the address of pwf dev.
func runDev(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("dev", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8790", "the address of the service")
	data := fs.String("data", "", "a folder for the data, so it stays at the next start (default: in memory)")
	src := fs.String("src", "", "the folder of the cloud module (default: found from the current folder)")
	fs.Parse(args)
	dir, err := findSrc(*src)
	if err != nil {
		return err
	}
	files, err := buildWeb(ctx, dir)
	if err != nil {
		return err
	}
	var st api.DevStore = api.NewMemStore()
	if *data != "" {
		st = &api.FileStore{Dir: *data}
	}
	for key, data := range files {
		st.Put(ctx, key, data, api.PutOptions{ContentType: mime.TypeByExtension(path.Ext(key)), CacheControl: "no-cache"})
	}
	// A passkey needs a domain, so the links use localhost in place of 127.0.0.1.
	base := "http://" + strings.Replace(*addr, "127.0.0.1", "localhost", 1)
	g := &api.LocalGranter{URL: base + "/upload"}
	d := &api.DevServer{API: &api.Server{Store: st, Grants: g, Base: base}, Store: st, Grants: g}
	code, err := api.NewCode(ctx, st, "deck", time.Now())
	if err != nil {
		return err
	}
	go func() {
		for range time.Tick(time.Minute) {
			if err := d.API.Sweep(ctx); err != nil {
				log.Printf("sweep: %v", err)
			}
		}
	}()
	fmt.Printf("The service runs at %s\n\nOn the deck: Settings, Cloud, Service %s, then Connect with the code\n\n    %s\n\n",
		base, base, code)
	return http.ListenAndServe(*addr, d)
}
