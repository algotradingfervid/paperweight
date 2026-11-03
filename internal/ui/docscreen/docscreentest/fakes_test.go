package docscreentest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// Count gives the Set Total and, like List, fails once Fail is called: the
// fake refuses what the real lister would.
func TestLister_CountFollowsSetAndFail(t *testing.T) {
	l := docscreentest.NewLister()
	dt := &doctype.DocType{Name: "x.Y"}
	l.Set(dt.Name, types.Result[docscreen.ListRow]{Total: 7})
	if n, err := l.Count(context.Background(), auth.Principal{}, dt, docscreen.ListQuery{}); n != 7 || err != nil {
		t.Errorf("Count = %d, %v; want 7, nil", n, err)
	}
	boom := errors.New("boom")
	l.Fail(boom)
	if n, err := l.Count(context.Background(), auth.Principal{}, dt, docscreen.ListQuery{}); n != 0 || !errors.Is(err, boom) {
		t.Errorf("Count after Fail = %d, %v; want 0, boom", n, err)
	}
}
