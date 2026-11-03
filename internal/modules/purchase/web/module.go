package purchaseweb

import (
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// module carries Deps to each screen task's Module functions.
type module struct {
	d Deps
}

// orderKeyFacts are the PO header fields the document header shows (UI v2
// §5.13 item 3).
var orderKeyFacts = []string{"vendor_id", "site_id", "order_date", "required_by"}

// itemsNoun keeps docscreen's words but for the Items step with no lines.
var itemsNoun = docscreen.LineNoun{Empty: "No items yet."}

// purchaseHome is the success block's "Go to Purchase" (UI v2 §5.13 item 2).
var purchaseHome = components.Link{Label: "Purchase", Href: homePath}

// Modules is what purchase adds to the generic document screens, by DocType,
// for internal/app's docscreen.Deps.Modules. Each field calls a function in
// its screen task's module_<task>.go (CD-256), which keeps docscreen's own
// behaviour until that task builds it.
func Modules(d Deps) map[string]docscreen.Module {
	m := &module{d: d}
	return map[string]docscreen.Module{
		purchase.DocType: {
			StepIntros: map[string]string{ // UI v2 C2
				"details": "Who you're buying from, where it goes and when you need it.",
				"terms":   "Payment terms, the vendor's quote and anything else that prints on the order.",
				"items":   "Add what you're buying, with the quantity and rate for each line.",
				"check":   "Check the order, then submit it for approval.",
			},
			StepIcons: map[string]string{"details": "handshake", "terms": "calendar-clock"}, // UI v2 C4c
			Steps: map[string]docscreen.Step{
				"details": withRejection(detailsStep()),      // R13
				"terms":   withRejection(m.orderTermsStep()), // P15a
				"items":   withRejection(m.orderItemsStep()), // P15c
			},
			LineNoun:    itemsNoun,                        // UX §5.6
			Hide:        m.orderHide,                      // P15a
			Check:       m.orderCheck,                     // P15a
			LineCard:    m.orderLineCard,                  // P15a
			LineGrid:    m.orderLineGrid,                  // P15a
			Page:        m.orderPageHook,                  // P15b
			Reason:      m.orderReason,                    // P15b
			KeyFacts:    orderKeyFacts,                    // UI v2 E5
			LifeSteps:   []string{lifeSent, lifeReceived}, // UI v2 E5b
			Home:        purchaseHome,                     // UI v2 E5b
			FooterTotal: "net_total",                      // UI v2 C3c (A-22): "Taxable value", the field's label
			TodayDates:  []string{"order_date"},           // PO spec §3.2

			SelfFill: []string{"project_id", "uom_id", "tax_code_id"}, // resolved when blank: no star (CD-387 (2))
		},
		purchase.RequestDocType: {
			StepIntros: map[string]string{ // UI v2 C2
				"details": "Where the items are needed and what they're for.",
				"items":   "Add what you need, with the quantity for each line.",
				"check":   "Check the request, then submit it.",
			},
			Steps:     map[string]docscreen.Step{"details": detailsStep()}, // R13
			LineNoun:  itemsNoun,                                           // UX §5.6
			CardsOnly: requestCardsOnly,                                    // P15d
			Page:      m.requestPage,                                       // P15d
			Reason:    m.requestReason,                                     // P15d
			Hide:      m.requestHide,                                       // P15d
			Check:     m.requestCheck,                                      // P15d
			LineCard:  m.requestLineCard,                                   // P15d
			Home:      purchaseHome,                                        // UI v2 E5b

			StepIcons: map[string]string{"details": "clipboard-list"}, // UI v2 C4c
			SelfFill:  []string{"project_id", "uom_id"},               // resolved when blank: no star (CD-387 (2))
		},
	}
}
