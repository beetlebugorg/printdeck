package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// FileStore is a Store in a folder, for pwf dev: the data stays when pwf dev starts again. Each
// file has a JSON file of its options and ETag beside it, with the suffix ".meta".
type FileStore struct {
	Dir string
	mu  sync.Mutex
}

type fileMeta struct {
	ETag string     `json:"etag"`
	Opt  PutOptions `json:"opt"`
}

func (f *FileStore) path(key string) (string, error) {
	if key == "" || strings.Contains(key, "..") || strings.HasPrefix(key, "/") || strings.HasSuffix(key, ".meta") {
		return "", errors.New("bad key")
	}
	return filepath.Join(f.Dir, filepath.FromSlash(key)), nil
}

func (f *FileStore) meta(p string) fileMeta {
	var m fileMeta
	b, _ := os.ReadFile(p + ".meta")
	json.Unmarshal(b, &m)
	return m
}

func (f *FileStore) Get(_ context.Context, key string) ([]byte, string, error) {
	p, err := f.path(key)
	if err != nil {
		return nil, "", ErrNotFound
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) || err == nil && isDir(p) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	return b, f.meta(p).ETag, nil
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func (f *FileStore) Put(_ context.Context, key string, data []byte, opt PutOptions) (string, error) {
	p, err := f.path(key)
	if err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	_, statErr := os.Stat(p)
	exists := statErr == nil
	if opt.IfNoneMatch && exists || opt.IfMatch != "" && (!exists || f.meta(p).ETag != opt.IfMatch) {
		return "", ErrConflict
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	m, _ := json.Marshal(fileMeta{ETag: etag, Opt: PutOptions{ContentType: opt.ContentType, CacheControl: opt.CacheControl}})
	return etag, os.WriteFile(p+".meta", m, 0o644)
}

func (f *FileStore) Copy(ctx context.Context, src, dst string, opt PutOptions) error {
	return copyFile(ctx, f, src, dst, opt)
}

func (f *FileStore) Delete(_ context.Context, key string) error {
	p, err := f.path(key)
	if err != nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	os.Remove(p + ".meta")
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (f *FileStore) List(_ context.Context, prefix string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	filepath.WalkDir(f.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(p, ".meta") {
			return nil
		}
		rel, _ := filepath.Rel(f.Dir, p)
		key := filepath.ToSlash(rel)
		if strings.HasPrefix(key, prefix) {
			out = append(out, key)
		}
		return nil
	})
	sort.Strings(out)
	return out, nil
}

// ContentType returns the content type of the file at key.
func (f *FileStore) ContentType(key string) string {
	p, err := f.path(key)
	if err != nil {
		return ""
	}
	return f.meta(p).Opt.ContentType
}

// CacheControl returns the Cache-Control of the file at key.
func (f *FileStore) CacheControl(key string) string {
	p, err := f.path(key)
	if err != nil {
		return ""
	}
	return f.meta(p).Opt.CacheControl
}
