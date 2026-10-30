//go:build nestcheck

package db

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
)

// NestCheck reports whether this binary carries the nested-transaction gate.
const NestCheck = true

var (
	nestMu sync.Mutex
	nest   = map[string]bool{} // goroutines inside a transaction, by id
)

// goid reads the id from the first line of the stack: "goroutine 7 [running]:".
func goid() string {
	b := make([]byte, 64)
	b = b[:runtime.Stack(b, false)]
	return strings.Fields(string(b))[1]
}

// enterTx exits the process when this goroutine is already inside a
// transaction (CD-213). It exits rather than panics or returns an error, so a
// recover or an `err == nil` caller can't swallow it. The returned func leaves
// the transaction.
func enterTx() func() {
	id := goid()
	nestMu.Lock()
	nested := nest[id]
	nest[id] = true
	nestMu.Unlock()
	if nested {
		buf := make([]byte, 1<<16)
		buf = buf[:runtime.Stack(buf, false)]
		fmt.Fprintf(os.Stderr, "db: a transaction was opened inside another on this goroutine\n%s\n", buf)
		os.Exit(2)
	}
	return func() {
		nestMu.Lock()
		delete(nest, id)
		nestMu.Unlock()
	}
}
