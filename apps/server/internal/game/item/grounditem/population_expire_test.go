/*
===========================================================================

population_expire_test.go - a population's own lifetime and drop cap

Port-only, not native: ExpirePopulation retires a short-lived layer's
drops after their lifetime and keeps the layer under a cap, oldest
first, without touching another population or the main world.

===========================================================================
*/
package grounditem

import (
	"testing"
	"time"
)

/*
================
TestExpirePopulationLifetimeAndCap
================
*/
func TestExpirePopulationLifetimeAndCap(t *testing.T) {
	r := NewRegistry()
	layer, other := Population{0x1000a, 2}, Population{0x1000b, 2}
	start := time.UnixMilli(1_000_000)
	var gids []uint32
	for i := range 5 {
		gids = append(gids, r.Add("a", Item{Population: layer, DroppedAt: start.Add(time.Duration(i) * time.Second)}).Gid)
	}
	foreign := r.Add("a", Item{Population: other, DroppedAt: start})
	main := r.Add("a", Item{DroppedAt: start})
	revision := r.Revision()

	// Nothing old enough and under the cap: nothing moves.
	if got := r.ExpirePopulation("a", layer, start.Add(10*time.Second), time.Minute, 5); len(got) != 0 || r.Revision() != revision {
		t.Fatalf("expired %d under the lifetime and cap", len(got))
	}
	// The first two lived a minute; the cap of two then retires the
	// oldest of the three left.
	got := r.ExpirePopulation("a", layer, start.Add(61*time.Second), time.Minute, 2)
	if len(got) != 3 || got[0].Gid != gids[0] || got[1].Gid != gids[1] || got[2].Gid != gids[2] {
		t.Fatalf("expired %+v", got)
	}
	for i, gid := range gids {
		if _, ok := r.Get("a", gid); ok != (i >= 3) {
			t.Fatalf("drop %d present %v", i, ok)
		}
	}
	for _, gid := range []uint32{foreign.Gid, main.Gid} {
		if _, ok := r.Get("a", gid); !ok {
			t.Fatal("another population's drop expired")
		}
	}
	if r.Revision() == revision {
		t.Fatal("revision not bumped")
	}
	if got := r.ExpirePopulation("a", Population{}, start.Add(time.Hour), time.Minute, 0); len(got) != 0 {
		t.Fatal("the main world expired through the population policy")
	}
}
