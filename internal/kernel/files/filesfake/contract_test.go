package filesfake_test

import (
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filesfake"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// fakeSetup is filestest.Setup for the fake. Parents and rights go into a
// docfake and an authfake, the same fakes filestest.Env gives the real Store;
// the fake has to consult them (or equivalent hooks) to meet the contract.
type fakeSetup struct {
	fake  *filesfake.Fake
	auth  *authfake.Fake
	docs  *docfake.Fake
	clock *clock.Fixed
}

func (s *fakeSetup) Parent(info doctype.Info) auth.RecordRef {
	s.docs.SetInfo(info)
	return auth.RecordRef{DocType: info.DocType, ID: info.ID, CreatedBy: info.CreatedBy}
}

func (s *fakeSetup) SetState(ref auth.RecordRef, state string) {
	s.docs.SetState(ref.DocType, ref.ID, doctype.State(state))
}

func (s *fakeSetup) Allow(u auth.UserID, dt string, rights ...auth.Right) {
	s.auth.Allow(u, dt, rights...)
}

func (s *fakeSetup) Deny(u auth.UserID, dt string, rights ...auth.Right) {
	s.auth.Deny(u, dt, rights...)
}

func (s *fakeSetup) Hide(ref auth.RecordRef) { s.auth.Hide(ref.DocType, ref.ID) }

func (s *fakeSetup) Spool(b []byte) *files.Spooled { return s.fake.Spool("", b) }

func (s *fakeSetup) Write(fn func(tx *db.Tx) error) error { return fn(nil) }

func (s *fakeSetup) Now() time.Time { return s.clock.Now() }

func (s *fakeSetup) Advance(d time.Duration) { s.clock.Advance(d) }

// WP3 Task 1 (CD-182): the fake meets the same files contract as the real Store.
func TestContract_Fake(t *testing.T) {
	filestest.Run(t, func(t *testing.T) (files.Service, filestest.Setup) {
		c := testkit.Clock()
		a, docs := authfake.New(), docfake.New()
		f := filesfake.NewWith(c, a, docs)
		return f, &fakeSetup{fake: f, auth: a, docs: docs, clock: c}
	})
}
