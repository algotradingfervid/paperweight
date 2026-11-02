// Package components holds the design-system components (design-system §7).
// The shared prop types are declared here so the component tasks can run in
// parallel; components take display strings and never format.
package components

import "github.com/a-h/templ"

// Link is a labelled URL.
type Link struct{ Label, Href string }

// Option is one choice in a select, radio group or picker. Radios and
// Checkboxes show Lines as muted lines under the label and Hint, LineIcon
// (a name from the generated icons set) before the first of them, and Bar
// beside the last (UI v2 §5.6); other components ignore all three.
type Option struct {
	Value, Label, Hint string
	Lines              []string
	LineIcon           string
	Bar                *Meter
}

// Meter is a small bar filled Value of Max, drawn beside a line whose text
// says the same, so screen readers skip it. Max is above 0 and Value from 0
// to Max; drawing panics on anything else, or on a Bar (or a LineIcon)
// without Lines.
type Meter struct{ Value, Max int }

// FieldErr is a message for the field with the given ID.
type FieldErr struct{ ID, Message string }

// Variant is a button style.
type Variant string

// Button variants.
const (
	Primary   Variant = "primary"
	Secondary Variant = "secondary"
	Ghost     Variant = "ghost"
)

// Danger is both a Variant and a BannerKind: it is untyped, so it converts
// to either.
const Danger = "danger"

// ButtonProps describes a button or a link styled as one.
type ButtonProps struct {
	Label, AriaLabel, Busy, Icon, Href, Name, Value, Type string
	Variant                                               Variant
	Attrs                                                 templ.Attributes
}

// BannerKind is a banner's status.
type BannerKind string

// Banner kinds.
const (
	Success BannerKind = "success"
	Info    BannerKind = "info"
	Warning BannerKind = "warning"
)

// BannerProps describes a banner. NoRole drops its role, for a banner inside
// a live region that already has one; Icon replaces the kind's icon.
type BannerProps struct {
	Kind           BannerKind
	Title, Message string
	Icon           string
	Link           *Link
	NoRole         bool
}

// ActionBarProps describes the sticky action bar. Summary is an optional
// row above the buttons (a running total).
type ActionBarProps struct {
	Primary, Secondary *ButtonProps
	Note               string
	FinishLater        bool
	Form               string
	Summary            templ.Component
}

// BarTotal is an action bar summary (A-22): the count, then the label and
// value when Label is set. ID is the element a later out-of-band swap
// replaces.
type BarTotal struct {
	ID, Count, Label, Value string
}

// MoreAction is one entry under "More actions".
type MoreAction struct {
	Label, Href          string
	Post, Danger, NewTab bool
	Hidden               []Param // a POST form's hidden inputs
}

// Pair is one label and value in a summary.
type Pair struct {
	Label, Value string
	Num, Code    bool
}
