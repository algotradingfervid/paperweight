package types

import "testing"

// wantMsg fails the test unless err is non-nil and its text is exactly msg.
func wantMsg(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil || err.Error() != msg {
		t.Fatalf("err = %v, want %q", err, msg)
	}
}

// pow10 returns 10^n as an int64 (0 <= n <= 18).
func pow10(n int) int64 {
	p := int64(1)
	for range n {
		p *= 10
	}
	return p
}
