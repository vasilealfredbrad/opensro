package grounditem

import (
	"sort"
	"time"
)

// Population is a ground object's owning world lifetime. The zero value is
// the persistent main-world population used by pre-instance ground records.
// Other populations require both the packed world ID and its live generation.
type Population struct {
	World      uint32
	Generation uint64
}

func (p Population) Valid() bool {
	return p == (Population{}) || p.World&0xffff != 0 && p.World>>16 != 0 && p.Generation != 0
}

func (r *Registry) GetInPopulation(division string, population Population, gid uint32) (Item, bool) {
	item, ok := r.Get(division, gid)
	if !ok || !population.Valid() || item.Population != population {
		return Item{}, false
	}
	return item, true
}

func (r *Registry) AllInPopulation(division string, population Population) []Item {
	var out []Item
	if !population.Valid() {
		return out
	}
	for _, item := range r.All(division) {
		if item.Population == population {
			out = append(out, item)
		}
	}
	return out
}

// RemovePopulation retires only this allocation's objects. A recycled packed
// world ID cannot remove its successor's ground items.
func (r *Registry) RemovePopulation(division string, population Population) []Item {
	r.mu.Lock()
	defer r.mu.Unlock()
	var removed []Item
	if !population.Valid() {
		return removed
	}
	for gid, item := range r.byDivision[division] {
		if item.Population == population {
			removed = append(removed, item)
			delete(r.byDivision[division], gid)
		}
	}
	if len(removed) != 0 {
		r.revision++
	}
	sort.Slice(removed, func(i, j int) bool { return removed[i].Gid < removed[j].Gid })
	return removed
}

/*
================
ExpirePopulation

Port-only, not native: a short-lived population's own ground policy (the
battle royale layer, battleroyale/economy.go). Removes and returns this
allocation's drops that lay lifetime or longer, then, while more than
limit remain, the oldest of the rest (by drop time, then gid), so a
busy layer never holds more than limit drops. Drops without a timestamp
never expire but count toward the limit. The caller emits one 0x36AB
despawn per returned entry, as the fixture sweep does; results are in
gid order.
================
*/
func (r *Registry) ExpirePopulation(division string, population Population, now time.Time, lifetime time.Duration, limit int) []Item {
	r.mu.Lock()
	defer r.mu.Unlock()
	var removed, kept []Item
	if !population.Valid() || population == (Population{}) {
		return removed
	}
	items := r.byDivision[division]
	for gid, item := range items {
		if item.Population != population {
			continue
		}
		if !item.DroppedAt.IsZero() && now.Sub(item.DroppedAt) >= lifetime {
			removed = append(removed, item)
			delete(items, gid)
			continue
		}
		kept = append(kept, item)
	}
	if over := len(kept) - max(limit, 0); over > 0 {
		sort.Slice(kept, func(i, j int) bool {
			if !kept[i].DroppedAt.Equal(kept[j].DroppedAt) {
				return kept[i].DroppedAt.Before(kept[j].DroppedAt)
			}
			return kept[i].Gid < kept[j].Gid
		})
		for _, item := range kept[:over] {
			removed = append(removed, item)
			delete(items, item.Gid)
		}
	}
	if len(removed) != 0 {
		r.revision++
	}
	sort.Slice(removed, func(i, j int) bool { return removed[i].Gid < removed[j].Gid })
	return removed
}
