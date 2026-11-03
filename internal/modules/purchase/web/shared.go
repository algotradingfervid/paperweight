package purchaseweb

import (
	"errors"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	modkitweb "github.com/vardhan/internaltools/internal/modules/modkit/web"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// Helpers more than one screen task uses (P15-0).

// Owner card texts (UX §5.8 item 8).
const (
	ownerCardTitle = "Owner"
	ownerCardText  = "You own this order. Only you can change or submit it."
	handOverLabel  = "Hand over"
)

// pairRaisedBy is the Raised by pair's label (UX §5.16).
const pairRaisedBy = "Raised by"

// metaSep joins the parts of a line.
const metaSep = " · "

// orderPage is PO id's module page at tail ("/send", "/handover", ...).
func orderPage(id int64, tail string) string {
	return "/modules/purchase/orders/" + strconv.FormatInt(id, 10) + tail
}

// allowed reports whether the viewer holds right r on dt (for rec), nil
// error meaning yes; a refusal is false with no error.
func allowed(c docscreen.Ctx, dt string, r auth.Right, rec *auth.RecordRef) (bool, error) {
	err := c.Deps.Auth.Can(c.Req.Context(), c.Principal, dt, r, rec)
	if errors.Is(err, auth.ErrForbidden) {
		return false, nil
	}
	return err == nil, err
}

// unitOf is line l's unit code, from its "{Code} {Name}" display (CD-68).
func unitOf(l docscreen.LineView) string {
	code, _, _ := strings.Cut(l.Display["uom_id"], " ")
	return code
}

// commentsCard is the record's Comments card, with a comment post refused
// without htmx back in the box (UX 5.10).
func (m *module) commentsCard(c docscreen.Ctx, dt string, id int64) (templ.Component, error) {
	ctx := c.Req.Context()
	comments, err := m.d.Comments.ListComments(ctx, c.Principal, dt, id)
	if err != nil {
		return nil, err
	}
	typed, msg := modkitweb.TakeCommentDraft(ctx, m.d.Sessions, dt, id)
	return modkitweb.CommentsPanelDraft(dt, id, comments, m.d.CSRF(c.Req), typed, msg), nil
}

// ownerName is user uid's name.
func ownerName(c docscreen.Ctx, uid types.UserID) (string, error) {
	var name string
	ctx := c.Req.Context()
	err := c.Deps.DB.Read(ctx, func(tx *db.Tx) error {
		u, _, err := c.Deps.Auth.User(ctx, tx, uid)
		name = u.Name
		return err
	})
	return name, err
}

// ownerCard is the Owner card with Hand over for PO poID, shown only to the
// document's owner: anyone else, an Administrator too, gets nil.
func ownerCard(c docscreen.Ctx, poID int64) templ.Component {
	if c.View == nil || c.Principal.ID != c.View.Info.CreatedBy {
		return nil
	}
	return ownerCardView("/modules/purchase/orders/" + strconv.FormatInt(poID, 10) + "/handover")
}

// readableLabel is record id of DocType dt's display label, ok false when the
// viewer may not read it (CD-248 family). It asks AUTH's CheckLinkTx rather
// than Deps.LinkRead: the app's LinkRead is the masters' check, which reads no
// project or site of a document, so it refuses a restricted owner her own PO.
func readableLabel(c docscreen.Ctx, dt string, id int64) (label string, ok bool, err error) {
	ctx := c.Req.Context()
	var refused *auth.LinkError
	err = c.Deps.DB.Read(ctx, func(tx *db.Tx) error {
		err := c.Deps.Auth.CheckLinkTx(ctx, tx, c.Principal, dt, id)
		if errors.As(err, &refused) || errors.Is(err, auth.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		info, err := c.Deps.Engine.Info(ctx, tx, dt, id)
		label, ok = info.Display, err == nil
		return err
	})
	return label, ok, err
}

// otherCount is the trailing line for k rows the viewer can't read,
// `{k} other {noun}s` (singular for one; CD-280 D6), and "" for none.
func otherCount(k int, noun string) string {
	if k < 1 {
		return ""
	}
	if k > 1 {
		noun += "s"
	}
	return strconv.Itoa(k) + " other " + noun
}

// joinAnd joins items as `a, b and c` (CD-258 Q12).
func joinAnd(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
