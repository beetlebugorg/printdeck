package queue

import (
	"path/filepath"
	"testing"
)

func names(q *Queue) string {
	s := ""
	for _, it := range q.List() {
		s += it.Name
	}
	return s
}

func TestQueue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	q := &Queue{Path: path}
	a, _ := q.Add(Item{Name: "a", Seconds: 60})
	b, _ := q.Add(Item{Name: "b", Seconds: 120})
	c, _ := q.Add(Item{Name: "c"})
	if names(q) != "abc" || q.Seconds() != 180 || a.ID == "" || a.ID == b.ID {
		t.Fatalf("queue %q, %d s", names(q), q.Seconds())
	}
	if !q.Move(c.ID, -1) || names(q) != "acb" {
		t.Fatalf("move: %q", names(q))
	}
	if q.Move(a.ID, -1) {
		t.Error("the first file moved to the front")
	}
	if !q.Remove(a.ID) || q.Remove(a.ID) {
		t.Error("remove")
	}
	// The queue stays in its file.
	again := &Queue{Path: path}
	if names(again) != "cb" {
		t.Fatalf("after a load: %q", names(again))
	}
	if n, ok := again.Next(); !ok || n.Name != "c" {
		t.Fatalf("next %+v", n)
	}
}

func TestQueueFull(t *testing.T) {
	q := &Queue{}
	for range MaxItems {
		if _, err := q.Add(Item{Name: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := q.Add(Item{Name: "y"}); err != ErrFull {
		t.Fatalf("err %v", err)
	}
}
