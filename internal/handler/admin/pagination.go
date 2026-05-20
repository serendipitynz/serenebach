package admin

import "strconv"

// listPagination turns the raw ?page= query value and the total row
// count into a clamped (page, totalPages, offset) triple. Bad input
// (non-numeric, < 1, past the last page) silently clamps so a stale
// bookmark renders the closest valid page instead of 500-ing.
func listPagination(rawPage string, total int64, pageSize int) (page, totalPages, offset int) {
	if pageSize <= 0 {
		pageSize = 1
	}
	page = 1
	if rawPage != "" {
		if v, err := strconv.Atoi(rawPage); err == nil && v > 0 {
			page = v
		}
	}
	totalPages = int((total + int64(pageSize) - 1) / int64(pageSize))
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	offset = (page - 1) * pageSize
	return page, totalPages, offset
}

// pagerNeighbours computes the pager's prev/next link targets. Zero
// means "no link in this direction" — the template renders the arrow
// as disabled.
func pagerNeighbours(page, totalPages int) (prev, next int) {
	if page > 1 {
		prev = page - 1
	}
	if page < totalPages {
		next = page + 1
	}
	return prev, next
}
