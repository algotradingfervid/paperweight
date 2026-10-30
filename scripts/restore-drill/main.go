// Command restore-drill is the Go half of scripts/restore-drill.sh: it verifies
// a restored data directory against the crashed one, and holds the temporary
// stand-in writer (standin.go).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"
)

// writerLimit stops a stand-in writer the drill failed to kill.
const writerLimit = 2 * time.Minute

func main() {
	if err := drill(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "restore-drill:", err)
		os.Exit(1)
	}
}

func drill(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: restore-drill verify|standin-writer [flags]")
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	switch args[0] {
	case "verify":
		source := fs.String("source", "", "crashed data directory")
		restored := fs.String("restored", "", "restored data directory")
		committed := fs.Int("committed", 0, "approvals the writer saw committed")
		countSQL := fs.String("count-sql", "", "query counting approved documents")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return verify(context.Background(), os.Stdout, *source, *restored, *committed, *countSQL)
	case "standin-writer":
		data := fs.String("data", "", "data directory")
		n := fs.Int("n", 0, "rows to commit before the in-flight transaction")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), writerLimit)
		defer cancel()
		if err := initDB(*data); err != nil {
			return err
		}
		return standInWriter(ctx, os.Stdout, *data, *n, 10*time.Millisecond)
	}
	return fmt.Errorf("unknown command %q", args[0])
}
