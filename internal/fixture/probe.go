package fixture

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// Probe records hook and service calls and injects faults into the fixture
// hooks. Fail, FailAfterWrite and Panic are keyed by hook name ("Validate",
// "ApplyEffects"). Set its fields before the action runs.
type Probe struct {
	mu                   sync.Mutex
	Calls                []string
	Fail, FailAfterWrite map[string]error
	Panic                map[string]bool
	ValidateErrors       []doctype.FieldError
	ForbidOverReceipt    bool
	Seen                 map[string]int64
}

type probeKey struct{}

// WithProbe returns ctx carrying p.
func WithProbe(ctx context.Context, p *Probe) context.Context {
	return context.WithValue(ctx, probeKey{}, p)
}

// ProbeFrom returns the probe in ctx, or nil. Every Probe method accepts nil.
func ProbeFrom(ctx context.Context) *Probe {
	p, _ := ctx.Value(probeKey{}).(*Probe)
	return p
}

// Add appends call to Calls.
func (p *Probe) Add(call string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Calls = append(p.Calls, call)
}

// Hooks returns the fixture hook calls without their DocType prefix, e.g.
// ["Validate", "BeforeSubmit"].
func (p *Probe) Hooks() []string {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, c := range p.Calls {
		for _, prefix := range []string{"Order.", "Receipt.", "Claim."} {
			if h, ok := strings.CutPrefix(c, prefix); ok {
				out = append(out, h)
				break
			}
		}
	}
	return out
}

func (p *Probe) fail(hook string) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.Fail[hook]
}

func (p *Probe) failAfterWrite(hook string) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.FailAfterWrite[hook]
}

func (p *Probe) panics(hook string) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.Panic[hook]
}

func (p *Probe) validateErrors() []doctype.FieldError {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.ValidateErrors)
}

func (p *Probe) forbidOverReceipt() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ForbidOverReceipt
}

func (p *Probe) see(key string, v int64) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Seen == nil {
		p.Seen = map[string]int64{}
	}
	p.Seen[key] = v
}
