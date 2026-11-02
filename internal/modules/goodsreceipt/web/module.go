package grnweb

import (
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// module carries Deps to each screen task's Module functions.
type module struct {
	d     Deps
	facts factsCache // S1: one Facts read per request
	lines lineCache  // S3c: one Lines read per request
}

// Modules is what the GRN adds to the generic document screens, for
// internal/app's docscreen.Deps.Modules. Each field calls a function in its
// screen task's module_<task>.go (CD-256), which keeps docscreen's own
// behaviour until that task builds it. The items slug is "items" (CD-256 (8)).
func Modules(d Deps) map[string]docscreen.Module {
	m := &module{d: d}
	return map[string]docscreen.Module{
		goodsreceipt.DocType: {
			StepLabels: m.stepLabels(), // S1
			StepIntros: map[string]string{ // UI v2 C2
				"details":  "Pick the purchase order these goods came against.",
				"delivery": "The challan or invoice that came with the goods, and how they came.",
				"items":    "Enter how much of each line arrived and how much you're rejecting.",
				"check":    "Check the receipt, then submit it.",
			},
			StepIcons: map[string]string{"details": "shopping-cart", "delivery": "truck", "items": "package-check"}, // UI v2 C4c
			Steps: map[string]docscreen.Step{
				"details":  m.detailsStep(),  // S1
				"delivery": m.deliveryStep(), // S1
				"items":    m.itemsStep(),    // S2
			},
			Hide:        m.hide,          // S1
			NoLinePages: m.noLinePages(), // S2
			Check:       m.check,         // S3
			Page:        m.page,          // S3
			Reason:      m.reason,        // S3
			LineCard:    m.lineCard,      // S3c
		},
	}
}
