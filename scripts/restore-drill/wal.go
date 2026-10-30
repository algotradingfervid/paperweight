package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"
)

// The WAL file format (sqlite.org/fileformat2.html, section 4.1).
const (
	walHeaderSize      = 32
	walFrameHeaderSize = 24
	walPageSizeAt      = 8
	walSaltsAt         = 16
	frameCommitAt      = 4
	frameSaltsAt       = 8
	saltsSize          = 8

	killPoll = 2 * time.Millisecond
	// stopSettle lets SIGSTOP reach every thread and any write in progress end.
	stopSettle = time.Millisecond
)

// uncommittedFrames counts the frames after the last commit frame of the WAL
// at path: pages of a transaction still open. Frames whose salts differ from
// the header's are left from before a WAL restart and end the walk.
func uncommittedFrames(path string) (int, error) {
	f, err := os.Open(path) //nolint:gosec // the drill's own temp tree
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	header := make([]byte, walHeaderSize)
	if _, err := io.ReadFull(f, header); errors.Is(err, io.EOF) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	step := walFrameHeaderSize + int64(binary.BigEndian.Uint32(header[walPageSizeAt:]))
	salts := header[walSaltsAt : walSaltsAt+saltsSize]
	frame := make([]byte, walFrameHeaderSize)
	n := 0
	for off := int64(walHeaderSize); ; off += step {
		switch _, err := f.ReadAt(frame, off); {
		case errors.Is(err, io.EOF):
			return n, nil
		case err != nil:
			return 0, err
		}
		if !bytes.Equal(frame[frameSaltsAt:frameSaltsAt+saltsSize], salts) {
			return n, nil
		}
		if binary.BigEndian.Uint32(frame[frameCommitAt:]) != 0 {
			n = 0
		} else {
			n++
		}
	}
}

// killInflight waits for want acknowledgements in the file acks, then freezes
// pid with SIGSTOP and reads the WAL, so no commit can land between the look
// and the kill. With uncommitted frames on disk it sends SIGKILL and prints how
// many, proving the kill landed inside a transaction; otherwise it resumes pid
// with SIGCONT and looks again. Only the frozen reading counts: once the writer
// is dead, Litestream may checkpoint the WAL away.
func killInflight(ctx context.Context, w io.Writer, acks string, want int, wal string, pid int) error {
	acked := 0
	for acked < want {
		body, err := os.ReadFile(acks) //nolint:gosec // the drill's own temp tree
		if err != nil {
			return err
		}
		acked = bytes.Count(body, []byte("\n"))
		if err := pause(ctx); err != nil {
			return fmt.Errorf("%d of %d acknowledgements: %w", acked, want, err)
		}
	}
	for {
		if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
			return fmt.Errorf("stop %d: %w", pid, err)
		}
		time.Sleep(stopSettle)
		n, err := uncommittedFrames(wal)
		if err != nil {
			return err
		}
		if n > 0 {
			if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
				return fmt.Errorf("kill %d: %w", pid, err)
			}
			_, err = fmt.Fprintln(w, n)
			return err
		}
		if err := syscall.Kill(pid, syscall.SIGCONT); err != nil {
			return fmt.Errorf("resume %d: %w", pid, err)
		}
		if err := pause(ctx); err != nil {
			return fmt.Errorf("no transaction reached the WAL: %w", err)
		}
	}
}

func pause(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(killPoll):
		return nil
	}
}
