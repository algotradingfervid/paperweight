package purchaseweb

import (
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// module carries Deps to each screen task's Module functions.
type module struct {
	d Deps
}

// Modules is what purchase adds to the generic document screens, by DocType,
// for internal/app's docscreen.Deps.Modules. Each field calls a function in
// its screen task's module_<task>.go (CD-256), which keeps docscreen's own
// behaviour until that task builds it.
func Modules(d Deps) map[string]docscreen.Module {
	m := &module{d: d}
	return map[string]docscreen.Module{
		purchase.DocType: {
			Steps: map[string]docscreen.Step{
				"terms": m.orderTermsStep(), // P15a
				"items": m.orderItemsStep(), // P15c
			},
			Hide:     m.orderHide,     // P15a
			Check:    m.orderCheck,    // P15a
			LineCard: m.orderLineCard, // P15a
			Page:     m.orderPage,     // P15b
			Reason:   m.orderReason,   // P15b
		},
		purchase.RequestDocType: {
			CardsOnly: requestCardsOnly,  // P15d
			Page:      m.requestPage,     // P15d
			Reason:    m.requestReason,   // P15d
			Hide:      m.requestHide,     // P15d
			Check:     m.requestCheck,    // P15d
			LineCard:  m.requestLineCard, // P15d
		},
	}
}
