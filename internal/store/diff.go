package store

import "sort"

// diffValues reports the changes between two datastore snapshots: one Change
// per path, ordered lexicographically by path. Paths present only in candidate
// are creates, paths present only in running are deletes, and paths present in
// both with different values are updates.
//
// Leaf values are comparable Go primitives (bool, int, uint32, float64,
// string), so the value comparison is a plain == and cannot panic.
func diffValues(running, candidate map[string]any) []Change {
	paths := make(map[string]struct{}, len(running)+len(candidate))
	for p := range running {
		paths[p] = struct{}{}
	}
	for p := range candidate {
		paths[p] = struct{}{}
	}

	ordered := make([]string, 0, len(paths))
	for p := range paths {
		ordered = append(ordered, p)
	}
	sort.Strings(ordered)

	var changes []Change
	for _, p := range ordered {
		old, inRunning := running[p]
		updated, inCandidate := candidate[p]
		switch {
		case inRunning && inCandidate:
			if old != updated {
				changes = append(changes, Change{Op: OpUpdate, Path: p, Old: old, New: updated})
			}
		case inCandidate:
			changes = append(changes, Change{Op: OpCreate, Path: p, New: updated})
		default:
			changes = append(changes, Change{Op: OpDelete, Path: p, Old: old})
		}
	}
	return changes
}
