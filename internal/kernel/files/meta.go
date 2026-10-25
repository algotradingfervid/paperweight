package files

import (
	"fmt"

	"github.com/vardhan/internaltools/internal/types"
)

// HumanSize shows n bytes as "512 B", "512 KB" (whole KB) or "2.3 MB" (one
// decimal), rounding half up.
func HumanSize(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%d KB", (n+1<<9)/(1<<10))
	}
	tenths := (n*10 + 1<<19) / (1 << 20)
	return fmt.Sprintf("%d.%d MB", tenths/10, tenths%10)
}

// ListEntry is an attachment's line in a document's list:
// "receipt.jpg · 2.3 MB · Ravi Kulkarni · 24-10-2026 10:30".
func ListEntry(m Meta, uploader string) string {
	return m.Name + " · " + HumanSize(m.Size) + " · " + uploader + " · " + types.FormatIST(m.UploadedAt)
}
