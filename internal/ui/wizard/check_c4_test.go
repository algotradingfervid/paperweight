package wizard_test

import (
	"context"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui/uitest"
	"github.com/vardhan/internaltools/internal/ui/wizard"
)

// UI v2 C4 (§5.6): each check section is marked Complete with a success badge
// or Needs attention with a warning badge; a section without a status shows
// no badge.
func TestCheckSection_StatusBadge(t *testing.T) {
	for _, tc := range []struct {
		status            wizard.StepStatus
		word, tone, other string
	}{
		{wizard.Complete, "Complete", "is-complete", "is-attention"},
		{wizard.Attention, "Needs attention", "is-attention", "is-complete"},
		{"", "", "", ""},
	} {
		d := uitest.Render(t, templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
			return wizard.CheckSection("Party", tc.status, "/doc/po/42/edit/party").Render(templ.WithChildren(ctx, templ.Raw(`<p class="summary-x">Acme</p>`)), w)
		}))
		sec := d.Find("section.card")
		b := sec.Find(".check-status")
		if tc.status == "" {
			if b.Length() != 0 {
				t.Errorf("no status: want no badge, got %q", b.Text())
			}
			continue
		}
		if b.Length() != 1 {
			t.Fatalf("%s: %d badges", tc.word, b.Length())
		}
		if !b.HasClass("badge") || !b.HasClass(tc.tone) || b.HasClass(tc.other) {
			t.Errorf("%s: badge classes = %q", tc.word, b.AttrOr("class", ""))
		}
		if got := strings.Join(strings.Fields(b.Text()), " "); got != tc.word {
			t.Errorf("%s: badge text = %q", tc.word, got)
		}
		if b.Find("svg").Length() != 1 {
			t.Errorf("%s: badge needs its icon, never colour alone", tc.word)
		}
		if b.NextAllFiltered("p.summary-x").Length() != 1 {
			t.Errorf("%s: badge must come before the summary", tc.word)
		}
	}
}

// The badges are pills tinted success (Complete) and warning (Needs attention).
func TestCheckSection_BadgeCSS(t *testing.T) {
	css, err := os.ReadFile("../css/components/stepper.css")
	if err != nil {
		t.Fatal(err)
	}
	s := string(css)
	i := strings.Index(s, "@utility check-status {")
	if i < 0 {
		t.Fatal("stepper.css has no check-status utility")
	}
	block := s[i:]
	block = block[:strings.Index(block, "\n}")]
	for _, want := range []string{
		"border-radius: var(--radius-pill);",
		`&.is-complete { background-color: var(--color-success-50); color: var(--color-success-800); }`,
		`&.is-attention { background-color: var(--color-warning-50); color: var(--color-warning-800); }`,
	} {
		if !strings.Contains(block, want) {
			t.Errorf("check-status block lacks %q", want)
		}
	}
	if regexp.MustCompile(`color-(success|warning)-700`).MatchString(block) {
		t.Error("check-status still uses the v1 plain-text tones")
	}
}
