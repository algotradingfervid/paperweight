// Command restore-drill is the Go half of scripts/restore-drill.sh: it kills
// the writer (./writer) inside an open transaction and verifies a restored
// data directory against the crashed one.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"
)

// killLimit stops the wait for a writer that never opens a transaction.
const killLimit = time.Minute

func main() {
	if err := drill(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "restore-drill:", err)
		os.Exit(1)
	}
}

func drill(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: restore-drill verify|kill-inflight [flags]")
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	switch args[0] {
	case "verify":
		source := fs.String("source", "", "crashed data directory")
		restored := fs.String("restored", "", "restored data directory")
		acks := fs.String("acks", "", "the writer's output: one line per committed approval")
		approvedSQL := fs.String("approved-sql", "", "query listing approved document ids")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		acked, err := readAcks(*acks)
		if err != nil {
			return err
		}
		return verify(context.Background(), os.Stdout, *source, *restored, acked, *approvedSQL)
	case "kill-inflight":
		acks := fs.String("acks", "", "the writer's output")
		want := fs.Int("after", 0, "acknowledgements to wait for")
		wal := fs.String("wal", "", "the database's WAL file")
		pid := fs.Int("pid", 0, "process to kill")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), killLimit)
		defer cancel()
		return killInflight(ctx, os.Stdout, *acks, *want, *wal, *pid)
	}
	return fmt.Errorf("unknown command %q", args[0])
}
