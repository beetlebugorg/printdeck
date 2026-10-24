// Package backup writes and restores an encrypted backup of the data of a Printdeck: the state of
// the deck (rolls, history, settings, the API keys and the paired phones), the Klipper config, and
// the G-code files if the user asks for them.
//
// The file is a tar archive, compressed with gzip, and encrypted with a key from the backup
// password. The format:
//
//	header: "PDBACKUP", version (1 byte), scrypt log2 N, r, p (1 byte each), salt (16 bytes)
//	chunks: AES-256-GCM of 64 KiB of the archive each, with the tag. The last chunk is shorter
//	        than 64 KiB (it can be empty) and has the final flag in its nonce.
//
// The nonce of a chunk is its number (11 bytes, big endian) and the final flag (1 byte). Each file
// has its own salt and key, so a nonce is used one time for a key. The header is the additional
// data of each chunk. A wrong password, a changed byte, a missing chunk and a cut file all fail
// in the decryption.
package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/scrypt"
)

const (
	magic     = "PDBACKUP"
	version   = 1
	chunkSize = 64 << 10
	tagSize   = 16
	saltSize  = 16
	headerLen = len(magic) + 4 + saltSize
	// The cost of scrypt: N = 2^15, r = 8, p = 1. It uses 32 MB of memory.
	logN, scryptR, scryptP = 15, 8, 1
	// manifestName is the first entry of the archive.
	manifestName = "printdeck-backup.json"
)

// ErrPassword is the error for a wrong password or a changed file. The two give the same failure
// of the decryption.
var ErrPassword = errors.New("the password is wrong, or the file is damaged")

// A Source is a folder in the backup. Its files go in the archive under Prefix.
type Source struct {
	Prefix string // the folder in the archive, for example "state"
	Dir    string // the folder on the deck
	// Skip excludes a file or a folder, by its path in Dir (with slashes). Restore also skips it,
	// so a backup cannot replace it.
	Skip func(rel string) bool
}

// Manifest is the first entry of the archive.
type Manifest struct {
	Version int       `json:"version"`
	Made    time.Time `json:"made"`
	Folders []string  `json:"folders"` // the prefixes in the archive
}

// Progress gets the bytes of the files done, and the total.
type Progress func(done, total int64)

// Write writes the backup of the sources to w, encrypted with password.
func Write(w io.Writer, password string, sources []Source, progress Progress) error {
	if password == "" {
		return errors.New("no password")
	}
	var total int64
	for _, s := range sources {
		walk(s, func(rel string, fi fs.FileInfo) error {
			if fi.Mode().IsRegular() {
				total += fi.Size()
			}
			return nil
		})
	}
	ew, err := newWriter(w, password)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(ew)
	tw := tar.NewWriter(gz)
	m := Manifest{Version: version, Made: time.Now().UTC()}
	for _, s := range sources {
		m.Folders = append(m.Folders, s.Prefix)
	}
	mb, _ := json.Marshal(m)
	if err := tw.WriteHeader(&tar.Header{Name: manifestName, Mode: 0o600, Size: int64(len(mb)), ModTime: m.Made, Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	if _, err := tw.Write(mb); err != nil {
		return err
	}
	var done int64
	for _, s := range sources {
		err := walk(s, func(rel string, fi fs.FileInfo) error {
			name := s.Prefix + "/" + rel
			switch {
			case fi.IsDir():
				return tw.WriteHeader(&tar.Header{Name: name + "/", Mode: int64(fi.Mode().Perm()), ModTime: fi.ModTime(), Typeflag: tar.TypeDir})
			case fi.Mode().IsRegular():
				n, err := addFile(tw, name, filepath.Join(s.Dir, filepath.FromSlash(rel)), fi)
				if err != nil {
					return err
				}
				done += n
				if progress != nil {
					progress(done, total)
				}
			}
			// Links and devices stay out of the backup.
			return nil
		})
		if err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return ew.Close()
}

func addFile(tw *tar.Writer, name, p string, fi fs.FileInfo) (int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: int64(fi.Mode().Perm()), Size: fi.Size(), ModTime: fi.ModTime(), Typeflag: tar.TypeReg}); err != nil {
		return 0, err
	}
	n, err := io.Copy(tw, io.LimitReader(f, fi.Size()))
	if err != nil {
		return n, err
	}
	if n != fi.Size() {
		return n, fmt.Errorf("%s changed during the backup", name)
	}
	return n, nil
}

// walk calls fn for each folder and file of a source (not its root), in a fixed order. A folder
// that is not there gives no entries.
func walk(s Source, fn func(rel string, fi fs.FileInfo) error) error {
	if _, err := os.Stat(s.Dir); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(s.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(s.Dir, p)
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if s.Skip != nil && s.Skip(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		return fn(rel, fi)
	})
}

// Restore reads a backup from r. It decrypts the whole backup into a folder in work first, and
// checks each entry. Only then does it copy the files into the sources. So a wrong password, a
// damaged file or a bad entry changes no file of the sources. The files of the sources that are
// not in the backup stay. A file of the backup replaces the file of the deck, and gets the owner of
// the old file (or of its folder, for a new file).
func Restore(r io.Reader, password string, sources []Source, work string, progress Progress) error {
	stage, err := os.MkdirTemp(work, "restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	files, total, err := extract(r, password, sources, stage)
	if err != nil {
		return err
	}
	var done int64
	for _, f := range files {
		src := filepath.Join(stage, filepath.FromSlash(f.src.Prefix+"/"+f.rel))
		dst := filepath.Join(f.src.Dir, filepath.FromSlash(f.rel))
		if err := install(src, dst, f.mode, f.modTime); err != nil {
			return fmt.Errorf("%s/%s: %w", f.src.Prefix, f.rel, err)
		}
		done += f.size
		if progress != nil {
			progress(done, total)
		}
	}
	return nil
}

type staged struct {
	src     Source
	rel     string
	mode    fs.FileMode
	size    int64
	modTime time.Time
}

// extract decrypts the backup into stage and returns its files. It fails on the first bad entry.
func extract(r io.Reader, password string, sources []Source, stage string) ([]staged, int64, error) {
	er, err := newReader(r, password)
	if err != nil {
		return nil, 0, err
	}
	gz, err := gzip.NewReader(er)
	if err != nil {
		return nil, 0, damaged(err)
	}
	tr := tar.NewReader(gz)
	var files []staged
	var total int64
	first := true
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, damaged(err)
		}
		if first {
			first = false
			if err := readManifest(h, tr); err != nil {
				return nil, 0, err
			}
			continue
		}
		src, rel, err := place(h.Name, sources)
		if err != nil {
			return nil, 0, err
		}
		if src.Skip != nil && src.Skip(rel) {
			continue
		}
		out := filepath.Join(stage, src.Prefix, filepath.FromSlash(rel))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(out, 0o700); err != nil {
				return nil, 0, err
			}
		case tar.TypeReg:
			if err := stageFile(out, tr); err != nil {
				return nil, 0, err
			}
			files = append(files, staged{src: src, rel: rel, mode: fs.FileMode(h.Mode).Perm(), size: h.Size, modTime: h.ModTime})
			total += h.Size
		default:
			return nil, 0, fmt.Errorf("%s: only files and folders can be in a backup", h.Name)
		}
	}
	if first {
		return nil, 0, errors.New("the file is not a Printdeck backup")
	}
	// Read to the end of the encrypted stream, so that a missing final chunk fails here.
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return nil, 0, damaged(err)
	}
	if _, err := io.Copy(io.Discard, er); err != nil {
		return nil, 0, damaged(err)
	}
	return files, total, nil
}

func readManifest(h *tar.Header, tr *tar.Reader) error {
	if h.Name != manifestName {
		return errors.New("the file is not a Printdeck backup")
	}
	var m Manifest
	if err := json.NewDecoder(io.LimitReader(tr, 1<<20)).Decode(&m); err != nil {
		return errors.New("the file is not a Printdeck backup")
	}
	if m.Version != version {
		return fmt.Errorf("backup version %d is not known", m.Version)
	}
	return nil
}

func stageFile(out string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
		return err
	}
	// O_EXCL: an archive with the same file two times fails.
	f, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return damaged(err)
	}
	return nil
}

// place finds the source of an entry, and the path in the source. It refuses an absolute path, a
// path with "." or "..", and a prefix that is not a source.
func place(name string, sources []Source) (Source, string, error) {
	bad := fmt.Errorf("%q: the path is not allowed", name)
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
		return Source{}, "", bad
	}
	for _, part := range strings.Split(strings.TrimSuffix(name, "/"), "/") {
		if part == ".." || part == "." || part == "" {
			return Source{}, "", bad
		}
	}
	prefix, rel, ok := strings.Cut(path.Clean(name), "/")
	if !ok || rel == "" {
		return Source{}, "", bad
	}
	for _, s := range sources {
		if s.Prefix == prefix {
			return s, rel, nil
		}
	}
	return Source{}, "", bad
}

// install copies a staged file to dst through a temporary file and a rename.
func install(src, dst string, mode fs.FileMode, mt time.Time) error {
	if err := mkdirOwned(filepath.Dir(dst)); err != nil {
		return err
	}
	if fi, err := os.Lstat(dst); err == nil && !fi.Mode().IsRegular() {
		return errors.New("a folder or a link has the name of the file")
	}
	uid, gid, ok := owner(dst)
	if !ok {
		uid, gid, ok = owner(filepath.Dir(dst))
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".restore"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if serr := out.Sync(); err == nil {
		err = serr
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, mode)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if ok {
		os.Chown(tmp, uid, gid) // only root can change the owner; the tests run as a user
	}
	os.Chtimes(tmp, mt, mt)
	return os.Rename(tmp, dst)
}

// mkdirOwned makes a folder and its missing parents. Each new folder gets the owner of its parent.
func mkdirOwned(dir string) error {
	if fi, err := os.Stat(dir); err == nil {
		if !fi.IsDir() {
			return fmt.Errorf("%s is not a folder", dir)
		}
		return nil
	}
	if err := mkdirOwned(filepath.Dir(dir)); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	if uid, gid, ok := owner(filepath.Dir(dir)); ok {
		os.Chown(dir, uid, gid)
	}
	return nil
}

func owner(p string) (uid, gid int, ok bool) {
	fi, err := os.Lstat(p)
	if err != nil {
		return 0, 0, false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return int(st.Uid), int(st.Gid), true
}

// damaged gives ErrPassword for a failure of the decryption under the gzip and tar readers, and
// a "damaged" error for the rest.
func damaged(err error) error {
	if errors.Is(err, ErrPassword) {
		return ErrPassword
	}
	return fmt.Errorf("the file is damaged: %w", err)
}

// ---------------------------------------------------------------- the encryption

func header(salt []byte) []byte {
	h := append([]byte(magic), version, logN, scryptR, scryptP)
	return append(h, salt...)
}

func newAEAD(password string, salt []byte, logn, r, p int) (cipher.AEAD, error) {
	k, err := scrypt.Key([]byte(password), salt, 1<<logn, r, p, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func nonce(n uint64, final bool) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint64(b[3:11], n)
	if final {
		b[11] = 1
	}
	return b
}

// writer encrypts the chunks. Close writes the final chunk.
type writer struct {
	w    io.Writer
	aead cipher.AEAD
	ad   []byte
	buf  []byte
	n    uint64
}

func newWriter(w io.Writer, password string) (*writer, error) {
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	aead, err := newAEAD(password, salt, logN, scryptR, scryptP)
	if err != nil {
		return nil, err
	}
	h := header(salt)
	if _, err := w.Write(h); err != nil {
		return nil, err
	}
	return &writer{w: w, aead: aead, ad: h, buf: make([]byte, 0, chunkSize)}, nil
}

func (e *writer) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		// A full buffer goes out only when more data comes, so the final chunk is short.
		if len(e.buf) == chunkSize {
			if err := e.flush(false); err != nil {
				return 0, err
			}
		}
		k := copy(e.buf[len(e.buf):chunkSize], p)
		e.buf = e.buf[:len(e.buf)+k]
		p = p[k:]
	}
	return n, nil
}

func (e *writer) flush(final bool) error {
	out := e.aead.Seal(nil, nonce(e.n, final), e.buf, e.ad)
	e.n++
	e.buf = e.buf[:0]
	_, err := e.w.Write(out)
	return err
}

func (e *writer) Close() error {
	if len(e.buf) == chunkSize {
		if err := e.flush(false); err != nil {
			return err
		}
	}
	return e.flush(true)
}

// reader decrypts the chunks. A chunk that does not decrypt, and a file with no final chunk, give
// ErrPassword.
type reader struct {
	r     io.Reader
	aead  cipher.AEAD
	ad    []byte
	n     uint64
	enc   []byte
	plain []byte
	done  bool
}

func newReader(r io.Reader, password string) (*reader, error) {
	h := make([]byte, headerLen)
	if _, err := io.ReadFull(r, h); err != nil || !bytes.HasPrefix(h, []byte(magic)) {
		return nil, errors.New("the file is not a Printdeck backup")
	}
	if h[len(magic)] != version {
		return nil, fmt.Errorf("backup version %d is not known", h[len(magic)])
	}
	logn, rr, p := int(h[len(magic)+1]), int(h[len(magic)+2]), int(h[len(magic)+3])
	// Limits for the cost, so that a changed header cannot use all the memory of the deck.
	if logn < 10 || logn > 18 || rr < 1 || rr > 16 || p < 1 || p > 4 {
		return nil, errors.New("the file is damaged: bad key settings")
	}
	aead, err := newAEAD(password, h[len(magic)+4:], logn, rr, p)
	if err != nil {
		return nil, err
	}
	return &reader{r: r, aead: aead, ad: h, enc: make([]byte, chunkSize+tagSize)}, nil
}

func (d *reader) Read(p []byte) (int, error) {
	for len(d.plain) == 0 {
		if d.done {
			return 0, io.EOF
		}
		k, err := io.ReadFull(d.r, d.enc)
		final := false
		switch {
		case err == io.ErrUnexpectedEOF || err == io.EOF:
			final = true
		case err != nil:
			return 0, err
		}
		if final && k < tagSize {
			return 0, ErrPassword // the final chunk is missing
		}
		plain, oerr := d.aead.Open(nil, nonce(d.n, final), d.enc[:k], d.ad)
		if oerr != nil {
			return 0, ErrPassword
		}
		d.n++
		d.plain, d.done = plain, final
	}
	k := copy(p, d.plain)
	d.plain = d.plain[k:]
	return k, nil
}
