package files_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
)

// WP3 Task 1 (CD-182): the real Store meets the files contract.
func TestContract_Store(t *testing.T) {
	filestest.Run(t, func(t *testing.T) (files.Service, filestest.Setup) {
		env := filestest.New(t)
		return env.Store, env
	})
}
