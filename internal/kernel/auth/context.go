package auth

import "context"

// principalKey is the context key for the signed-in Principal (moved from
// contract.go, CD-84).
type principalKey struct{}

// WithPrincipal returns ctx carrying p (CD-12).
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the Principal stored by WithPrincipal (CD-12).
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}
