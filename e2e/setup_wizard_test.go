//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// TestE2E_SetupWizardPhone walks the first-run setup wizard on a new install
// at 320 px (UI v2 §5.4, E2): no step scrolls sideways, the GSTIN echoes its
// state and PAN as it is checked while State code waits in a closed More
// details (A-8), Back keeps what was typed, and Save company lands on the
// inbox with the success banner.
func TestE2E_SetupWizardPhone(t *testing.T) {
	h := StartEmpty(t)
	h.createAdmin()
	s := h.Browser(freshAdmin, phone320)
	s.Open("/signin")
	s.Fill("email", freshAdmin)
	s.Fill("password", password)
	s.Press("Sign in")
	s.Until("the setup wizard", `location.pathname === "/setup/company" && document.querySelector("input[name=legal_name]") !== null`)
	s.WaitText("Welcome to Paperweight.")
	noOverflow(s, "Company")

	s.Fill("legal_name", "Vardhan Infra Projects Pvt Ltd")
	s.Fill("gstin", "27AAFCV2449G1Z7")
	s.Tab()
	s.Until("the GSTIN echo", `document.getElementById("f-gstin-msg").innerText.includes("Maharashtra (27)")`)
	if echo := Eval[string](s, `document.getElementById("f-gstin-msg").innerText`); !strings.Contains(echo, "PAN AAFCV2449G") {
		t.Errorf("GSTIN echo %q, want PAN AAFCV2449G (A-8)", echo)
	}
	if !Eval[bool](s, `(() => { const d = document.getElementById("f-state_code").closest("details"); return d !== null && !d.open; })()`) {
		t.Error("State code isn't inside a closed More details (A-8)")
	}
	noOverflow(s, "Company with the echo")
	s.Press("Continue")
	s.Until("the Head office step", `location.pathname === "/setup/company/office" && document.getElementById("f-branch_code") !== null`)
	noOverflow(s, "Head office")

	s.Fill("branch_code", "PUNE")
	s.Press("Back")
	s.Until("the Company step again", `location.pathname === "/setup/company/company" && document.getElementById("f-legal_name") !== null`)
	if got := Eval[string](s, `document.getElementById("f-legal_name").value`); got != "Vardhan Infra Projects Pvt Ltd" {
		t.Errorf("legal name after Back = %q", got)
	}
	s.Press("Continue")
	s.Until("the Head office step", `location.pathname === "/setup/company/office" && document.getElementById("f-branch_code") !== null`)
	if got := Eval[string](s, `document.getElementById("f-branch_code").value`); got != "PUNE" {
		t.Errorf("head office code after Back and Continue = %q, want PUNE", got)
	}
	s.Fill("address_line1", "Plot 12, MIDC Bhosari")
	s.Fill("address_city", "Pune")
	s.Fill("address_pincode", "411026")
	s.Press("Continue")
	s.Until("the Check step", `location.pathname === "/setup/company/check"`)
	s.WaitText("Maharashtra (27)")
	noOverflow(s, "Check")

	s.Press("Save company")
	s.Until("the inbox", `location.pathname === "/approvals"`)
	s.WaitText("Your company is set up. Next, add your users.")
}

// TestE2E_FillAfterStepSwap: Fill waits for htmx to settle a swapped step.
// Settling focuses the step heading ([autofocus]), so keys typed across it
// went to the heading and the field kept only the first ones. The settle
// delay is stretched so the race always shows: Fill ends before it, and the
// key typed after it is lost unless Fill waited.
func TestE2E_FillAfterStepSwap(t *testing.T) {
	h := StartEmpty(t)
	h.createAdmin()
	s := h.Browser(freshAdmin, phone320)
	s.Open("/signin")
	s.Fill("email", freshAdmin)
	s.Fill("password", password)
	s.Press("Sign in")
	s.Until("the setup wizard", `location.pathname === "/setup/company" && document.querySelector("input[name=legal_name]") !== null`)
	s.Fill("legal_name", "Vardhan Infra Projects Pvt Ltd")
	s.Fill("gstin", "27AAFCV2449G1Z7")
	Eval[bool](s, `(htmx.config.defaultSettleDelay = 300, true)`)
	s.Press("Continue")
	s.Until("the Head office step", `location.pathname === "/setup/company/office" && document.getElementById("f-branch_code") !== null`)
	s.Fill("branch_code", "PUN")
	s.Until("the step to settle", `!document.querySelector(".htmx-settling")`)
	s.Keys("E")
	if got := Eval[string](s, `document.getElementById("f-branch_code").value`); got != "PUNE" {
		t.Errorf("head office code = %q, want PUNE", got)
	}
}
