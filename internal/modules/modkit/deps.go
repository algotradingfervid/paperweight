package modkit

import (
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Deps are what modkit's services need; internal/app fills them.
type Deps struct {
	DB    *db.DB
	Auth  auth.Service
	Audit audit.Writer
	Clock clock.Clock
}

// Kit is modkit's service: business settings (M0-7) and comments (M0-14).
type Kit struct{ d Deps }

// New returns a Kit over d.
func New(d Deps) *Kit { return &Kit{d: d} }
