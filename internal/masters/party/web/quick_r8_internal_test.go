package partyweb

import (
	"strings"
	"testing"
)

// stateEcho is the live check's echo only for a valid GSTIN naming a state.
func TestStateEcho(t *testing.T) {
	const valid = "27AAFCV2449G1Z7"
	for _, c := range []struct {
		g    string
		want bool
	}{
		{"", false}, {"bad", false}, {valid, true}, {strings.ToLower(valid), true}, {"27AAFCV2449G1Z8", false},
	} {
		got := stateEcho(c.g)
		if (got != "") != c.want {
			t.Errorf("stateEcho(%q) = %q, want a value: %v", c.g, got, c.want)
		}
		if c.want && !strings.Contains(got, "Maharashtra (27)") {
			t.Errorf("stateEcho(%q) = %q, want it to name the state", c.g, got)
		}
	}
}
