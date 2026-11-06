package app

// Filtering precedes paging so matching records are never hidden on other pages.
func pagedItems[T any](a *App, key, scope string, items []T, match func(T) bool) ([]T, int) {
	filtered := items
	if match != nil {
		filtered = make([]T, 0, len(items))
		for _, item := range items {
			if match(item) {
				filtered = append(filtered, item)
			}
		}
	}
	start, end := a.pageRange(key, scope, len(filtered))
	return filtered[start:end], len(filtered)
}
