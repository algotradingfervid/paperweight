package approvalfake_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/approval/approvalfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

var priya = auth.Principal{ID: 4}

// CD-23: after a reject the document must be revised before it is submitted again.
func TestFake_OpenAfterRejectNeedsRevise(t *testing.T) {
	ctx := context.Background()
	f := approvalfake.New()
	req := approval.Requirement{Needed: true}
	if err := f.Open(ctx, nil, "fixture.Order", 1, req); err != nil {
		t.Fatalf("Open error = %v", err)
	}
	if err := f.Record(ctx, nil, "fixture.Order", 1, doctype.ActionReject, priya, "Wrong rate"); err != nil {
		t.Fatalf("Record(reject) error = %v", err)
	}
	if err := f.Open(ctx, nil, "fixture.Order", 1, req); !errors.Is(err, approvalfake.ErrProtocol) {
		t.Errorf("Open straight after reject error = %v, want ErrProtocol", err)
	}
}

// CD-23: revise is only valid while the rejected request is closed, not after a new Open.
func TestFake_ReviseAfterReopenRejected(t *testing.T) {
	ctx := context.Background()
	f := approvalfake.New()
	req := approval.Requirement{Needed: true}
	steps := []struct {
		name string
		call func() error
	}{
		{"Open", func() error { return f.Open(ctx, nil, "fixture.Order", 1, req) }},
		{"Record(reject)", func() error {
			return f.Record(ctx, nil, "fixture.Order", 1, doctype.ActionReject, priya, "Wrong rate")
		}},
		{"Record(revise)", func() error { return f.Record(ctx, nil, "fixture.Order", 1, doctype.ActionRevise, priya, "") }},
		{"Open again", func() error { return f.Open(ctx, nil, "fixture.Order", 1, req) }},
	}
	for _, s := range steps {
		if err := s.call(); err != nil {
			t.Fatalf("%s error = %v, want nil", s.name, err)
		}
	}
	if err := f.Record(ctx, nil, "fixture.Order", 1, doctype.ActionRevise, priya, ""); !errors.Is(err, approvalfake.ErrProtocol) {
		t.Errorf("Record(revise) while a request is open error = %v, want ErrProtocol", err)
	}
}
