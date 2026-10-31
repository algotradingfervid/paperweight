# Contract changes

Intro text that names docscreen but is not an entry.

## Round 1 (24 Oct 2026): all decided

| ID | Decision | Answers |
|---|---|---|
| CD-01 | **Shared types move to `internal/types`.** `auth.UserID` becomes an alias. | DOC 1 |
| CD-02 | **Expense docscreen gaps.** The gaps are cited as UX-E1 to UX-E7. | AD-10 |

*Gaps.*
- UX-E5: `hx-disabled-elt` through `UploadProps.AlsoDisable`. Amended by CD-310.
- UX-E6: totals render after the grand total.
| CD-03 | **csvio may import naming.** No cycle; docscreen is unaffected. | AD-11 |

## Round 2 (25 Oct 2026)

| ID | Decision | Answers |
|---|---|---|
| CD-104 | **`types` stubs return their own error.** | WP0 |
| CD-310 | **UX-E5 focus return.** When an upload ends, `upload.js` in internal/ui/js returns focus to the upload's input unless focus is in a form field outside that upload; it never leaves a field the user is typing in, and so on for long enough to pass the cut. | AD-10 |
| CD-311 | **E10 rulings.** An expense claim's `less_advance` is NonNegative; see `Validate` in internal/modules/expense/claim.go. | AD-11 |
| CD-312 | **Document link targets are checked by auth.** `docscreen.Deps.LinkRead` is for masters only; a formatter is not a form. | AD-13 |
