# WP1-FILE: Files and Printing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Run it under `docs/superpowers/plans/2026-10-25-execution-protocol.md` (roles, risk tier per task, review cadence; CD-96).

**Goal:** Build the kernel attachment store (`internal/kernel/files`) and the A4 print views with watermarks and amount in words (`internal/kernel/print`), plus their HTTP handlers, so that every P1 case in `docs/tests/core/05-files-print.md` passes.

**Architecture:** Files are stored once per SHA-256 under `<data_dir>/files/<2 hex>/<64 hex>`, with one `attachment` row per (parent, file). Uploads are spooled by `Store.Spool` to `files/tmp/` and hashed *outside* the write transaction into a `files.Spooled` (CD-29), then `Store.Attach` takes that `*Spooled` inside `db.Write` (checks, atomic rename, row, audit). Removing a row never deletes bytes; a GC with a 30-minute grace period does, re-checking references inside a write transaction. Printing is a registry of per-DocType templ views wrapped by a kernel-owned page shell; the shell alone decides watermark, badge, number and title from database state, so request parameters can't change them. Both packages learn about a document only through `doctype.InfoSource` (CD-05: state, number, display label, DocType label, record ref) and decide nothing about permissions themselves (`auth.Service`). Print gets company and branch details through its own `print.Letterhead` interface (CD-30). Wave 1 tests use `docfake`, `authfake` (with its CD-56 seeding helpers), `auditfake`, a `printtest` letterhead fake and `printtest` print views for the fixture DocTypes (CD-58); WP3 wires the DOC engine, AUTH and `masters/company`.

**Tech Stack:** Go 1.27.3, `a-h/templ` v0.3.1070, `gabriel-vasile/mimetype` v1.4.16, `go-chi/chi/v5` v5.5.0, `modernc.org/sqlite` v1.61.0 via `internal/platform/db`, `sqlc` v1.31.1, `pressly/goose/v3` v3.29.0, `google/go-cmp` v0.7.0. Browser tests (`chromedp`, and any PDF reader they need) belong to WP3's `e2e/` (CD-40); this WP adds neither.

**Spec:** `docs/specs/2026-10-24-core-platform-spec.md` (overrides the test file where they differ; §4.2 import table, §5 `files`, `print`, `doctype`, `auth`, `audit`, `types` formatters). Decisions: `docs/specs/contract-changes.md` (Round 1: CD-02, CD-03, CD-04, CD-05, CD-06, CD-12, CD-17, CD-29, CD-30, CD-33, CD-40, CD-41; Round 2: CD-44; Round 3: CD-53, CD-54, CD-56, CD-58, CD-63, CD-68, CD-73, CD-74, CD-76, CD-77; Round 4: CD-82, CD-85, CD-86, CD-92 apply here). Test cases: `docs/tests/core/05-files-print.md`. Fixtures: `docs/tests/core/00-fixtures.md`.

## Global Constraints

- Open-source only; latest stable versions; prefer well-maintained existing Go libraries over our own code (spec §2). Versions checked on the Go proxy 24-10-2026: mimetype v1.4.16, templ v0.3.1070, go-cmp v0.7.0.
- Work only in `internal/kernel/files/`, `internal/kernel/print/`, and migrations `migrations/00500_*.sql`–`migrations/00599_*.sql`. Branch `wp/files-print` in its own worktree (spec §9 rule 1).
- §5 contracts are frozen (CD-29, CD-30): `files.Meta{ID int64; Name, MIME string; Size int64; SHA256 string; UploadedBy auth.UserID; UploadedAt time.Time}`, `files.Spooled`, `files.Service` (`Attach(ctx, tx, p, parent auth.RecordRef, name string, f *Spooled) (AttachmentID int64, err error)`, `Remove`, `Open`, `List(ctx, p, parent) ([]Meta, error)`, `CopyAll` (copies uploaded by `to.CreatedBy`), `DeleteAll`), `print.Register(doctype string, view func(ctx context.Context, tx *db.Tx, id int64) (templ.Component, error))`, `print.InWordsINR(m types.Money) (string, error)`, `print.Letterhead{ ForBranch(ctx, tx, branchID int64) (LetterheadInfo, error) }`, `print.LetterheadInfo{ LegalName, LogoURL, BranchName string; Address []string; StateName, StateCode, GSTIN string }` (CD-58: now in §5 and WP0's stub, so this plan no longer defines its fields). Any change goes to `docs/specs/contract-changes.md` first (§9 rule 2). Extra exported names (`Store`, `Store.Spool`, `Policy`, `Registry`, …) are fine.
- Imports (§4.2 table, CD-04): `files` may import `types`, `platform/*`, `doctype`, `auth`, `audit`; `print` may import `types`, `platform/*`, `doctype`, `auth`, `files`. Never `doc`, `naming`, `approval`, `masters`. Document facts come from `doctype.InfoSource` and `doctype.Info` (CD-05); the printable DocTypes come from `doctype.All()` (CD-02).
- Subpackages (CD-85; spec §4.2 rule 5): `TestContracts_KernelImportsFollowTable` checks `files` and `print` themselves against the table above; their subpackages follow these rules instead. `files/filestest` and `print/printtest` are `<pkg>test` helpers: besides their own package and what it may import, they may import `testkit` and any fake (`filestest` imports `files`, `testkit`, `authfake`, `docfake`, `auditfake`, `platform/db` and `platform/clock`; `printtest` imports only `print`, `files`, `doctype`, `platform/*` and `testkit`). `files/web` and `print/web` are screen subpackages: they may import `platform/web` and, in their tests, the FILE area's own test helpers; neither imports `internal/ui`. `print/web/webtest` is both a `web` subpackage and a `<pkg>test` helper: it imports `print/web`, `files/web`, `printtest`, `filestest`, `auth` and `authfake`, which fits both rules because `files` and `print` are the one FILE area and `print` may import `files`.
- Typed SQL is built with `dbq.New(tx.SQL())`; `tx.SQL()` returns `db.DBTX` (CD-06).
- Audit writes go through an `audit.Writer` (CD-03): production `audit.Default`, tests `auditfake.New()`, which returns `*auditfake.Recorder` (CD-86).
- HTTP handlers read the signed-in user with `auth.PrincipalFrom(r.Context())` (CD-12); tests put one in with `auth.WithPrincipal`. With no principal they answer like AUTH's middleware (CD-17, CD-63: routes `/signin` and `/signout`, redirects are 303): browser GET → 303 `/signin?next=…`; `HX-Request`, `/api/*` or non-GET → 401 "Your session has expired. Sign in again." (htmx also gets `HX-Redirect`).
- Every write goes through `db.Write` (`BEGIN IMMEDIATE`); the packages never open their own transactions (§4.3).
- Every error that reaches a handler implements `doctype.HTTPError{ Status() int; FieldErrors() []FieldError }` (CD-44): `*files.Error` does (Task 2), and the handlers map any other error by that interface (`auth.ErrForbidden` 403, `auth.ErrNotFound` 404); `db.ErrBusy` does not implement it and maps to 503 with its own text (CD-76), before falling back to 500. Not-found is the one sentinel `doctype.ErrNotFound`; `auth.ErrNotFound` is the same value (CD-54).
- Timestamps stored as fixed-width UTC text `2026-10-24T05:00:00.000Z` (§4.3). Tests never use the wall clock: `testkit.Clock()` is fixed at 2026-10-24T05:00:00Z (10:30 IST).
- Typed SQL: own `sqlc.yaml`, queries in `<pkg>/queries/`, generated code in `<pkg>/dbq/`; templ files next to the Go that uses them; `make generate` regenerates both (§4.4).
- R3: printing needs the `print` right on the DocType **plus** visibility of the record. D-FILE-15 ("read is enough") is overridden.
- R1/R5: unnumbered documents display as `Draft {id}`; canonical names, GSTINs and numbers come from `00-fixtures.md` (`PO/26-27/00001`, Vardhan Infra Projects Pvt Ltd, branch PUNE GSTIN `27AAFCV2449G1Z7`, SHREE GSTIN `27AABCS1429B1ZU`, approver Ravi Kulkarni).
- Limits: 10,485,760 bytes per file, 20 attachments per document, logo 1,048,576 bytes, GC grace 30 minutes, display names at most 120 characters.
- Allowlist: PDF, JPG/JPEG, PNG, WEBP, HEIC/HEIF, XLSX, DOCX, CSV, checked by extension **and** content.
- Download headers: `X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox; default-src 'none'`, `Cache-Control: private, no-store`. Print page CSP: `default-src 'self'; img-src 'self' data:`.
- Print CSS: `@page { size: A4 portrait; margin: 15mm; }`. WP3's e2e tests use chromedp (spec §2), not Playwright.
- Number formatting comes only from the §5 `internal/types` formatters (CD-33), called directly with no adapter: `FormatINR` ("₹94,164.00"), `FormatMoney`, `FormatQty` ("1,200"), `FormatQtyUoM` ("1,200 kg"), `FormatRate`, `FormatPercent` ("18%"), `FormatDate` ("24-10-2026"), `FormatIST` ("24-10-2026 10:30", the only timestamp format). NUM's `types` package (NUM Tasks 1–5) merges to `main` first, as Wave 1a, and this WP rebases onto it (CD-53), so cases that go through the formatters (FILE-001's list line, FILE-030, FILE-051, FILE-065, FILE-066, FILE-069, FILE-082) are ordinary Wave 1 cases, not deferred.
- Done (spec §9 rule 3, CD-40) = every P1 case that doesn't need another WP's real implementation passes, `make lint` is clean, and `go run ./scripts/checktestids FILE --exclude-level e2e` reports nothing missing. Every `TestE2E_*` (FILE-033, 035, 056, 057, 058, 060, 070) lives in `e2e/` and is owned and run by WP3; this WP supplies the helpers (Task 14). FILE-066 passes in Wave 1 against this WP's `printtest` views for `fixture.Order`, `fixture.Receipt` and `fixture.Claim` (CD-58, Task 12).

## Review Focus

1. **A document state the print shell doesn't know** (for example a state added later). People expect a print that isn't explicitly final to carry a DRAFT watermark, not a clean "final" look. → `TestPrint_UnknownStateIsWatermarked` in Task 11.
2. **Browser PDF viewers and download managers send `Range` and `HEAD`.** People expect a large PDF to open and resume, not to fail. → `TestServe_RangeAndHead` in Task 8.
3. **A file name that contains `"`, `;`, CR or LF.** People expect the download to work with the name intact on screen, with no header injection. → `TestServe_QuoteInFileName` in Task 8.
4. **An upload form with no file part, an empty file part, or two file parts.** People expect a clear message and nothing saved. → `TestUpload_FilePartRules` in Task 7.
5. **iPhone HEIC whose major brand is `mif1` (or a `.heif` with brand `heic`).** People expect phone photos to upload whichever brand the phone wrote. → `TestSniff_HEICBrandVariants` in Task 2.

---

## Resolutions this plan applies to the test file

The spec overrides `05-files-print.md`. The test file was reconciled with the fixtures on 24 Oct 2026, with CD-73 (FILE-050 standard 403 text, FILE-026 403 per AUTH-109, one storage-failure message for FILE-015/037), CD-58 (FILE-066 over the fixture DocTypes) and CD-82 (FILE-065 shows Qty and UoM in separate cells, `05-files-print.md:490`), so every case is written exactly as the test file says. No difference remains; the earlier FILE-065 row was stale and is removed (CD-92).

## File Structure

```
migrations/00500_attachment.sql          attachment table + indexes
internal/kernel/files/
  contract.go      (WP0) §5 Meta, Spooled, Service: declarations stay; stub bodies move out
  sqlc.yaml, queries/attachment.sql, dbq/  typed SQL (generated)
  name.go          SanitizeName, Ext
  blobpath.go      BlobPath, ErrBadHash
  policy.go        Policy, DefaultPolicy, LogoPolicy, allowlist table
  sniff.go         Check (extension + magic bytes + CSV rules)
  errors.go        Kind, Error, KindOf, message constants
  meta.go          HumanSize, ListEntry
  store.go         Config, New, Store, refFrom(doctype.Info), compile-time Service check
  spool.go         Store.Spool, Spooled methods
  attach.go        Store.Attach, blob placement
  read.go          Store.Open, Store.List
  remove.go        Store.Remove, Store.DeleteAll, Store.CopyAll
  gc.go            Store.GC, GCReport
  check.go         Store.Check (P2)
  filestest/       Env: test DB, authfake (CD-56 helpers), docfake, auditfake, content builders
  web/routes.go, web/upload.go, web/download.go, web/remove.go
internal/kernel/print/
  contract.go      (WP0) §5 Register, InWordsINR, Letterhead
  words.go         InWordsINR, errors
  registry.go      Registry, Default, Register, Lookup, Missing, PrintableNames
  status.go        Status, StatusFor, SafeTitle, Header, HeaderFrom
  letterhead.go    WithLetterhead, LetterheadFor, ErrNoLetterhead (LetterheadInfo is §5, in contract.go)
  render.go        Registry.Render
  assets.go        //go:embed print.css (+ print.js in P2)
  print.css
  page.templ       Page shell, watermark
  title.templ      DocTitle (title, badge, number)
  trade.go         TradeDoc model + methods
  trade.templ      LetterheadBlock, Party, Lines, Totals, Words, Terms, Signatory, TradeDocument
  printtest/       ReferencePO, Letterhead fake, Golden helper, test parent table,
                   fixture print views for fixture.Order/Receipt/Claim (CD-58)
  testdata/print/  *.golden.html (incl. fixture_order/receipt/claim_approved)
  web/routes.go, web/print.go
  web/webtest/     Server helper for WP3's e2e tests
```

## Task order and parallelism

```
Task 0 ─┬─ Files: 1 ∥ 2 ─▶ 3 ─▶ 4 ─▶ 5 ─▶ 6 ─▶ 9
        │                       └▶ 7 (after 4, ∥ 5/6) ─▶ 8 (after 5 and 7)
        └─ Print: 10 ∥ 11 ─▶ 12 ─▶ 13 ─▶ 14 (e2e helpers for WP3)
Task 15 (P2 files) after 9; Task 16 (P2 print) after 14.
```

NUM's `types` package (Wave 1a, CD-53) is on `main` before Task 0; rebase onto it if it lands later. The files track and the print track share no files and can run as two agents at once. Inside a track, tasks marked "∥" touch disjoint files and may run as separate agents in separate worktrees; all others run in order.

---

### Task 0: Preflight against WP0 and the Round 1–3 contracts

**Files:** none (read-only checks).

**Interfaces:**
- Consumes: WP0's `internal/platform/db` (`DB`, `Tx`, `Tx.SQL() DBTX`, `Write`, `Read`), `internal/testkit` (`NewDB`, `Clock`, `Seed`, `Principal`, `DataDir`), the §5 stubs for `doctype`, `auth`, `audit`, `files`, `print`, and the fakes `authfake`, `auditfake`, `docfake`.
- Produces: confirmed facts later tasks rely on.

- [ ] **Step 1: Confirm versions are still latest**

Run: `for m in github.com/gabriel-vasile/mimetype github.com/a-h/templ; do curl -s https://proxy.golang.org/$m/@latest; echo; done`
Expected: v1.4.16, v0.3.1070. If newer stable versions exist, stop and tell the lead; don't upgrade inside a WP.

- [ ] **Step 2: Confirm the WP0 surfaces match §5**

Run: `go doc ./internal/platform/db Tx && go doc ./internal/testkit && go doc ./internal/kernel/files && go doc ./internal/kernel/print && go doc ./internal/kernel/doctype && go doc ./internal/kernel/doc/docfake && go doc ./internal/kernel/auth/authfake && go doc ./internal/kernel/audit/auditfake && grep -n "sqlc\|templ" Makefile`
Expected: `files.Service` has `Attach(…, f *Spooled)` and `List`; `files.Meta` has exactly the §5 fields; `print.Letterhead` exists and `print.LetterheadInfo` has the §5 fields `LegalName, LogoURL, BranchName, Address, StateName, StateCode, GSTIN` (CD-58); `doctype.Info`, `doctype.InfoSource`, `doctype.All`, `doctype.ErrNotFound`, `doctype.HTTPError` and the `doctype.State*` constants exist, and `auth.ErrNotFound == doctype.ErrNotFound` (CD-44, CD-54); `docfake` has `SetInfo(doctype.Info)` and `SetState` (CD-54); `authfake` has `Allow`, `Deny`, `Grant(role, doctype, right, scope)`, `AddRole(user, role)`, `Restrict(user, projects, sites)` and `Hide(doctype, id)` (CD-56); `auth.PrincipalFrom`/`WithPrincipal` exist; `go test ./internal/types/...` passes (NUM's Wave 1a formatters are real, CD-53); `make generate` covers `**/sqlc.yaml` and templ. If any §5 name differs, or the `types` tests fail, stop and add a "Requested" entry to `docs/specs/contract-changes.md` (§9 rule 2) or tell the lead.

No commit.

---

### Task 1: File names and blob paths (files track; ∥ Task 2)

**Files:**
- Create: `internal/kernel/files/name.go`, `internal/kernel/files/blobpath.go`
- Test: `internal/kernel/files/name_test.go`, `internal/kernel/files/blobpath_test.go`

**Interfaces:**
- Produces: `func SanitizeName(raw string) string`; `func Ext(name string) string` (lower-case with dot, `""` when none); `const MaxNameRunes = 120`; `func BlobPath(dataDir, hash string) (string, error)`; `var ErrBadHash = errors.New("files: bad blob hash")`.

- [ ] **Step 1: Write the failing test `TestFileName_Sanitize` (FILE-012)**

```go
func TestFileName_Sanitize(t *testing.T) {
	long := strings.Repeat("x", 300) + ".pdf"
	for _, tc := range []struct{ in, want string }{
		{"../../etc/passwd.pdf", "passwd.pdf"},
		{`C:\temp\bill.pdf`, "bill.pdf"},
		{"a/b%?#.pdf", "b___.pdf"},
		{"  .bill.pdf. ", "bill.pdf"},
		{"bi\x00ll\x1f.pdf", "bill.pdf"},
		{".pdf", "file.pdf"},
		{"RECEIPT.JPG", "RECEIPT.JPG"},
		{"बिल रसीद.jpg", "बिल रसीद.jpg"},
	} {
		if got := files.SanitizeName(tc.in); got != tc.want { t.Errorf("SanitizeName(%q) = %q, want %q", tc.in, got, tc.want) }
	}
	got := files.SanitizeName(long)
	if utf8.RuneCountInString(got) != 120 || !strings.HasSuffix(got, ".pdf") { t.Errorf("long name = %d runes %q", utf8.RuneCountInString(got), got) }
	if files.Ext("Bill.Pdf") != ".pdf" || files.Ext("README") != "" || files.Ext("bill.exe.pdf") != ".pdf" { t.Error("Ext") }
}
```

- [ ] **Step 2: Write the failing test `TestBlobPath_RejectsTraversal` (FILE-014)**

Rows `../../app.db`, `ab/../../x`, 63 hex chars, 64 upper-case hex → `errors.Is(err, files.ErrBadHash)`. Valid lower-case hash `h` → `filepath.Join(dir, "files", h[:2], h)`. Symlink sub-case: `real := t.TempDir()`; `os.Symlink(real, filepath.Join(dir, "files"))`; write a file at the returned path after `os.MkdirAll(filepath.Dir(p))`; assert it exists under `real/<h[:2]>/<h>`.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/kernel/files/ -run 'TestFileName_Sanitize|TestBlobPath_RejectsTraversal' -v`
Expected: FAIL (undefined: files.SanitizeName, files.BlobPath)

- [ ] **Step 4: Implement `SanitizeName`, `Ext` in `name.go`**

Algorithm (order matters; the tests don't fix it alone):
1. Keep the text after the last `/` or `\`.
2. Drop control runes (`< 0x20`, `0x7f`); replace each of `% ? # " ; : * < > |` with `_`.
3. Trim trailing spaces and dots.
4. Split extension at the last `.` (the ext keeps its original case for display).
5. Trim leading spaces and dots from the base; empty base becomes `file`.
6. Truncate the base by runes so base+ext ≤ 120 runes.

`Ext` returns `strings.ToLower(filepath.Ext(name))`.

- [ ] **Step 5: Implement `BlobPath` in `blobpath.go`**

Accept only `^[0-9a-f]{64}$`; otherwise `ErrBadHash`. Return `filepath.Join(dataDir, "files", hash[:2], hash)`. No `EvalSymlinks` (the path is built only from a validated hash).

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/kernel/files/ -run 'TestFileName_Sanitize|TestBlobPath_RejectsTraversal' -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/kernel/files/name.go internal/kernel/files/blobpath.go internal/kernel/files/name_test.go internal/kernel/files/blobpath_test.go
git commit -m "feat(files): sanitize display names and resolve blob paths from hashes only"
```

---

### Task 2: Type allowlist and content sniffing (files track; ∥ Task 1)

**Files:**
- Create: `internal/kernel/files/policy.go`, `internal/kernel/files/sniff.go`, `internal/kernel/files/errors.go`
- Create: `internal/kernel/files/filestest/content.go` (content builders, used again in Tasks 3–9)
- Test: `internal/kernel/files/sniff_test.go`

**Interfaces:**
- Consumes: `SanitizeName`, `Ext` (Task 1; if run in parallel, write against these signatures).
- Produces:
  - `type Policy struct{ MaxBytes int64; MaxCount int; Exts []string; ReadAnySignedIn, IgnoreState bool; TypeMessage, SizeMessage string }`
  - `var DefaultPolicy = Policy{MaxBytes: 10_485_760, MaxCount: 20, Exts: []string{".pdf", ".jpg", ".jpeg", ".png", ".webp", ".heic", ".heif", ".xlsx", ".docx", ".csv"}}`
  - `const MaxFileBytes int64 = 10_485_760`, `const MaxAttachments = 20` (used by `DefaultPolicy` and the upload handler)
  - `const SniffBytes = 64 << 10`
  - `type Checked struct{ Name, Ext, ContentType string }`
  - `func Check(pol Policy, rawName string, size int64, head []byte) (Checked, error)` — `head` is the first `min(size, SniffBytes)` bytes.
  - `type Kind int` with constants `KindTooLarge, KindEmpty, KindType, KindMismatch, KindCSVEncoding, KindLimit, KindParentMissing, KindForbidden, KindCancelled, KindRemoveLocked, KindNotFound, KindBlobMissing, KindStorage, KindDuplicate, KindInUse`
  - `type Error struct{ Kind Kind; Code int; Message string; Err error }` with `Error() string { return e.Message }`, `Unwrap()`, and `Status() int { return e.Code }` + `FieldErrors() []doctype.FieldError { return nil }`, so `*Error` satisfies `doctype.HTTPError` (CD-44, Round 2); `func KindOf(err error) Kind` (0 when not a `*Error`). The "Status" column below is `Code`.
  - `filestest`: `PDF(n int) []byte`, `JPEG(n int) []byte`, `PNG(n int) []byte`, `WEBP(n int) []byte`, `HEIC(brand string) []byte`, `XLSX() []byte`, `DOCX() []byte`, `HTML() []byte`, `PE() []byte`, `SVG() []byte`, `JPEGWithEXIF(orientation int) []byte` — each with real magic bytes, padded to `n`.

Messages and statuses (copy is exact; `%s` is the lower-case extension including the dot, or `(none)`):

| Kind | Status | Message |
|---|---|---|
| TooLarge | 413 | `File is larger than 10 MB. Choose a smaller file or compress it.` (policy `SizeMessage` when set) |
| Empty | 422 | `File is empty. Choose another file.` |
| Type | 422 | `File type %s is not allowed. Use PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or CSV.` (policy `TypeMessage` when set) |
| Mismatch | 422 | `File content does not match its %s extension. Upload the original file.` |
| CSVEncoding | 422 | `CSV files must be UTF-8. Save the file as CSV UTF-8 and try again.` |

- [ ] **Step 1: Write the failing tests**

`TestSniff_AllowedTypes` (FILE-006), table: `bill.pdf`→`application/pdf`; `r.jpg`,`r.jpeg`→`image/jpeg`; `r.png`→`image/png`; `r.webp`→`image/webp`; `r.heic` (`HEIC("heic")`)→`image/heic`; `r.heif` (`HEIC("mif1")`)→`image/heif`; `rates.xlsx`→`application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`; `quote.docx`→`application/vnd.openxmlformats-officedocument.wordprocessingml.document`; `items.csv` (`"code,name\nBOLT-M12,Hex Bolt\n"`)→`text/csv`. Assert `Checked.ContentType` exactly.

`TestAttachment_DisallowedExtensions` (FILE-007): names `setup.exe page.html logo.svg a.js docs.zip x.phtml old.xls README` with PDF content → `KindOf(err) == KindType` and message `File type .exe is not allowed. Use PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or CSV.` (ext matches row; `(none)` for `README`).

`TestAttachment_ExtensionCaseInsensitive` (FILE-008): `RECEIPT.JPG` with JPEG and `Bill.Pdf` with PDF accepted; `Checked.Name` keeps `RECEIPT.JPG` / `Bill.Pdf`.

`TestSniff_ContentExtensionMismatch` (FILE-009): `invoice.pdf` with `HTML()`, `photo.jpg` with `PNG(100)`, `items.csv` with `PE()` → `KindMismatch` with the `.pdf` / `.jpg` / `.csv` message; `photo.jpeg` with JPEG accepted.

`TestSniff_CSVTextRules` (FILE-010): `a.csv` = `"<html><body>x</body></html>"` → Mismatch; `b.csv` = `"code,name\n\xC3\x28\n"` → CSVEncoding; `c.csv` = `"\xEF\xBB\xBFcode,name\nA,B\n"` → accepted `text/csv`.

`TestAttachment_DoubleExtension` (FILE-011): `bill.pdf.exe` (PDF bytes) → Type with `.exe`; `bill.exe.pdf` → accepted.

`TestAttachment_EmptyFileRejected` (FILE-005): `blank.pdf`, size 0 → `KindEmpty`, message `File is empty. Choose another file.`

`TestSniff_HEICBrandVariants` (Review Focus 5): `.heic` with brand `mif1` and `.heif` with brand `heic` are both accepted; stored type is the detected one.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/kernel/files/ -run 'TestSniff_|TestAttachment_DisallowedExtensions|TestAttachment_ExtensionCaseInsensitive|TestAttachment_DoubleExtension|TestAttachment_EmptyFileRejected' -v`
Expected: FAIL (undefined: files.Check)

- [ ] **Step 3: Implement `Check` in `sniff.go`, the types in `policy.go` and `errors.go`**

`go get github.com/gabriel-vasile/mimetype@v1.4.16`. Order: sanitize name → ext in `pol.Exts` (else Type) → `size == 0` (Empty) → `size > pol.MaxBytes` (TooLarge) → `mt := mimetype.Detect(head)` → per-extension rule:

| Ext | Accept when | Stored type |
|---|---|---|
| `.pdf` | `mt.Is("application/pdf")` | `application/pdf` |
| `.jpg`, `.jpeg` | `mt.Is("image/jpeg")` | `image/jpeg` |
| `.png`, `.webp` | same type | same |
| `.heic`, `.heif` | `mt.Is("image/heic") \|\| mt.Is("image/heif")` (also the `-sequence` variants) | detected (without parameters) |
| `.xlsx`, `.docx` | the matching OOXML type | that type |
| `.csv` | detected is `text/plain`, `text/csv`, `text/tab-separated-values` or `application/octet-stream` **and** head has no NUL byte; then BOM-stripped head (ignoring a cut-off final rune) must be valid UTF-8, else CSVEncoding | `text/csv` |

Anything else → Mismatch. If mimetype misdetects a generated OOXML fixture, fix the fixture to Excel's entry order (`[Content_Types].xml`, `_rels/.rels`, `xl/workbook.xml` or `word/document.xml`); never loosen the rule.

- [ ] **Step 4: Run tests to verify they pass**

Run: same command as Step 2.
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/kernel/files/policy.go internal/kernel/files/sniff.go internal/kernel/files/errors.go internal/kernel/files/filestest/content.go internal/kernel/files/sniff_test.go go.mod go.sum
git commit -m "feat(files): allowlist by extension and magic bytes with CSV text rules"
```

---

### Task 3: Schema, store, spooling and attaching bytes (files track; after Tasks 1, 2)

**Files:**
- Create: `migrations/00500_attachment.sql`
- Create: `internal/kernel/files/sqlc.yaml`, `internal/kernel/files/queries/attachment.sql`, `internal/kernel/files/dbq/` (generated)
- Create: `internal/kernel/files/store.go`, `spool.go`, `attach.go`, `meta.go`
- Modify: `internal/kernel/files/contract.go` (WP0; move the `Attach`/`List`/… stub bodies out, keep the §5 declarations)
- Create: `internal/kernel/files/filestest/env.go`
- Test: `internal/kernel/files/attach_test.go`

**Interfaces:**
- Consumes: Tasks 1–2; `auth.Service`, `auth.Principal`, `auth.RecordRef`, `auth.Right`, `audit.Writer`, `audit.Entry`, `doctype.InfoSource`, `doctype.Info`, `clock.Clock`, `db.DB`, `db.Tx`, `types.FormatIST` (CD-33, called directly), `authfake`, `docfake`, `auditfake`.
- Produces:
  - `type Config struct{ DataDir string; DB *db.DB; Auth auth.Service; Info doctype.InfoSource; Clock clock.Clock; Audit audit.Writer; Policies map[string]Policy; Logger *slog.Logger }` (`Audit` nil → `audit.Default`; missing policy → `DefaultPolicy`)
  - `func New(cfg Config) (*Store, error)`: creates `files/` and `files/tmp/` with mode 0700; `var _ Service = (*Store)(nil)`
  - `func refFrom(info doctype.Info) auth.RecordRef` (unexported): `{DocType, ID, CreatedBy, EmployeeID, ProjectIDs, SiteIDs}` copied from `Info`. Permission checks always use this ref, never the caller's `RecordRef` beyond `DocType`/`ID`. `Info.State` (`""` for masters) and `Info.Display` (`PO/26-27/00001` or `Draft 42`; for a master parent it is the master display `{Code} {Name}`, CD-68) and `Info.DocLabel` feed the rules and messages.
  - §5 `Spooled` (fields unexported): `func (s *Store) Spool(r io.Reader, limit int64) (*Spooled, error)` runs **outside** any transaction (CD-29); methods `SHA256() string`, `Size() int64`, `Head() []byte`, `Cleanup() error` (idempotent; removes the temp file if still there).
  - §5 `func (s *Store) Attach(ctx context.Context, tx *db.Tx, p auth.Principal, parent auth.RecordRef, name string, f *Spooled) (int64, error)`. The local `Temp` type and the "spool an `io.Reader` inside Attach" fallback are gone.
  - §5 `Meta{ID int64; Name, MIME string; Size int64; SHA256 string; UploadedBy auth.UserID; UploadedAt time.Time}` used as is (no parent fields; tests read the parent columns with `env.Rows`).
  - `func HumanSize(n int64) string`; `func ListEntry(m Meta, uploader string) string` → `"receipt.jpg · 2.3 MB · Ravi Kulkarni · 24-10-2026 10:30"` (time via `types.FormatIST`)
  - `filestest.New(t) *Env` with fields `Store *files.Store; DB *db.DB; DataDir string; Auth *authfake.Fake; Docs *docfake.Fake; Audit *auditfake.Recorder; Clock *Clock` (`*auditfake.Recorder` is what `auditfake.New()` returns, CD-86; `Docs` uses the concrete type `docfake.New()` returns), and helpers `User(email) auth.Principal` (via `testkit.Principal`), `Doc(doctype string, id int64, state, display string, createdBy auth.UserID) auth.RecordRef` (seeds `docfake` with `SetInfo(doctype.Info{…})` (CD-54) whose `DocLabel` comes from a fixed map: `expense` → `Expense Claim`, `purchase_order` → `Purchase Order`, `grn` → `GRN`, `company` → `Company`; `DocInfo(ref, func(*doctype.Info))` edits the stored `Info`, e.g. to set `ProjectIDs`), `SetState(ref, state)`, `Attach(p, ref, name string, b []byte) (int64, error)` (`Store.Spool` outside the transaction, then `db.Write` + `Attach`), `Blobs() []string` (relative paths under `files/` excluding `tmp/`), `Rows(ref) []files.Meta` and `RowParent(id) (doctype string, parentID int64)` (test-only SQL), `DeleteRow(id int64)`. `Clock` is a settable fake implementing `clock.Clock`, starting at `testkit.Clock().Now()`, with `Set(time.Time)`. `Auth` is `*authfake.Fake` used directly (CD-56): `Allow`/`Deny` per user, `Grant`/`AddRole` for role-based setups, `Restrict(user, projects, sites)` for record rules over `Info.ProjectIDs`/`SiteIDs`, and `Hide(doctype, id)` (→ `doctype.ErrNotFound` for everyone). There is no local hiding wrapper and no local parents fake: `docfake` is the `InfoSource`.

Migration:

```sql
-- +goose Up
CREATE TABLE attachment (
  id             INTEGER PRIMARY KEY,
  parent_doctype TEXT    NOT NULL,
  parent_id      INTEGER NOT NULL,
  file_name      TEXT    NOT NULL CHECK (length(file_name) BETWEEN 1 AND 480),
  size           INTEGER NOT NULL CHECK (size > 0),
  content_type   TEXT    NOT NULL,
  sha256         TEXT    NOT NULL CHECK (length(sha256) = 64),
  uploaded_by    INTEGER NOT NULL,
  uploaded_at    TEXT    NOT NULL
) STRICT;
CREATE INDEX attachment_parent ON attachment(parent_doctype, parent_id, uploaded_at DESC);
CREATE INDEX attachment_sha    ON attachment(sha256);
-- +goose Down
DROP TABLE attachment;
```

`attachment` rows are immutable (insert and delete only), so the §4.3 `version/created_*/updated_*` columns are replaced by `uploaded_by/uploaded_at`. No foreign key to the AUTH users table (its name isn't in §5).

Queries (`queries/attachment.sql`): `InsertAttachment`, `GetAttachment`, `CountForParent`, `ListForParent` (ORDER BY `uploaded_at DESC, id DESC`), `ExistsForParentSHA`, `CountForSHA`.

- [ ] **Step 1: Write the failing test `TestAttachment_UploadJPEGToDraft` (FILE-001)**

Env: Ravi (`ravi@vardhan.test`) with `read, write` on `expense`; `exp := env.Doc("expense", 42, "draft", "Draft 42", ravi.ID)`. Attach `receipt.jpg` = `filestest.JPEG(2456789)`. Assert `env.RowParent(id) == ("expense", 42)` and one `Meta` with `Name "receipt.jpg"`, `Size 2456789`, `MIME "image/jpeg"`, `SHA256 == hex(sha256(bytes))`, `UploadedBy ravi.ID`, `UploadedAt == 2026-10-24T05:00:00Z` and the stored text is `2026-10-24T05:00:00.000Z`; `env.Blobs() == []string{sha[:2]+"/"+sha}`; `files.ListEntry(m, "Ravi Kulkarni") == "receipt.jpg · 2.3 MB · Ravi Kulkarni · 24-10-2026 10:30"`.

- [ ] **Step 2: Write the failing test `TestAttachment_SizeLimitBoundary` (FILE-002)**

`PDF(10_485_760)` → saved. `PDF(10_485_761)` → `KindTooLarge`, message `File is larger than 10 MB. Choose a smaller file or compress it.`; row count stays 1; `len(env.Blobs()) == 1`; `files/tmp` empty.

- [ ] **Step 3: Write the failing tests `TestAttachment_DedupeAcrossDocuments` (FILE-016) and `TestAttachment_BytesUnchanged` (FILE-034)**

FILE-016: same `PDF(50_000)` attached to `purchase_order` 12 (`Draft 12`) and `grn` 34 (`GRN/26-27/00034`, approved) → two rows with equal `SHA256`, `len(env.Blobs()) == 1`, and the single blob file's bytes equal the input (download parity is covered again in Task 6, FILE-018).
FILE-034: `filestest.JPEGWithEXIF(6)` (APP1 `Exif\0\0` segment with orientation 6) → blob bytes `bytes.Equal` input, and contain `"Exif\x00\x00"`.

- [ ] **Step 4: Write the failing test `TestUpload_ConcurrentSameContent` (FILE-047)**

100 iterations, each with a fresh env: two goroutines attach the same `PDF(3<<20)` to two different documents (each `Store.Spool` first, outside any transaction, then `db.Write` + `Attach`). Both return nil error, `len(env.Blobs()) == 1`, and `sha256(blob) == name`. Run with `-race`.

- [ ] **Step 5: Run tests to verify they fail**

Run: `make generate && go test ./internal/kernel/files/ -run 'TestAttachment_UploadJPEGToDraft|TestAttachment_SizeLimitBoundary|TestAttachment_DedupeAcrossDocuments|TestAttachment_BytesUnchanged|TestUpload_ConcurrentSameContent' -race -v`
Expected: FAIL (undefined: files.New)

- [ ] **Step 6: Implement `Store.Spool` and the `Spooled` methods in `spool.go`**

Create `files/tmp/up-*` with `os.CreateTemp`; `io.Copy` through `io.MultiWriter(file, sha256)` from `io.LimitReader(r, limit+1)`; keep the first `SniffBytes` in `Head`. If more than `limit` bytes arrive, remove the file and return `KindTooLarge`. Spooling never runs inside `db.Write`; `Attach` only consumes the finished `*Spooled`.

- [ ] **Step 7: Implement `Attach` (bytes path only) in `attach.go`**

Steps in this order (Task 4 inserts the rule checks marked ★):
1. ★ resolve the parent with `Info`, permission, state.
2. `Check(policy, name, f.Size(), f.Head())`.
3. ★ count and duplicate rules.
4. `placeBlob(f)`: `final := BlobPath(...)`; if `final` exists with the same size → `os.Chtimes(final, now, now)` and `f.Cleanup()`; else `fsync` the spooled file, `os.MkdirAll(dir, 0o700)`, `os.Rename(spooledPath, final)`, `fsync` the directory, `os.Chtimes(final, now, now)` (mtime follows the clock so GC grace uses the same time base). Any OS error → `KindStorage`, status 500, `Couldn't save the file. Try again or contact your administrator.`, logged with `slog` attrs `op`, `path`, `err`.
5. Insert the row (`uploaded_at` = `clock.Now().UTC()` formatted `2006-01-02T15:04:05.000Z`).
6. ★ audit.

`HumanSize`: `< 1024` → `"%d B"`; `< 1 MiB` → whole KB rounded half-up (`"512 KB"`); else MB with one decimal rounded half-up (`"2.3 MB"`).

- [ ] **Step 8: Run tests to verify they pass**

Run: same command as Step 5.
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add migrations/00500_attachment.sql internal/kernel/files/
git commit -m "feat(files): attachment table, spool-then-attach, atomic content-addressed blobs"
```

---

### Task 4: Attach rules — parent, permission, state, count, audit (files track; after Task 3)

**Files:**
- Modify: `internal/kernel/files/attach.go`, `internal/kernel/files/errors.go`, `internal/kernel/files/queries/attachment.sql`
- Test: `internal/kernel/files/attach_rules_test.go`

**Interfaces:**
- Consumes: Task 3 (`Store`, `Config.Info`, `refFrom`, `filestest.Env`).
- Produces: the ★ steps of `Attach`; audit entries through `cfg.Audit.Append(ctx, tx, audit.Entry{DocType: parent.DocType, DocID: parent.ID, Action: "attachment_added", Changes: []audit.Change{{Field: "file_name", After: name}, {Field: "size", After: "<bytes>"}, {Field: "sha256", After: hash}}, By: p.ID, At: now})`.

Rules and copy:

| Rule (in this order) | Kind / status | Message |
|---|---|---|
| `cfg.Info.Info` returns `doctype.ErrNotFound`, or `auth.Can(write)` returns `auth.ErrNotFound` (the same value, CD-54) | ParentMissing / 404 | `Save the document before attaching files.` |
| `auth.Can(write)` returns `auth.ErrForbidden` | Forbidden / 403 | `You don't have permission to add files to {Info.Display}.` |
| state `cancelled` (unless `IgnoreState`) | Cancelled / 422 | `This document is cancelled. Attach files to its amended copy instead.` |
| `Check` (Task 2) | as Task 2 | as Task 2 |
| `CountForParent >= pol.MaxCount` | Limit / 422 | `A document can have at most 20 attachments. Remove one first.` |

Permission uses `refFrom(info)`, never the caller's `RecordRef` fields beyond `DocType`/`ID`. The count check and insert share the caller's `BEGIN IMMEDIATE` transaction.

- [ ] **Step 1: Write the failing tests**

`TestAttachment_ParentMustExist` (FILE-023): attach to `expense` 999999 → `KindParentMissing`, status 404, message above; no row; the spooled temp is removed (`files/tmp` empty) and no blob (step order puts parent checks before `placeBlob`).

`TestAttachment_AddNeedsParentWrite` (FILE-024): Meena (`meena@vardhan.test`) has `read` only on `purchase_order`; doc 12 display `PO/26-27/00012` approved → `KindForbidden`, 403, `You don't have permission to add files to PO/26-27/00012.`; no row.

`TestAttachment_AddAfterApproval` (FILE-040): Suresh with `read, write` on `grn`; doc 34 `approved`, display `GRN/26-27/00034`; attach `vendor-bill.pdf` → ok; the `docfake` state is still `approved` (FILE never changes document state); `env.Audit.Entries()` has one entry with `Action "attachment_added"`, `By suresh.ID`, changes `file_name=vendor-bill.pdf`, `size`, `sha256`.

`TestAttachment_AddToCancelledBlocked` (FILE-041): `purchase_order` 12 `cancelled` → `KindCancelled`, message above.

`TestAttachment_PerDocumentLimit` (FILE-021): 20 distinct `PDF(1000+i)` attached → ok; 21st → `KindLimit`, message above; after `Store.Remove` of one (Task 5) — for this task, delete one row with a test-only query helper `env.DeleteRow(id)` — a new attach succeeds.

`TestAttachment_DedupeNoLeak` (FILE-029): Suresh (`suresh@vardhan.test`) owns `expense` 7 with `salary.pdf`; Deepak (`deepak@vardhan.test`) has `read, write` on `expense`; after Suresh's upload the test calls `env.Auth.Hide("expense", 7)` (CD-56); Deepak attaches identical bytes to his `expense` 8 → ok, row on 8 only; `Open` of Suresh's row as Deepak → `KindNotFound`. Then Deepak attaches never-seen bytes to `expense` 8 and the test compares both results: same error (nil), and the two `Meta` values differ only in `ID`, `Name`, `Size`, `SHA256`. Timing is not asserted: it is flaky, and the dedupe path differs only by a skipped rename.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/kernel/files/ -run 'TestAttachment_ParentMustExist|TestAttachment_AddNeedsParentWrite|TestAttachment_AddAfterApproval|TestAttachment_AddToCancelledBlocked|TestAttachment_PerDocumentLimit|TestAttachment_DedupeNoLeak' -v`
Expected: FAIL (rules missing: rows created where refusals are expected)

- [ ] **Step 3: Implement the ★ steps in `attach.go`**

On any refusal call `f.Cleanup()` before returning.

- [ ] **Step 4: Run tests to verify they pass, then the whole package**

Run: `go test ./internal/kernel/files/... -race`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/kernel/files/
git commit -m "feat(files): enforce parent existence, write right, state and per-document limit; audit adds"
```

---

### Task 5: Open, List, Remove, CopyAll, DeleteAll (files track; after Task 4)

**Files:**
- Create: `internal/kernel/files/read.go`, `internal/kernel/files/remove.go`
- Modify: `internal/kernel/files/queries/attachment.sql` (`DeleteAttachment`, `DeleteForParent`, `CopyRow`)
- Test: `internal/kernel/files/read_test.go`, `internal/kernel/files/remove_test.go`

**Interfaces:**
- Consumes: Tasks 3–4.
- Produces:
  - `func (s *Store) Open(ctx context.Context, p auth.Principal, id int64) (io.ReadCloser, Meta, error)` — returned reader is an `*os.File` (so handlers can type-assert `io.ReadSeeker`). Unknown id or hidden parent → `KindNotFound` 404 `File not found.`; `auth.ErrForbidden` → `KindForbidden` 403 with AUTH's text `You don't have permission to read {Label}. Ask your administrator for a role that allows it.` where `{Label}` is `Info.DocLabel` (CD-05); blob missing → `KindBlobMissing` 404 `The file is missing from storage. Ask your administrator to restore it from backup.` and the `Meta` is still returned.
  - §5 `func (s *Store) List(ctx context.Context, p auth.Principal, parent auth.RecordRef) ([]Meta, error)` (CD-29) — needs `read`; newest first.
  - `func (s *Store) Remove(ctx context.Context, tx *db.Tx, p auth.Principal, id int64) error` — needs `write`; allowed only when state is `draft`, `rejected` or `""`; else `KindRemoveLocked` 422 `Files can't be removed after the document is submitted. Cancel and amend it to change files.`; audits `attachment_removed` with the same three changes (as `Before`).
  - `func (s *Store) CopyAll(ctx context.Context, tx *db.Tx, from, to auth.RecordRef) error` — copies every row (same name, size, type, sha256), `uploaded_by = to.CreatedBy`, `uploaded_at = now` (CD-29: the user who amended, at the amendment time); one audit `attachment_added` per row with `Reason: "copied from " + Info(from).Display`. No permission check (DOC has already checked `amend`).
  - `func (s *Store) DeleteAll(ctx context.Context, tx *db.Tx, parent auth.RecordRef) error` — deletes rows only; no audit (DOC audits the deleted draft).

- [ ] **Step 1: Write the failing tests**

`TestAttachment_ReadFollowsParentRecordRule` (FILE-025): Ravi's approved `expense` 1 (`EXP/26-27/00001`, on PRJ-TECHPARK) has `receipt.jpg`; Priya has `read` on `expense` and `env.Auth.Restrict(priya.ID, []int64{prjMetro}, nil)` (her PRJ-METRO restriction; the doc's `Info.ProjectIDs` is `[prjTechpark]`) → `Open` and `List` both `KindNotFound`; Meena (Director) with `read` → `Open` ok with the same bytes, `List` returns 1.

`TestAttachment_NoOwnerBypass` (FILE-026): Deepak uploaded `receipt.jpg` to his draft `expense` 42 (`Draft 42`); his only role is gone, modelled as `env.Auth.Deny(deepak.ID, "expense", "read", "write")` → `Open` returns an error whose kind is `KindForbidden`, status 403, message `You don't have permission to read Expense Claim. Ask your administrator for a role that allows it.` (AUTH-109, CD-73) and a nil reader; then the `env.Auth.Hide("expense", 42)` variant → `KindNotFound`, 404 `File not found.`; no bytes either way.

`TestAttachment_RemoveFromDraftAudited` (FILE-038): Ravi removes `receipt.jpg` (2,456,789 bytes) from draft `expense` 42 → row gone; audit entry `Action "attachment_removed"`, `By ravi.ID`, `At 2026-10-24T05:00:00Z`, changes `file_name=receipt.jpg`, `size=2456789`, `sha256=<hash>`.

`TestAttachment_RemoveBlockedAfterSubmit` (FILE-039), table over `pending_approval, approved, closed, cancelled` → `KindRemoveLocked` with the message; `rejected` → removed.

`TestAttachment_CopiedOnAmend` (FILE-043): cancelled `purchase_order` 1 (`PO/26-27/00001`) with `quote.pdf`, `drawing.pdf` uploaded by Priya; Ravi amends it: `to := env.Doc("purchase_order", 43, "draft", "Draft 43", ravi.ID)`; `env.Clock.Set(2026-10-24T05:10:00Z)`; `CopyAll(from, to)` in `db.Write` → 2 rows on 43 with the same names, sizes, sha256; `len(env.Blobs())` unchanged; `UploadedBy == ravi.ID` (`to.CreatedBy`, not Priya), `UploadedAt == 2026-10-24T05:10:00Z`; two audit entries on 43 with `Reason "copied from PO/26-27/00001"`.

`TestAttachment_AmendCopyIndependent` (FILE-044): after the copy, remove `drawing.pdf` from 43 → `List` on 1 still has `drawing.pdf` and `Open` returns its bytes.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/kernel/files/ -run 'TestAttachment_ReadFollowsParentRecordRule|TestAttachment_NoOwnerBypass|TestAttachment_RemoveFromDraftAudited|TestAttachment_RemoveBlockedAfterSubmit|TestAttachment_CopiedOnAmend|TestAttachment_AmendCopyIndependent' -v`
Expected: FAIL (undefined: Store.Open / Store.Remove / Store.CopyAll)

- [ ] **Step 3: Implement `read.go` and `remove.go`**

`Open` and `List` use `s.db.Read`. `Open` opens the blob after the permission check; on `fs.ErrNotExist` it logs `slog.Error("attachment blob missing", "attachment_id", id, "sha256", hash)` and returns `KindBlobMissing`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/kernel/files/... -race`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/kernel/files/
git commit -m "feat(files): open, list, remove by state, copy on amend, delete with parent"
```

---

### Task 6: Garbage collection (files track; after Task 5)

**Files:**
- Create: `internal/kernel/files/gc.go`
- Test: `internal/kernel/files/gc_test.go`

**Interfaces:**
- Consumes: Tasks 3–5.
- Produces: `const GCGrace = 30 * time.Minute`; `type GCReport struct{ Deleted, Kept, TempDeleted int }`; `func (s *Store) GC(ctx context.Context, grace time.Duration) (GCReport, error)`. WP3 wires it to `app files gc` and an hourly job.

Algorithm: walk `files/??/` (skip `tmp`); for each candidate whose mtime ≤ `now - grace`, run one `db.Write`: re-`stat` the file inside the transaction (skip if mtime is now inside the grace window), `CountForSHA`, and `os.Remove` only when the count is 0. `Attach` places blobs inside its own write transaction (Task 3), so GC and Attach are serialised by `BEGIN IMMEDIATE`. Then remove `files/tmp/*` with mtime ≤ `now - grace` (no transaction needed).

- [ ] **Step 1: Write the failing tests**

`TestBlobGC_GracePeriod` (FILE-019): write three blob files directly with `BlobPath`; set mtimes with `os.Chtimes` to `now-10m` (no row), `now-31m` (no row), `now-48h` (has a row); plus `files/tmp/old` at `now-31m` and `files/tmp/new` at `now-1m`. `GC(ctx, 30*time.Minute)` → first kept, second deleted, third kept, `tmp/old` deleted, `tmp/new` kept; report `{Deleted:1, Kept:2, TempDeleted:1}`.

`TestAttachment_RemoveKeepsSharedBlob` (FILE-018): FILE-016 setup; remove the row on draft `purchase_order` 12; `GC(ctx, 0)` → blob still exists; `Open` of the GRN row returns the bytes.

`TestAttachment_DraftDeleteCascades` (FILE-042): draft `expense` 12 with `a.pdf` (unique) and `b.pdf` (also on `grn` 34); `DeleteAll` in `db.Write` → both rows gone; `GC(ctx, 0)` → `a`'s blob deleted, `b`'s kept.

`TestBlobGC_ConcurrentUploadSameContent` (FILE-020): 200 iterations: an orphan blob of `PDF(4096)` with mtime `now-1h`; concurrently `env.Attach(...)` of the same bytes and `GC(ctx, 0)`; afterwards every row's blob exists and hashes to its name. Run with `-race`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/kernel/files/ -run 'TestBlobGC_|TestAttachment_RemoveKeepsSharedBlob|TestAttachment_DraftDeleteCascades' -race -v`
Expected: FAIL (undefined: Store.GC)

- [ ] **Step 3: Implement `GC` in `gc.go`**

- [ ] **Step 4: Run tests to verify they pass**

Run: same command as Step 2, then `go test ./internal/kernel/files/... -race -count=3`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/kernel/files/gc.go internal/kernel/files/gc_test.go
git commit -m "feat(files): grace-period blob GC that re-checks references under the write lock"
```

---

### Task 7: Upload and remove handlers (files track; after Task 4; ∥ Tasks 5–6)

**Files:**
- Create: `internal/kernel/files/web/routes.go`, `internal/kernel/files/web/upload.go`, `internal/kernel/files/web/remove.go`, `internal/kernel/files/web/respond.go`
- Test: `internal/kernel/files/web/upload_test.go`

**Interfaces:**
- Consumes: `files.Store` (Spool, Attach; Remove from Task 5 for the remove route — if Task 5 isn't merged yet, wire the route in Task 8), `files.KindOf`, `*files.Error`, `filestest.Env`, `auth.PrincipalFrom` / `auth.WithPrincipal` (CD-12).
- Produces:
  - `type Config struct{ Store *files.Store; DB *db.DB; Logger *slog.Logger }` (no injected principal function: handlers call `auth.PrincipalFrom(r.Context())`, CD-12)
  - `func Routes(cfg Config) chi.Router` with `POST /attachments/{doctype}/{id}` (multipart field `file`), `POST /files/{id}/remove`, and (Task 8) `GET /files/{id}`.
  - `const MultipartOverhead int64 = 64 << 10`
  - Success: `201`, `Content-Type: application/json`, body `{"id":<int>,"file_name":"…","size":<int>,"content_type":"…"}`. Errors: `*files.Error` status, `text/plain; charset=utf-8`, body = `Message`. Any other error implementing `doctype.HTTPError` (CD-44) uses its `Status()` and `Error()`; `errors.Is(err, db.ErrBusy)` → 503 with its text (CD-76; `db` doesn't implement `HTTPError`). Unknown errors → 500 with the storage message, logged.
  - Signed out (no principal in the context; CD-17 behaviour): browser GET → `303 Location: /signin?next=<url.QueryEscape(r.URL.RequestURI())>`; `HX-Request: true`, `/api/*` or any non-GET (uploads, removes) → `401` with body `Your session has expired. Sign in again.` (htmx also gets `HX-Redirect: /signin?next=…`). Tests put a principal in with `auth.WithPrincipal(req.Context(), p)`.

- [ ] **Step 1: Write the failing tests**

`TestUpload_DeclaredSizeTooLarge` (FILE-003): request with `ContentLength = 52428800` and a body that counts bytes read → 413, body `File is larger than 10 MB. Choose a smaller file or compress it.`, bytes read == 0, `files/tmp` empty.

`TestUpload_StreamedOverLimitCleansTemp` (FILE-004): `io.Pipe` body, `ContentLength = -1`, multipart part streaming 12 MB → 413 with the same message; `files/tmp` empty; no row.

`TestUpload_DBFailureLeavesNoRow` (FILE-015): add trigger `CREATE TRIGGER fail_attach BEFORE INSERT ON attachment BEGIN SELECT RAISE(ABORT, 'forced'); END;` via `db.Write`; upload `bill.pdf` → 500, body `Couldn't save the file. Try again or contact your administrator.`; no row; `files/tmp` empty; any file at the final blob path hashes to its name (complete, never partial).

`TestUpload_StorageWriteError` (FILE-037, same message as FILE-015 per CD-73; `t.Skip` when `os.Geteuid() == 0`): `os.Chmod(filepath.Join(dataDir, "files"), 0o555)` after `New` (tmp stays writable) → 500 with the same message; no row; `files/tmp` empty; captured `slog` JSON has `"err"` with `permission denied`.

`TestUpload_FilePartRules` (Review Focus 4): no `file` part → 422 `Choose a file to upload.`; two `file` parts → 422 `Upload one file at a time.` and no row; empty `file` part → 422 `File is empty. Choose another file.`

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/kernel/files/web/ -run 'TestUpload_' -v`
Expected: FAIL (undefined: web.Routes)

- [ ] **Step 3: Implement the upload handler in `upload.go`**

Order: `auth.PrincipalFrom(r.Context())` (else 401, since uploads are POSTs) → parse `{id}` as positive int64 (else 404 `File not found.`) → `r.ContentLength > files.MaxFileBytes+MultipartOverhead` → 413 without reading → `r.Body = http.MaxBytesReader(w, r.Body, files.MaxFileBytes+MultipartOverhead)` → `r.MultipartReader()` → first part with `FormName() == "file"` → `f, err := Store.Spool(part, files.MaxFileBytes)` (outside the transaction, CD-29); `defer f.Cleanup()` → read on to EOF to reject a second `file` part → `cfg.DB.Write(ctx, func(tx) { id, err = Store.Attach(ctx, tx, p, auth.RecordRef{DocType: doctype, ID: id}, part.FileName(), f) })` → respond. A `*http.MaxBytesError` maps to 413. 

- [ ] **Step 4: Implement `POST /files/{id}/remove` in `remove.go`**

`cfg.DB.Write` → `Store.Remove`; success `204`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/kernel/files/web/ -race -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/kernel/files/web/
git commit -m "feat(files/web): streaming upload with size cut-off, temp cleanup and remove route"
```

---

### Task 8: Download handler (files track; after Tasks 5 and 7)

**Files:**
- Create: `internal/kernel/files/web/download.go`
- Modify: `internal/kernel/files/web/routes.go` (mount `GET /files/{id}`)
- Test: `internal/kernel/files/web/download_test.go`

**Interfaces:**
- Consumes: `Store.Open` (Task 5), `Config`/`Routes` (Task 7).
- Produces: `GET /files/{id}[?download=1]`; `func contentDisposition(kind, name string) string` (unexported).

Headers on 200: `Content-Type` = stored `Meta.MIME` (never from the name), `X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox; default-src 'none'`, `Cache-Control: private, no-store`, `Content-Length` = stored size, `Content-Disposition` = `inline` for `application/pdf`, `image/jpeg`, `image/png`, `image/webp`, else `attachment`; `?download=1` forces `attachment`. Disposition value: `{kind}; filename="{ascii}"` where `ascii` replaces every non-ASCII rune and `"`, `\`, `;`, CR, LF with `_`, plus `; filename*=UTF-8''{url.PathEscape(name)}` when `ascii != name`. Serve with `http.ServeContent(w, r, "", modtime, file)` so `Range` and `HEAD` work. Error bodies never contain the sha256.

- [ ] **Step 1: Write the failing tests**

`TestServe_RequiresLogin` (FILE-027): no principal → `303`, `Location: /signin?next=%2Ffiles%2F1`; with `HX-Request: true` → 401 `Your session has expired. Sign in again.`; body has none of the file bytes. `GET /files/ab/<sha>` and `GET /files/` → 404 (no static route).

`TestServe_IDORAndHashGuess` (FILE-028): Priya can read attachment A (doc 41) but doc 42 is hidden (`env.Auth.Hide("expense", 42)`); `GET /files/{B}`, `GET /files/<sha of B>`, `GET /files/{A}/../{B}` (raw path) → 404 `File not found.`; for the allowed `GET /files/{A}`, no header value or body contains B's or A's sha256.

`TestServe_SecurityHeaders` (FILE-031): `bill.pdf` → exactly the headers above, `Content-Length` = stored size.

`TestServe_InlineVsAttachment` (FILE-032): table over `bill.pdf r.jpg r.png r.webp` → `inline; filename="…"`; `r.heic rates.xlsx quote.docx items.csv` → `attachment; filename="…"`; every row with `?download=1` → `attachment`.

`TestServe_MissingBlob` (FILE-036): delete the blob file → 404, body `The file is missing from storage. Ask your administrator to restore it from backup.`; captured log line has `attachment_id` and `sha256`.

`TestServe_RangeAndHead` (Review Focus 2): `Range: bytes=0-99` → 206 with 100 bytes and the same security headers; `HEAD` → 200, no body, `Content-Length` = size.

`TestServe_QuoteInFileName` (Review Focus 3): stored name `bill "final";x.pdf` (stored as sanitised by Task 1, i.e. `bill _final__x.pdf`) and a direct row insert with `a"b\r\nc.pdf` → header is a single line, contains `filename="a_b__c.pdf"` and a `filename*=` part; response is 200.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/kernel/files/web/ -run 'TestServe_' -v`
Expected: FAIL (404 for every GET: route not mounted)

- [ ] **Step 3: Implement `download.go` and mount the route**

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/kernel/files/... -race`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/kernel/files/web/
git commit -m "feat(files/web): private downloads with safe headers, inline rules and range support"
```

---

### Task 9: Company logo policy (files track; after Task 8)

**Files:**
- Modify: `internal/kernel/files/policy.go` (add `LogoPolicy`), `internal/kernel/files/read.go` (honour `ReadAnySignedIn`)
- Test: `internal/kernel/files/logo_test.go`

**Interfaces:**
- Consumes: Tasks 3–8.
- Produces: `var LogoPolicy = Policy{MaxBytes: 1_048_576, MaxCount: 20, Exts: []string{".png", ".jpg", ".jpeg"}, ReadAnySignedIn: true, IgnoreState: true, TypeMessage: "Logo must be a PNG or JPG image.", SizeMessage: "Logo must be 1 MB or smaller."}`. WP2/WP3 register it as `Config.Policies["company"] = files.LogoPolicy`; the print letterhead links the logo as `/files/{id}`.

- [ ] **Step 1: Write the failing test `TestCompanyLogo_Rules` (FILE-048)**

Env with `Policies: {"company": files.LogoPolicy}`; Kavya (`kavya@vardhan.test`) has `read, write` on `company`; `company` 1, state `""`. `logo.svg` (SVG bytes) → `KindType`, `Logo must be a PNG or JPG image.`; `logo.png` = `PNG(1_572_864)` → `KindTooLarge`, `Logo must be 1 MB or smaller.`; `logo.png` = `PNG(184_320)` → ok. Deepak (no `company` rights) → `Open` ok; through `web.Routes` with no principal → 303 to `/signin?next=%2Ffiles%2F{id}`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/kernel/files/... -run TestCompanyLogo_Rules -v`
Expected: FAIL (undefined: files.LogoPolicy)

- [ ] **Step 3: Implement `LogoPolicy`; in `Open`, skip the `read` check when the parent DocType's policy has `ReadAnySignedIn`**

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/kernel/files/... -race && go run ./scripts/checktestids FILE --exclude-level e2e --report`
Expected: PASS; the checker lists only print and P2 IDs as missing.

- [ ] **Step 5: Commit**

```bash
git add internal/kernel/files/
git commit -m "feat(files): company logo policy readable by any signed-in user"
```

---

### Task 10: Amount in words (print track; ∥ Task 11)

**Files:**
- Create: `internal/kernel/print/words.go`
- Test: `internal/kernel/print/words_test.go`, `internal/kernel/print/words_fuzz_test.go`

**Interfaces:**
- Consumes: `types.Money` (§5).
- Produces: `func InWordsINR(m types.Money) (string, error)`; `var ErrNegativeAmount = errors.New("Amount in words needs an amount of 0 or more.")`; `var ErrAmountTooLarge = errors.New("Amount is too large to write in words.")`; `const MaxWordsAmount types.Money = 99_999_999_999_999` (10^14 − 1 paise).

- [ ] **Step 1: Write the failing tests for FILE-072 to FILE-078** (one function per case, as the test file names them; each is a table of paise → words)

```go
func TestInWordsINR(t *testing.T) // FILE-072, rows: {"brief example", 1411280, "Rupees Fourteen Thousand One Hundred Twelve and Paise Eighty Only"},
	// supporting row {"max", 99999999999999, "Rupees Ninety Nine Thousand Nine Hundred Ninety Nine Crore Ninety Nine Lakh Ninety Nine Thousand Nine Hundred Ninety Nine and Paise Ninety Nine Only"}
func TestInWordsINR_ZeroAndPaiseOnly(t *testing.T) // FILE-073: 0 → "Rupees Zero Only"; 1, 5, 10, 50, 80, 99 → "Paise One Only", "Paise Five Only", "Paise Ten Only", "Paise Fifty Only", "Paise Eighty Only", "Paise Ninety Nine Only"
func TestInWordsINR_WholeRupees(t *testing.T)      // FILE-074: 100, 2100, 500000 → "Rupees One Only", "Rupees Twenty One Only", "Rupees Five Thousand Only"
func TestInWordsINR_BelowThousand(t *testing.T)    // FILE-075: rupees 11 15 19 20 40 99 100 101 110 115 999 (×100 paise) → Eleven, Fifteen, Nineteen, Twenty, Forty,
	// Ninety Nine, One Hundred, One Hundred One, One Hundred Ten, One Hundred Fifteen, Nine Hundred Ninety Nine
func TestInWordsINR_Thousands(t *testing.T)        // FILE-076: rupees 1000 1001 1010 10000 99999 → One Thousand, One Thousand One, One Thousand Ten, Ten Thousand, Ninety Nine Thousand Nine Hundred Ninety Nine
func TestInWordsINR_Lakh(t *testing.T)             // FILE-077: rupees 100000 100001 110000 1234567 9999999 → One Lakh, One Lakh One, One Lakh Ten Thousand,
	// Twelve Lakh Thirty Four Thousand Five Hundred Sixty Seven, Ninety Nine Lakh Ninety Nine Thousand Nine Hundred Ninety Nine
func TestInWordsINR_Crore(t *testing.T)            // FILE-078: 1000000000 → "Rupees One Crore Only";
	// 1234567890 → "Rupees One Crore Twenty Three Lakh Forty Five Thousand Six Hundred Seventy Eight and Paise Ninety Only";
	// 100000000000 → "Rupees One Hundred Crore Only";
	// 9999999999999 → "Rupees Nine Thousand Nine Hundred Ninety Nine Crore Ninety Nine Lakh Ninety Nine Thousand Nine Hundred Ninety Nine and Paise Ninety Nine Only"
```

Every FILE-075/076/077 row is `"Rupees " + words + " Only"`. Every function also asserts no output contains `"Million"`, `"-"` or `"Fourty"` (shared helper `checkShape(t, s)`).

- [ ] **Step 2: Write the failing tests `TestInWordsINR_Errors` (FILE-079) and `TestInWordsINR_NoFloatDrift` (FILE-080)**

Errors: `-100` → `errors.Is(err, ErrNegativeAmount)` and `err.Error() == "Amount in words needs an amount of 0 or more."`; `100_000_000_000_000` → `ErrAmountTooLarge`, `"Amount is too large to write in words."`; `99_999_999_999_999` → no error.
Drift: `28`/`29` → `"Paise Twenty Eight Only"` / `"Paise Twenty Nine Only"`; `1411279` and `1411280` differ.

- [ ] **Step 3: Write the failing fuzz test `FuzzInWordsINR_RoundTrip` (FILE-081)**

Seeds: 0, 1, 99, 100, 1411280, 1234567890, 99999999999999. For `m := types.Money(abs(x) % 1e14)`: result starts with `"Rupees "` or `"Paise "`, ends with `" Only"`, has no `"  "`, no `"-"`, every word starts upper-case, `strings.Count(s, " and Paise ") <= 1`, and `parseWordsINR(s) == m`. `parseWordsINR` lives in the test file: split on `" and Paise "`; parse each side as a sequence of number words with multipliers Hundred=100, Thousand=1e3, Lakh=1e5, Crore=1e7, where a multiplier ≥ Thousand closes the current group (Crore multiplies everything accumulated before it); paise-only strings start with `"Paise "`.

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/kernel/print/ -run 'TestInWordsINR' -v`
Expected: FAIL (InWordsINR stub returns ErrNotImplemented)

- [ ] **Step 5: Implement `InWordsINR` in `words.go`**

`words(n)`: `n >= 1e7` → `words(n/1e7) + " Crore"` + rest; then Lakh (1e5), Thousand (1e3), Hundred (100); below 100 from a 0–19 table and tens table (`Forty`). Rupee part 0 with paise > 0 → only `Paise … Only`; paise 0 → drop the paise clause.

- [ ] **Step 6: Run tests to verify they pass, then fuzz briefly**

Run: `go test ./internal/kernel/print/ -run 'TestInWordsINR' -v && go test ./internal/kernel/print/ -run '^$' -fuzz FuzzInWordsINR_RoundTrip -fuzztime 30s`
Expected: PASS; fuzz finds no failures.

- [ ] **Step 7: Commit**

```bash
git add internal/kernel/print/words.go internal/kernel/print/words_test.go internal/kernel/print/words_fuzz_test.go
git commit -m "feat(print): InWordsINR with Indian lakh/crore scale"
```

---

### Task 11: Print registry, page shell, status, watermark and title (print track; ∥ Task 10)

**Files:**
- Create: `internal/kernel/print/registry.go`, `status.go`, `render.go`, `assets.go`, `print.css`, `page.templ`, `title.templ`
- Modify: `internal/kernel/print/contract.go` (WP0; move the `Register` stub body into `registry.go`)
- Create: `internal/kernel/print/printtest/render.go`
- Test: `internal/kernel/print/render_test.go`, `internal/kernel/print/status_test.go`, `internal/kernel/print/registry_test.go`

**Interfaces:**
- Consumes: `templ.Component`, `db.Tx`, `doctype.Info` (CD-05), `doctype.DocType` (CD-02).
- Produces:
  - `type View = func(ctx context.Context, tx *db.Tx, id int64) (templ.Component, error)`
  - `type Registry struct{…}`; `func NewRegistry() *Registry`; `var Default = NewRegistry()`; `func Register(doctype string, view View)` (= `Default.Register`; panics on empty name, nil view or duplicate); `func (r *Registry) Register(doctype string, view View)`; `func (r *Registry) Lookup(doctype string) (View, bool)`; `func (r *Registry) Missing(printable []string) error` — `errors.Join` of `fmt.Errorf("No print template for DocType %s.", name)` for each name without a view, nil if none; `func PrintableNames(dts []*doctype.DocType) []string` — names of those with `Printable`, in the given order (FILE-066 passes `doctype.All()`).
  - `type Status struct{ Code, Label string }`; `func StatusFor(state string) Status` — `approved`, `closed` → `{"", ""}`; `cancelled` → `{"cancelled", "CANCELLED"}`; every other value (including `draft`, `pending_approval`, `rejected`, unknown) → `{"draft", "DRAFT"}`.
  - `func SafeTitle(s string) string` — each of `/ \ % ? #` → `_`.
  - `type Header struct{ Status Status; Number, Title string }`; `func HeaderFrom(ctx context.Context) Header`.
  - `var ErrNoTemplate = errors.New("print: no template")`. The local `DocInfo`, `InfoSource`, `ErrNoDocument` and `DisplayNumber` are gone: document facts are `doctype.Info` from `doctype.InfoSource` (CD-05), whose `Display` is already `PO/26-27/00001` or `Draft 42`.
  - `func (r *Registry) Render(ctx context.Context, tx *db.Tx, w io.Writer, info doctype.Info) error` — looks up `info.DocType`, builds `Header{StatusFor(string(info.State)), info.Display, SafeTitle(info.Display)}`, puts it in `ctx`, calls the view with `info.ID`, renders `Page(header, body)`.
  - `var CSS string` (`//go:embed print.css`).
  - templ: `Page(h Header, body templ.Component)`; `DocTitle(title string)` renders `<h1 class="doc-title">{title}` + (when `Status.Code != ""`) `<span class="badge" data-status="{code}">{label}</span>` + `</h1><p class="doc-number">{number}</p>`, reading `HeaderFrom(ctx)`.
  - `printtest.RenderString(t, reg *print.Registry, info doctype.Info) string`. Tests that need an `InfoSource` use `docfake`.

Page shell (exact parts the tests check): `<!doctype html>`, `<html lang="en">`, `<title>{h.Title}</title>`, `<link rel="stylesheet" href="/print/assets/print.css">`, and when `Status.Code != ""` the first body child `<div class="watermark" data-status="{code}">{label}</div>`. No inline `<style>` or `<script>` (the CSP forbids them).

Branding (CD-99): the page head also links `/print/assets/brand.css`, served by the same router from `brand.PrintCSS(brand.Load())` (`internal/platform/brand`, WP0 Task 7b), and `print.css` colours headings, rules and the totals band with `var(--doc-accent)`; it never hard-codes a brand colour. `print.css` must contain these lines verbatim: `@page { size: A4 portrait; margin: 15mm; }`, `.watermark { position: fixed;` (with `transform: rotate(-30deg)` and `pointer-events: none`), `thead { display: table-header-group; }`, `tr { break-inside: avoid; }`, `.closing { break-inside: avoid; }`, `img { image-orientation: from-image; }`.

- [ ] **Step 1: Write the failing tests**

`TestPrint_TitleIsSafeNumber` (FILE-061): rendering approved `Info{Display: "PO/26-27/00001"}` gives `<title>PO_26-27_00001</title>`, approved amendment `PO/26-27/00001-1` gives `<title>PO_26-27_00001-1</title>`, draft `Info{ID: 42, Display: "Draft 42"}` gives `<title>Draft 42</title>`; `SafeTitle(`a\b%c?d#e`) == "a_b_c_d_e"`.

`TestPrintCSS_A4` (supporting, no case ID; feeds WP3's FILE-056, CD-77): `strings.Contains(print.CSS, "@page { size: A4 portrait; margin: 15mm; }")` and the other verbatim lines above.

`TestPrint_DraftWatermark` (FILE-052), table over `draft`, `pending_approval`, `rejected`, view = `DocTitle("Purchase Order")`, `doctype.Info{DocType: "test.PurchaseOrder", ID: 42, Number: "", Display: "Draft 42"}` → HTML contains `<div class="watermark" data-status="draft">DRAFT</div>`, `<span class="badge" data-status="draft">DRAFT</span>`, `Draft 42`, and no `PO/26-27/`.

`TestPrint_CancelledWatermark` (FILE-053): `cancelled`, number and display `PO/26-27/00001` → `<div class="watermark" data-status="cancelled">CANCELLED</div>` and the CANCELLED badge.

`TestPrint_NoWatermarkWhenFinal` (FILE-054): `approved`, `closed` → no `class="watermark"`, no `DRAFT`, no `CANCELLED`.

`TestPrint_UnknownStateIsWatermarked` (Review Focus 1): state `"archived"` and `""` → DRAFT watermark.

Registry: `Register` duplicate and nil view panic (`TestRegistry_Register`); `Missing` and `PrintableNames` over hand-built `[]*doctype.DocType{{Name: "test.PurchaseOrder", Printable: true}, {Name: "test.NoTemplate", Printable: true}, {Name: "test.Master"}}` → names `["test.PurchaseOrder", "test.NoTemplate"]`, error `No print template for DocType test.NoTemplate.`, and nil once a view is registered (`TestRegistry_Missing`). Both are supporting tests with no case ID; they let Wave 1 prove FILE-066's logic without DOC's registry.

- [ ] **Step 2: Run tests to verify they fail**

Run: `make generate && go test ./internal/kernel/print/ -run 'TestPrint_TitleIsSafeNumber|TestPrintCSS_A4|TestPrint_DraftWatermark|TestPrint_CancelledWatermark|TestPrint_NoWatermarkWhenFinal|TestPrint_UnknownStateIsWatermarked|TestRegistry_' -v`
Expected: FAIL (undefined: print.NewRegistry)

- [ ] **Step 3: Implement the files listed above; `go get github.com/a-h/templ@v0.3.1070`**

- [ ] **Step 4: Run tests to verify they pass**

Run: same command as Step 2.
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/kernel/print/ go.mod go.sum
git commit -m "feat(print): registry, A4 page shell, state-driven watermark, badge and safe title"
```

---

### Task 12: Letterhead, the standard purchase-document template and the fixture print views with golden prints (print track; after Tasks 10, 11)

**Files:**
- Create: `internal/kernel/print/letterhead.go`, `internal/kernel/print/trade.go`, `internal/kernel/print/trade.templ`
- Create: `internal/kernel/print/printtest/reference.go`, `internal/kernel/print/printtest/letterhead.go`, `internal/kernel/print/printtest/golden.go`, `internal/kernel/print/printtest/fixtures.go`
- Create: `internal/kernel/print/testdata/print/po_approved.golden.html`, `fixture_order_approved.golden.html`, `fixture_receipt_approved.golden.html`, `fixture_claim_approved.golden.html`
- Test: `internal/kernel/print/trade_test.go`, `internal/kernel/print/letterhead_test.go`, `internal/kernel/print/alltypes_test.go` (package `print_test`)

**Interfaces:**
- Consumes: Tasks 10–11; the §5 formatters (CD-33) called directly: `types.FormatINR`, `types.FormatQty`, `types.FormatRate`, `types.FormatPercent`, `types.FormatDate`, `types.FormatIST`. No `format.go` adapter. They are real on `main` (Wave 1a, CD-53), so FILE-051, FILE-065, FILE-066 and FILE-082 go green in this task.
- Produces (module print views — PO, GRN, Expense — build these models and call `TradeDocument`):
  - Letterhead (CD-30, CD-58). §5 declares `type Letterhead interface{ ForBranch(ctx context.Context, tx *db.Tx, branchID int64) (LetterheadInfo, error) }`, implemented by `masters/company` (MST) and wired by `internal/app`, and `type LetterheadInfo struct{ LegalName, LogoURL, BranchName string; Address []string; StateName, StateCode, GSTIN string }` (in WP0's stub; this plan doesn't touch it). Meanings used here: `LogoURL` is `"/files/{id}"` or `""`; `StateName`/`StateCode` e.g. `"Maharashtra"`/`"27"`; `GSTIN` is `""` when the branch has none.
  - `func WithLetterhead(ctx context.Context, l Letterhead) context.Context`; `func LetterheadFor(ctx context.Context, tx *db.Tx, branchID int64) (LetterheadInfo, error)` — what a view calls; `var ErrNoLetterhead = errors.New("print: no letterhead source")` when the context has none. The print handler (Task 13) puts `Config.Letterhead` into the context, so views keep the §5 `print.Register` signature.
  - `printtest.Letterhead` — map-backed fake implementing `print.Letterhead`, seeded from `00-fixtures.md`: branch PUNE → `{LegalName: "Vardhan Infra Projects Pvt Ltd", LogoURL: "/files/1", BranchName: "Pune Works", Address: {"Pune, Maharashtra"}, StateName: "Maharashtra", StateCode: "27", GSTIN: "27AAFCV2449G1Z7"}`; branch BLR → `{…, BranchName: "Bengaluru Site Office", Address: {"Bengaluru, Karnataka"}, StateName: "Karnataka", StateCode: "29", GSTIN: "29AAFCV2449G1Z3"}`.
  - `type Party struct{ Label, Name string; Address []string; GSTIN string }` (`Label` e.g. `"Vendor"`; empty `GSTIN` prints `GSTIN: Unregistered`)
  - `type TradeLine struct{ Sr int; Item, Description, HSN string; Qty types.Qty; UoM string; Rate types.Rate; Amount types.Money }`
  - `type TaxRow struct{ Name string; Rate types.Percent; Amount types.Money }` (Name `CGST`, `SGST`, `IGST`)
  - `type TradeDoc struct{ Title string; Letterhead LetterheadInfo; Date types.Date; Party Party; Lines []TradeLine; Taxable types.Money; Taxes []TaxRow; GrandTotal, RoundedTotal types.Money; HasRoundedTotal bool; Terms string; ApprovedBy string; ApprovedAt time.Time; AmendsNumber string }`
  - `func (d TradeDoc) PayableTotal() types.Money` (RoundedTotal when `HasRoundedTotal`, else GrandTotal); `func (d TradeDoc) AmountInWords() (string, error)` = `InWordsINR(d.PayableTotal())`
  - templ `TradeDocument(d TradeDoc)`: letterhead block (`<img class="logo" src=…>` only when `LogoURL != ""`; branch name and address; `{StateName} ({StateCode})`; `GSTIN: …` line only when `GSTIN != ""`), `DocTitle(d.Title)`, `Date: dd-mm-yyyy`, party block, lines table with header `Sr | Item | HSN | Qty | UoM | Rate | Amount` inside `<thead>`, a `No items` row when empty, `Taxable Value`, one row per tax `"{Name} {pct}"`, `Grand Total ₹…`, `Rounded Total ₹…` only when `HasRoundedTotal && RoundedTotal != GrandTotal`, the words, `Terms` heading only when non-empty, and `<div class="closing">` with `For {LegalName}` / `Authorised Signatory`.
  - `printtest.ReferencePO() print.TradeDoc` — fixtures' reference PO: Letterhead = `printtest.Letterhead`'s PUNE value; Party `{Label: "Vendor", Name: "Shree Steel Traders", Address: {"Mumbai, Maharashtra"}, GSTIN: "27AABCS1429B1ZU"}`; Date 2026-10-24; lines `{1, "MS-ANGLE-50 MS Angle 50x50x5", "", "7216", 1_200_000, "kg", 625_000, 7_500_000}`, `{2, "WIRE-18G Binding Wire 18G", "", "7217", 50_000, "kg", 960_000, 480_000}` (the Item cell is the master display `{Code} {Name}`, CD-68; the party block keeps the name and address saved on the document, FILE-063); Taxable 7_980_000; taxes CGST 9000 → 718_200, SGST 9000 → 718_200; Grand 9_416_400; Rounded 9_416_400, `HasRoundedTotal: true`; Terms `"Delivery within 7 days of PO date."`; Title `"Purchase Order"`.
  - `printtest.Golden(t, name string, got []byte)` — compares to `testdata/print/<name>`; `-update` flag rewrites it.
  - Fixture print views (CD-58), in `printtest/fixtures.go`: `var FixtureDocTypes = []string{"fixture.Order", "fixture.Receipt", "fixture.Claim"}` (the names DOC's `internal/fixture` registers; string constants, because FILE may not import that package and it isn't on `main` in Wave 1); `func RegisterFixtureViews(r *print.Registry)` (idempotent via `sync.Once` when `r == print.Default`) registers one view per name. The views read no tables: `fixture.Order` renders `TradeDocument(ReferencePO())`; `fixture.Receipt` renders `TradeDocument(ReferenceReceipt())` (Title `"Goods Receipt"`, the two reference lines with the received quantities, no taxes); `fixture.Claim` renders `TradeDocument(ReferenceClaim())` (Title `"Expense Claim"`, lines `TRAVEL` ₹1,250.00 and `FOOD` ₹480.50, no taxes, `HasRoundedTotal: true`). Each uses `LetterheadFor(ctx, tx, puneID)`.

- [ ] **Step 1: Write the failing tests `TestLetterhead_FromContext` (supporting) and `TestPrint_PurchaseOrderGolden` (FILE-051)**

`TestLetterhead_FromContext`: `LetterheadFor(WithLetterhead(ctx, printtest.Letterhead), tx, blrID)` returns GSTIN `29AAFCV2449G1Z3`; a bare context → `errors.Is(err, ErrNoLetterhead)`.

FILE-051: registry with `"test.PurchaseOrder"` → a view that calls `LetterheadFor(ctx, tx, puneID)` and renders `TradeDocument(printtest.ReferencePO())` with that letterhead; `doctype.Info{DocType: "test.PurchaseOrder", ID: 1, State: "approved", Number: "PO/26-27/00001", Display: "PO/26-27/00001"}`. Golden `po_approved.golden.html`. Plus `strings.Contains` for: `src="/files/1"`, `Vardhan Infra Projects Pvt Ltd`, `Pune Works`, `GSTIN: 27AAFCV2449G1Z7`, `Purchase Order`, `PO/26-27/00001`, `Date: 24-10-2026`, `Shree Steel Traders`, `MS-ANGLE-50 MS Angle 50x50x5`, `GSTIN: 27AABCS1429B1ZU`, each header `Sr Item HSN Qty UoM Rate Amount`, `Maharashtra (27)`, `7216`, `7217`, `₹75,000.00`, `₹4,800.00`, `CGST 9%`, `SGST 9%`, `₹7,182.00`, `Grand Total ₹94,164.00`, `Rupees Ninety Four Thousand One Hundred Sixty Four Only`, `Delivery within 7 days`, `For Vardhan Infra Projects Pvt Ltd`, `Authorised Signatory`.

- [ ] **Step 2: Write the failing tests `TestPrint_EscapesUserContent` (FILE-059) and `TestPrint_OptionalPartsOmitted` (FILE-064)**

Escaping: Party.Name `<script>alert(1)</script>`, line Description `"><img src=x onerror=alert(1)>`, Terms `{{.Secret}}` → contains `&lt;script&gt;alert(1)&lt;/script&gt;`, does not contain `onerror=` outside escaped text (assert `!strings.Contains(html, "<img src=x")`), contains `{{.Secret}}`.
Optional: `LogoURL ""`, letterhead `GSTIN ""`, party `GSTIN ""`, `Terms ""` → no `class="logo"`, letterhead has no `GSTIN:` (assert the only `GSTIN:` occurrence is `GSTIN: Unregistered`), no `Terms` heading.

- [ ] **Step 3: Write the failing tests `TestPrint_UsesNUMFormatting` (FILE-065) and `TestPrint_WordsUsePayableTotal` (FILE-082)**

Formatting: one line `Amount 123456789`, `Qty 1_200_000`, `UoM "kg"`, Date 2026-10-24, `ApprovedBy "Ravi Kulkarni"`, `ApprovedAt 2026-10-24T05:00:00Z` (fixture clock) → contains `₹12,34,567.89`, a Qty cell `1,200` next to a UoM cell `kg` (see Resolutions), `24-10-2026`, `24-10-2026 10:30`.
Payable: Grand 1_411_280, Rounded 1_411_300, `HasRoundedTotal: true` → `Rounded Total ₹14,113.00` and `Rupees Fourteen Thousand One Hundred Thirteen Only`; `HasRoundedTotal: false`, Rounded = Grand → no `Rounded Total`, words `Rupees Fourteen Thousand One Hundred Twelve and Paise Eighty Only`.

- [ ] **Step 4: Write `TestPrint_AllPrintableDocTypesHaveTemplates` (FILE-066; Wave 1, CD-58)**

External package `print_test`: `printtest.RegisterFixtureViews(print.Default)`; `names` = `print.PrintableNames(doctype.All())` (CD-02; WP0's registry is real, so this is whatever the test binary registered) merged with `printtest.FixtureDocTypes`, without duplicates; `print.Default.Missing(names) == nil`. For each fixture name and each state `draft pending_approval approved rejected closed cancelled` (the `doctype.State*` constants, CD-54), render through `print.Default.Render` with a `doctype.Info{DocType: name, ID: 1, State: s, Number: …, Display: …}` (`Draft 1` for unnumbered states; `PO/26-27/00001`, `GRN/26-27/00001`, `EXP/26-27/00001` otherwise) without error; compare the approved output to `fixture_order_approved.golden.html`, `fixture_receipt_approved.golden.html`, `fixture_claim_approved.golden.html`. `TestRegistry_Missing` (Task 11) pins the "No print template for DocType <name>." message. Once DOC's `internal/fixture` is on `main`, WP3 adds `_ ".../internal/fixture"` to this test so the `doctype.All()` half also sees the real fixture DocTypes (Hand-off notes).

- [ ] **Step 5: Run tests to verify they fail**

Run: `make generate && go test ./internal/kernel/print/ -run 'TestLetterhead_FromContext|TestPrint_PurchaseOrderGolden|TestPrint_EscapesUserContent|TestPrint_OptionalPartsOmitted|TestPrint_UsesNUMFormatting|TestPrint_WordsUsePayableTotal|TestPrint_AllPrintableDocTypesHaveTemplates' -v`
Expected: FAIL (undefined: print.TradeDocument, print.LetterheadFor, printtest.RegisterFixtureViews)

- [ ] **Step 6: Implement `letterhead.go`, `trade.go`, `trade.templ`, `printtest/reference.go`, `printtest/letterhead.go`, `printtest/golden.go`, `printtest/fixtures.go`**

If `AmountInWords` errors (cannot happen for valid totals), render the figures and leave the words line out; never fail the print.

- [ ] **Step 7: Generate the golden, inspect it, then run tests**

Run: `go test ./internal/kernel/print/ -run 'TestPrint_PurchaseOrderGolden|TestPrint_AllPrintableDocTypesHaveTemplates' -update && git diff --stat internal/kernel/print/testdata && go test ./internal/kernel/print/... -v`
Expected: four golden files created; open them in a browser and check the layout by eye once; all tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/kernel/print/
git commit -m "feat(print): standard purchase-document template, fixture print views, golden tests"
```

---

### Task 13: Print handler (print track; after Task 11; Task 12 for the snapshot test's template)

**Files:**
- Create: `internal/kernel/print/web/routes.go`, `internal/kernel/print/web/print.go`
- Create: `internal/kernel/print/printtest/table.go` (test parent table and a view that reads it)
- Test: `internal/kernel/print/web/print_test.go`

**Interfaces:**
- Consumes: `print.Registry`, `print.Letterhead`, `print.WithLetterhead`, `print.CSS`, `doctype.InfoSource`, `auth.Service`, `auth.PrincipalFrom`, `db.DB`, `printtest`, `docfake`, `authfake`.
- Produces:
  - `type Config struct{ DB *db.DB; Auth auth.Service; Info doctype.InfoSource; Letterhead print.Letterhead; Registry *print.Registry }` (nil Registry → `print.Default`). The injected `Principal` and `Label` functions are gone: the principal comes from `auth.PrincipalFrom` (CD-12) and the label from `Info.DocLabel` (CD-05).
  - `func Routes(cfg Config) chi.Router` with `GET /print/{doctype}/{id}` and `GET /print/assets/print.css`.
  - Flow: `auth.PrincipalFrom(r.Context())` (else 303/401 as in Task 7) → `cfg.DB.Read` → `cfg.Info.Info(ctx, tx, doctype, id)` (`doctype.ErrNotFound` → 404, CD-54) → `auth.Can(ctx, p, doctype, "print", &ref)` with `ref` built from the `Info` (same mapping as `files.refFrom`): `auth.ErrNotFound` → 404 `This record doesn't exist or you don't have access to it.`; `auth.ErrForbidden` → 403 `You don't have permission to print {Info.DocLabel}. Ask your administrator for a role that allows it.` → no view → 404 → `ctx = print.WithLetterhead(ctx, cfg.Letterhead)` → `Registry.Render(ctx, tx, buf, info)`, then write. Headers: `Content-Type: text/html; charset=utf-8`, `Content-Security-Policy: default-src 'self'; img-src 'self' data:`, `X-Content-Type-Options: nosniff`, `Cache-Control: private, no-store`. Query parameters are never read.
  - `printtest.CreateTable(t, db)` creates `print_test_po(id, state, number, party_name, party_address, version, updated_at)` and `print_test_party(id, name, address)`; `printtest.TableView` reads only `print_test_po` and builds `ReferencePO()` with `Party.Name/Address` from it; `printtest.TableInfo` implements `doctype.InfoSource` over the same table (`DocLabel "Purchase Order"`, `Display` = number or `Draft {id}`).

- [ ] **Step 1: Write the failing tests**

`TestPrint_RequiresPrintRight` (FILE-050, per R3): approved `PO/26-27/00001` (id 1, `Info.ProjectIDs` `[prjTechpark]`); `authfake` seeded with the CD-56 helpers from the rights matrix in `00-fixtures.md` (`AddRole` + `Grant`) plus this test's own `Grant("Masters Manager", "test.PurchaseOrder", "read", "all")`: Priya (Purchase User, read+print) with `Restrict(priya.ID, []int64{prjMetro}, nil)` → 404 with the AUTH message; Kavya (Masters Manager, read but not print) → 403 `You don't have permission to print Purchase Order. Ask your administrator for a role that allows it.` (CD-73); Ravi (Purchase Manager: read, print, no write) → 200.

`TestPrint_WatermarkNotOverridable` (FILE-055): draft doc 42; `GET /print/test.PurchaseOrder/42?watermark=0&no_draft=1&status=approved` → body contains `<div class="watermark" data-status="draft">DRAFT</div>`.

`TestPrint_IsReadOnly` (FILE-062): approved row in `print_test_po` with `version 1`, `updated_at` fixed; three GETs → 200 each; row's `version` and `updated_at` unchanged. `Config` has no writer and the handler uses only `DB.Read` (assert by grepping `print/web/*.go` for `.Write(` in the test, so a later change fails loudly).

`TestPrint_UsesDocumentSnapshot` (FILE-063): row saved with `party_name "Shree Steel Traders"`, `party_address "Mumbai, Maharashtra"`; then update `print_test_party` to `"SG Steel Pvt Ltd"`, `"Navi Mumbai"` → print shows `Shree Steel Traders` and `Mumbai, Maharashtra`, not `SG Steel`.

`TestPrint_HeadersAndCSS` (supporting): print response carries the four headers above; `GET /print/assets/print.css` → 200 `text/css; charset=utf-8` with `print.CSS`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/kernel/print/web/ -v`
Expected: FAIL (undefined: web.Routes)

- [ ] **Step 3: Implement `routes.go`, `print.go` and `printtest/table.go`**

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/kernel/print/... -race -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/kernel/print/
git commit -m "feat(print/web): print view handler gated by print right, state from database only"
```

---

### Task 14: Helpers for WP3's print and attachment e2e tests (print track; after Tasks 12, 13)

Every `TestE2E_*` lives in `e2e/` and is owned and run by WP3 (CD-40); this WP writes none of them and adds no browser dependency. It ships a test server WP3 can drive, so the e2e cases test this WP's real handlers.

**Files:**
- Create: `internal/kernel/print/web/webtest/server.go` (package `webtest`; imports `print/web`, `files/web`, `printtest` and `filestest`)
- Test: `internal/kernel/print/web/webtest/server_test.go`

**Interfaces:**
- Consumes: print `web.Routes` (Task 13), `files/web.Routes` (Task 8), `filestest`, `printtest.ReferencePO()`, `printtest.Letterhead`, `printtest.TableInfo`, `auth.WithPrincipal`, `authfake`.
- Produces: `func NewServer(t testing.TB, as auth.Principal) *httptest.Server` — mounts the print and files routes behind a middleware that calls `auth.WithPrincipal`; seeds approved reference PO id 1 (`PO/26-27/00001`), draft `Draft 42` with 60 lines (every third with a two-line description), attachment 1 = a small PNG logo, and an expense with `r.jpg`, `r.heic` and an EXIF-orientation-6 JPEG for the attachment previews.

- [ ] **Step 1: Write the failing test `TestServer_ServesReferencePO`** (supporting, no case ID): `GET /print/test.PurchaseOrder/1` as Ravi → 200, body contains `PO/26-27/00001` and `href="/print/assets/print.css"`; `GET /files/1` → 200 `image/png`; `GET /print/test.PurchaseOrder/42` → body contains `Draft 42` and 60 `<tr class="line">` rows.
- [ ] **Step 2: Run it to verify it fails** — `go test ./internal/kernel/print/web/webtest/ -v` → FAIL (undefined: webtest.NewServer)
- [ ] **Step 3: Implement `server.go`**
- [ ] **Step 4: Run it to verify it passes, then the area check**

Run: `go test ./internal/kernel/print/... -race && make lint && go run ./scripts/checktestids FILE --exclude-level e2e`
Expected: PASS; the checker reports no missing P1 case (FILE-066 included, CD-58).

- [ ] **Step 5: Commit**

```bash
git add internal/kernel/print/web/webtest/
git commit -m "test(print): e2e test server for WP3's browser tests"
```

E2E cases owned by WP3 (`e2e/`, CD-40), each still mapped to its FILE ID: FILE-033 `TestE2E_ExpenseAttachmentPreview` (P2), FILE-035 `TestE2E_ExifOrientation` (P2), FILE-056 `TestE2E_PrintA4` (P1; this WP's unit test `TestPrintCSS_A4`, which has no case ID (CD-77), pins the CSS lines), FILE-057 `TestE2E_PrintLongTable` (P1), FILE-058 `TestE2E_PrintPageNumbers` (P2), FILE-060 `TestE2E_PrintNoExternal` (P1), FILE-070 `TestE2E_PrintButton` (P2). This WP supplies what they check: A4 and break rules in `print.css` (Task 11), inline JPEG serving and `img { image-orientation: from-image; }` (Tasks 8, 11), the CSP header (Task 13), the toolbar and `page.css` footer (Task 16), and `webtest.NewServer`.

---

### Task 15: P2 files cases (files track; after Task 9)

**Files:**
- Modify: `internal/kernel/files/attach.go`, `remove.go`, `store.go` (Config `RemoveGuards`), `meta.go`, `web/download.go`
- Create: `internal/kernel/files/check.go`
- Test: `internal/kernel/files/p2_test.go`, `internal/kernel/files/web/p2_test.go`

**Interfaces:**
- Produces:
  - Attach duplicate rule (after Check, before count): `ExistsForParentSHA` → `KindDuplicate` 422 `This file is already attached to {Info.Display}.`
  - `type RemoveGuard func(ctx context.Context, tx *db.Tx, parentID, attachmentID int64) (line int, err error)`; `Config.RemoveGuards map[string]RemoveGuard`; line > 0 → `KindInUse` 422 `This file is the receipt on line {n}. Clear it there first.`
  - `func (s *Store) CheckReceipt(ctx context.Context, tx *db.Tx, parent auth.RecordRef, line int, attachmentID int64, docLabel string) error` → `The receipt on line {n} belongs to another document. Attach it to this {docLabel} first.`
  - `type CheckReport struct{ MissingBlobs []Meta; Orphans []string; Corrupt []string }`; `func (s *Store) Check(ctx context.Context) (CheckReport, error)`; `func WriteCheckReport(w io.Writer, r CheckReport) (exitCode int)` (1 when any finding). WP3 wires `app files check`.

- [ ] **Step 1: Write the failing tests** — each with the case's values, mapped to R5 names:
  - `TestServe_UnicodeFileName` (FILE-013): `बिल रसीद.jpg` → `Content-Disposition: inline; filename="___ ____.jpg"; filename*=UTF-8''%E0%A4%AC%E0%A4%BF%E0%A4%B2%20%E0%A4%B0%E0%A4%B8%E0%A5%80%E0%A4%A6.jpg`; `List` returns the original name.
  - `TestAttachment_SameContentSameDocRejected` (FILE-017): second upload of the same bytes as `copy.jpg` to draft `expense` 42 → `This file is already attached to Draft 42.`; count unchanged.
  - `TestAttachment_LimitConcurrent` (FILE-022): 19 rows; two goroutines attach different files → exactly one success, the other `KindLimit`.
  - `TestAttachmentList_Formatting` (FILE-030): uploads at 04:30, 04:35, 04:40 UTC on 2026-10-24 (`env.Clock.Set` before each) → `List` order newest first; `ListEntry` times `24-10-2026 10:10`, `24-10-2026 10:05`, `24-10-2026 10:00` (`types.FormatIST`); `HumanSize(524288) == "512 KB"`, `HumanSize(2456789) == "2.3 MB"`.
  - `TestExpenseLine_ReceiptMustBelongToDoc` (FILE-045): attachment on `expense` 13 used as line 2's receipt on `expense` 12 → `The receipt on line 2 belongs to another document. Attach it to this expense first.`
  - `TestAttachment_RemoveBlockedWhileReferencedByLine` (FILE-046): guard returns line 2 for `taxi.jpg` → `This file is the receipt on line 2. Clear it there first.`; guard returns 0 → removed.
  - `TestFilesCheck_Report` (FILE-049): one row with missing blob, one orphan blob, one blob whose content no longer hashes to its name → report lists all three with ids/hashes; `WriteCheckReport` returns 1; files and rows unchanged.
- [ ] **Step 2: Run to verify they fail** — `go test ./internal/kernel/files/... -run 'TestServe_UnicodeFileName|TestAttachment_SameContentSameDocRejected|TestAttachment_LimitConcurrent|TestAttachmentList_Formatting|TestExpenseLine_ReceiptMustBelongToDoc|TestAttachment_RemoveBlockedWhileReferencedByLine|TestFilesCheck_Report' -race -v` → FAIL
- [ ] **Step 3: Implement the interfaces above**
- [ ] **Step 4: Run to verify they pass** — same command → PASS; then `go test ./internal/kernel/files/... -race`
- [ ] **Step 5: Commit** — `git add internal/kernel/files/ && git commit -m "feat(files): P2 duplicate guard, line receipts, list formatting, integrity check"`

FILE-033 and FILE-035 (`TestE2E_ExpenseAttachmentPreview`, `TestE2E_ExifOrientation`) are WP3's (Task 14 list). The UI's preview styles must include `img { image-orientation: from-image; }` as `print.css` does.

---

### Task 16: P2 print cases (print track; after Task 14)

**Files:**
- Modify: `internal/kernel/print/trade.templ`, `trade.go`, `print.css`, `assets.go`, `page.templ`, `internal/kernel/print/web/routes.go`
- Create: `internal/kernel/print/grn.templ`, `internal/kernel/print/expense.templ`, `internal/kernel/print/print.js`, `testdata/print/grn_approved.golden.html`, `testdata/print/expense_approved.golden.html`
- Test: `internal/kernel/print/p2_test.go`, `internal/kernel/print/web/p2_test.go`

**Interfaces:**
- Produces:
  - `type GRNDoc struct{ Letterhead LetterheadInfo; Date types.Date; Party Party; PONumber string; PODate types.Date; ChallanNo string; ChallanDate types.Date; Lines []GRNLine }`, `type GRNLine struct{ Sr int; Item, HSN, UoM string; Received, Accepted, Rejected types.Qty }`, templ `GRNDocument(d GRNDoc)`.
  - `type ExpenseDoc struct{ Letterhead LetterheadInfo; Employee string; PeriodFrom, PeriodTo types.Date; Lines []ExpenseLine; GrandTotal, RoundedTotal types.Money; HasRoundedTotal bool; Attachments []string; ApprovedBy string; ApprovedAt time.Time }`, `type ExpenseLine struct{ Sr int; Date types.Date; Type, Description string; Amount types.Money; HasReceipt bool }`, templ `ExpenseDocument(d ExpenseDoc)`.
  - Toolbar in `Page`: `<div class="toolbar"><button id="print-btn" type="button">Print / Save as PDF</button><button id="close-btn" type="button">Close</button></div>`, hidden by `@media print { .toolbar { display: none; } }`; `/print/assets/print.js` binds `print-btn` → `window.print()` and `close-btn` → `window.close()` (no inline script; the CSP forbids it).
  - Footer: `GET /print/{doctype}/{id}/page.css` (same permission checks) returning `@page { @bottom-center { content: "{Info.Display} · Page " counter(page) " of " counter(pages); } }` (display value CSS-string-escaped), linked from `Page`.
- [ ] **Step 1: Write the failing tests**
  - `TestPrint_GRNGolden` (FILE-067): GRN `GRN/26-27/00001` against `PO/26-27/00001` dated 24-10-2026, MS Angle received 1,200.000 / accepted 1,180.000 / rejected 20.000 kg → golden `grn_approved.golden.html`; contains `Goods Receipt Note`, `Against PO: PO/26-27/00001 dated 24-10-2026`, challan no./date, headers `Received Accepted Rejected`.
  - `TestPrint_ExpenseGolden` (FILE-068): `EXP/26-27/00001`, employee `Ravi Kulkarni (EMP-003)`, 3 lines (2 with receipts), attachments `taxi.jpg`, `hotel.pdf` → golden; contains `Expense Claim`, `Receipt: Yes`, `Receipt: No`, total in figures and words, `Attachments: taxi.jpg, hotel.pdf`. Extend `TestRegistry_Missing`'s table with `test.GRN` and `test.Expense` (FILE-066 itself keeps the `printtest` fixture views from Task 12).
  - `TestPrint_ApprovalAndAmendLines` (FILE-069): `ApprovedBy "Ravi Kulkarni"`, `ApprovedAt 2026-10-24T05:00:00Z` → `Approved by Ravi Kulkarni on 24-10-2026 10:30` (`types.FormatIST`); approved amendment `PO/26-27/00001-1` with `AmendsNumber "PO/26-27/00001"` → `Amends PO/26-27/00001`.
  - `TestPrint_EmptyDraft` (FILE-071): no lines, totals 0, state draft → `No items`, `₹0.00`, `Rupees Zero Only`, DRAFT watermark.
  - `TestPrintModel_WordsDerived` (FILE-083): `ReferencePO().AmountInWords()` equals `InWordsINR(ReferencePO().PayableTotal())`; `migrations/00500_attachment.sql` and every `migrations/005*.sql` contain no `in_words` (the PO table belongs to the PO module; its own plan repeats this check).
  - `TestPrint_ToolbarAndFooterServed` (supporting, no case ID; feeds WP3's FILE-058 and FILE-070): `Page` contains the toolbar markup and `<script src="/print/assets/print.js">`; `print.css` has `@media print { .toolbar { display: none; } }`; `GET /print/test.PurchaseOrder/42/page.css` → content `"Draft 42 · Page " counter(page) " of " counter(pages)`, and for id 1 `"PO/26-27/00001 · Page " …`; the same permission checks as the page apply (Priya, restricted to PRJ-METRO, on the PRJ-TECHPARK PO → 404). The browser checks (`TestE2E_PrintPageNumbers`, `TestE2E_PrintButton`) are WP3's; the form's "Print" button and its hiding for users without the print right belong to the UI WP.
- [ ] **Step 2: Run to verify they fail** — `make generate && go test ./internal/kernel/print/... -run 'TestPrint_GRNGolden|TestPrint_ExpenseGolden|TestPrint_ApprovalAndAmendLines|TestPrint_EmptyDraft|TestPrintModel_WordsDerived|TestPrint_ToolbarAndFooterServed|TestRegistry_Missing' -v` → FAIL
- [ ] **Step 3: Implement the interfaces above; generate goldens with `-update` and inspect them**
- [ ] **Step 4: Run to verify they pass** — `go test ./internal/kernel/print/... -v` → PASS
- [ ] **Step 5: Final checks and commit** — `make test && make lint && go run ./scripts/checktestids FILE --exclude-level e2e` all green; `git add internal/kernel/print/ && git commit -m "feat(print): GRN and expense templates, approval/amend lines, toolbar, page footer"`

---

## Hand-off notes for WP3 (Integration)

- Wire the real `doctype.InfoSource` (the DOC engine, CD-05) into `files.Config.Info` and `print/web.Config.Info`; `masters/company`'s `print.Letterhead` (CD-30) into `print/web.Config.Letterhead`; `audit.Default` into `files.Config.Audit`. AUTH's session middleware sets the principal that handlers read with `auth.PrincipalFrom` (CD-12).
- Mount `files/web.Routes` and `print/web.Routes` behind the session middleware; schedule `Store.GC(ctx, files.GCGrace)` hourly; add `app files gc` and `app files check` (exit code from `WriteCheckReport`).
- Register `files.LogoPolicy` for the company master; `LetterheadInfo.LogoURL` is `/files/{logo attachment id}`.
- Wire DOC's amend to `CopyAll` and draft delete to `DeleteAll` inside the same transaction.
- Own and run the FILE e2e cases listed in Task 14 using `webtest.NewServer` (CD-40).
- Once DOC's `internal/fixture` is on `main`, add `_ ".../internal/fixture"` to `print/alltypes_test.go` so FILE-066's `doctype.All()` half iterates the real fixture DocTypes; the `printtest` views already cover `fixture.Order`, `fixture.Receipt` and `fixture.Claim` (CD-58). Real module print views (PO, GRN, Expense) replace them in the module plans.

## Resolved by (contract-changes.md, Rounds 1–3)

The plan's requests CC-FILE-1 to CC-FILE-5 (Round 1) and the open points from the alignment pass (Round 3) are settled; the tasks above already follow the decisions.

- **CC-FILE-1** (`List` on `files.Service`) → **CD-29**, as proposed. Also from CD-29: `Attach` takes a `*files.Spooled` made by `Store.Spool` outside the transaction (the plan's `Temp` and its `io.Reader` fallback are gone), and §5 `Meta` uses `Name`/`MIME` with no parent fields.
- **CC-FILE-2** (read-only document facts) → **CD-05**, differently: no `doc.Engine.Info`/`doc.Info`; FILE and PRINT take `doctype.InfoSource` and read `doctype.Info` (state, number, `Display`, `DocLabel`, record ref fields). The local `files.Parents`/`Parent`/`ErrNoParent`, `print.InfoSource`/`DocInfo`/`ErrNoDocument`/`DisplayNumber` and the handler's `Label` func are deleted; Wave 1 tests use `docfake`.
- **CC-FILE-3** (imports) → **CD-04**, as proposed, plus `doctype` for both packages.
- **CC-FILE-4** (`CopyAll` without a principal) → **CD-29**, as proposed: copies are uploaded by `to.CreatedBy` at the amendment time.
- **CC-FILE-5** (registry enumeration and principal accessor) → **CD-02** (`doctype.All()`; FILE-066 iterates it together with the `printtest` fixture views, so it runs in Wave 1 (CD-58), with `TestRegistry_Missing` covering the message) and **CD-12** (`auth.PrincipalFrom`; the injected `Principal func(*http.Request)` is gone).
- Also applied: **CD-03** (`audit.Writer`/`auditfake` instead of an audit func), **CD-06** (`dbq.New(tx.SQL())`), **CD-17** (signed-out handling: 303 `/signin?next=…`, 401 for htmx/non-GET), **CD-30** (`print.Letterhead`; this plan defines `LetterheadInfo`'s fields), **CD-33** (formatters called directly; adapters deleted), **CD-40/CD-41** (e2e owned by WP3; checker `go run ./scripts/checktestids FILE --exclude-level e2e`), **CD-44** (`files.Error` implements `doctype.HTTPError`).

Round 3 decisions applied:

- **FILE 8 → CD-58**: `print.LetterheadInfo`'s fields are in §5 and WP0's stub (Task 12 no longer edits `contract.go`); `printtest` registers print views for `fixture.Order`, `fixture.Receipt` and `fixture.Claim`, so FILE-066 is a Wave 1 case (Task 12, Step 4).
- **CD-53**: NUM's `types` package merges first (Wave 1a); the "after NUM merge" markings are gone and formatter-dependent cases are ordinary Wave 1 cases.
- **CD-54**: one not-found sentinel `doctype.ErrNotFound` (= `auth.ErrNotFound`); `docfake.SetInfo` seeds `doctype.Info` in `filestest.Env.Doc`.
- **CD-56**: `filestest.Env.Auth` is `*authfake.Fake`; its `Hide`, `Restrict`, `Grant`, `AddRole` replace the local hiding wrapper (`Hide(u, …)`, `Revoke`).
- **FILE 10 → CD-63**: `/signin` and `/signout`, 303 redirects (already used; no `/login` or 302 anywhere).
- **CD-68**: master display `{Code} {Name}` for master parents' `Info.Display` and the print Item cell.
- **FILE 9 → CD-73**: FILE-050 standard 403 text, FILE-026 403 per AUTH-109, one storage-failure message for FILE-015/037; the test file now says so and the matching Resolutions rows are removed.
- **CD-74 / CD-77**: every e2e-level FILE case names only a `TestE2E_*` test; FILE-056 is `TestE2E_PrintA4` (WP3), and `TestPrintCSS_A4` (Task 11) is a supporting unit test with no case ID.
- **CD-76**: `db.ErrBusy` doesn't implement `doctype.HTTPError`; the files handlers map it to 503 with its own text.

Round 4 decisions applied:

- **CD-82 / CD-92**: FILE-065 in the test file already has separate Qty and UoM cells; the stale Resolutions row is removed and `TestPrint_UsesNUMFormatting` follows the test file as written.
- **CD-85**: `filestest`, `printtest`, `files/web`, `print/web` and `print/web/webtest` imports checked against the subpackage rules (Global Constraints).
- **CD-86**: `auditfake.New()` returns `*auditfake.Recorder`; `filestest.Env.Audit` has that type.
