package api

import (
	"context"
	"errors"
	"mime"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// The service keeps its data as files in one S3 bucket. Store is that bucket. MemStore is a
// store in memory for the tests, and S3Store (s3store.go) is the bucket.

// Errors of a Store.
var (
	ErrNotFound = errors.New("not found")
	// ErrConflict is a write with IfMatch or IfNoneMatch that failed: another write came first.
	ErrConflict = errors.New("the file changed")
)

// PutOptions are the options of Store.Put.
type PutOptions struct {
	ContentType  string
	CacheControl string
	// IfMatch writes only when the file has this ETag. IfNoneMatch writes only when the file does
	// not exist.
	IfMatch     string
	IfNoneMatch bool
}

// Store is the bucket of the service.
type Store interface {
	// Get returns the file at key and its ETag, or ErrNotFound.
	Get(ctx context.Context, key string) ([]byte, string, error)
	// Put writes the file at key, and returns its new ETag.
	Put(ctx context.Context, key string, data []byte, opt PutOptions) (string, error)
	// Delete removes the file at key. A key with no file is no error.
	Delete(ctx context.Context, key string) error
	// List returns the keys that start with prefix, in order.
	List(ctx context.Context, prefix string) ([]string, error)
	// Copy copies the file at src to dst, with the options of opt (an empty content type is the
	// type of the extension of dst). It returns ErrNotFound for no file at src.
	Copy(ctx context.Context, src, dst string, opt PutOptions) error
}

// copyFile is Copy with Get and Put, for the stores with no copy on the server.
func copyFile(ctx context.Context, st Store, src, dst string, opt PutOptions) error {
	b, _, err := st.Get(ctx, src)
	if err != nil {
		return err
	}
	if opt.ContentType == "" {
		opt.ContentType = mime.TypeByExtension(path.Ext(dst))
	}
	_, err = st.Put(ctx, dst, b, opt)
	return err
}

func (m *MemStore) Copy(ctx context.Context, src, dst string, opt PutOptions) error {
	return copyFile(ctx, m, src, dst, opt)
}

// MemStore is a Store in memory.
type MemStore struct {
	mu    sync.Mutex
	files map[string]memFile
	n     int
}

type memFile struct {
	data []byte
	etag string
	opt  PutOptions
}

// NewMemStore returns an empty MemStore.
func NewMemStore() *MemStore { return &MemStore{files: map[string]memFile{}} }

func (m *MemStore) Get(_ context.Context, key string) ([]byte, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[key]
	if !ok {
		return nil, "", ErrNotFound
	}
	return append([]byte(nil), f.data...), f.etag, nil
}

func (m *MemStore) Put(_ context.Context, key string, data []byte, opt PutOptions) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[key]
	if opt.IfNoneMatch && ok || opt.IfMatch != "" && (!ok || f.etag != opt.IfMatch) {
		return "", ErrConflict
	}
	m.n++
	etag := `"` + strconv.Itoa(m.n) + `"`
	m.files[key] = memFile{data: append([]byte(nil), data...), etag: etag, opt: opt}
	return etag, nil
}

func (m *MemStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, key)
	return nil
}

func (m *MemStore) List(_ context.Context, prefix string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for k := range m.files {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out, nil
}

// ContentType returns the content type of the file at key, for the tests.
func (m *MemStore) ContentType(key string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.files[key].opt.ContentType
}

// CacheControl returns the Cache-Control of the file at key, for pwf dev.
func (m *MemStore) CacheControl(key string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.files[key].opt.CacheControl
}
