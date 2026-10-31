package expense_test

import (
	"context"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/expensetype"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
)

// UX §5.13 (head ruling): the open claims report narrows to one employee;
// 0 is every employee, and scope still applies (Priya, read team, sees no
// Kavya rows even when she asks for Kavya).
func TestS4_OpenClaimsEmployeeFilter(t *testing.T) {
	ctx := context.Background()
	e := expensetest.New(t)
	e.E8OpenScenario(t)
	deepak, kavya := e.E8Employee(t, "EMP-007"), e.E8Employee(t, "EMP-006")

	for _, tc := range []struct {
		name, email string
		employee    int64
		want        []string
	}{
		{"every employee", "meena@vardhan.test", 0, []string{"EX/26-27/00001", "EX/26-27/00002"}},
		{"kavya", "meena@vardhan.test", kavya, []string{"EX/26-27/00002"}},
		{"deepak", "meena@vardhan.test", deepak, []string{"EX/26-27/00001"}},
		{"priya asks for kavya", "priya@vardhan.test", kavya, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := e.Service.OpenClaimsReport(ctx, e.P(t, tc.email), expense.OpenClaimsFilter{EmployeeID: tc.employee})
			if err != nil {
				t.Fatalf("OpenClaimsReport: %v", err)
			}
			var got []string
			for _, r := range rows {
				got = append(got, r.Claim)
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Errorf("claims = %q, want %q", got, tc.want)
			}
		})
	}
}

// The expense lines report's rows carry their line number (UX §5.14
// "EX/26-27/00004 · line 2").
func TestS4_LineRowsCarryLineNo(t *testing.T) {
	e := expensetest.New(t)
	e.E8LinesScenario(t)
	rep, err := e.Service.ExpenseLinesReport(context.Background(), e.P(t, "meena@vardhan.test"), expense.LinesFilter{})
	if err != nil {
		t.Fatalf("ExpenseLinesReport: %v", err)
	}
	got := map[string]int{}
	for _, r := range rep.Rows {
		got[r.Note] = r.LineNo
	}
	want := map[string]int{"Hotel Pune 2 nights": 1, "Team lunch": 2, "Diesel for site jeep": 1}
	for note, n := range want {
		if got[note] != n {
			t.Errorf("%s: line no = %d, want %d", note, got[note], n)
		}
	}
}

// The lines report's filter choices: the last 24 months, newest first, from
// the IST business date, and every expense type by name; no types for a
// user who can't read Expense Type (AD ruling: no Type filter then).
func TestS4_LinesChoices(t *testing.T) {
	e := expensetest.New(t)
	e.E8LinesScenario(t) // the clock is 2026-10-24 10:30 IST
	c, err := e.Service.LinesChoices(context.Background(), e.P(t, "meena@vardhan.test"))
	if err != nil {
		t.Fatalf("LinesChoices: %v", err)
	}
	if len(c.Months) != 24 || c.Months[0] != "2026-10" || c.Months[1] != "2026-09" || c.Months[23] != "2024-11" {
		t.Errorf("months = %q, want 24 from 2026-10 back to 2024-11", c.Months)
	}
	var names []string
	for _, ty := range c.Types {
		names = append(names, ty.Name)
		if ty.Name == "Food" && ty.ID != e.E8Type(t, "FOOD") {
			t.Errorf("Food id = %d, want %d", ty.ID, e.E8Type(t, "FOOD"))
		}
	}
	if !slices.Contains(names, "Food") || !slices.Contains(names, "Lodging") || !slices.IsSorted(names) {
		t.Errorf("types = %q, want every type (Food, Lodging, ...) by name", names)
	}

	e.S4RevokeRead(t, "priya@vardhan.test", expensetype.DocType)
	c, err = e.Service.LinesChoices(context.Background(), e.P(t, "priya@vardhan.test"))
	if err != nil {
		t.Fatalf("LinesChoices as Priya: %v", err)
	}
	if c.Types != nil || len(c.Months) != 24 {
		t.Errorf("Priya without Expense Type read: types = %v, months %d; want no types, 24 months", c.Types, len(c.Months))
	}
}

// MasterLabel names a master row only when p may read it (CD-233): Meena
// reads Kavya and PRJ-METRO; Priya (restricted to PRJ-METRO) can't read
// PRJ-TECHPARK, and no one gets a missing row.
func TestS4_MasterLabel(t *testing.T) {
	ctx := context.Background()
	e := expensetest.New(t)
	e.S4Restrict(t, "priya@vardhan.test", "PRJ-METRO")
	meena, priya := e.P(t, "meena@vardhan.test"), e.P(t, "priya@vardhan.test")
	for _, tc := range []struct {
		name   string
		p      auth.Principal
		dt     string
		id     int64
		ok     bool
		code   string
		master string
	}{
		{"meena employee", meena, employee.DocType, e.E8Employee(t, "EMP-006"), true, "EMP-006", "Kavya Rao"},
		{"meena project", meena, project.DocType, e.E8Project(t, "PRJ-METRO"), true, "PRJ-METRO", ""},
		{"priya own project", priya, project.DocType, e.E8Project(t, "PRJ-METRO"), true, "PRJ-METRO", ""},
		{"priya other project", priya, project.DocType, e.E8Project(t, "PRJ-TECHPARK"), false, "", ""},
		{"missing row", meena, employee.DocType, 999999, false, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info, ok, err := e.Service.MasterLabel(ctx, tc.p, tc.dt, tc.id)
			if err != nil {
				t.Fatalf("MasterLabel: %v", err)
			}
			if ok != tc.ok || info.Code != tc.code || (tc.master != "" && info.Name != tc.master) {
				t.Errorf("MasterLabel = %+v, %v; want ok %v, code %q, name %q", info, ok, tc.ok, tc.code, tc.master)
			}
		})
	}
	e.S4RevokeRead(t, "priya@vardhan.test", employee.DocType)
	if _, ok, err := e.Service.MasterLabel(ctx, e.P(t, "priya@vardhan.test"), employee.DocType, e.E8Employee(t, "EMP-007")); ok || err != nil {
		t.Errorf("Priya without Employee read: ok %v, err %v; want hidden", ok, err)
	}
}
