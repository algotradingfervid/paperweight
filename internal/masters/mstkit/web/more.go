package mstweb

import (
	"context"
	"io"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// moreInput is a Field.More field's input, which grouped puts in the form's
// MoreDetails (UI v2 §5.5 item 3). It renders as its input where nothing
// groups it: a row's cell. Only inputs a form draws can be one, so a field
// the form leaves out (Disabled) is never inside the group, named in its
// summary or opening it.
type moreInput struct {
	label string
	open  bool // a value or an error
	in    templ.Component
}

func (m moreInput) Render(ctx context.Context, w io.Writer) error {
	return m.in.Render(ctx, w)
}

// asMore is in as fd's moreInput, shown with value and errMsg.
func asMore(fd doctype.Field, value, errMsg string, in templ.Component) moreInput {
	label := fd.Label
	if fd.Invert != "" {
		label = fd.Invert
	}
	return moreInput{label: label, open: errMsg != "" || filled(fd, value), in: in}
}

// filled reports whether value holds something: a master's Check posts and
// stores "1" or "0", and "0" holds nothing.
func filled(fd doctype.Field, value string) bool {
	if fd.Type == doctype.Check {
		return value == "1"
	}
	return value != ""
}

// grouped is inputs with the moreInputs moved, in order, into one MoreDetails
// at the end. Without one, or when every input is one (a section of only
// More fields, which the user chose to change), inputs are as they are.
func grouped(inputs []templ.Component) []templ.Component {
	var out []templ.Component
	var box moreBox
	for _, in := range inputs {
		m, ok := in.(moreInput)
		if !ok {
			out = append(out, in)
			continue
		}
		box.labels = append(box.labels, m.label)
		box.inputs = append(box.inputs, m.in)
		box.open = box.open || m.open
	}
	if len(box.inputs) == 0 || len(out) == 0 {
		return inputs
	}
	return append(out, moreGroup(box))
}

// moreBox is the inputs of one MoreDetails, their labels and whether it opens.
type moreBox struct {
	labels []string
	inputs []templ.Component
	open   bool
}
