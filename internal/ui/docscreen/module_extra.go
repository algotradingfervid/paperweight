package docscreen

import (
	"errors"
	"slices"
	"strconv"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// closeRight is the right DocType name's close needs: its CloseRight, blank
// meaning submit (CD-247 (3), CD-255). ok is false when its CloseRight is
// doctype.CloseNone: it has no Close action and no close page (CD-277).
func closeRight(name string) (r auth.Right, ok bool) {
	dt, found := doctype.Lookup(name)
	switch {
	case !found || dt.CloseRight == "":
		return auth.RightSubmit, true
	case dt.CloseRight == doctype.CloseNone:
		return "", false
	}
	return auth.Right(dt.CloseRight), true
}

// checkExtra is the module's check page additions; none without a Check
// hook. It runs outside any transaction (hooks spec §5 rule 1).
func checkExtra(c Ctx) (CheckExtra, error) {
	if h := c.Deps.modOf(c.DT).Check; h != nil {
		return h(c)
	}
	return CheckExtra{}, nil
}

// pageExtra is the module's document page additions; none without a Page
// hook.
func pageExtra(c Ctx) (PageExtra, error) {
	if h := c.Deps.modOf(c.DT).Page; h != nil {
		return h(c)
	}
	return PageExtra{}, nil
}

// reasonExtra is the module's additions to action a's reason page; none
// without a Reason hook.
func reasonExtra(c Ctx, a doctype.Action) (ReasonExtra, error) {
	if h := c.Deps.modOf(c.DT).Reason; h != nil {
		return h(c, a)
	}
	return ReasonExtra{}, nil
}

// noteLines puts each line's note from notes on its card. lines is v.Lines
// after applyLineCards, a copy of its own.
func noteLines(lines []LineView, notes map[int]string) {
	for i := range lines {
		if n, ok := notes[lines[i].No]; ok {
			lines[i].Card.Note = n
		}
	}
}

// applyPageExtra puts pv.Extra's Notice, More and Primary on the page
// (CD-263 G3 to G5): a blank Notice Kind means Info; the module's More
// entries go before docscreen's; its Primary, which must have an Href,
// becomes the bar's primary only when the bar has no primary and no note.
func applyPageExtra(pv *pageView) error {
	x := pv.Extra
	if x.Notice != nil && x.Notice.Kind == "" {
		n := *x.Notice
		n.Kind = components.Info
		pv.Extra.Notice = &n
	}
	pv.More = slices.Concat(x.More, pv.More)
	if x.Primary == nil {
		return nil
	}
	if x.Primary.Href == "" {
		return errors.New("docscreen: PageExtra.Primary " + strconv.Quote(x.Primary.Label) + " has no Href")
	}
	if pv.Bar.Primary == nil && pv.Bar.Note == "" {
		b := *x.Primary
		b.Variant = components.Primary
		pv.Bar.Primary = &b
	}
	return nil
}
