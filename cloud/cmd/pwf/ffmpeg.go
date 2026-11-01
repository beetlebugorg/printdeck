package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// The ffmpeg of the jobs function, for the videos of the time lapses (api/lapse.go): the static build
// of John Van Sickle for Linux arm64, with libx264. It is GPL v3. The function runs it as a
// separate program, and the zip has its license next to it. pwf keeps the download in the cache
// folder of the user, and checks its SHA-256 at each build.
const (
	ffmpegVersion = "7.0.2"
	ffmpegSHA256  = "f4149bb2b0784e30e99bdda85471c9b5930d3402014e934a5098b41d0f7201b1"
	ffmpegDir     = "ffmpeg-" + ffmpegVersion + "-arm64-static"
)

// ffmpegURLs are the addresses of the archive. The site moves the current release to
// old-releases when a new release comes.
var ffmpegURLs = []string{
	"https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-arm64-static.tar.xz",
	"https://johnvansickle.com/ffmpeg/old-releases/" + ffmpegDir + ".tar.xz",
}

// ffmpegFiles returns the ffmpeg program and its license from the archive, for the zip of the
// function.
func ffmpegFiles(ctx context.Context) (bin, license []byte, err error) {
	archive, err := ffmpegArchive(ctx)
	if err != nil {
		return nil, nil, err
	}
	tmp, err := os.MkdirTemp("", "pwf-ffmpeg")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(tmp)
	cmd := exec.CommandContext(ctx, "tar", "-xJf", archive, "-C", tmp, ffmpegDir+"/ffmpeg", ffmpegDir+"/GPLv3.txt")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, nil, fmt.Errorf("unpack ffmpeg: %w", err)
	}
	if bin, err = os.ReadFile(filepath.Join(tmp, ffmpegDir, "ffmpeg")); err != nil {
		return nil, nil, err
	}
	if license, err = os.ReadFile(filepath.Join(tmp, ffmpegDir, "GPLv3.txt")); err != nil {
		return nil, nil, err
	}
	return bin, license, nil
}

// ffmpegArchive returns the path of the archive in the cache, and downloads it first when the
// cache has no good copy.
func ffmpegArchive(ctx context.Context) (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "printdeck")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, ffmpegDir+".tar.xz")
	if b, err := os.ReadFile(path); err == nil && sha256Hex(b) == ffmpegSHA256 {
		return path, nil
	}
	fmt.Println("Downloading ffmpeg " + ffmpegVersion)
	var errs []error
	for _, u := range ffmpegURLs {
		b, err := download(ctx, u)
		if err == nil && sha256Hex(b) != ffmpegSHA256 {
			err = fmt.Errorf("the SHA-256 is %s, and pwf needs %s", sha256Hex(b), ffmpegSHA256)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", u, err))
			continue
		}
		tmp := path + ".part"
		if err := os.WriteFile(tmp, b, 0o644); err != nil {
			return "", err
		}
		return path, os.Rename(tmp, path)
	}
	return "", fmt.Errorf("download ffmpeg: %w", errors.Join(errs...))
}

// download returns the body of a GET of the address u.
func download(ctx context.Context, u string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 100<<20))
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
