package docscreen_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// A reason or confirm page needs read plus its own right; without it the
// GET is AUTH's 403 and the engine is never called (CD-46).
func TestReasonPage_GetNeedsRight(t *testing.T) {
	for _, c := range []struct {
		page  string
		state doctype.State
		right auth.Right
	}{
		{"reject", doctype.StatePending, "approve"},
		{"cancel", doctype.StateApproved, "cancel"},
		{"close", doctype.StateApproved, "submit"},
		{"delete", doctype.StateDraft, "delete"},
	} {
		t.Run(c.page, func(t *testing.T) {
			var rights []auth.Right
			for _, r := range allRights {
				if r != c.right {
					rights = append(rights, r)
				}
			}
			e := newDocEnv(t, rights...)
			e.seed(c.state)
			rec := e.get(t, doc42+"/"+c.page)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", rec.Code)
			}
			v := sampleInfo(42, c.state)
			want := e.az.Can(context.Background(), priya, docscreentest.Sample, c.right, &auth.RecordRef{DocType: v.DocType, ID: v.ID, CreatedBy: v.CreatedBy})
			if want == nil || !strings.Contains(uitest.Doc(t, rec).Text(), want.Error()) {
				t.Errorf("body lacks AUTH's message %v", want)
			}
			if len(e.docs.Calls()) != 0 {
				t.Error("GET called the engine")
			}
		})
	}
}
