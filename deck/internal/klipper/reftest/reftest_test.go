package reftest

import (
	"os"
	"strings"
	"testing"
)

func TestCompare(t *testing.T) {
	want := "allocate_oids count=2\nconfig_spi oid=0 pin=PD7\nqueue_step oid=1 interval=5\nspi_send oid=0 data=b'\\x01'\nqueue_step oid=1 interval=6\n"
	// Commands of different objects in another order are equal.
	other := "allocate_oids count=2\nconfig_spi oid=0 pin=PD7\nqueue_step oid=1 interval=5\nqueue_step oid=1 interval=6\nspi_send oid=0 data=b'\\x01'\n"
	if err := Compare(other, want); err != nil {
		t.Errorf("reordered between objects: %v", err)
	}
	// Commands of one object in another order are different.
	swapped := "allocate_oids count=2\nconfig_spi oid=0 pin=PD7\nqueue_step oid=1 interval=6\nspi_send oid=0 data=b'\\x01'\nqueue_step oid=1 interval=5\n"
	if err := Compare(swapped, want); err == nil {
		t.Error("reordered within one object, but equal")
	}
	// A missing command is a difference.
	if err := Compare(strings.Replace(want, "queue_step oid=1 interval=6\n", "", 1), want); err == nil {
		t.Error("a missing command, but equal")
	}
}

// The reference stream is equal to itself, and has a group for each object.
func TestReferenceGroups(t *testing.T) {
	ref, err := os.ReadFile("../testdata/basic.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := Compare(string(ref), string(ref)); err != nil {
		t.Fatal(err)
	}
	if g := Groups(string(ref)); len(g) < 10 {
		t.Errorf("only %d groups", len(g))
	}
}

// The runs of klippy for one G-code file are equal by object: the rule of Compare matches klippy.
func TestKlippyRunsAreEqual(t *testing.T) {
	ref, err := os.ReadFile("../testdata/basic.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range []string{"basic.run2.txt", "basic.run3.txt"} {
		other, err := os.ReadFile("../testdata/" + run)
		if err != nil {
			t.Fatal(err)
		}
		if err := Compare(string(other), string(ref)); err != nil {
			t.Errorf("%s: %v", run, err)
		}
	}
}
