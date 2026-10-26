package project_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/platform/db"
)

var errRestrictedBoom = errors.New("restricted lookup failed")

// failingRestricted is the real auth.Service with Restricted failing.
type failingRestricted struct{ auth.Service }

func (failingRestricted) Restricted(context.Context, *db.Tx, auth.Principal, string) (bool, error) {
	return false, errRestrictedBoom
}

// TestProject_CreateRestrictedError: an error from auth.Restricted refuses the
// create and is returned as is (CD-174).
func TestProject_CreateRestrictedError(t *testing.T) {
	env := msttest.New(t)
	kit := env.Kit
	kit.Auth = failingRestricted{kit.Auth}
	_, err := project.New(kit).Create(context.Background(), env.Kavya, project.Input{Code: "PRJ-X", Name: "X"})
	if !errors.Is(err, errRestrictedBoom) {
		t.Errorf("Create with failing Restricted = %v; want %v", err, errRestrictedBoom)
	}
}
