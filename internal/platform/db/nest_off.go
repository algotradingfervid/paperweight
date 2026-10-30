//go:build !nestcheck

package db

// NestCheck reports whether this binary carries the nested-transaction gate.
const NestCheck = false

func noop() {}

// enterTx is the gate's hook in run; without the nestcheck tag it does nothing.
func enterTx() func() { return noop }
