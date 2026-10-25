package approvaltest

import "github.com/vardhan/internaltools/internal/platform/clock"

// Clock is the harness's fixed clock (testkit.Clock: 2026-10-24T05:00:00.000Z);
// tests move it with Set.
type Clock = clock.Fixed
