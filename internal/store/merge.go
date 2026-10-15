package store

import (
	"reflect"
	"time"

	"github.com/jinghaihan/sift/internal/review"
)

// Merge only the changes made since this writer's last submitted snapshot.
// Foreign additions omitted from the GUI's snapshot are not deletions.
func mergeRecord(base, incoming, latest Record) (Record, error) {
	if incoming.Scope != latest.Scope {
		return latest, ErrConcurrentWrite
	}
	if latest.Tip != base.Tip && incoming.Tip != latest.Tip && !equalRecord(base, incoming) {
		return latest, ErrConcurrentWrite
	}
	result := latest.Clone()
	if incoming.Tip != base.Tip {
		if latest.Tip != base.Tip && latest.Tip != incoming.Tip {
			return latest, ErrConcurrentWrite
		}
		result.Tip = incoming.Tip
	}
	var err error
	result.Comments, err = mergeItems(base.Comments, incoming.Comments, latest.Comments, func(c Comment) string { return c.ID })
	if err != nil {
		return latest, err
	}
	result.Provenance, err = mergeItems(base.Provenance, incoming.Provenance, latest.Provenance, func(c review.CommitProvenance) string { return c.ID })
	if err != nil {
		return latest, err
	}
	result.Script, err = mergeScript(base.Script, incoming.Script, latest.Script)
	if err != nil {
		return latest, err
	}
	for key, value := range incoming.Viewed {
		if value == base.Viewed[key] {
			continue
		}
		if latest.Viewed[key] != base.Viewed[key] && latest.Viewed[key] != value {
			return latest, ErrConcurrentWrite
		}
		if result.Viewed == nil {
			result.Viewed = map[string]string{}
		}
		result.Viewed[key] = value
	}
	for key, value := range base.Viewed {
		if _, exists := incoming.Viewed[key]; !exists {
			if latest.Viewed[key] != value && latest.Viewed[key] != "" {
				return latest, ErrConcurrentWrite
			}
			delete(result.Viewed, key)
		}
	}
	return result, nil
}

func equalRecord(a, b Record) bool {
	a.Sequence, b.Sequence = 0, 0
	return reflect.DeepEqual(a, b)
}

func mergeItems[T any](base, incoming, latest []T, id func(T) string) ([]T, error) {
	before, wanted, current := map[string]T{}, map[string]T{}, map[string]T{}
	for _, item := range base {
		before[id(item)] = item
	}
	for _, item := range incoming {
		wanted[id(item)] = item
	}
	for _, item := range latest {
		current[id(item)] = item
	}
	for key, old := range before {
		next, exists := wanted[key]
		if exists && sameItem(old, next) {
			continue
		}
		disk, found := current[key]
		if found && !sameItem(disk, old) && (!exists || !sameItem(disk, next)) {
			return nil, ErrConcurrentWrite
		}
		if !found && exists {
			return nil, ErrConcurrentWrite
		}
		if exists {
			current[key] = next
		} else {
			delete(current, key)
		}
	}
	for key, next := range wanted {
		if _, existed := before[key]; existed {
			continue
		}
		if disk, exists := current[key]; exists && !sameItem(disk, next) {
			return nil, ErrConcurrentWrite
		}
		current[key] = next
	}
	var result []T
	seen := map[string]bool{}
	for _, items := range [][]T{latest, incoming} {
		for _, item := range items {
			key := id(item)
			if next, exists := current[key]; exists && !seen[key] {
				result = append(result, next)
				seen[key] = true
			}
		}
	}
	return result, nil
}

func sameItem[T any](left, right T) bool {
	if a, ok := any(left).(Comment); ok {
		b := any(right).(Comment)
		// JSON removes monotonic readings and may reconstruct a different
		// timezone location. Those differences are not concurrent edits.
		if !a.Created.Equal(b.Created) {
			return false
		}
		a.Created, b.Created = time.Time{}, time.Time{}
		return a == b
	}
	return reflect.DeepEqual(left, right)
}
