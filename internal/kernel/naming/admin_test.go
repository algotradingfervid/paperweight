package naming

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// seriesTemplate reads the stored template for dt. Test goroutine only.
func seriesTemplate(t *testing.T, h *harness, dt string) string {
	t.Helper()
	ctx := context.Background()
	var s seriesRow
	if err := h.db.Read(ctx, func(tx *db.Tx) error {
		var err error
		s, err = loadSeries(ctx, tx.SQL(), dt)
		return err
	}); err != nil {
		t.Fatalf("loadSeries(%s): %v", dt, err)
	}
	return s.Template
}

// wantNamingError checks errors.Is(err, kind) and the exact user-facing text.
func wantNamingError(t *testing.T, call string, err, kind error, msg string) {
	t.Helper()
	if !errors.Is(err, kind) {
		t.Errorf("%s: err = %v, want errors.Is %v", call, err, kind)
		return
	}
	if err.Error() != msg {
		t.Errorf("%s: message = %q, want %q", call, err.Error(), msg)
	}
}

// TestSettings_List supports NAM-051 (TestE2E_NamingSettingsList is WP3's,
// CD-40): every series with its template and counters, ordered by label; the
// "Next: …" column comes from Preview.
func TestSettings_List(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.preset(dtPO, "PO/26-27/", 7)

	got, err := h.nm.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []SeriesInfo{
		{DocType: dtEXP, Label: "Expense Claim", Template: "EX/{FY}/{#####}", Counters: map[string]int64{}},
		{DocType: dtGRN, Label: "GRN", Template: "GR/{FY}/{#####}", Counters: map[string]int64{}},
		{DocType: dtPO, Label: "Purchase Order", Template: "PO/{FY}/{#####}", Counters: map[string]int64{"PO/26-27/": 7}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List =\n%#v\nwant\n%#v", got, want)
	}
	for _, s := range got {
		if s.Counters == nil {
			t.Errorf("List: %s Counters is nil, want an empty map", s.DocType)
		}
	}

	d := day(2026, 10, 24)
	for _, c := range []struct{ dt, want string }{
		{dtPO, "PO/26-27/00008"},
		{dtGRN, "GR/26-27/00001"},
		{dtEXP, "EX/26-27/00001"},
	} {
		next, err := h.nm.Preview(ctx, c.dt, d)
		if err != nil {
			t.Errorf("Preview(%s): %v", c.dt, err)
			continue
		}
		if next != c.want {
			t.Errorf("Preview(%s) = %q, want %q", c.dt, next, c.want)
		}
	}
}

// TestSettings_TemplateChangeNewPrefix covers NAM-052.
func TestSettings_TemplateChangeNewPrefix(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	d := day(2026, 10, 24)
	h.preset(dtPO, "PO/26-27/", 6)

	a := h.create(dtPO, d)
	must(t, h.submit(a, true))
	must(t, h.approve(a))
	wantDoc(t, h, a, doctype.StateApproved, "PO/26-27/00007")

	if err := h.nm.SetTemplate(ctx, admin, dtPO, "PR/{FY}/{#####}"); err != nil {
		t.Fatalf("SetTemplate(admin, PO, PR/{FY}/{#####}): %v", err)
	}

	b := h.create(dtPO, d)
	must(t, h.submit(b, true))
	must(t, h.approve(b))
	wantDoc(t, h, b, doctype.StateApproved, "PR/26-27/00001")
	wantDoc(t, h, a, doctype.StateApproved, "PO/26-27/00007")
}

// TestSettings_DuplicatePrefix covers NAM-055 and Review Focus 5.
func TestSettings_DuplicatePrefix(t *testing.T) {
	t.Run("same_static_prefix", func(t *testing.T) {
		h := newHarness(t)
		err := h.nm.SetTemplate(context.Background(), admin, dtGRN, "PO/{FY}/{####}")
		wantNamingError(t, "SetTemplate(admin, GRN, PO/{FY}/{####})", err, ErrDuplicatePrefix,
			"Prefix PO/{FY}/ is already used by Purchase Order. Use a different prefix.")
		if got := seriesTemplate(t, h, dtGRN); got != "GR/{FY}/{#####}" {
			t.Errorf("GRN template = %q, want unchanged %q", got, "GR/{FY}/{#####}")
		}
	})
	t.Run("literal_vs_token", func(t *testing.T) { // Review Focus 5
		h := newHarness(t)
		err := h.nm.SetTemplate(context.Background(), admin, dtGRN, "PO/26-27/{####}")
		wantNamingError(t, "SetTemplate(admin, GRN, PO/26-27/{####})", err, ErrDuplicatePrefix,
			"Prefix PO/26-27/ is already used by Purchase Order. Use a different prefix.")
		if got := seriesTemplate(t, h, dtGRN); got != "GR/{FY}/{#####}" {
			t.Errorf("GRN template = %q, want unchanged %q", got, "GR/{FY}/{#####}")
		}
	})
}

// TestSettings_SetCounter covers NAM-056.
func TestSettings_SetCounter(t *testing.T) {
	h := newHarness(t)
	if n := h.count(`SELECT COUNT(*) FROM naming_counters WHERE doctype = ? AND prefix = ?`, dtPO, "PO/26-27/"); n != 0 {
		t.Fatalf("PO/26-27/ counter rows before SetCounter = %d, want 0", n)
	}

	if err := h.nm.SetCounter(context.Background(), admin, dtPO, "PO/26-27/", 143); err != nil {
		t.Fatalf("SetCounter(admin, PO, PO/26-27/, 143): %v", err)
	}
	wantCounter(t, h, dtPO, "PO/26-27/", 143)

	if got := approveNew(t, h, dtPO, day(2026, 10, 24)); got != "PO/26-27/00144" {
		t.Errorf("next approved PO = %q, want %q", got, "PO/26-27/00144")
	}
}

// TestSettings_CounterNotBelowIssued covers NAM-057.
func TestSettings_CounterNotBelowIssued(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.preset(dtPO, "PO/26-27/", 149)
	if got := approveNew(t, h, dtPO, day(2026, 10, 24)); got != "PO/26-27/00150" {
		t.Fatalf("approved PO = %q, want %q", got, "PO/26-27/00150")
	}

	err := h.nm.SetCounter(ctx, admin, dtPO, "PO/26-27/", 120)
	wantNamingError(t, "SetCounter(admin, PO, PO/26-27/, 120)", err, ErrCounterBelowIssued,
		"PO/26-27/ has already issued numbers up to 00150. Set 150 or more.")
	wantCounter(t, h, dtPO, "PO/26-27/", 150)

	if err := h.nm.SetCounter(ctx, admin, dtPO, "PO/26-27/", 150); err != nil {
		t.Errorf("SetCounter(admin, PO, PO/26-27/, 150) = %v, want nil", err)
	}
	wantCounter(t, h, dtPO, "PO/26-27/", 150)
	if err := h.nm.SetCounter(ctx, admin, dtPO, "PO/26-27/", 200); err != nil {
		t.Errorf("SetCounter(admin, PO, PO/26-27/, 200) = %v, want nil", err)
	}
	wantCounter(t, h, dtPO, "PO/26-27/", 200)
}

// TestSettings_AdminOnly covers NAM-060 (message per CD-66).
func TestSettings_AdminOnly(t *testing.T) {
	const msg = "You don't have permission to change document numbering. Ask an Administrator."
	h := newHarness(t)
	ctx := context.Background()

	check := func(call string, err error) {
		t.Helper()
		wantNamingError(t, call, err, auth.ErrForbidden, msg)
		var ne *Error
		if !errors.As(err, &ne) {
			t.Errorf("%s: err = %T, want *naming.Error", call, err)
			return
		}
		if ne.Status() != 403 {
			t.Errorf("%s: Status() = %d, want 403", call, ne.Status())
		}
	}
	for _, p := range []auth.Principal{ravi, priya} {
		check("SetTemplate("+p.Name+", PO, PR/{FY}/{#####})",
			h.nm.SetTemplate(ctx, p, dtPO, "PR/{FY}/{#####}"))
		check("SetCounter("+p.Name+", PO, PO/26-27/, 12)",
			h.nm.SetCounter(ctx, p, dtPO, "PO/26-27/", 12))
	}

	if got := seriesTemplate(t, h, dtPO); got != "PO/{FY}/{#####}" {
		t.Errorf("PO template = %q, want unchanged %q", got, "PO/{FY}/{#####}")
	}
	if n := h.count(`SELECT COUNT(*) FROM naming_counters WHERE doctype = ? AND prefix = ?`, dtPO, "PO/26-27/"); n != 0 {
		t.Errorf("PO/26-27/ counter rows = %d, want 0", n)
	}
	if n := len(h.audit.Entries()); n != 0 {
		t.Errorf("audit entries = %d, want 0", n)
	}
}

// TestSettings_Audited covers NAM-061 (actor is the fixture admin, Anil
// Kapoor, per R5).
func TestSettings_Audited(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.nm.SetTemplate(ctx, admin, dtPO, "PR/{FY}/{#####}"); err != nil {
		t.Fatalf("SetTemplate(admin, PO, PR/{FY}/{#####}): %v", err)
	}
	if err := h.nm.SetCounter(ctx, admin, dtPO, "PR/26-27/", 12); err != nil {
		t.Fatalf("SetCounter(admin, PO, PR/26-27/, 12): %v", err)
	}

	at := time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)
	want := []audit.Entry{
		{DocType: "naming.Series", Action: "update", Changes: []audit.Change{{Field: "template", Before: "PO/{FY}/{#####}", After: "PR/{FY}/{#####}", Op: "set"}}, By: 1, At: at},
		{DocType: "naming.Counter", Action: "update", Changes: []audit.Change{{Field: "last", Before: "0", After: "12", Op: "set"}}, By: 1, At: at},
	}
	got := h.audit.Entries()
	if len(got) != len(want) {
		t.Fatalf("audit entries = %d (%#v), want %d", len(got), got, len(want))
	}
	// Compare without DocID (checked > 0) and with At via time.Equal (CD-110).
	for i := range got {
		e := got[i]
		if e.DocID <= 0 {
			t.Errorf("entry %d DocID = %d, want > 0", i, e.DocID)
		}
		if !e.At.Equal(want[i].At) || e.At.Location() != time.UTC {
			t.Errorf("entry %d At = %v, want %v UTC", i, e.At, want[i].At)
		}
		e.DocID, e.At = 0, time.Time{}
		w := want[i]
		w.At = time.Time{}
		if !reflect.DeepEqual(e, w) {
			t.Errorf("entry %d =\n%#v\nwant (ignoring DocID)\n%#v", i, got[i], want[i])
		}
	}
}

// TestSettings_TrimsTemplate covers Review Focus 3: surrounding spaces are
// trimmed before validation and storage.
func TestSettings_TrimsTemplate(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.nm.SetTemplate(ctx, admin, dtPO, "  PR/{FY}/{#####}  "); err != nil {
		t.Fatalf("SetTemplate(admin, PO, %q) = %v, want nil", "  PR/{FY}/{#####}  ", err)
	}
	list, err := h.nm.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, s := range list {
		if s.DocType == dtPO {
			found = true
			if s.Template != "PR/{FY}/{#####}" {
				t.Errorf("List: PO Template = %q, want %q", s.Template, "PR/{FY}/{#####}")
			}
		}
	}
	if !found {
		t.Errorf("List has no %s entry: %#v", dtPO, list)
	}
}

// TestSettings_SetCounterPrefixMismatch covers Review Focus 6.
func TestSettings_SetCounterPrefixMismatch(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	for _, prefix := range []string{"PUR/26-27/", "PO/26-27"} {
		err := h.nm.SetCounter(ctx, admin, dtPO, prefix, 12)
		wantNamingError(t, "SetCounter(admin, PO, "+prefix+", 12)", err, ErrPrefixMismatch,
			"Prefix "+prefix+" doesn't match series PO/{FY}/{#####}.")
	}
	if n := h.count(`SELECT COUNT(*) FROM naming_counters`); n != 0 {
		t.Errorf("naming_counters rows = %d, want 0", n)
	}
	if n := len(h.audit.Entries()); n != 0 {
		t.Errorf("audit entries = %d, want 0", n)
	}
}

// TestSettings_CounterRange covers NAM-058.
func TestSettings_CounterRange(t *testing.T) {
	const tpl = "PO/{FY}/{#####}"
	const msg = "Last number used must be a whole number from 0 to 99999."
	for _, s := range []string{"-1", "100000", "12a"} {
		got, err := ParseLastNumber(tpl, s)
		wantNamingError(t, "ParseLastNumber("+tpl+", "+s+")", err, ErrCounterRange, msg)
		if err == nil {
			t.Errorf("ParseLastNumber(%q, %q) = %d, nil; want error", tpl, s, got)
		}
	}
	if got, err := ParseLastNumber(tpl, "143"); err != nil || got != 143 {
		t.Errorf("ParseLastNumber(%q, %q) = %d, %v; want 143, nil", tpl, "143", got, err)
	}

	h := newHarness(t)
	err := h.nm.SetCounter(context.Background(), admin, dtPO, "PO/26-27/", 100000)
	wantNamingError(t, "SetCounter(admin, PO, PO/26-27/, 100000)", err, ErrCounterRange, msg)
}

// TestSettings_InvalidEditRejected covers NAM-054: the first failing rule's
// error is returned and nothing is saved.
func TestSettings_InvalidEditRejected(t *testing.T) {
	h := newHarness(t)
	err := h.nm.SetTemplate(context.Background(), admin, dtPO, "PO {FY}/{##}")
	wantNamingError(t, "SetTemplate(admin, PO, PO {FY}/{##})", err, ErrInvalidTemplate,
		"Series can use only letters, digits, '-' and '/'.")
	if got := seriesTemplate(t, h, dtPO); got != "PO/{FY}/{#####}" {
		t.Errorf("PO template = %q, want unchanged %q", got, "PO/{FY}/{#####}")
	}
	if n := len(h.audit.Entries()); n != 0 {
		t.Errorf("audit entries = %d, want 0", n)
	}
}

// TestSettings_PresetNextFY covers NAM-059 (harness clock moved to
// 2027-03-20).
func TestSettings_PresetNextFY(t *testing.T) {
	h := newHarness(t)
	h.nm = New(h.db, fixedClock(time.Date(2027, time.March, 20, 5, 0, 0, 0, time.UTC)), h.roles, h.audit)
	if n := h.count(`SELECT COUNT(*) FROM naming_counters WHERE doctype = ? AND prefix = ?`, dtPO, "PO/27-28/"); n != 0 {
		t.Fatalf("PO/27-28/ counter rows before SetCounter = %d, want 0", n)
	}

	if err := h.nm.SetCounter(context.Background(), admin, dtPO, "PO/27-28/", 50); err != nil {
		t.Fatalf("SetCounter(admin, PO, PO/27-28/, 50): %v", err)
	}
	if n := h.count(`SELECT COUNT(*) FROM naming_counters WHERE doctype = ? AND prefix = ?`, dtPO, "PO/27-28/"); n != 1 {
		t.Errorf("PO/27-28/ counter rows = %d, want 1", n)
	}
	wantCounter(t, h, dtPO, "PO/27-28/", 50)

	if got := approveNew(t, h, dtPO, day(2027, 4, 1)); got != "PO/27-28/00051" {
		t.Errorf("PO dated 2027-04-01 = %q, want %q", got, "PO/27-28/00051")
	}
}

// TestSettings_Preview covers NAM-062: the next three numbers joined with
// ", " (CD-57, CD-83), and nothing written. The new-prefix template is
// PR/{FY}/{#####} because PUR/… would render 15 characters (over the cap).
func TestSettings_Preview(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	d := day(2026, 10, 24)
	h.preset(dtPO, "PO/26-27/", 7)

	for _, c := range []struct{ tpl, want string }{
		{"PO/{FY}/{#####}", "PO/26-27/00008, PO/26-27/00009, PO/26-27/00010"},
		{"PR/{FY}/{#####}", "PR/26-27/00001, PR/26-27/00002, PR/26-27/00003"},
	} {
		got, err := h.nm.PreviewTemplate(ctx, c.tpl, d)
		if err != nil {
			t.Errorf("PreviewTemplate(%q, 2026-10-24): %v", c.tpl, err)
			continue
		}
		if got != c.want {
			t.Errorf("PreviewTemplate(%q, 2026-10-24) = %q, want %q", c.tpl, got, c.want)
		}
	}
	wantCounter(t, h, dtPO, "PO/26-27/", 7)

	got, err := h.nm.PreviewTemplate(ctx, "PO {FY}/{##}", d)
	wantNamingError(t, "PreviewTemplate(PO {FY}/{##}, 2026-10-24)", err, ErrInvalidTemplate,
		"Series can use only letters, digits, '-' and '/'.")
	if got != "" {
		t.Errorf("PreviewTemplate(PO {FY}/{##}) = %q, want \"\" with the error", got)
	}

	if n := h.count(`SELECT COUNT(*) FROM naming_counters`); n != 1 {
		t.Errorf("naming_counters rows = %d, want 1 (only the PO/26-27/ preset)", n)
	}
	wantCounter(t, h, dtPO, "PO/26-27/", 7)
	if n := h.count(`SELECT COUNT(*) FROM naming_issued`); n != 0 {
		t.Errorf("naming_issued rows = %d, want 0", n)
	}
	if n := len(h.audit.Entries()); n != 0 {
		t.Errorf("audit entries = %d, want 0", n)
	}
}

// TestSettings_EnsureSeriesDuplicatePrefix: EnsureSeries runs the same
// prefix-free check as SetTemplate and inserts no series row on failure.
func TestSettings_EnsureSeriesDuplicatePrefix(t *testing.T) {
	h := newHarness(t)
	err := h.nm.EnsureSeries(context.Background(), "nam.Other", "Other", "PO/{FY}/{####}")
	wantNamingError(t, "EnsureSeries(nam.Other, Other, PO/{FY}/{####})", err, ErrDuplicatePrefix,
		"Prefix PO/{FY}/ is already used by Purchase Order. Use a different prefix.")
	if n := h.count(`SELECT COUNT(*) FROM naming_series WHERE doctype = ?`, "nam.Other"); n != 0 {
		t.Errorf("naming_series rows for nam.Other = %d, want 0", n)
	}
	if n := h.count(`SELECT COUNT(*) FROM naming_series`); n != len(harnessSeries) {
		t.Errorf("naming_series rows = %d, want %d", n, len(harnessSeries))
	}
}

// TestSettings_AuditDocIDs: a template change's audit DocID is the series
// row id; a counter change's is the counter row id.
func TestSettings_AuditDocIDs(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	// Counter ids 1 (GR) and 2 (PO), so no counter id equals the EX series id
	// and the PO counter id differs from the PO series id.
	h.preset(dtGRN, "GR/26-27/", 3)
	h.preset(dtPO, "PO/26-27/", 7)

	id := func(query string, args ...any) int64 {
		t.Helper()
		var v int64
		if err := h.db.Read(ctx, func(tx *db.Tx) error {
			return tx.SQL().QueryRowContext(ctx, query, args...).Scan(&v)
		}); err != nil {
			t.Fatalf("%q: %v", query, err)
		}
		return v
	}
	seriesID := id(`SELECT id FROM naming_series WHERE doctype = ?`, dtEXP)
	poSeriesID := id(`SELECT id FROM naming_series WHERE doctype = ?`, dtPO)
	counterID := id(`SELECT id FROM naming_counters WHERE doctype = ? AND prefix = ?`, dtPO, "PO/26-27/")
	if counterID == poSeriesID || counterID == seriesID {
		t.Fatalf("setup: counter id %d equals a series id (PO %d, EX %d)", counterID, poSeriesID, seriesID)
	}

	if err := h.nm.SetTemplate(ctx, admin, dtEXP, "EC/{FY}/{#####}"); err != nil {
		t.Fatalf("SetTemplate(admin, EX, EC/{FY}/{#####}): %v", err)
	}
	if err := h.nm.SetCounter(ctx, admin, dtPO, "PO/26-27/", 20); err != nil {
		t.Fatalf("SetCounter(admin, PO, PO/26-27/, 20): %v", err)
	}

	got := h.audit.Entries()
	if len(got) != 2 {
		t.Fatalf("audit entries = %d (%#v), want 2", len(got), got)
	}
	if got[0].DocType != "naming.Series" || got[0].DocID != seriesID {
		t.Errorf("entry 0 = %s/%d, want naming.Series/%d (the EX series id)", got[0].DocType, got[0].DocID, seriesID)
	}
	if got[1].DocType != "naming.Counter" || got[1].DocID != counterID {
		t.Errorf("entry 1 = %s/%d, want naming.Counter/%d (the PO/26-27/ counter id)", got[1].DocType, got[1].DocID, counterID)
	}
}

// TestSettings_ParseLastNumberTrims: the form value is trimmed before parsing.
func TestSettings_ParseLastNumberTrims(t *testing.T) {
	const tpl = "PO/{FY}/{#####}"
	if got, err := ParseLastNumber(tpl, " 143 "); err != nil || got != 143 {
		t.Errorf("ParseLastNumber(%q, %q) = %d, %v; want 143, nil", tpl, " 143 ", got, err)
	}
}
