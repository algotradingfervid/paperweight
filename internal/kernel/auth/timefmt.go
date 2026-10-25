package auth

import "time"

// timeLayout is the stored timestamp format: fixed-width UTC (§4.3).
const timeLayout = "2006-01-02T15:04:05.000Z"

// FormatTime formats t for storage.
func FormatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

// ParseTime parses a stored timestamp.
func ParseTime(s string) (time.Time, error) { return time.Parse(timeLayout, s) }
