package naming

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// RoleChecker is the one auth.Service method naming calls; production passes
// the real auth.Service.
type RoleChecker interface {
	HasRole(ctx context.Context, u auth.UserID, role string) (bool, error)
}

// Namer implements Service and Admin.
type Namer struct {
	db    *db.DB
	clk   clock.Clock
	roles RoleChecker
	audit audit.Writer
}

var (
	_ Service = (*Namer)(nil)
	_ Admin   = (*Namer)(nil)
)

// New returns a Namer. Production passes audit.Default as aw (CD-03).
func New(d *db.DB, clk clock.Clock, roles RoleChecker, aw audit.Writer) *Namer {
	return &Namer{db: d, clk: clk, roles: roles, audit: aw}
}

func (n *Namer) ValidateTemplate(tpl string) error {
	_, err := parseTemplate(tpl)
	return err
}
