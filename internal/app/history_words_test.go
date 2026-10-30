package app

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// TestAuditAction_EveryActionInWords: DOC-089, CD-248. Every action the audit
// log holds for a document reads in words; deleting any word fails a row.
func TestAuditAction_EveryActionInWords(t *testing.T) {
	cause := &audit.Ref{DocType: "fixture.Receipt", DocID: 2, Number: "GR/26-27/00001", Action: "approve"}
	cases := []struct {
		entry audit.Entry
		shown bool
		want  string
	}{
		{audit.Entry{Action: "create"}, false, "Created"},
		{audit.Entry{Action: "save"}, false, "Saved"},
		{audit.Entry{Action: "submit"}, false, "Submitted"},
		{audit.Entry{Action: "withdraw"}, false, "Withdrawn"},
		{audit.Entry{Action: "approve"}, false, "Approved"},
		{audit.Entry{Action: "reject"}, false, "Rejected"},
		{audit.Entry{Action: "revise"}, false, "Revised"},
		{audit.Entry{Action: "close"}, false, "Closed"},
		{audit.Entry{Action: "cancel"}, false, "Cancelled"},
		{audit.Entry{Action: "delete"}, false, "Deleted"},
		{audit.Entry{Action: "amended"}, false, "Amended"},
		{audit.Entry{Action: "attachment_added"}, false, "Attachment added"},
		{audit.Entry{Action: "attachment_removed"}, false, "Attachment removed"},
		{audit.Entry{Action: "imported"}, false, "Imported"},
		{audit.Entry{Action: "effect"}, false, "Updated by a linked document"},
		{audit.Entry{Action: "effect", CausedBy: cause}, false, "Updated by a linked document"},
		{audit.Entry{Action: "effect", CausedBy: cause}, true, "Updated by GR/26-27/00001"},
	}
	for _, c := range cases {
		if got := auditAction(c.entry, c.shown); got != c.want {
			t.Errorf("auditAction(%q, cause %v, shown %t) = %q, want %q", c.entry.Action, c.entry.CausedBy != nil, c.shown, got, c.want)
		}
	}
	if len(auditActions) != len(cases)-2 {
		t.Errorf("auditActions has %d words, the table covers %d: add a row for each", len(auditActions), len(cases)-2)
	}
}

// TestChangeText_UnparsableValueShownAsStored: an entry older than its field's
// type shows its stored text, not a zero value (CD-248).
func TestChangeText_UnparsableValueShownAsStored(t *testing.T) {
	f := doctype.Field{Name: "order_date", Label: "Order date", Type: doctype.Date}
	if got := changeText(t.Context(), f, `"next week"`); got != "next week" {
		t.Errorf("changeText = %q, want %q", got, "next week")
	}
}
