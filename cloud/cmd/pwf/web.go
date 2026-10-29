package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"printdeck/cloud/deploy"
)

// The pages of the service: the static files of deploy/web, and the logic of the pages, which
// cmd/pwf-web builds to WebAssembly. TinyGo makes a file about 5 times smaller than Go, so pwf
// uses TinyGo when it finds it, and Go when it does not. Each compiler has its own wasm_exec.js.

// boot starts the WebAssembly after wasm_exec.js, in assets/pwf.js. %s is the path of the
// WebAssembly file.
const boot = `
(() => {
  const go = new Go();
  WebAssembly.instantiateStreaming(fetch("%s"), go.importObject)
    .then((r) => go.run(r.instance));
})();
`

// tinygo returns the command line that runs TinyGo, or nil when TinyGo is not installed. Mise
// installs TinyGo with no default version, so pwf also tries the newest version of mise.
func tinygo(ctx context.Context) []string {
	for _, c := range [][]string{{"tinygo"}, {"mise", "exec", "tinygo@latest", "--", "tinygo"}} {
		if exec.CommandContext(ctx, c[0], append(c[1:], "version")...).Run() == nil {
			return c
		}
	}
	return nil
}

// buildWeb returns the files of the pages, by their keys in the bucket.
func buildWeb(ctx context.Context, src string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(deploy.Web, "web", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := deploy.Web.ReadFile(p)
		files[strings.TrimPrefix(p, "web/")] = b
		return err
	})
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "pwf-web")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	out := filepath.Join(tmp, "pwf-web.wasm")
	var execJS string
	if tg := tinygo(ctx); tg != nil {
		fmt.Println("Building the pages with TinyGo")
		cmd := exec.CommandContext(ctx, tg[0], append(tg[1:], "build", "-o", out, "-target", "wasm", "-no-debug", "-opt=z", "./cmd/pwf-web")...)
		cmd.Dir, cmd.Stdout, cmd.Stderr = src, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("tinygo build: %w", err)
		}
		root, err := exec.CommandContext(ctx, tg[0], append(tg[1:], "env", "TINYGOROOT")...).Output()
		if err != nil {
			return nil, fmt.Errorf("tinygo env: %w", err)
		}
		execJS = filepath.Join(strings.TrimSpace(string(root)), "targets", "wasm_exec.js")
	} else {
		fmt.Println("Building the pages with Go (install TinyGo for a smaller file)")
		cmd := exec.CommandContext(ctx, "go", "build", "-ldflags", "-s -w", "-o", out, "./cmd/pwf-web")
		cmd.Dir, cmd.Stdout, cmd.Stderr = src, os.Stdout, os.Stderr
		cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("go build of the pages: %w", err)
		}
		root, err := exec.CommandContext(ctx, "go", "env", "GOROOT").Output()
		if err != nil {
			return nil, err
		}
		execJS = filepath.Join(strings.TrimSpace(string(root)), "lib", "wasm", "wasm_exec.js")
	}
	wasm, err := os.ReadFile(out)
	if err != nil {
		return nil, err
	}
	js, err := os.ReadFile(execJS)
	if err != nil {
		return nil, fmt.Errorf("wasm_exec.js: %w", err)
	}
	files["assets/pwf-web.wasm"] = wasm
	wasmKey := hashed("assets/pwf-web.wasm", wasm)
	files[wasmKey] = wasm
	files["assets/pwf.js"] = append(bytes.TrimRight(js, "\n"), []byte("\n"+fmt.Sprintf(boot, "/"+wasmKey))...)
	hashAssets(files)
	return files, nil
}

// hashAssets adds a copy of each script and style file of assets/ with a hash of its content in
// its name, and points the pages at the copies. A browser or CloudFront can keep an old file
// for a time (pageCache), so a page of a new deploy gets new names and never mixes old code with
// new HTML. CloudFront does not use the query string in its cache key, so the hash is in the
// name. The files keep their plain names too, for a page that a browser has from before.
func hashAssets(files map[string][]byte) {
	refs := map[string]string{}
	for key, data := range files {
		if dir, name := path.Split(key); dir == "assets/" && (path.Ext(name) == ".css" || path.Ext(name) == ".js") {
			refs["/"+key] = "/" + hashed(key, data)
		}
	}
	for from, to := range refs {
		files[strings.TrimPrefix(to, "/")] = files[strings.TrimPrefix(from, "/")]
	}
	for key, data := range files {
		if path.Ext(key) != ".html" {
			continue
		}
		s := string(data)
		for from, to := range refs {
			s = strings.ReplaceAll(s, `"`+from+`"`, `"`+to+`"`)
		}
		files[key] = []byte(s)
	}
}

// hashed is the key of a file with the first 10 hex digits of the SHA-256 of data before its
// extension, for example assets/share.0a1b2c3d4e.css.
func hashed(key string, data []byte) string {
	sum := sha256.Sum256(data)
	ext := path.Ext(key)
	return strings.TrimSuffix(key, ext) + "." + hex.EncodeToString(sum[:5]) + ext
}

// hashedName matches a key from hashed.
var hashedName = regexp.MustCompile(`\.[0-9a-f]{10}\.[a-z]+$`)

// pageCache is the Cache-Control of the files of the pages with plain names. The names stay the
// same at a new deploy, so the time is short. A file with a hash in its name never changes
// (immutableCache).
const (
	pageCache      = "public, max-age=300"
	immutableCache = "public, max-age=31536000, immutable"
)
