package api

import (
	"context"
	"reflect"
	"testing"
)

type recordCDN struct{ paths [][]string }

func (r *recordCDN) Invalidate(_ context.Context, paths []string) error {
	r.paths = append(r.paths, paths)
	return nil
}

// A delete of a public file removes it from the edge at the end of the request. A page with many
// files becomes one path with "*". The files of the projects (x/) are not public.
func TestCDNInvalidate(t *testing.T) {
	ctx := context.Background()
	st := &CDNStore{Store: NewMemStore()}
	cdn := &recordCDN{}
	s := &Server{Store: st, CDN: cdn}
	for _, k := range []string{"p/a/page.json", "p/a/1.jpg", "p/a/2.jpg", "p/a/3.jpg", "p/a/4.jpg", "p/a/5.jpg",
		"s/b/page.html", "gh/c.jpg", "x/d/photos/e.jpg"} {
		st.Put(ctx, k, []byte("x"), PutOptions{})
		st.Delete(ctx, k)
	}
	s.flushCDN(ctx)
	want := [][]string{{"/gh/c.jpg", "/p/a/*", "/s/b/page.html"}}
	if !reflect.DeepEqual(cdn.paths, want) {
		t.Fatalf("paths %v, want %v", cdn.paths, want)
	}
	s.flushCDN(ctx)
	if len(cdn.paths) != 1 {
		t.Errorf("a second flush with no delete sent %v", cdn.paths[1:])
	}
}
