package cloud

import (
	"slices"
	"time"
)

// NewestFirst returns a stable sorted copy. Missing timestamps stay last;
// equal or unknown times preserve the source order, including across paging.
func NewestFirst[T any](items []T, timestamp func(T) time.Time) []T {
	out := slices.Clone(items)
	slices.SortStableFunc(out, func(a, b T) int {
		ta, tb := timestamp(a), timestamp(b)
		if ta.IsZero() && !tb.IsZero() {
			return 1
		}
		if !ta.IsZero() && tb.IsZero() {
			return -1
		}
		return tb.Compare(ta)
	})
	return out
}
