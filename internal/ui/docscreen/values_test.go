package docscreen_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

func TestListQuery_RoundTrip(t *testing.T) {
	for _, raw := range []string{
		"q=steel&sort=amount.desc&f=state:=:approved&page=2",
		"f=remarks:=:a:b&f=state:=:approved&f=state:=:draft&size=50",
		"",
	} {
		v, err := url.ParseQuery(raw)
		if err != nil {
			t.Fatal(err)
		}
		q := docscreen.ParseListQuery(v)
		if got := q.Encode(); got != v.Encode() {
			t.Errorf("Encode(Parse(%q)) = %q, want %q", raw, got, v.Encode())
		}
	}

	v, _ := url.ParseQuery("q=steel&sort=amount.desc&f=state:=:approved&f=remarks:=:a:b&f=state:=:draft&page=2")
	got := docscreen.ParseListQuery(v)
	want := docscreen.ListQuery{
		Search: "steel",
		Sort:   "amount.desc",
		Filters: []docscreen.Filter{
			{Field: "state", Op: "=", Values: []string{"approved", "draft"}},
			{Field: "remarks", Op: "=", Values: []string{"a:b"}},
		},
		Page: 2,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseListQuery = %+v, want %+v", got, want)
	}
}

func TestValues_Display(t *testing.T) {
	docscreentest.RegisterSample()
	docscreentest.SetMaster(docscreentest.Item, 7, doctype.MasterInfo{Code: "CEMENT-OPC", Name: "OPC Cement 53 Grade"})
	eng := docfake.New()
	eng.SetInfo(doctype.Info{DocType: docscreentest.Sample, ID: 42, State: doctype.StateDraft, Display: "Draft 42"})

	for _, c := range []struct {
		f           doctype.Field
		plain, want string
	}{
		{doctype.Field{Name: "amount", Type: doctype.Money}, "94164.00", "₹94,164.00"},
		{doctype.Field{Name: "rate", Type: doctype.Rate}, "62.5000", "₹62.50"},
		{doctype.Field{Name: "order_date", Type: doctype.Date}, "2026-10-24", "24-10-2026"},
		{doctype.Field{Name: "amount", Type: doctype.Money}, "", ""},
		{doctype.Field{Name: "remarks", Type: doctype.Text}, "<b>as typed</b>", "<b>as typed</b>"},
		{doctype.Field{Name: "item_id", Type: doctype.Link, Link: docscreentest.Item}, "7", "CEMENT-OPC OPC Cement 53 Grade"},
		{doctype.Field{Name: "source_id", Type: doctype.Link, Link: docscreentest.Sample}, "42", "Draft 42"},
	} {
		got, err := docscreen.Display(context.Background(), nil, eng, c.f, c.plain)
		if err != nil {
			t.Errorf("Display(%s, %q): %v", c.f.Name, c.plain, err)
			continue
		}
		if got != c.want {
			t.Errorf("Display(%s, %q) = %q, want %q", c.f.Name, c.plain, got, c.want)
		}
	}
}

func TestValues_ToPlain(t *testing.T) {
	money := doctype.Field{Name: "amount", Type: doctype.Money}
	date := doctype.Field{Name: "order_date", Type: doctype.Date}

	if got, fe := docscreen.ToPlain(money, "14,11,200.00"); fe != nil || got != "1411200.00" {
		t.Errorf("ToPlain(money) = %q, %v; want 1411200.00", got, fe)
	}
	if got, fe := docscreen.ToPlain(date, "27-03-2026"); fe != nil || got != "2026-03-27" {
		t.Errorf("ToPlain(date) = %q, %v; want 2026-03-27", got, fe)
	}
	text := doctype.Field{Name: "remarks", Type: doctype.Text}
	if got, fe := docscreen.ToPlain(text, " as typed "); fe != nil || got != " as typed " {
		t.Errorf("ToPlain(text) = %q, %v; want it unchanged", got, fe)
	}

	_, perr := types.ParseMoney("1.234,56")
	if perr == nil {
		t.Fatal("ParseMoney accepted 1.234,56")
	}
	_, fe := docscreen.ToPlain(money, "1.234,56")
	if fe == nil || fe.Field != "amount" || fe.Message != perr.Error() {
		t.Errorf("ToPlain(1.234,56) error = %+v, want field amount with %q", fe, perr.Error())
	}
}

type statusErr struct {
	status int
	msg    string
}

func (e statusErr) Error() string                     { return e.msg }
func (e statusErr) Status() int                       { return e.status }
func (e statusErr) FieldErrors() []doctype.FieldError { return nil }

func TestClassify_HTTPErrorAndNotFound(t *testing.T) {
	stale := statusErr{409, "Someone else changed this document."}
	p, ok := docscreen.Classify(fmt.Errorf("save: %w", stale))
	if !ok || p.Status != 409 || p.Message != "save: "+stale.msg {
		t.Errorf("Classify(409) = %+v, %v", p, ok)
	}

	p, ok = docscreen.Classify(doctype.ErrNotFound)
	if !ok || p.Status != 404 || p.Message != doctype.ErrNotFound.Error() {
		t.Errorf("Classify(ErrNotFound) = %+v, %v", p, ok)
	}

	if p, ok := docscreen.Classify(db.ErrBusy); ok {
		t.Errorf("Classify(ErrBusy) = %+v, want not classified", p)
	}
	if _, ok := docscreen.Classify(errors.New("boom")); ok {
		t.Error("Classify(plain error) classified it")
	}

	e := newEnv(t, "read")
	e.lister.Fail(db.ErrBusy)
	rec := e.get(t, "/doc/docscreentest.Sample")
	if rec.Code != 503 {
		t.Errorf("busy list status = %d, want 503 from web.ErrorFrom", rec.Code)
	}
}
