package approval

import (
	"context"
	"errors"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// MsgNoApproval is the preview of a document no rule applies to.
const MsgNoApproval = "No approval needed. It will be approved and numbered when you submit."

// Preview is what a draft's check page says about approval (design-system
// §8.3): Needed when the submit would wait for an approver, Text the sentence.
type Preview struct {
	Needed bool
	Text   string
}

// Preview says what submitting the document at amount would need, from
// Evaluate, in the caller's tx. A submit no one could approve is not an error
// here: Text is APR's refusal, so the page still opens.
func (a *Approvals) Preview(ctx context.Context, tx *db.Tx, dt string, id int64, amount types.Money, requester auth.Principal) (Preview, error) {
	req, err := a.Evaluate(ctx, tx, dt, id, amount, requester)
	var refused *UserError
	if errors.As(err, &refused) {
		return Preview{Text: refused.Msg}, nil
	}
	if err != nil {
		return Preview{}, err
	}
	if !req.Needed {
		return Preview{Text: MsgNoApproval}, nil
	}
	in, err := a.info.Info(ctx, tx, dt, id)
	if err != nil {
		return Preview{}, err
	}
	who := withArticle(joinRoles(req.ApproverRoles))
	if req.ManagerUser != 0 {
		if who, err = a.userName(ctx, tx, req.ManagerUser); err != nil {
			return Preview{}, err
		}
	}
	text := "This " + strings.ToLower(in.DocLabel) + " needs approval from " + who
	if req.FromAmount > 0 {
		text += " because it is " + WhyText(req.FromAmount)
	}
	return Preview{Needed: true, Text: text + "."}, nil
}

// withArticle puts "a" or "an" before a role name.
func withArticle(role string) string {
	if strings.IndexAny(role, "AEIOU") == 0 {
		return "an " + role
	}
	return "a " + role
}
