package expenseweb

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// The expense line card (UX §5.4, UX-E3) and the check page additions
// (UX §5.6). The card is also the one on the claim document and approval
// pages (design-system §8.5: receipts beside each line).

const (
	noReceiptYet = "No receipt yet"
	noReceipt    = "No receipt: "
	lessLabel    = "Less advance"
	minus        = "−" // U+2212, as the UX doc writes the figure
	billedTo     = " · Billed to "
	unusedTitle  = "Photos not on any expense"
	unusedOne    = "1 photo isn't on any expense. Remove it below, or use it on an expense."
	unusedMany   = "%d photos aren't on any expense. Remove them below, or use them on an expense."
)

// claimLineCard is an expense line's card: the type, the date (and its
// project or site code), the amount, GST on a company bill, the note, and
// the receipt's thumbnail or why there is none.
func claimLineCard(d Deps) func(docscreen.Ctx, docscreen.LineView) components.LineCard {
	return func(c docscreen.Ctx, l docscreen.LineView) components.LineCard {
		card := l.Card // Item is the type's name, Amount the amount
		card.Code = ""
		card.QtyRate = l.Display["expense_date"]
		if tag, ok := tagCode(c, l); ok {
			card.QtyRate += " · " + tag
		}
		if (l.Inputs["company_bill"] == "1" || l.Inputs["company_bill"] == "true") && l.Display["gst_amount"] != "" {
			card.Tax = "GST " + l.Display["gst_amount"]
			if g := billedGSTIN(c, l); g != "" {
				card.Tax += billedTo + g
			}
		}
		card.Note = l.Display["description"]
		switch hash := l.Inputs["receipt_sha256"]; {
		case hash != "":
			card.Receipt = d.receiptOf(c, hash)
		case l.Inputs["no_receipt_reason"] != "":
			card.ReceiptNote = noReceipt + l.Inputs["no_receipt_reason"]
		default:
			card.ReceiptNote, card.ReceiptWarn = noReceiptYet, true
		}
		return card
	}
}

// tagCode is the line's project code, else its site code.
func tagCode(c docscreen.Ctx, l docscreen.LineView) (string, bool) {
	for _, t := range []struct{ field, dt string }{{"project_id", project.DocType}, {"site_id", site.DocType}} {
		id, err := strconv.ParseInt(l.Inputs[t.field], 10, 64)
		if err != nil {
			continue
		}
		if code := masterCode(c, t.dt, id); code != "" {
			return code, true
		}
	}
	return "", false
}

// masterCode is master dt's code for id, "" when it can't be read. A card
// can't fail: a failed read leaves the code out.
func masterCode(c docscreen.Ctx, dt string, id int64) string {
	typ, _ := doctype.Lookup(dt) // project and site are registered masters
	var code string
	_ = c.Deps.DB.Read(c.Req.Context(), func(tx *db.Tx) error {
		mi, _, err := typ.Master.Lookup(c.Req.Context(), tx, id)
		code = mi.Code
		return err
	})
	return code
}

// billedGSTIN is line l's snapshotted Billed-to GSTIN while the company has
// more than one branch GSTIN (CD-271 G4, EUX-045), else "". A card can't
// fail: a failed read leaves it out.
func billedGSTIN(c docscreen.Ctx, l docscreen.LineView) string {
	id, _ := strconv.ParseInt(l.ID, 10, 64) // a stored line's id; 0 reads no row
	ctx := c.Req.Context()
	var g string
	_ = c.Deps.DB.Read(ctx, func(tx *db.Tx) error {
		n, err := expense.BranchGSTINs(ctx, tx)
		if err != nil || n < 2 {
			return err
		}
		g, err = expense.BilledGSTIN(ctx, tx, id)
		return err
	})
	return g
}

// receiptOf is the receipt holding hash among the claim's own files (ListTx),
// nil when there is none or the read fails.
func (d Deps) receiptOf(c docscreen.Ctx, hash string) *components.Attachment {
	ctx := c.Req.Context()
	var metas []files.Meta
	_ = c.Deps.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		metas, err = d.Files.ListTx(ctx, tx, auth.RecordRef{DocType: expense.ClaimDocType, ID: c.View.Info.ID})
		return err
	})
	return ownReceipt(metas, hash)
}

// ownReceipt is the file among metas holding hash, nil when none does: a
// card never links another document's file.
func ownReceipt(metas []files.Meta, hash string) *components.Attachment {
	for _, m := range metas {
		if m.SHA256 == hash {
			a := attachmentOf(m)
			return &a
		}
	}
	return nil
}

// attachmentOf is file m as components show it.
func attachmentOf(m files.Meta) components.Attachment {
	return components.Attachment{
		ID: m.ID, Name: m.Name, Size: files.HumanSize(m.Size),
		URL: "/files/" + strconv.FormatInt(m.ID, 10), Kind: attachmentKind(m.MIME),
	}
}

// unusedPhotos are the claim's own files whose hash no line holds
// (abandoned uploads and replaced photos, UX §5.6), each with FILE's Remove.
func (d Deps) unusedPhotos(ctx context.Context, c docscreen.Ctx) ([]components.Attachment, error) {
	used := map[string]bool{}
	for _, l := range c.View.Lines {
		used[l.Inputs["receipt_sha256"]] = true
	}
	var out []components.Attachment
	err := c.Deps.DB.Read(ctx, func(tx *db.Tx) error {
		metas, err := d.Files.ListTx(ctx, tx, auth.RecordRef{DocType: expense.ClaimDocType, ID: c.View.Info.ID})
		if err != nil {
			return err
		}
		for _, m := range metas {
			if used[m.SHA256] {
				continue
			}
			u, _, err := d.Auth.User(ctx, tx, m.UploadedBy)
			if err != nil {
				return err
			}
			a := attachmentOf(m)
			a.UploadedBy, a.UploadedAt = u.Name, types.FormatIST(m.UploadedAt)
			a.RemoveURL = "/files/" + strconv.FormatInt(m.ID, 10) + "/remove"
			out = append(out, a)
		}
		return nil
	})
	return out, err
}

// attachmentKind is how components show a file: browsers can't show HEIC,
// so it is a download chip; anything else not an image is a file chip
// (FILE-033).
func attachmentKind(mime string) string {
	switch {
	case mime == "image/heic", mime == "image/heif":
		return "heic"
	case strings.HasPrefix(mime, "image/"):
		return "image"
	}
	return "other"
}

// totalsFields are the fields the check page's totals show or reveal: none
// is drawn when the registered Hide hides one of them (CD-319).
var totalsFields = []string{"advance_id", "total", "less_advance", "net_payable"}

// claimCheck adds the claim's warnings (ClaimWarnings, which names another
// claim only when the viewer can read it), the photos not on any expense
// with their warning and card, and, with an advance, the totals Total, Less
// advance and To pay after the grand figure (UX §5.6, UX-E6).
func claimCheck(d Deps) func(docscreen.Ctx) (docscreen.CheckExtra, error) {
	return func(c docscreen.Ctx) (docscreen.CheckExtra, error) {
		ctx := c.Req.Context()
		ws, err := d.Service.ClaimWarnings(ctx, c.Principal, c.View.Info.ID)
		if err != nil {
			return docscreen.CheckExtra{}, err
		}
		ex := docscreen.CheckExtra{Warnings: ws}
		unused, err := d.unusedPhotos(ctx, c)
		if err != nil {
			return ex, err
		}
		switch n := len(unused); {
		case n == 1:
			ex.Warnings = append(ex.Warnings, unusedOne)
		case n > 1:
			ex.Warnings = append(ex.Warnings, fmt.Sprintf(unusedMany, n))
		}
		if len(unused) > 0 {
			ex.Cards = []templ.Component{unusedCard(unused)}
		}
		adv := c.View.Inputs["advance_id"]
		total, net := c.View.Inputs["total"], c.View.Inputs["net_payable"]
		if adv == "" || total == "" || net == "" || slices.ContainsFunc(totalsFields, func(n string) bool { return hides(c, n) }) {
			return ex, nil
		}
		t, err := types.ParseMoney(total)
		if err != nil {
			return ex, err
		}
		n, err := types.ParseMoney(net)
		if err != nil {
			return ex, err
		}
		label := lessLabel
		if id, err := strconv.ParseInt(adv, 10, 64); err == nil {
			info, ok, err := d.readableAdvance(ctx, c, id)
			if err != nil {
				return ex, err
			}
			if ok {
				label += " (" + info.Display + ")"
			}
		}
		ex.Totals = []components.Pair{
			{Label: "Total", Value: types.FormatINR(t), Num: true},
			{Label: label, Value: minus + types.FormatINR(t-n), Num: true},
			{Label: "To pay", Value: types.FormatINR(n), Num: true},
		}
		return ex, nil
	}
}
