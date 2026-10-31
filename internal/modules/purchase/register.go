package purchase

import (
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/modules/modkit"
)

// DocType names (frozen for MOD-GRN, CD-276).
const (
	DocType        = "purchase.Order"
	RequestDocType = "purchase.Request"
)

func init() {
	doctype.Register(doctype.DocType{
		Name: DocType, Label: "Purchase Order", PluralLabel: "Purchase Orders",
		Table: "purchase_order", LinesTable: "purchase_order_line", Series: "PO/{FY}/{#####}",
		Submittable: true, Approvable: true, Printable: true, HasRoundedTotal: true,
		Attachable:    true, // CD-243, CD-258 (3)
		OwnerField:    "created_by",
		ProjectFields: []string{"project_id"},
		SiteFields:    []string{"site_id"},
		DefaultRights: orderRights(),
		CloseRight:    "amend", // any Purchase User closes an approved PO (CD-247)
		Fields:        orderFields(),
		SearchFields:  []string{"number", "remarks"}, // CD-317; the vendor snapshot isn't a Field
		// A PO line names the request row it orders; no RollUp, because
		// ordered quantities are the module's (§3.2, §20.3).
		Links: []doctype.LinkSpec{{Field: "request_line_id", Target: RequestDocType, OnLine: true}},
		Hooks: orderHooks{},
	})
	doctype.Register(doctype.DocType{
		Name: RequestDocType, Label: "Purchase Request", PluralLabel: "Purchase Requests",
		Table: "purchase_request", LinesTable: "purchase_request_line", Series: "PR/{FY}/{#####}",
		Submittable:   true,
		Attachable:    true, // CD-243
		OwnerField:    "created_by",
		ProjectFields: []string{"project_id"},
		SiteFields:    []string{"site_id"},
		DefaultRights: requestRights(),
		CloseRight:    "approve", // the requester, a Purchase Manager or a Director (CD-253, CD-255)
		StateLabels:   map[doctype.State]string{doctype.StateApproved: "Submitted"},
		Fields:        requestFields(),
		SearchFields:  []string{"number", "purpose"}, // CD-317; no requester snapshot column
		Hooks:         requestHooks{},
	})
	uom.RegisterQtyUse(uom.QtyUse{DocType: DocType, LinesTable: "purchase_order_line", ParentColumn: "parent_id", UoMColumn: "uom_id", QtyColumn: "qty"})
	uom.RegisterQtyUse(uom.QtyUse{DocType: RequestDocType, LinesTable: "purchase_request_line", ParentColumn: "parent_id", UoMColumn: "uom_id", QtyColumn: "qty"})
	modkit.RegisterComments(DocType)
	modkit.RegisterComments(RequestDocType)
}
