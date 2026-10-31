package purchase

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/auth"
)

// Can is AUTH's check of right on DocType dt with no record: nil when p
// holds it in any scope, else AUTH's refusal. The module pages ask it for
// their New buttons, Pickers and the home's read check (UX §5.1, §5.2,
// §5.19).
func (s *Service) Can(ctx context.Context, p auth.Principal, dt string, right auth.Right) error {
	return s.d.Auth.Can(ctx, p, dt, right, nil)
}
