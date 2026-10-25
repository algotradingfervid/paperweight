package auth_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
)

const priyaEmail = "priya@vardhan.test"

// checkPolicyErr asserts CheckPolicy's result matches want (nil means
// accepted), both by errors.Is and by the exact user-facing text.
func checkPolicyErr(t *testing.T, password, email, currentHash string, want error) {
	t.Helper()
	err := auth.CheckPolicy(password, email, currentHash)
	if want == nil {
		if err != nil {
			t.Errorf("CheckPolicy(%q, %q) = %v, want nil", password, email, err)
		}
		return
	}
	if !errors.Is(err, want) {
		t.Errorf("CheckPolicy(%q, %q) = %v, want %v", password, email, err, want)
		return
	}
	if err.Error() != want.Error() {
		t.Errorf("CheckPolicy(%q, %q) text = %q, want %q", password, email, err.Error(), want.Error())
	}
}

// AUTH-025
func TestPasswordPolicy_MinLength(t *testing.T) {
	checkPolicyErr(t, "Short-123", priyaEmail, "", auth.ErrPasswordTooShort)
	if got, want := auth.ErrPasswordTooShort.Error(), "Password must be at least 10 characters."; got != want {
		t.Errorf("ErrPasswordTooShort text = %q, want %q", got, want)
	}
	checkPolicyErr(t, "Short-1234", priyaEmail, "", nil)
}

// AUTH-026
func TestPasswordPolicy_MaxLength(t *testing.T) {
	longest := strings.Repeat("a1-B", 64)
	if n := len([]rune(longest)); n != 256 {
		t.Fatalf("test setup: password has %d runes, want 256", n)
	}
	checkPolicyErr(t, longest, priyaEmail, "", nil)
	checkPolicyErr(t, longest+"x", priyaEmail, "", auth.ErrPasswordTooLong)
	if got, want := auth.ErrPasswordTooLong.Error(), "Password must be at most 256 characters."; got != want {
		t.Errorf("ErrPasswordTooLong text = %q, want %q", got, want)
	}
}

// AUTH-027
func TestPasswordPolicy_CommonList(t *testing.T) {
	for _, pw := range []string{"password123", "Password@123", "qwertyuiop"} {
		checkPolicyErr(t, pw, priyaEmail, "", auth.ErrPasswordCommon)
	}
	if got, want := auth.ErrPasswordCommon.Error(), "This password is too common. Choose a longer or less predictable one."; got != want {
		t.Errorf("ErrPasswordCommon text = %q, want %q", got, want)
	}
	// The fixture passwords used elsewhere must not be on the list (Task 2 Step 4).
	for _, pw := range []string{"Short-1234", "Correct-Horse-42", "Chai-Break-Ledger-7", "Temp-Godown-2026"} {
		checkPolicyErr(t, pw, priyaEmail, "", nil)
	}
}

// AUTH-029
func TestPasswordPolicy_NotSameAsCurrent(t *testing.T) {
	cur, err := auth.Hash("Correct-Horse-42")
	if err != nil {
		t.Fatalf("Hash error: %v", err)
	}
	checkPolicyErr(t, "Correct-Horse-42", priyaEmail, cur, auth.ErrPasswordSame)
	if got, want := auth.ErrPasswordSame.Error(), "New password must be different from your current password."; got != want {
		t.Errorf("ErrPasswordSame text = %q, want %q", got, want)
	}
	checkPolicyErr(t, "Chai-Break-Ledger-7", priyaEmail, cur, nil)
}
