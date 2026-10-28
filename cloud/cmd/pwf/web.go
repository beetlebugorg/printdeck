package main

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"printdeck/cloud/deploy"
)

// The pages of the service: the static files of deploy/web, and the logic of the pages, which
// cmd/pwf-web builds to WebAssembly. TinyGo makes a file about 5 times smaller than Go, so pwf
// uses TinyGo when it finds it, and Go when it does not. Each compiler has its own wasm_exec.js.

// boot starts the WebAssembly after wasm_exec.js, in assets/pwf.js.
const boot = `
(() => {
  const go = new Go();
  WebAssembly.instantiateStreaming(fetch("/assets/pwf-web.wasm"), go.importObject)
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
	files["assets/pwf.js"] = append(bytes.TrimRight(js, "\n"), []byte("\n"+boot)...)
	return files, nil
}

// pageCache is the Cache-Control of the files of the pages. The names stay the same at a new
// deploy, so the time is short.
const pageCache = "public, max-age=300"
