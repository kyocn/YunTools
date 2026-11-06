package cloud

import (
	"testing"
	"time"
)

func TestNewestFirstStableMissingLastWithoutMutatingInput(t *testing.T) {
	old, newest := time.Unix(100, 0), time.Unix(200, 0)
	in := []Domain{{ID: "missing"}, {ID: "old", Created: old}, {ID: "new1", Created: newest}, {ID: "new2", Created: newest}, {ID: "missing2"}}
	got := NewestFirst(in, func(d Domain) time.Time { return d.Created })
	for i, id := range []string{"new1", "new2", "old", "missing", "missing2"} {
		if got[i].ID != id {
			t.Fatal(got)
		}
	}
	if in[0].ID != "missing" {
		t.Fatal("input mutated")
	}
}
