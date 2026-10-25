package list

import (
	"fmt"
	"math"
	"slices"
)

// PageSizes are the page sizes a list offers; 0 means the first.
var PageSizes = []int{25, 50, 100, 500}

// NoMoreResults is shown for a page past the end.
const NoMoreResults = "No more results."

// ShowingText is the list footer, "Showing 26–50 of 61". It is only for a
// page holding rows: callers show NoMoreResults instead when total is 0 or
// the page is past the end.
func ShowingText(page, size, total int) string {
	return fmt.Sprintf("Showing %d–%d of %d", (page-1)*size+1, min(page*size, total), total)
}

// pageBounds checks q's page and size and returns LIMIT and OFFSET.
func pageBounds(q Query) (limit, offset int, err error) {
	size := q.PageSize
	if size == 0 {
		size = PageSizes[0]
	}
	if !slices.Contains(PageSizes, size) {
		return 0, 0, badQuery("Page size must be 25, 50, 100 or 500.")
	}
	if q.Page < 1 {
		return 0, 0, badQuery("Page must be 1 or more.")
	}
	if q.Page-1 > math.MaxInt/size {
		return 0, 0, fmt.Errorf("list: page %d is out of range", q.Page)
	}
	return size, (q.Page - 1) * size, nil
}
