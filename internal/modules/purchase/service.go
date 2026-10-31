package purchase

import (
	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// ServiceDeps are what the Service needs; internal/app and purchasetest fill
// them.
type ServiceDeps struct {
	DB        *db.DB
	Engine    doc.Engine
	Auth      auth.Service
	Audit     audit.Writer
	Approvals *approval.Approvals
	Comments  *modkit.Comments
	Clock     clock.Clock
}

// Service is everything purchase does for a principal. Each task adds its
// methods in its own file.
type Service struct {
	d ServiceDeps //nolint:unused // read by the methods later tasks add (plan "Service shape")
}

// New returns the Service over d.
func New(d ServiceDeps) *Service { return &Service{d: d} }
