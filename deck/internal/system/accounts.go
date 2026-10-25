package system

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// MergeAccounts adds the users and groups of the image to the writable copies of the account files.
// The writable layer keeps its own copy of an account file after a change (for example the
// password of a user), and that copy hides the file of a newer image. upper is the folder of the
// copies (/data/etc), lower the /etc of the image.
//
// The groups of the copy get the members of the same groups of the image. A new user or group whose
// id the copy already uses gets a free id from 100. A new user gets its group by the name. Without
// a copy of a file, the image file is in use and nothing changes.
func MergeAccounts(upper, lower string) error {
	read := func(dir, name string) ([]string, bool, error) {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		return lines(string(b)), true, nil
	}
	lowerGroup, _, err := read(lower, "group")
	if err != nil {
		return err
	}
	upperGroup, haveGroup, err := read(upper, "group")
	if err != nil {
		return err
	}
	if haveGroup {
		upperGroup = mergeGroup(upperGroup, lowerGroup)
		if err := writeFile(filepath.Join(upper, "group"), join(upperGroup), 0o644); err != nil {
			return err
		}
	}
	if upperPasswd, ok, err := read(upper, "passwd"); err != nil {
		return err
	} else if ok {
		lowerPasswd, _, err := read(lower, "passwd")
		if err != nil {
			return err
		}
		merged := mergePasswd(upperGroup, lowerGroup, upperPasswd, lowerPasswd)
		if err := writeFile(filepath.Join(upper, "passwd"), join(merged), 0o644); err != nil {
			return err
		}
	}
	if upperShadow, ok, err := read(upper, "shadow"); err != nil {
		return err
	} else if ok {
		lowerShadow, _, err := read(lower, "shadow")
		if err != nil {
			return err
		}
		merged := append(upperShadow, newEntries(upperShadow, lowerShadow)...)
		fi, _ := os.Stat(filepath.Join(upper, "shadow"))
		mode := os.FileMode(0o600)
		if fi != nil {
			mode = fi.Mode().Perm()
		}
		if err := writeFile(filepath.Join(upper, "shadow"), join(merged), mode); err != nil {
			return err
		}
	}
	return nil
}

// mergeGroup returns the group lines of the copy with the members of the image added, then the
// groups of the image that the copy does not have.
func mergeGroup(upper, lower []string) []string {
	have := map[string]bool{}
	used := map[string]bool{}
	for _, l := range upper {
		f := fields(l, 4)
		have[f[0]] = true
		used[f[2]] = true
	}
	var out []string
	for _, l := range upper {
		f := fields(l, 4)
		for _, g := range lower {
			lf := fields(g, 4)
			if lf[0] != f[0] {
				continue
			}
			for _, m := range strings.Split(lf[3], ",") {
				if m != "" && !strings.Contains(","+f[3]+",", ","+m+",") {
					if f[3] == "" {
						f[3] = m
					} else {
						f[3] += "," + m
					}
				}
			}
		}
		out = append(out, strings.Join(f[:4], ":"))
	}
	for _, g := range lower {
		f := fields(g, 4)
		if have[f[0]] {
			continue
		}
		f[2] = freeID(f[2], used)
		out = append(out, strings.Join(f[:4], ":"))
	}
	return out
}

// mergePasswd returns the user lines of the copy, then the users of the image that the copy does
// not have. group is the merged group file of the copy, lowerGroup the group file of the image.
func mergePasswd(group, lowerGroup, upper, lower []string) []string {
	gid := map[string]string{}
	for _, l := range group {
		f := fields(l, 4)
		gid[f[0]] = f[2]
	}
	gname := map[string]string{}
	for _, l := range lowerGroup {
		f := fields(l, 4)
		gname[f[2]] = f[0]
	}
	have := map[string]bool{}
	used := map[string]bool{}
	out := append([]string{}, upper...)
	for _, l := range upper {
		f := fields(l, 7)
		have[f[0]] = true
		used[f[2]] = true
	}
	for _, l := range lower {
		f := fields(l, 7)
		if have[f[0]] {
			continue
		}
		f[2] = freeID(f[2], used)
		if name, ok := gname[f[3]]; ok {
			if id, ok := gid[name]; ok {
				f[3] = id
			}
		}
		out = append(out, strings.Join(f[:7], ":"))
	}
	return out
}

// newEntries returns the lines of lower whose first field is not in upper.
func newEntries(upper, lower []string) []string {
	have := map[string]bool{}
	for _, l := range upper {
		have[fields(l, 1)[0]] = true
	}
	var out []string
	for _, l := range lower {
		if !have[fields(l, 1)[0]] {
			out = append(out, l)
		}
	}
	return out
}

// freeID returns id, or the first free id from 100 when id is in use. It marks the id as used.
func freeID(id string, used map[string]bool) string {
	if used[id] {
		for n := 100; ; n++ {
			if s := itoa(n); !used[s] {
				id = s
				break
			}
		}
	}
	used[id] = true
	return id
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// fields splits a line at ":" into at least n fields.
func fields(l string, n int) []string {
	f := strings.Split(l, ":")
	for len(f) < n {
		f = append(f, "")
	}
	return f
}

// lines returns the lines of a file, with no empty line at the end.
func lines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func join(l []string) []byte {
	if len(l) == 0 {
		return nil
	}
	return []byte(strings.Join(l, "\n") + "\n")
}
