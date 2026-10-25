// Package components holds the design-system components (design-system §7).
// The shared prop types are declared here so the component tasks can run in
// parallel; components take display strings and never format.
package components

import "github.com/a-h/templ"

// Link is a labelled URL.
type Link struct{ Label, Href string }

// Option is one choice in a select, radio group or picker.
type Option struct{ Value, Label, Hint string }

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
// a live region that already has one.
type BannerProps struct {
	Kind           BannerKind
	Title, Message string
	Link           *Link
	NoRole         bool
}

// ActionBarProps describes the sticky action bar.
type ActionBarProps struct {
	Primary, Secondary *ButtonProps
	Note               string
	FinishLater        bool
	Form               string
}

// MoreAction is one entry under "More actions".
type MoreAction struct {
	Label, Href          string
	Post, Danger, NewTab bool
}

// Pair is one label and value in a summary.
type Pair struct {
	Label, Value string
	Num, Code    bool
}
