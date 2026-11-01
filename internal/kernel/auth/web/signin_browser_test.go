//go:build e2e

package authweb_test

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// A-7 (UI v2 §8): a wrong password in the real form shows the kernel text in
// a role="alert" banner, marks no field invalid, keeps the typed email, and
// leaves the password empty and focused, described by the banner (CD-361).
func TestSignInBrowser_WrongPassword(t *testing.T) {
	h := webtest.New(t, authtest.NewWorld(t))
	ctx := uibrowser.New(t)
	if err := uibrowser.At(ctx, 1280, h.HTTP.URL+"/signin"); err != nil {
		t.Fatal(err)
	}
	if err := p8Do(ctx,
		chromedp.Evaluate[chromedp.Void](`(() => {
  const f = document.querySelector("input[name=password]").form;
  f.elements.email.value = "`+t13Admin+`";
  f.elements.password.value = "not-the-password";
  f.requestSubmit();
})()`),
		chromedp.WaitVisible(chromedp.CSS(`.solo__card [role="alert"]`)),
	); err != nil {
		t.Fatalf("submit: %v", err)
	}
	// Autofocus applies once the page has loaded, which under load can be
	// after the banner shows; a timeout here is reported by the checks below.
	_ = p8Do(ctx, chromedp.Poll[chromedp.Void](`document.readyState === "complete" && document.activeElement.name === "password"`))
	type state struct {
		Banner, Active, Value, Described, Email string
		Invalid                                 int
	}
	got, err := chromedp.Run(ctx, chromedp.Evaluate[state](`({
  Banner: document.querySelector('.solo__card [role="alert"]').textContent,
  Active: document.activeElement.name || "",
  Value: document.querySelector("input[name=password]").value,
  Described: document.activeElement.getAttribute("aria-describedby") || "",
  Email: document.querySelector("input[name=email]").value,
  Invalid: document.querySelectorAll("[aria-invalid]").length,
})`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(strings.Fields(got.Banner), " "), auth.ErrLoginInvalid.Msg) {
		t.Errorf("banner = %q, want %q", got.Banner, auth.ErrLoginInvalid.Msg)
	}
	if got.Active != "password" || got.Value != "" {
		t.Errorf("focus on %q, password %q; want the empty password focused", got.Active, got.Value)
	}
	// CD-361: the focused password is described by the banner, and the
	// email box keeps what was typed.
	if got.Described != "f-password-msg signin-error" {
		t.Errorf("focused element aria-describedby = %q, want %q", got.Described, "f-password-msg signin-error")
	}
	if got.Email != t13Admin {
		t.Errorf("email box = %q, want %q", got.Email, t13Admin)
	}
	if got.Invalid != 0 {
		t.Errorf("%d elements with aria-invalid, want 0", got.Invalid)
	}
}
