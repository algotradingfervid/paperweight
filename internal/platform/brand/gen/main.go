// Command gen checks brand.json and assets/ and writes brand_gen.go (run by go generate
// in internal/platform/brand).
package main

import (
	"fmt"
	"os"

	"github.com/vardhan/internaltools/internal/platform/brand"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	b, err := os.ReadFile("brand.json")
	if err != nil {
		return err
	}
	files := os.DirFS("assets")
	c, err := brand.Parse(b, files)
	if err != nil {
		return err
	}
	src, err := brand.Generate(c, files)
	if err != nil {
		return err
	}
	return os.WriteFile("brand_gen.go", src, 0o644) //nolint:gosec // generated source is meant to be world-readable
}
