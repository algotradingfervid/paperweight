-- name: SetCopyNotes :execrows
-- Copy's notes on the draft it just created (P12, spec 24.1). Only while the
-- draft is still at version 1: 0 rows means someone saved it first, and the
-- caller skips the notes rather than write over a changed draft.
UPDATE purchase_order SET copy_notes = ? WHERE id = ? AND version = 1;
