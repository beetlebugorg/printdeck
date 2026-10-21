package system

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The releases of Printdeck are the GitHub releases of the repository. A tag vX.Y.Z is a stable
// release, and a tag vX.Y.Z-beta.N is a beta (a pre-release). The update file of a release is its
// asset printdeck-update.tar.xz.

// ReleasesURL is the API of the releases. Tests change it.
var ReleasesURL = "https://api.github.com/repos/beetlebugorg/printdeck/releases?per_page=30"

// Release is a release of Printdeck.
type Release struct {
	Tag    string
	Name   string
	Notes  string
	Beta   bool
	Bundle string // the URL of printdeck-update.tar.xz
	Size   int64
}

// LatestRelease returns the newest release with an update file, for the channel "stable" or
// "beta" (a beta also gets the stable releases). ok is false when there is none.
func LatestRelease(ctx context.Context, channel string) (r Release, ok bool, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", ReleasesURL, nil)
	if err != nil {
		return r, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return r, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return r, false, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	return latestOf(resp.Body, channel)
}

func latestOf(body io.Reader, channel string) (best Release, ok bool, err error) {
	var list []struct {
		Tag        string `json:"tag_name"`
		Name       string `json:"name"`
		Body       string `json:"body"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(body).Decode(&list); err != nil {
		return best, false, err
	}
	for _, rel := range list {
		if rel.Draft || rel.Prerelease && channel != "beta" {
			continue
		}
		if _, ok := parseVersion(rel.Tag); !ok {
			continue
		}
		r := Release{Tag: rel.Tag, Name: rel.Name, Notes: rel.Body, Beta: rel.Prerelease}
		for _, a := range rel.Assets {
			if a.Name == "printdeck-update.tar.xz" {
				r.Bundle, r.Size = a.URL, a.Size
			}
		}
		if r.Bundle == "" {
			continue
		}
		if !ok || Newer(r.Tag, best.Tag) {
			best, ok = r, true
		}
	}
	return best, ok, nil
}

// version is a version for the comparison: the numbers, the pre-release (a beta is before its
// release), and the commits after the tag of a development build (git describe).
type version struct {
	num     [3]int
	pre     string // "" for a release
	commits int
}

// parseVersion reads "v1.2.3", "v1.2.3-beta.2", "v1.2.3-4-gabcdef" or "v1.2.3-beta.2-4-gabcdef"
// (and "-dirty" at the end).
func parseVersion(s string) (v version, ok bool) {
	s = strings.TrimSuffix(strings.TrimPrefix(s, "v"), "-dirty")
	parts := strings.Split(s, "-")
	// git describe: the last two parts are the count and "g" with the commit.
	if n := len(parts); n >= 3 && strings.HasPrefix(parts[n-1], "g") {
		if c, err := strconv.Atoi(parts[n-2]); err == nil {
			v.commits, parts = c, parts[:n-2]
		}
	}
	nums := strings.Split(parts[0], ".")
	if len(nums) != 3 {
		return v, false
	}
	for i, x := range nums {
		n, err := strconv.Atoi(x)
		if err != nil {
			return v, false
		}
		v.num[i] = n
	}
	v.pre = strings.Join(parts[1:], "-")
	return v, true
}

// Newer reports that version a is newer than version b. A version that is not a release version
// (a build with no tag) is older than every release.
func Newer(a, b string) bool {
	va, oka := parseVersion(a)
	vb, okb := parseVersion(b)
	switch {
	case !oka:
		return false
	case !okb:
		return true
	}
	for i := range va.num {
		if va.num[i] != vb.num[i] {
			return va.num[i] > vb.num[i]
		}
	}
	if va.pre != vb.pre {
		switch {
		case va.pre == "":
			return true
		case vb.pre == "":
			return false
		}
		return comparePre(va.pre, vb.pre) > 0
	}
	return va.commits > vb.commits
}

// comparePre compares two pre-release names, such as "beta.2" and "beta.10", by their parts.
func comparePre(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		na, ea := strconv.Atoi(pa[i])
		nb, eb := strconv.Atoi(pb[i])
		switch {
		case ea == nil && eb == nil && na != nb:
			if na > nb {
				return 1
			}
			return -1
		case (ea != nil || eb != nil) && pa[i] != pb[i]:
			return strings.Compare(pa[i], pb[i])
		}
	}
	return len(pa) - len(pb)
}

// Download writes the update file of a release to UpdateBundle, for the update action of the
// helper. progress gets the bytes done and all bytes.
func Download(ctx context.Context, r Release, progress func(done, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, "GET", r.Bundle, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 30 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the download failed: %s", resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(UpdateBundle), 0o755); err != nil {
		return err
	}
	tmp := UpdateBundle + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				f.Close()
				return err
			}
			done += int64(n)
			progress(done, total)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, UpdateBundle)
}
