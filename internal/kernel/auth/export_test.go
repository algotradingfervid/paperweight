package auth

// Grant exposes the unexported grant for tests in package auth_test.
var Grant = (*Auth).grant

// RecordFailure exposes the unexported recordFailure for tests in package auth_test.
var RecordFailure = (*Auth).recordFailure
