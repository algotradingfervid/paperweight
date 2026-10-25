package gstin_test

import (
	"cmp"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/testkit"
)

func TestStates_MatchGSTCodes(t *testing.T) { // MST-039
	states := gstin.States()
	if len(states) != 38 {
		t.Fatalf("States() has %d rows, want 38", len(states))
	}
	// STATE_NUMBERS, india_compliance/gst_india/constants/__init__.py:96, verbatim.
	stateNumbers := map[string]string{
		"Andaman and Nicobar Islands": "35", "Andhra Pradesh": "37", "Arunachal Pradesh": "12", "Assam": "18",
		"Bihar": "10", "Chandigarh": "04", "Chhattisgarh": "22", "Dadra and Nagar Haveli and Daman and Diu": "26",
		"Delhi": "07", "Goa": "30", "Gujarat": "24", "Haryana": "06", "Himachal Pradesh": "02",
		"Jammu and Kashmir": "01", "Jharkhand": "20", "Karnataka": "29", "Kerala": "32", "Ladakh": "38",
		"Lakshadweep Islands": "31", "Madhya Pradesh": "23", "Maharashtra": "27", "Manipur": "14",
		"Meghalaya": "17", "Mizoram": "15", "Nagaland": "13", "Odisha": "21", "Other Countries": "96",
		"Other Territory": "97", "Puducherry": "34", "Punjab": "03", "Rajasthan": "08", "Sikkim": "11",
		"Tamil Nadu": "33", "Telangana": "36", "Tripura": "16", "Uttar Pradesh": "09", "Uttarakhand": "05",
		"West Bengal": "19",
	}
	var wantAll []gstin.State
	for name, code := range stateNumbers {
		wantAll = append(wantAll, gstin.State{Code: code, Name: name})
	}
	byCode := func(a, b gstin.State) int { return cmp.Compare(a.Code, b.Code) }
	slices.SortFunc(wantAll, byCode)
	if got := slices.SortedFunc(slices.Values(states), byCode); !slices.Equal(got, wantAll) {
		t.Errorf("States() differs from STATE_NUMBERS:\n got:  %v\n want: %v", got, wantAll)
	}
	// Spot values named by MST-039.
	for name, code := range map[string]string{
		"Maharashtra": "27", "Karnataka": "29", "Delhi": "07", "Ladakh": "38", "Andhra Pradesh": "37",
		"Telangana": "36", "Dadra and Nagar Haveli and Daman and Diu": "26", "Other Territory": "97",
		"Other Countries": "96",
	} {
		s, ok := gstin.StateByCode(code)
		if !ok || s != (gstin.State{Code: code, Name: name}) {
			t.Errorf("StateByCode(%s) = %+v, %v; want %s", code, s, ok, name)
		}
	}
	if gstin.IsIndianState("96") {
		t.Error(`IsIndianState("96") = true, want false`)
	}
	if !gstin.IsIndianState("27") {
		t.Error(`IsIndianState("27") = false, want true`)
	}
	for _, code := range []string{"25", "28"} {
		if s, ok := gstin.StateByCode(code); ok {
			t.Errorf("StateByCode(%s) = %+v, want absent", code, s)
		}
		if gstin.IsIndianState(code) {
			t.Errorf("IsIndianState(%s) = true, want false", code)
		}
	}

	d := testkit.NewDB(t)
	rows, err := d.SQL().QueryContext(t.Context(), `SELECT code, name FROM gst_state ORDER BY code`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var fromDB []gstin.State
	for rows.Next() {
		var s gstin.State
		if err := rows.Scan(&s.Code, &s.Name); err != nil {
			t.Fatal(err)
		}
		fromDB = append(fromDB, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if want := slices.SortedFunc(slices.Values(states), byCode); !slices.Equal(fromDB, want) {
		t.Errorf("gst_state rows differ from States():\n db:     %v\n States: %v", fromDB, want)
	}
}
