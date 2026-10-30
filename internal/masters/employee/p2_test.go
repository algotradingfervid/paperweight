package employee_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
)

// fuzzMaxOps bounds one input, so each run stays well under a second.
const fuzzMaxOps = 24

// Fuzz inputs are (op, a, b) triples over the fixture employees, indexed in
// code order, then any created during the run. Op 0 sets a's manager to b (b
// past the end clears it), 1 creates an employee reporting to b, 2 flips a
// between Active and Left.
const (
	opManager = iota
	opCreate
	opStatus
	opCount
)

// assertAcyclic walks every stored manager link: each chain must end within
// len(codes) steps without revisiting anyone.
func assertAcyclic(t *testing.T, w *world, codes []string) {
	t.Helper()
	manager := make(map[int64]int64, len(codes))
	for _, code := range codes {
		manager[w.ids[code]] = w.get(t, w.ids[code]).ManagerID
	}
	for _, code := range codes {
		seen := map[int64]bool{}
		id := w.ids[code]
		for id != 0 {
			if seen[id] {
				t.Fatalf("manager chain from %s revisits employee %d: %v", code, id, manager)
			}
			seen[id] = true
			if len(seen) > len(codes) {
				t.Fatalf("manager chain from %s is longer than %d", code, len(codes))
			}
			id = manager[id]
		}
	}
}

func FuzzEmployee_ManagerChainAcyclic(f *testing.F) { // MST-115
	f.Add([]byte{})
	f.Add([]byte{opManager, 1, 3})                                                   // MST-114's EMP-002 → EMP-004: refused
	f.Add([]byte{opManager, 0, 0})                                                   // self: refused
	f.Add([]byte{opManager, 1, 0, opManager, 0, 6})                                  // EMP-002 → EMP-001 → EMP-007: refused at the second
	f.Add([]byte{opManager, 6, 8, opManager, 3, 8, opManager, 1, 3})                 // clear EMP-007, then a chain moved away from its root
	f.Add([]byte{opCreate, 0, 6, opManager, 3, 8, opManager, 6, 8})                  // EMP-009 under EMP-007, then EMP-004 reparented to it
	f.Add([]byte{opManager, 6, 8, opManager, 4, 0, opStatus, 6, 0, opManager, 3, 6}) // EMP-007 leaves, EMP-004 can't report to a leaver
	f.Add([]byte{opStatus, 7, 0, opManager, 7, 6, opManager, 3, 7, opManager, 6, 8}) // EMP-008 re-joins into the chain
	f.Fuzz(func(t *testing.T, ops []byte) {
		w := newWorld(t)
		w.seedFixtureEmployees(t)
		codes := []string{"EMP-001", "EMP-002", "EMP-003", "EMP-004", "EMP-005", "EMP-006", "EMP-007", "EMP-008"}
		for i := 0; i+2 < len(ops) && i/3 < fuzzMaxOps; i += 3 {
			a, b := codes[int(ops[i+1])%len(codes)], int(ops[i+2])%(len(codes)+1)
			var manager int64
			if b < len(codes) {
				manager = w.ids[codes[b]]
			}
			var err error
			switch ops[i] % opCount {
			case opManager:
				err = w.update(t, a, func(in *employee.Input) { in.ManagerID = manager })
			case opCreate:
				in := w.input(fmt.Sprintf("EMP-%03d", len(codes)+1), "Fuzz Hire")
				in.ManagerID = manager
				var s mstkit.Saved
				if s, err = w.svc.Create(t.Context(), w.env.Kavya, in); err == nil {
					w.ids[in.Code], w.ins[in.Code], w.versions[in.Code] = s.ID, in, s.Version
					codes = append(codes, in.Code)
				}
			case opStatus:
				if a == "EMP-001" || a == "EMP-006" { // the Administrator's and the acting user stay enabled
					continue
				}
				err = w.update(t, a, func(in *employee.Input) {
					if in.Status == employee.Active {
						in.Status, in.Relieving = employee.Left, date(2026, time.March, 31)
					} else {
						in.Status, in.Relieving = employee.Active, date(0, 0, 0)
					}
				})
			}
			var ve *mstkit.ValidationError
			if err != nil && !errors.As(err, &ve) {
				t.Fatalf("op %d: %v; want saved or a validation error", i/3, err)
			}
			assertAcyclic(t, w, codes)
		}
	})
}
