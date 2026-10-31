package dbq

import (
	"strings"
	"testing"
)

func TestSQLWeak(t *testing.T) {
	if !strings.Contains(OpenItems(), "SELECT") {
		t.Fatal("weak")
	}
}

func TestSQLStrong(t *testing.T) {
	want := "-- name: OpenItems :many\nSELECT DISTINCT id\nFROM item\nWHERE owner = ?\n  AND state IN ('open', 'held')\nORDER BY id DESC\nLIMIT 10\n"
	want2 := "-- name: MineOrShared :many\nSELECT id\nFROM item\nWHERE owner = ?1\n  OR shared_by = ?1\nORDER BY id\n"
	if OpenItems() != want || MineOrShared() != want2 {
		t.Fatal("strong")
	}
}
