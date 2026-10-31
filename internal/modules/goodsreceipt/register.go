package goodsreceipt

import (
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// DocType is the Goods Receipt's name (GRN spec §3).
const DocType = "goodsreceipt.Receipt"

func init() {
	doctype.Register(doctype.DocType{
		Name: DocType, Label: "Goods Receipt", PluralLabel: "Goods Receipts",
		Table: "goods_receipt", LinesTable: "goods_receipt_line", Series: "GR/{FY}/{#####}",
		Submittable: true, Approvable: true, Printable: true,
		Attachable:    true, // challan and invoice photos (CD-243)
		OwnerField:    "created_by",
		ProjectFields: []string{"project_id"},
		SiteFields:    []string{"site_id"},
		DefaultRights: defaultRights(),
		CloseRight:    doctype.CloseNone, // nobody closes a GRN (spec §5, CD-277)
		Fields:        fields(),
		// The header link makes CD-70 run CheckLinkTx on the PO (AUTH-101);
		// the line link gives DOC-017, DOC-038 and DOC-052 on the PO (spec §3).
		Links: []doctype.LinkSpec{
			{Field: "purchase_order_id", Target: purchase.DocType},
			{Field: "po_line_id", Target: purchase.DocType, OnLine: true},
		},
		RollUps: []doctype.RollUp{{TargetDocType: purchase.DocType, TargetField: "received_qty", SourceField: "accepted_qty", JoinField: "po_line_id"}},
		Hooks:   receiptHooks{},
	})
	for _, col := range []string{"received_qty", "rejected_qty"} {
		uom.RegisterQtyUse(uom.QtyUse{DocType: DocType, LinesTable: "goods_receipt_line", ParentColumn: "parent_id", UoMColumn: "uom_id", QtyColumn: col})
	}
}
