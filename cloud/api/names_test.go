package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"printdeck/internal/cloud"
)

func TestSlugOf(t *testing.T) {
	for name, want := range map[string]string{
		"Garden planters":         "garden-planters",
		"  Shop wall -- v2! ":     "shop-wall-v2",
		"Crème brûlée molds":      "creme-brulee-molds",
		"Straße":                  "strasse",
		"???":                     "project",
		"日本":                      "project",
		strings.Repeat("ab ", 40): strings.TrimRight(strings.Repeat("ab-", 16), "-"),
	} {
		if got := slugOf(name); got != want {
			t.Errorf("slugOf(%q) = %q, want %q", name, got, want)
		}
		if !validSlug(slugOf(name)) {
			t.Errorf("slugOf(%q) is not valid", name)
		}
	}
	for _, bad := range []string{"", "-a", "a-", "A", "a/b", "a.json", strings.Repeat("a", 60)} {
		if validSlug(bad) {
			t.Errorf("validSlug(%q)", bad)
		}
	}
	if !slugFits("garden-planters-12", "garden-planters") || slugFits("garden-planters-x", "garden-planters") || slugFits("garden", "garden-planters") {
		t.Error("slugFits")
	}
}

// A project from before the slugs gets a slug and numbers for its prints, and its public page
// moves to the slug. A rename moves the address again, and the old addresses still work.
func TestOldProjectAddresses(t *testing.T) {
	st, s, _ := testServer(t)
	ctx := context.Background()
	st.Put(ctx, PublicPage, []byte("<!doctype html><title>Project · Printdeck</title><!-- preview -->"), PutOptions{ContentType: "text/html"})
	now := time.Now().UTC()
	old := Project{ID: newID(13), Name: "Garden planters", Created: now, Changed: now, Public: "oldpublicid"}
	if err := s.putJSON(ctx, projectKey(old.ID), old); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"First", "Second"} {
		rec := PrintRecord{PrintRequest: cloud.PrintRequest{Name: name, Result: "complete"}, ID: newID(8),
			Added: now.Add(time.Duration(i) * time.Minute), Done: true}
		s.putJSON(ctx, printKey(old.ID, rec.ID), rec)
	}
	st.Put(ctx, "p/oldpublicid", []byte("old page"), PutOptions{})

	p, err := s.ensureSlug(ctx, &old)
	if err != nil {
		t.Fatal(err)
	}
	if p.Slug != "garden-planters" || p.Public != "garden-planters" || !p.Changed.Equal(now) {
		t.Fatalf("after ensureSlug %+v", p)
	}
	if b, _, err := st.Get(ctx, "p/oldpublicid"); err != nil || !strings.Contains(string(b), `url=/p/garden-planters"`) {
		t.Errorf("the old public address: %s %v", b, err)
	}
	if _, _, err := st.Get(ctx, "p/garden-planters/page.json"); err != nil {
		t.Errorf("the public page at the slug: %v", err)
	}
	prints, _, err := s.projectFiles(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, pr := range prints {
		got[pr.Name] = pr.N
	}
	if got["First"] != 1 || got["Second"] != 2 {
		t.Errorf("the numbers of the old prints %v", got)
	}
	if p, _, _ = s.loadProject(ctx, p.ID); p.LastPrint != 2 {
		t.Errorf("last print %d", p.LastPrint)
	}

	// A rename: a new slug, the old slug still finds the project, and the public page moves.
	p.Name = "Window boxes"
	if p, err = s.renameSlug(ctx, p); err != nil {
		t.Fatal(err)
	}
	if p.Slug != "window-boxes" || p.Public != "window-boxes" || len(p.OldSlugs) != 1 || p.OldSlugs[0] != "garden-planters" {
		t.Fatalf("after the rename %+v", p)
	}
	if rec, err := s.loadName(ctx, "garden-planters"); err != nil || rec.Project != p.ID {
		t.Errorf("the old slug: %+v %v", rec, err)
	}
	if b, _, _ := st.Get(ctx, "p/garden-planters"); !strings.Contains(string(b), `url=/p/window-boxes"`) {
		t.Errorf("the old public address after the rename: %s", b)
	}
	// A name with the same slug keeps the slug.
	p.Name = "Window Boxes"
	if p, _ = s.renameSlug(ctx, p); p.Slug != "window-boxes" || len(p.OldSlugs) != 1 {
		t.Errorf("a name with the same slug %+v", p)
	}

	// Public off deletes the pages at the old addresses too.
	if _, err := s.setPublic(ctx, p.ID, cloud.PublicRequest{On: false}); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"p/oldpublicid", "p/garden-planters", "p/window-boxes", "p/window-boxes/page.json"} {
		if _, _, err := st.Get(ctx, k); err == nil {
			t.Errorf("%s after public off", k)
		}
	}

	// The delete of the project frees its slugs.
	p, _, _ = s.loadProject(ctx, p.ID)
	if err := s.deleteProject(ctx, p); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"garden-planters", "window-boxes"} {
		if _, err := s.loadName(ctx, slug); err == nil {
			t.Errorf("the slug %s after the delete", slug)
		}
	}
}
