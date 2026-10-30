package mastersweb

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// The wizard's shape on every step, by the framework's own check.
func TestModel_ValidOnEveryStep(t *testing.T) {
	for step := range steps {
		if err := model(step, &doctype.DocType{PluralLabel: "Parties"}).Validate(); err != nil {
			t.Errorf("step %d: %v", step, err)
		}
	}
}
