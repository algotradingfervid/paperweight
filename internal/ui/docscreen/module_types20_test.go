package docscreen_test

import (
	"reflect"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// M0-20 (a): every type CD-275 adds for UX-E1 to UX-E7 (hooks spec §7 item
// 6), pinned as CD-275 states them. M0-20's first commit adds them all; the
// behaviour of LineNoun, AddTop and Line.Top is M0-20's (module_line20_test.go),
// the rest is M0-21's and M0-22's.

// UX-E2: Module.LineNoun LineNoun{Add, Title, Empty string}; Module.AddTop bool.
var (
	m20ModLineNoun docscreen.LineNoun = docscreen.Module{}.LineNoun
	m20ModAddTop   bool               = docscreen.Module{}.AddTop
	m20NounAdd     string             = docscreen.LineNoun{}.Add
	m20NounTitle   string             = docscreen.LineNoun{}.Title
	m20NounEmpty   string             = docscreen.LineNoun{}.Empty
)

// UX-E1: Line.Top func(c Ctx, no int, s StepState) (templ.Component, error).
var m20LineTop func(docscreen.Ctx, int, docscreen.StepState) (templ.Component, error) = docscreen.Line{}.Top

// UX-E7: PageExtra.Files *FilesCard; FilesCard{Title string; Items []components.Attachment}.
var (
	m20PageFiles  *docscreen.FilesCard    = docscreen.PageExtra{}.Files
	m20FilesTitle string                  = docscreen.FilesCard{}.Title
	m20FilesItems []components.Attachment = docscreen.FilesCard{}.Items
)

// UX-E3: LineCard.Receipt *Attachment, ReceiptNote string, ReceiptWarn bool.
var (
	m20CardReceipt     *components.Attachment = components.LineCard{}.Receipt
	m20CardReceiptNote string                 = components.LineCard{}.ReceiptNote
	m20CardReceiptWarn bool                   = components.LineCard{}.ReceiptWarn
)

// UX-E2: LineListProps.AddLabel string, AddTop bool, Empty string.
var (
	m20ListAddLabel string = components.LineListProps{}.AddLabel
	m20ListAddTop   bool   = components.LineListProps{}.AddTop
	m20ListEmpty    string = components.LineListProps{}.Empty
)

// UX-E5: UploadProps.AlsoDisable string.
var m20UploadAlsoDisable string = components.UploadProps{}.AlsoDisable

// TestM20_GapTypes pins the CD-275 additions: the vars above compile only
// when every name exists with the stated type; LineNoun and FilesCard have
// exactly the fields CD-275 lists.
func TestM20_GapTypes(t *testing.T) {
	_ = []any{
		m20ModLineNoun, m20ModAddTop, m20NounAdd, m20NounTitle, m20NounEmpty, m20LineTop,
		m20PageFiles, m20FilesTitle, m20FilesItems,
		m20CardReceipt, m20CardReceiptNote, m20CardReceiptWarn,
		m20ListAddLabel, m20ListAddTop, m20ListEmpty, m20UploadAlsoDisable,
	}
	for _, c := range []struct {
		typ  reflect.Type
		want []string
	}{
		{reflect.TypeFor[docscreen.LineNoun](), []string{"Add", "Title", "Empty"}},
		{reflect.TypeFor[docscreen.FilesCard](), []string{"Title", "Items"}},
	} {
		var got []string
		for i := range c.typ.NumField() {
			got = append(got, c.typ.Field(i).Name)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s has fields %q, want exactly %q (CD-275)", c.typ, got, c.want)
		}
	}
}
