package types

import "testing"

// TestMoney_Add is mentioned here only in a comment.
// func TestMoney_Add(t *testing.T) {}

/*
func TestMoney_Add(t *testing.T) {}
*/

type suite struct{}

// A method is not a test function.
func (suite) TestMoney_Add(t *testing.T) {}

func TestOther(t *testing.T) {
	_ = "func TestMoney_Add(t *testing.T) {}"
	_ = `TestMoney_Add`
}
