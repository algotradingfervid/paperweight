package testkit

import (
	"testing"
	"time"
)

// SetNoConnTimes sets NoSecondConn's deadline and hard cap for one test.
func SetNoConnTimes(t *testing.T, deadline, hardCap time.Duration) {
	t.Helper()
	oldDeadline, oldCap := noConnDeadline, noConnCap
	noConnDeadline, noConnCap = deadline, hardCap
	t.Cleanup(func() { noConnDeadline, noConnCap = oldDeadline, oldCap })
}
