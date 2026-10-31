# Contract changes

Intro text that names docscreen but is not an entry.

## Round 1 (24 Oct 2026): all decided

| ID | Decision | Answers |
|---|---|---|
| CD-01 | **Shared types move to `internal/types`.** `auth.UserID` becomes an alias. | DOC 1 |
| CD-02 | **Expense docscreen gaps.** The gaps are cited as UX-E1 to UX-E7. | AD-10 |

*Gaps.*
- UX-E5: `hx-disabled-elt` through `UploadProps.AlsoDisable`.
- UX-E6: totals render after the grand total.
| CD-03 | **csvio may import naming.** No cycle; docscreen is unaffected. | AD-11 |

## Round 2 (25 Oct 2026)

| ID | Decision | Answers |
|---|---|---|
| CD-104 | **`types` stubs return their own error.** | WP0 |
