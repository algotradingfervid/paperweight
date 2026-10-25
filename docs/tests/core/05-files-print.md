# Files & printing (FILE)

## Scope
FILE owns attachments on documents and masters: upload, checks on type, size and content, storage on disk, de-duplication, access, serving, removal, copy on amend and clean-up of unused files. It also owns printing: one compiled print template per printable DocType, the letterhead, A4 page layout, DRAFT and CANCELLED watermarks, and amount in words in INR using lakh and crore.
It relies on AUTH (who is signed in, read, write and print rights per DocType, record-level rules), DOC (document state, amend link, audit trail, deleting drafts), NAM (document number, given at first approval, used as the print file name; unnumbered documents show `Draft {id}`), APR (approver name and time shown on prints), MST (company, branch GSTIN, party, item, HSN) and NUM (money and quantity formatting, rounded payable total, date format, IST display).

## Decisions
| ID | Question | ERPNext/Frappe | Odoo | Decision | Why |
|---|---|---|---|---|---|
| D-FILE-01 | Where are file bytes stored? | On disk under `public/files` or `private/files`, named by file name with a hash suffix on clash (`frappe/frappe/core/doctype/file/file_v1.py:396`). Storage v2 (alpha) adds a blob table keyed by sha256 (`frappe/frappe/storage/SPEC.md`). | On disk in a filestore keyed by sha (`odoo/odoo/addons/base/models/ir_attachment.py:140`), with a database option (`test_01_store_in_db`). | **On disk in `<data_dir>/files/<first 2 hex>/<sha256>`, next to the SQLite file. SQLite holds only the metadata (`attachment` table).** The path is built only from the hash, never from the user's file name. | Phone receipts add several GB a year. Keeping them out of SQLite keeps the database small and fast, and keeps backups quick to diff. Hash paths make traversal attacks impossible and give de-duplication for free (Odoo's model). Backups must copy both the database and `files/` (see FILE-049). |
| D-FILE-02 | Size limits | 25 MB default per file, set in System Settings (`frappe/frappe/core/api/file.py:82`). Optional per-DocType `max_attachments` (`file.py:423`). | No per-file limit in `ir.attachment`. Images over 1920 px are resized (`ir_attachment.py:507`). | **10 MB per file (10,485,760 bytes) and at most 20 attachments per document. Logo at most 1 MB.** Limits are Go config, not browser settings. | A 12 MP phone JPEG is 3 to 5 MB and a scanned bill PDF is under 5 MB. 10 MB covers real use and limits abuse. 20 per document is plenty for an expense claim. |
| D-FILE-03 | Allowed file types | Optional extension allowlist (`file.py:479`). Storage v2 checks content against the extension for active types (`frappe/frappe/storage/blob.py:125`). | Mimetype guessed from name, then content. Unsafe XML/HTML types are turned into `text/plain` for non-admins (`ir_attachment.py:534`). | **Fixed allowlist, checked by extension AND by the content's magic bytes: PDF, JPG/JPEG, PNG, WEBP, HEIC/HEIF, XLSX, DOCX, CSV.** Anything else is refused, including SVG, HTML, XML, JS, ZIP, EXE, old XLS/DOC and files with no extension. | Odoo's neutering and Frappe's optional list both leave room for mistakes. A small allowlist covers receipts, bills and spreadsheets and can't be used for stored XSS. |
| D-FILE-04 | Who can see or change an attachment? | Public files can be read by anyone. Private files can be read by the owner, a user it was shared with, or anyone who can read the parent. Write needs write on the parent (`file.py:1003`). | Readable by anyone who can read the parent. Unattached files are readable only by the creator and admins (`ir_attachment.py:699`). Create, write and delete need write on the parent (`test_basic_modifications`). | **Every file is private and has a parent document or master. Reading needs read on the parent, and adding or removing needs write on it. The uploader gets no extra rights, and there are no public URLs or unattached files.** If you can't read the parent, the server answers 404. | This is the simplest rule that can't leak. Frappe's owner bypass lets someone keep reading a receipt after losing access to the expense. Unattached files add an orphan lifecycle we don't need. |
| D-FILE-05 | De-duplication and clean-up | v1 shares a file on disk when the content hash matches and deletes it when the last row goes (`file_v1.py:296`). v2 shares blobs and has a GC (`frappe/frappe/storage/gc.py:18`). | One filestore file per sha (`test_03_no_duplication`). Unlink only marks it, and a GC with a 30-minute grace period removes it (`ir_attachment.py:48`, `test_12_gc`, `test_13_rollback`). | **One blob per sha256. Each attachment is its own row. Removing a row never deletes bytes directly. A GC job deletes blobs that no row references and that are older than 30 minutes.** | Odoo's grace-period GC is safe under concurrent uploads and rolled-back transactions. Deleting at remove time races with a parallel upload of the same bytes. |
| D-FILE-06 | Adding and removing files by document state | Files can be added in any state. Removing a file from a submitted document is blocked only when the DocType sets `protect_attached_files` (`file.py:576`). | Needs write access on the parent record. State is not checked. | **Add: allowed in `draft`, `rejected`, `pending_approval`, `approved` and `closed`, for users with write on the DocType; every add is audited. Not allowed in `cancelled`. Remove: allowed only in `draft` and `rejected`.** | A vendor bill or delivery photo often arrives after approval, so adding must stay open. Removing evidence after submit would break F5 ("submitted documents are never edited"). |
| D-FILE-07 | Attachments on amend | `copy_attachments_from_amended_from` copies the rows to the new document (`frappe/frappe/model/document.py:1180`, `test_attached_to_field_and_folder_copied_on_amend`). | n/a in base | **Amend copies every attachment row to the new draft, pointing to the same blob.** | Follows ERPNext. The user shouldn't have to upload the receipts again. |
| D-FILE-08 | Phone photos (HEIC/JPEG) | Saved as uploaded. Thumbnails are made separately (`file.py:512`). | Images over 1920×1920 are resized and re-encoded at JPEG quality 80 (`ir_attachment.py:494`, `test_10_image_autoresize`). | **Saved byte for byte, with no resizing or re-encoding. HEIC is accepted and served as a download (most desktop browsers can't show it). JPEG/PNG/WEBP are previewed inline.** The file input uses `accept="image/*,application/pdf,..."` and `capture` isn't forced. | A receipt is evidence, so its hash must match what was uploaded. Re-encoding HEIC in pure Go needs a cgo/LGPL decoder, which breaks the single open-source binary. **Couldn't verify** whether iOS always converts HEIC to JPEG on upload, so the server accepts both. |
| D-FILE-09 | Active content inside files | Refuses PDFs that contain JavaScript (`file.py:495`, `frappe/frappe/utils/pdf.py:200`). Forces SVG/HTML/XML to download (`blob.py:41`). | Turns XML-like types into text/plain (`test_08_neuter_xml_mimetype`). | **SVG/HTML/XML are never accepted (D-FILE-03). Every file is served with `X-Content-Type-Options: nosniff` and `Content-Security-Policy: sandbox; default-src 'none'`. Scanning PDFs for JavaScript is deferred.** | No pure-Go PDF JavaScript scan that we could verify handles compressed object streams. A naive byte scan gives false confidence. Sandbox headers plus login-only access is enough risk control for 20 internal users. |
| D-FILE-10 | How PDFs are made | A headless Chromium (`chrome`) or PlutoPrint (`frappe/frappe/utils/pdf.py:92`; default `PlutoPrint` in `frappe/frappe/printing/doctype/print_settings/print_settings.json:57`). | wkhtmltopdf (`odoo/odoo/addons/base/models/ir_actions_report.py:202`). | **v1: print first in the browser. The binary serves a self-contained HTML print view with `@page { size: A4; margin: 15mm }`, and the user prints or saves it as PDF from the browser. Server-side PDF is deferred.** When it's needed (email to a vendor, bulk PDF), use a pure-Go library that draws from the same print view-model: `github.com/signintech/gopdf` (MIT, v0.38.2 at time of writing) or `github.com/johnfercher/maroto/v2` (MIT, v2.4.4, released 22 Oct 2026, so wait until it is two weeks old). | Both reference systems need an external browser engine, which breaks "single binary". Browser print gives exact HTML/CSS fidelity for free, and the app has no email in v1. |
| D-FILE-11 | Template engine and who edits templates | Jinja Print Formats plus a drag-and-drop builder, editable in the browser (`frappe/frappe/printing/doctype/print_format/`). | QWeb views, editable in the database. | **`templ` components compiled into the binary, one per printable DocType (PO, GRN, Expense), with golden-file tests. Users can't edit templates in v1.** | Fixed stack (templ). There is no runtime metadata in v1. Escaping is automatic, which removes the XSS class Frappe tests for (`test_labels_are_escaped`). |
| D-FILE-12 | Letterhead | A separate Letter Head DocType with image or HTML, a default and a disabled flag (`frappe/frappe/printing/doctype/letter_head/letter_head.py:58`). | Company layout (logo, address) from `res.company`. | **The letterhead is built from MST: company legal name and logo, plus the address, GSTIN and state of the document's branch. There is no Letter Head DocType.** | One company, a few branches. The branch decides the GSTIN shown, so a free-form letterhead could show the wrong GSTIN. |
| D-FILE-13 | Page size | A4 by default, with other sizes and custom sizes (`print_settings.json:69`, `test_custom_page_size_converted_from_mm`). India Compliance's GST format uses 15 mm margins (`india-compliance/india_compliance/gst_india/print_format/gst_tax_invoice/gst_tax_invoice.json:19`). | Paper formats A4/Letter (`test_format_A4`). | **A4 portrait only, 15 mm margins.** | Indian business standard. Landscape and other sizes come later. |
| D-FILE-14 | Printing drafts and cancelled documents | Drafts print by default with a "DRAFT" heading. Cancelled documents are blocked by default, and when allowed print with "CANCELLED" (`frappe/frappe/www/printview.py:462`, `frappe/frappe/templates/print_formats/standard_macros.html:213`, `print_settings.json:88,98,110`, `test_print_error`). | Title changes to "Draft Invoice" / "Cancelled Invoice" (`odoo/addons/account/views/report_invoice.xml:11`). | **Every state can be printed. `draft`, `pending_approval` and `rejected` carry a diagonal "DRAFT" watermark plus a heading badge, and show `Draft {id}` (e.g. `Draft 42`) instead of a number, because numbers are given only at first approval (spec R1). `cancelled` carries "CANCELLED". `approved` and `closed` have none. Request parameters can't switch this off.** | The brief asks for this. A cancelled print is still needed for records, and a watermark that can't be removed stops a draft PO from reaching a vendor as if it were final. Frappe also ignores request overrides for these flags (`printview.py:440`). |
| D-FILE-15 | Permission to print | Read or Print right (`printview.py:479`). | Read access on the record. | **Printing and the print view need the `print` right on the DocType plus visibility of the record (read and record-level rules). Read alone is not enough: a visible record without `print` gives 403, a record the user can't see gives 404. (changed by spec R3)** Attachments still follow the parent's read/write rights (D-FILE-04). | AUTH has a separate `print` right (D-AUTH-14) and the spec makes AUTH win (R3). A print is a document that can leave the building, so it is controlled separately from viewing. |
| D-FILE-16 | Amount in words | `money_in_words` uses num2words `en_IN` when the number format is `#,##,###.##`, with the currency code in front and the "Paisa" fraction name (`frappe/frappe/utils/data.py:1538`, `frappe/frappe/geo/country_info.json:1255`). Negative gives "" (`data.py:1555`). | `amount_to_text` via num2words with currency labels (`odoo/odoo/addons/base/models/res_currency.py:247`). | **Our own small Go function `print.InWordsINR(m types.Money) (string, error)` (`types.Money` is int64 paise). Output: `Rupees <words> and Paise <words> Only`, using the Indian scale (Thousand, Lakh, Crore), Title Case, no hyphens, and no "and" inside the rupee part.** Zero paise drops the paise clause. Zero rupees with non-zero paise gives `Paise <words> Only`. | Gives the exact wording the brief asks for. No library is needed (num2words is Python). Integer paise avoids Odoo's float issue (`test_amount_to_text_10`). |
| D-FILE-17 | Which total goes into words | The rounded total when rounding is on, otherwise the grand total, as an absolute value (`erpnext/erpnext/controllers/buying_controller.py:662`). | Invoice total | **The document's final payable total from NUM (rounded total if rounding applies), worked out at render time and not stored.** | Follows ERPNext. Working it out at render time can't go stale. |
| D-FILE-18 | Keep a PDF copy of approved prints | No | `attachment_use` saves the first PDF and reloads it after that (`test_report_reload_from_attachment`) | **Deferred.** Prints are regenerated from stored document data. | No server-side PDF in v1 (D-FILE-10). Document data is immutable after approval, so a reprint matches unless the template changed. |

## Test cases

### Upload and validation

### FILE-001 · A receipt photo uploads to a draft expense and its metadata is saved
- **Priority:** P1
- **Level:** integration
- **Given** draft expense `Draft 42` owned by Ravi (Employee role, write on Expense) **When** Ravi uploads `receipt.jpg` (2,456,789 bytes, JPEG magic `FF D8 FF`) **Then** an `attachment` row exists with `parent_doctype=expense`, `parent_id` = the expense id, `file_name="receipt.jpg"`, `size=2456789`, `content_type="image/jpeg"`, `sha256` = the hash of the bytes, `uploaded_by` = Ravi and `uploaded_at` in UTC. The blob exists at `files/<sha[0:2]>/<sha>`, and the expense attachment list shows "receipt.jpg · 2.3 MB · Ravi Kulkarni · 24-10-2026 10:30" (IST, fixture clock).
- **Go test:** `TestAttachment_UploadJPEGToDraft`
- **Sources:** `frappe/frappe/core/doctype/file/test_file.py:122` (`test_save`), `odoo/odoo/addons/base/tests/test_ir_attachment.py:70` (`test_02_store_on_disk`)

### FILE-002 · A file exactly at the size limit is accepted and one byte over is refused
- **Priority:** P1
- **Level:** unit
- **Given** a 10 MB limit **When** files of 10,485,760 and 10,485,761 bytes are uploaded **Then** the first is saved. The second is refused with "File is larger than 10 MB. Choose a smaller file or compress it." No row and no blob is created for it.
- **Go test:** `TestAttachment_SizeLimitBoundary`
- **Sources:** `frappe/frappe/core/doctype/file/file.py:811` (`check_max_file_size`), `frappe/frappe/core/api/file.py:82`

### FILE-003 · A declared Content-Length over the limit is refused before the body is read
- **Priority:** P1
- **Level:** integration
- **Given** a multipart upload with `Content-Length: 52428800` **When** it hits the upload handler **Then** the response is 413 with "File is larger than 10 MB. Choose a smaller file or compress it." The server reads no more than the limit plus multipart overhead, and leaves no temporary file in `files/tmp/`.
- **Go test:** `TestUpload_DeclaredSizeTooLarge`
- **Sources:** `frappe/frappe/core/doctype/file/test_file.py:1129` (`test_declared_size_beyond_max_file_size_is_rejected`)

### FILE-004 · A streamed body that grows past the limit is cut off and its temp file removed
- **Priority:** P1
- **Level:** integration
- **Given** a chunked upload with no Content-Length that sends 12 MB **When** the stream passes 10,485,760 bytes **Then** reading stops (`http.MaxBytesReader`), the response is 413 with the same message, and the temporary file is deleted.
- **Go test:** `TestUpload_StreamedOverLimitCleansTemp`
- **Sources:** `frappe/frappe/core/doctype/file/test_file.py:1123` (`test_chunks_beyond_max_file_size_are_rejected`)

### FILE-005 · An empty file is refused
- **Priority:** P1
- **Level:** unit
- **Given** `blank.pdf` of 0 bytes **When** uploaded **Then** it is refused with "File is empty. Choose another file."
- **Go test:** `TestAttachment_EmptyFileRejected`
- **Sources:** `frappe/frappe/core/doctype/file/file.py:501` (`set_file_name`, which requires content or a URL)

### FILE-006 · Each allowed type is accepted when its content matches
- **Priority:** P1
- **Level:** unit
- **Given** fixtures `bill.pdf` (`%PDF-`), `r.jpg` and `r.jpeg` (`FF D8 FF`), `r.png` (`89 50 4E 47`), `r.webp` (`RIFF....WEBP`), `r.heic` (`ftypheic`), `r.heif` (`ftypmif1`), `rates.xlsx` and `quote.docx` (ZIP with `[Content_Types].xml` and `xl/` or `word/`), `items.csv` (UTF-8 text) **When** each is checked **Then** each is accepted with content types `application/pdf`, `image/jpeg`, `image/png`, `image/webp`, `image/heic`, `image/heif`, `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`, `application/vnd.openxmlformats-officedocument.wordprocessingml.document`, `text/csv`.
- **Go test:** `TestSniff_AllowedTypes` (table-driven)
- **Sources:** `frappe/frappe/storage/blob.py:114` (`sniff_mime`), `odoo/odoo/addons/base/tests/test_ir_attachment.py:104` (`test_07_write_mimetype`)

### FILE-007 · Disallowed extensions are refused with the allowed list
- **Priority:** P1
- **Level:** unit
- **Given** files named `setup.exe`, `page.html`, `logo.svg`, `a.js`, `docs.zip`, `x.phtml`, `old.xls`, `README` (no extension) **When** uploaded **Then** each is refused with "File type .exe is not allowed. Use PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or CSV." (the extension in the message matches the file; "File type (none) is not allowed..." when there is no extension).
- **Go test:** `TestAttachment_DisallowedExtensions`
- **Sources:** `frappe/frappe/core/doctype/file/test_file.py:200` (`test_allowed_extension`), `frappe/frappe/core/doctype/file/test_file.py:211` (`test_allowlist_blocks_extension_without_known_mimetype`)

### FILE-008 · Extensions are matched without regard to case
- **Priority:** P1
- **Level:** unit
- **Given** `RECEIPT.JPG` and `Bill.Pdf` with valid content **When** uploaded **Then** both are accepted, and the displayed name keeps the original case.
- **Go test:** `TestAttachment_ExtensionCaseInsensitive`
- **Sources:** `frappe/frappe/core/doctype/file/file.py:479` (compares `.upper()`)

### FILE-009 · Content that doesn't match the extension is refused
- **Priority:** P1
- **Level:** unit
- **Given** `invoice.pdf` whose bytes start `<!doctype html><script>`, `photo.jpg` containing PNG bytes, and `items.csv` containing `MZ` (PE executable) **When** uploaded **Then** each is refused with "File content does not match its .pdf extension. Upload the original file." (with the matching extension). `.jpg` and `.jpeg` count as the same type.
- **Go test:** `TestSniff_ContentExtensionMismatch`
- **Sources:** `frappe/frappe/storage/blob.py:125` (`validate_upload`), `odoo/odoo/addons/base/tests/test_ir_attachment.py:243` (`test_14_invalid_mimetype_with_correct_file_extension_no_post_processing`, where Odoo trusts the given mimetype; we don't)

### FILE-010 · A CSV that is really HTML, or isn't UTF-8, is refused, and a UTF-8 BOM is fine
- **Priority:** P1
- **Level:** unit
- **Given** `a.csv` beginning `<html>`, `b.csv` with invalid UTF-8 bytes `C3 28`, and `c.csv` beginning with BOM `EF BB BF` then `code,name` **When** uploaded **Then** `a.csv` is refused with "File content does not match its .csv extension. Upload the original file.", `b.csv` is refused with "CSV files must be UTF-8. Save the file as CSV UTF-8 and try again.", and `c.csv` is accepted.
- **Go test:** `TestSniff_CSVTextRules`
- **Sources:** `frappe/frappe/storage/blob.py:55` (`_HTML_MARKERS`), `frappe/frappe/core/doctype/file/test_file.py:426` (`test_utf8_bom_content_decoding`)

### FILE-011 · A double extension is judged by its last extension
- **Priority:** P1
- **Level:** unit
- **Given** `bill.pdf.exe` with PDF content and `bill.exe.pdf` with PDF content **When** uploaded **Then** the first is refused ("File type .exe is not allowed...") and the second is accepted.
- **Go test:** `TestAttachment_DoubleExtension`
- **Sources:** `frappe/frappe/core/doctype/file/file.py:488` (`os.path.splitext`)

### FILE-012 · Path parts and unsafe characters are removed from file names, and storage never uses the name
- **Priority:** P1
- **Level:** unit
- **Given** uploaded names `../../etc/passwd.pdf`, `C:\temp\bill.pdf`, `a/b%?#.pdf`, `  .bill.pdf. ` and a name made of 300 `x` characters plus `.pdf` **When** cleaned **Then** the display names are `passwd.pdf`, `bill.pdf`, `b___.pdf`, `bill.pdf` and 120 characters ending in `.pdf`. Control characters are removed. A name that is empty after cleaning becomes `file.pdf`. In every case the blob path is `files/<2 hex>/<64 hex>`.
- **Go test:** `TestFileName_Sanitize`
- **Sources:** `frappe/frappe/core/doctype/file/utils.py:559` (`get_safe_file_name`), `frappe/frappe/core/doctype/file/test_file.py:162` (`test_write_file_rejects_target_outside_files_dir`)

### FILE-013 · Non-ASCII file names are kept and served correctly
- **Priority:** P2
- **Level:** integration
- **Given** an upload named `बिल रसीद.jpg` **When** it is downloaded **Then** the response has `Content-Disposition: inline; filename="___ ____.jpg"; filename*=UTF-8''%E0%A4%AC%E0%A4%BF%E0%A4%B2%20%E0%A4%B0%E0%A4%B8%E0%A5%80%E0%A4%A6.jpg`, and the list shows `बिल रसीद.jpg`.
- **Go test:** `TestServe_UnicodeFileName`
- **Sources:** `frappe/frappe/utils/response.py:370`

### FILE-014 · The blob path resolver only accepts a 64-hex hash and stays inside the files directory
- **Priority:** P1
- **Level:** unit
- **Given** `BlobPath(dataDir, hash)` **When** called with `../../app.db`, `ab/../../x`, a 63-character hex string, an upper-case hex string, or a valid lower-case 64-hex hash **Then** the first four return `ErrBadHash`, and the valid one returns `<dataDir>/files/ab/<hash>`. A symlinked `files/` directory still resolves inside the real directory.
- **Go test:** `TestBlobPath_RejectsTraversal`
- **Sources:** `frappe/frappe/core/doctype/file/test_file.py:1990` (`test_resolved_file_path_stays_within_files_directory`), `frappe/frappe/core/doctype/file/test_file.py:2027` (`test_resolved_file_path_rejects_sibling_directory_prefix_match`), `frappe/frappe/core/doctype/file/test_file.py:974` (`test_symlinked_files_folder`), `frappe/frappe/core/doctype/file/utils.py:563` (`check_path_safety`)

### FILE-015 · Blob writes are atomic: temp file, fsync, rename, then the database row
- **Priority:** P1
- **Level:** integration
- **Given** an upload where the database insert is forced to fail after the blob is written **When** the request ends **Then** no `attachment` row exists, the response is 500 with "Couldn't save the file. Try again or contact your administrator." (the one storage-failure message, same as FILE-037, CD-73), no partial file exists at the final path, and the blob that was written is only removed later by GC (FILE-019).
- **Go test:** `TestUpload_DBFailureLeavesNoRow`
- **Sources:** `frappe/frappe/core/doctype/file/test_file.py:152` (`test_rollback_from_file_system`), `odoo/odoo/addons/base/tests/test_ir_attachment.py:230` (`test_13_rollback`)

### FILE-016 · Identical content is stored once and shared by separate attachment rows
- **Priority:** P1
- **Level:** integration
- **Given** the same `bill.pdf` bytes are attached to draft PO `Draft 42` and approved `GR/26-27/00001` **When** both uploads finish **Then** there are two `attachment` rows with the same `sha256`, exactly one file in `files/`, and both documents download identical bytes.
- **Go test:** `TestAttachment_DedupeAcrossDocuments`
- **Sources:** `odoo/odoo/addons/base/tests/test_ir_attachment.py:75` (`test_03_no_duplication`), `frappe/frappe/core/doctype/file/test_file.py:332` (`TestSameContent.test_saved_content`)

### FILE-017 · Attaching the same content twice to one document is refused
- **Priority:** P2
- **Level:** integration
- **Given** `receipt.jpg` is already attached to draft expense `Draft 42` **When** the same bytes are uploaded again (any name) to that expense **Then** it is refused with "This file is already attached to Draft 42." and the count doesn't change.
- **Go test:** `TestAttachment_SameContentSameDocRejected`
- **Sources:** `frappe/frappe/core/doctype/file/test_file.py:1611` (`test_batched_attach_does_not_duplicate_already_attached_files`)

### FILE-018 · Removing one row keeps a blob that another row still uses
- **Priority:** P1
- **Level:** integration
- **Given** FILE-016's two rows sharing one blob **When** the attachment on draft PO `Draft 42` is removed and GC runs with a 0 grace period **Then** the blob is still on disk and `GR/26-27/00001` still downloads it.
- **Go test:** `TestAttachment_RemoveKeepsSharedBlob`
- **Sources:** `odoo/odoo/addons/base/tests/test_ir_attachment.py:80` (`test_04_keep_file`), `frappe/frappe/core/doctype/file/test_file.py:2364` (`test_private_remains_private_even_if_same_hash`)

### FILE-019 · GC deletes unreferenced blobs only after the grace period
- **Priority:** P1
- **Level:** integration
- **Given** a blob with no rows whose mtime is 10 minutes ago, and another with no rows whose mtime is 31 minutes ago **When** GC runs with a 30-minute grace period **Then** the first is kept and the second is deleted. A blob with at least one row is never deleted, whatever its age. GC also deletes `files/tmp/*` older than 30 minutes.
- **Go test:** `TestBlobGC_GracePeriod`
- **Sources:** `odoo/odoo/addons/base/tests/test_ir_attachment.py:215` (`test_12_gc`), `odoo/odoo/addons/base/models/ir_attachment.py:247` (`_gc_file_store`), `frappe/frappe/storage/gc.py:18`

### FILE-020 · GC and a parallel upload of the same bytes don't lose data
- **Priority:** P1
- **Level:** integration
- **Given** an unreferenced blob older than the grace period **When** an upload of the same bytes and GC run at the same time (repeated 200 times with `-race`) **Then** every committed attachment row points to a blob that exists. GC re-checks references inside a write transaction before it deletes, and the upload re-writes the blob if it's missing before it commits.
- **Go test:** `TestBlobGC_ConcurrentUploadSameContent`
- **Sources:** `frappe/frappe/storage/gc.py:156` (`is_still_orphan`), `frappe/frappe/storage/SPEC.md` (refcount = count of rows)

### FILE-021 · A document can have at most 20 attachments
- **Priority:** P1
- **Level:** integration
- **Given** an expense with 20 attachments **When** a 21st is uploaded **Then** it is refused with "A document can have at most 20 attachments. Remove one first." Once one is removed, a new upload succeeds.
- **Go test:** `TestAttachment_PerDocumentLimit`
- **Sources:** `frappe/frappe/core/doctype/file/test_file.py:335` (`test_attachment_limit`), `frappe/frappe/core/doctype/file/file.py:423`

### FILE-022 · The attachment limit holds under concurrent uploads
- **Priority:** P2
- **Level:** integration
- **Given** an expense with 19 attachments **When** two different files are uploaded at the same time **Then** exactly one succeeds and the other gets the FILE-021 message. The count check and the insert run in one `BEGIN IMMEDIATE` transaction.
- **Go test:** `TestAttachment_LimitConcurrent`
- **Sources:** `frappe/frappe/core/doctype/file/file.py:423` (check-then-insert, not race-safe in the source)

### FILE-023 · A file can only be attached to a saved document that exists
- **Priority:** P1
- **Level:** integration
- **Given** expense id 999999 doesn't exist **When** a file is uploaded to it **Then** the response is 404 "Save the document before attaching files." No blob is kept past GC and no row is created.
- **Go test:** `TestAttachment_ParentMustExist`
- **Sources:** `odoo/odoo/addons/base/models/ir_attachment.py:699` (unattached files are a special case we remove), `frappe/frappe/core/doctype/file/file.py:232` (`validate_attachment_references`)

### FILE-024 · Attaching needs write on the parent
- **Priority:** P1
- **Level:** integration
- **Given** Meena has read but not write on Purchase Order **When** she uploads to approved `PO/26-27/00001` **Then** the response is 403 "You don't have permission to add files to PO/26-27/00001." and no row is created.
- **Go test:** `TestAttachment_AddNeedsParentWrite`
- **Sources:** `frappe/frappe/core/doctype/file/test_file.py:1858` (`test_attach_to_doc_without_write_permission_is_blocked`), `odoo/odoo/addons/base/tests/test_ir_attachment.py:529` (`test_basic_modifications`)

### FILE-025 · Reading needs read on the parent, and a record-level rule hides the file
- **Priority:** P1
- **Level:** integration
- **Given** approved expense `EXP/26-27/00001` of Ravi on project `PRJ-TECHPARK` with `receipt.jpg`, and Priya, whose restriction limits her to project `PRJ-METRO` **When** Priya requests `/files/{attachment_id}` or the expense's attachment list **Then** the download is 404 "File not found." and the list call is 404 (the expense itself is hidden). Meena (Director, read on Expense, no restriction) gets 200.
- **Go test:** `TestAttachment_ReadFollowsParentRecordRule`
- **Sources:** `frappe/frappe/core/doctype/file/file.py:1003` (`has_permission`), `odoo/odoo/addons/base/tests/test_ir_attachment.py:398` (`test_read_permission`), `frappe/frappe/core/doctype/file/test_file.py:2692` (`test_restricted_user_excludes_out_of_scope_file`)

### FILE-026 · The uploader gets no extra access after losing access to the parent
- **Priority:** P1
- **Level:** integration
- **Given** Deepak uploaded `receipt.jpg` to his draft expense `Draft 42`, and his Employee role (his only role) was then removed **When** Deepak requests the file **Then** the response is 403 "You don't have permission to read Expense Claim. Ask your administrator for a role that allows it." (no right on the DocType, AUTH-109, CD-73) and no bytes are sent. If instead he keeps the right but the record is out of his scope, the response is 404 "File not found.". This differs from Frappe, where the owner can always read (`file.py:1015`).
- **Go test:** `TestAttachment_NoOwnerBypass`
- **Sources:** `frappe/frappe/core/doctype/file/file.py:1015`, `odoo/odoo/addons/base/models/ir_attachment.py:673`

### FILE-027 · Files can't be reached without signing in
- **Priority:** P1
- **Level:** integration
- **Given** a valid attachment id **When** it is requested with no session cookie **Then** the response is 303 to `/signin?next=%2Ffiles%2F{id}` (or 401 "Your session has expired. Sign in again." for htmx, `/api/*` and non-GET requests, CD-17) and no bytes are sent. No static route serves `files/` directly.
- **Go test:** `TestServe_RequiresLogin`
- **Sources:** `frappe/frappe/core/doctype/file/file.py:1012` (public files open to anyone, which we drop), `odoo/odoo/addons/base/tests/test_ir_attachment.py:398`

### FILE-028 · Download URLs use the attachment id, and guessing ids or hashes gives nothing
- **Priority:** P1
- **Level:** integration
- **Given** Priya can read attachment 41 (on a `PRJ-METRO` document) but not 42 (on a `PRJ-TECHPARK` document) **When** she requests `/files/42`, `/files/<sha256 of 42's blob>` and `/files/41/../42` **Then** each returns 404 "File not found." The blob hash never appears in any response body or header seen by users.
- **Go test:** `TestServe_IDORAndHashGuess`
- **Sources:** `frappe/frappe/core/doctype/file/test_file.py:2009` (`test_traversal_file_url_cannot_reach_other_private_file`), `frappe/frappe/storage/SPEC.md` (guessable `/files/invoice.pdf` problem)

### FILE-029 · De-duplication doesn't leak access or reveal whether content already exists
- **Priority:** P1
- **Level:** integration
- **Given** Suresh attached `salary.pdf` to his own expense, which Deepak can't read **When** Deepak uploads identical bytes to his own expense **Then** Deepak gets a new row on his expense only, still can't open Suresh's row (404), and his upload response has the same status, fields and timing class as an upload of new content.
- **Go test:** `TestAttachment_DedupeNoLeak`
- **Sources:** `frappe/frappe/core/doctype/file/test_file.py:2402` (`test_toggling_is_private_does_not_leak_shared_file`), `frappe/frappe/core/doctype/file/test_file.py:1586` (`test_child_table_does_not_adopt_another_users_private_orphan`)

### FILE-030 · The attachment list shows name, size, uploader and IST time, newest first
- **Priority:** P2
- **Level:** integration
- **Given** three attachments uploaded at 04:30, 04:35 and 04:40 UTC on 24-10-2026 (before the fixture clock) **When** the document form loads **Then** the list shows them newest first, with times 10:10, 10:05, 10:00 (IST) and sizes like "512 KB" and "2.3 MB" (1 KB = 1024 bytes, one decimal place for MB).
- **Go test:** `TestAttachmentList_Formatting`
- **Sources:** `frappe/frappe/core/doctype/file/test_file.py:1801` (`test_list_private_attachments`)

### Serving

### FILE-031 · Downloads carry safe headers
- **Priority:** P1
- **Level:** integration
- **Given** a readable `bill.pdf` **When** it is downloaded **Then** the headers are `Content-Type: application/pdf` (the content type sniffed at upload, never re-guessed from the name), `X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox; default-src 'none'`, `Cache-Control: private, no-store`, and `Content-Length` = the stored size.
- **Go test:** `TestServe_SecurityHeaders`
- **Sources:** `frappe/frappe/storage/blob.py:38` (force-download rules), `frappe/frappe/storage/serve.py:247`

### FILE-032 · PDFs and web images open inline, and everything else downloads
- **Priority:** P1
- **Level:** integration
- **Given** attachments `bill.pdf`, `r.jpg`, `r.png`, `r.webp`, `r.heic`, `rates.xlsx`, `quote.docx`, `items.csv` **When** each is opened **Then** PDF/JPG/PNG/WEBP get `Content-Disposition: inline`, and the rest get `attachment`. Adding `?download=1` forces `attachment` for all of them.
- **Go test:** `TestServe_InlineVsAttachment`
- **Sources:** `frappe/frappe/storage/blob.py:58` (`served_type`), `frappe/frappe/utils/response.py:165`

### FILE-033 · Image attachments show a preview, and HEIC shows a file icon with a download link
- **Priority:** P2
- **Level:** e2e
- **Given** an expense with `r.jpg` and `r.heic` **When** the form is viewed in Chrome **Then** `r.jpg` shows a lazy-loaded preview no larger than 160 px, and `r.heic` shows a file icon labelled "HEIC image, opens as download".
- **Go test:** `TestE2E_ExpenseAttachmentPreview`
- **Sources:** `frappe/frappe/core/doctype/file/test_file.py:773` (`test_make_thumbnail`)

### FILE-034 · Stored bytes equal uploaded bytes: no resize and no re-encode
- **Priority:** P1
- **Level:** integration
- **Given** a 4000×3000 JPEG with EXIF orientation 6 **When** it is uploaded and downloaded **Then** the sha256 of the downloaded bytes equals the sha256 of the original, and the EXIF data is still there.
- **Go test:** `TestAttachment_BytesUnchanged`
- **Sources:** `odoo/odoo/addons/base/tests/test_ir_attachment.py:140` (`test_10_image_autoresize`, which we deliberately don't follow)

### FILE-035 · A portrait phone photo shows upright in the preview
- **Priority:** P2
- **Level:** e2e
- **Given** the FILE-034 JPEG (EXIF orientation 6) **When** its preview renders **Then** it shows taller than wide, because the browser applies `image-orientation: from-image`.
- **Go test:** `TestE2E_ExifOrientation`
- **Sources:** `frappe/frappe/core/doctype/file/test_file.py:2279` (`test_image_header_guessing`)

### FILE-036 · A blob missing from disk gives a clear error and a log entry
- **Priority:** P1
- **Level:** integration
- **Given** an attachment row whose blob file was deleted outside the app **When** it is downloaded **Then** the response is 404 with "The file is missing from storage. Ask your administrator to restore it from backup." and an error log names the attachment id and sha256.
- **Go test:** `TestServe_MissingBlob`
- **Sources:** `odoo/odoo/addons/base/models/ir_attachment.py:145` (`_file_read`, which logs and returns empty), `frappe/frappe/core/doctype/file/test_file.py:964` (`test_create_file_without_file_url`)

### FILE-037 · A storage write error fails the upload cleanly
- **Priority:** P1
- **Level:** integration
- **Given** `files/` is read-only (or the disk is full) **When** a file is uploaded **Then** the response is 500 "Couldn't save the file. Try again or contact your administrator." No row is created, the temp file is removed, and the error is logged with its cause.
- **Go test:** `TestUpload_StorageWriteError`
- **Sources:** `odoo/odoo/addons/base/tests/test_ir_attachment.py:569` (`test_write_error`), `odoo/odoo/addons/base/tests/test_ir_attachment.py:575` (`test_write_chmod_permission_error`)

### Lifecycle

### FILE-038 · Removing an attachment from a draft works and is audited
- **Priority:** P1
- **Level:** integration
- **Given** draft expense `Draft 42` with `receipt.jpg`, and Ravi with write **When** he removes it **Then** the row is gone. The audit trail gets `{action: "attachment_removed", file_name: "receipt.jpg", size: 2456789, sha256: <hash>, by: Ravi, at: <UTC>}`.
- **Go test:** `TestAttachment_RemoveFromDraftAudited`
- **Sources:** `frappe/frappe/core/doctype/file/file.py:832` (`create_attachment_record`), `frappe/frappe/utils/file_manager.py:288` (`remove_file`)

### FILE-039 · Attachments can't be removed once a document is submitted
- **Priority:** P1
- **Level:** integration
- **Given** expenses in `pending_approval`, `approved`, `closed` and `cancelled`, each with `receipt.jpg` **When** a user with write tries to remove it **Then** each is refused with "Files can't be removed after the document is submitted. Cancel and amend it to change files." `rejected` behaves like `draft` (removal allowed).
- **Go test:** `TestAttachment_RemoveBlockedAfterSubmit` (table-driven by state)
- **Sources:** `frappe/frappe/core/doctype/file/file.py:576` (`validate_protected_file`), `frappe/frappe/core/doctype/file/test_file.py:1355` (`test_delete_file_referenced_on_submitted_document_is_allowed`, the Frappe default we tighten)

### FILE-040 · Files can be added to a submitted document that isn't cancelled, and the add is audited
- **Priority:** P1
- **Level:** integration
- **Given** `GR/26-27/00001` in `approved` for site `PUNE-WH`, and storekeeper Suresh with write on GRN **When** he attaches `vendor-bill.pdf` **Then** it is saved. The document's fields, state and `modified_at` don't change. The audit trail gets `attachment_added` with name, size, sha256, by and at.
- **Go test:** `TestAttachment_AddAfterApproval`
- **Sources:** `frappe/frappe/core/doctype/file/file.py:576` (only deletion is protected), `odoo/odoo/addons/base/tests/test_ir_attachment.py:518` (`test_with_write_permissions`)

### FILE-041 · Files can't be added to a cancelled document
- **Priority:** P1
- **Level:** integration
- **Given** `PO/26-27/00001` in `cancelled` **When** a file is attached **Then** it is refused with "This document is cancelled. Attach files to its amended copy instead."
- **Go test:** `TestAttachment_AddToCancelledBlocked`
- **Sources:** `frappe/frappe/core/doctype/file/file.py:597` (cancelled docs are treated specially)

### FILE-042 · Deleting a draft removes its attachments, and GC removes blobs nobody else uses
- **Priority:** P1
- **Level:** integration
- **Given** draft expense `Draft 42` with two attachments, one of whose blobs is shared with another document **When** the draft is deleted and GC runs after the grace period **Then** both attachment rows are gone, the unshared blob is deleted and the shared blob stays.
- **Go test:** `TestAttachment_DraftDeleteCascades`
- **Sources:** `frappe/frappe/core/doctype/file/test_file.py:1374` (`test_document_delete_cascades_referenced_attachment`), `frappe/frappe/model/delete_doc.py:210`, `frappe/frappe/utils/file_manager.py:266` (`remove_all`)

### FILE-043 · Amend copies attachment rows to the new draft
- **Priority:** P1
- **Level:** integration
- **Given** cancelled `PO/26-27/00001` with `quote.pdf` and `drawing.pdf` **When** it is amended into a new draft `Draft 43` (it gets `PO/26-27/00001-1` only when approved) **Then** the new draft has two rows with the same names, sizes and sha256. No new blob is written. `uploaded_by` and `uploaded_at` show the user who amended (the new draft's creator, `to.CreatedBy`, CD-29) and the time of the amend, and the audit trail notes "copied from PO/26-27/00001".
- **Go test:** `TestAttachment_CopiedOnAmend`
- **Sources:** `frappe/frappe/core/doctype/file/test_file.py:1416` (`test_attached_to_field_and_folder_copied_on_amend`), `frappe/frappe/model/document.py:1180`

### FILE-044 · Removing a copied attachment from the amended draft leaves the original alone
- **Priority:** P1
- **Level:** integration
- **Given** FILE-043's result **When** `drawing.pdf` is removed from the amended draft `Draft 43` **Then** `PO/26-27/00001` still lists and downloads `drawing.pdf`.
- **Go test:** `TestAttachment_AmendCopyIndependent`
- **Sources:** `frappe/frappe/core/doctype/file/test_file.py:1325` (`test_delete_file_sharing_url_with_another_file_is_allowed`)

### FILE-045 · A line's receipt must be an attachment of the same document
- **Priority:** P2
- **Level:** integration
- **Given** expense line 2 with `receipt_attachment_id` pointing to an attachment of a different expense **When** the expense is saved **Then** it is refused with "The receipt on line 2 belongs to another document. Attach it to this expense first."
- **Go test:** `TestExpenseLine_ReceiptMustBelongToDoc`
- **Sources:** `frappe/frappe/core/doctype/file/test_file.py:1566` (`test_child_table_attach_field_is_linked_to_parent`)

### FILE-046 · An attachment used as a line's receipt can't be removed until the line is cleared
- **Priority:** P2
- **Level:** integration
- **Given** draft expense line 2 references `taxi.jpg` **When** `taxi.jpg` is removed **Then** it is refused with "This file is the receipt on line 2. Clear it there first." After the line's receipt is cleared, the removal succeeds.
- **Go test:** `TestAttachment_RemoveBlockedWhileReferencedByLine`
- **Sources:** `frappe/frappe/core/doctype/file/test_file.py:1283` (`test_delete_file_referenced_in_child_table_attach_field`), `frappe/frappe/core/doctype/file/file.py:606`

### FILE-047 · Two simultaneous uploads of identical bytes both succeed
- **Priority:** P1
- **Level:** integration
- **Given** two users upload the same 3 MB PDF to two documents at the same moment (run 100 times) **When** both finish **Then** both return 201, there is exactly one blob, and its bytes match the hash. Renaming the temp file over an existing identical blob counts as success.
- **Go test:** `TestUpload_ConcurrentSameContent`
- **Sources:** `frappe/frappe/storage/SPEC.md` ("two concurrent identical uploads may each get a blob"), `odoo/odoo/addons/base/models/ir_attachment.py:155` (`_file_write`)

### FILE-048 · The company logo is a PNG or JPEG of at most 1 MB that any signed-in user can see
- **Priority:** P1
- **Level:** integration
- **Given** the Company master **When** `logo.svg` is uploaded, then a 1.5 MB `logo.png`, then a 180 KB `logo.png` **Then** the SVG is refused with "Logo must be a PNG or JPG image.", the 1.5 MB file is refused with "Logo must be 1 MB or smaller.", and the 180 KB PNG is saved. Every signed-in user can load the logo (needed on every print), and a signed-out request is redirected to login.
- **Go test:** `TestCompanyLogo_Rules`
- **Sources:** `frappe/frappe/printing/doctype/letter_head/test_letter_head.py:39` (`test_auto_image`), `odoo/odoo/addons/base/tests/test_ir_attachment.py:117` (`test_08_neuter_xml_mimetype`)

### FILE-049 · An integrity check reports rows with missing blobs and blobs with no rows
- **Priority:** P2
- **Level:** integration
- **Given** one row whose blob is missing, one unreferenced blob, and one blob whose bytes no longer match its name **When** `app files check` runs **Then** it prints those three with their ids and hashes, exits with code 1, and changes nothing.
- **Go test:** `TestFilesCheck_Report`
- **Sources:** `frappe/frappe/core/doctype/file/file_v1.py:214` (`validate_file_on_disk`), `odoo/odoo/addons/base/models/ir_attachment.py:263` (`_gc_file_store_unsafe`)

### Printing

### FILE-050 · Print needs the print right on the DocType and visibility of the record
- **Priority:** P1
- **Level:** integration
- **Given** approved `PO/26-27/00001` on project `PRJ-TECHPARK`; Priya, whose restriction limits her to `PRJ-METRO`; Kavya, whose Masters Manager role is set up in this test with read but not print on Purchase Order (she has no restrictions); and Ravi (Purchase Manager, read and print) **When** each opens `/print/purchase-order/{id}` **Then** Priya gets 404 (she can't see the record, so whether she has print doesn't matter). Kavya can open the PO form but gets 403 "You don't have permission to print Purchase Order. Ask your administrator for a role that allows it." (AUTH's standard text, CD-73) on the print view, because read alone is not enough. Ravi gets 200. Write is not needed to print. (changed by spec R3)
- **Go test:** `TestPrint_RequiresPrintRight`
- **Sources:** `frappe/frappe/www/printview.py:479` (`validate_print_permission`)

### FILE-051 · An approved purchase order prints with full letterhead, party, lines, taxes and totals
- **Priority:** P1
- **Level:** integration (golden file)
- **Given** the reference PO, approved as `PO/26-27/00001` and dated 24-10-2026: company Vardhan Infra Projects Pvt Ltd, branch `PUNE` (Pune Works, GSTIN `27AAFCV2449G1Z7`, Maharashtra), vendor `SHREE` Shree Steel Traders (Mumbai, GSTIN `27AABCS1429B1ZU`), lines `MS-ANGLE-50` MS Angle 50x50x5, HSN `7216`, 1,200.000 kg × ₹62.5000 = ₹75,000.00 and `WIRE-18G` Binding Wire 18G, HSN `7217`, 50.000 kg × ₹96.0000 = ₹4,800.00, taxable ₹79,800.00, CGST 9% ₹7,182.00 and SGST 9% ₹7,182.00, grand total ₹94,164.00 **When** it is rendered **Then** the HTML matches `testdata/print/po_approved.golden.html` and contains: the logo, "Vardhan Infra Projects Pvt Ltd", the Pune Works address and "GSTIN: 27AAFCV2449G1Z7", the title "Purchase Order", "PO/26-27/00001", "Date: 24-10-2026", the vendor name "Shree Steel Traders", its Mumbai address and "GSTIN: 27AABCS1429B1ZU", a lines table with columns Sr, Item, HSN, Qty, UoM, Rate, Amount, the CGST and SGST rows, "Grand Total ₹94,164.00", "Rupees Ninety Four Thousand One Hundred Sixty Four Only", terms, and "For Vardhan Infra Projects Pvt Ltd" / "Authorised Signatory".
- **Go test:** `TestPrint_PurchaseOrderGolden`
- **Sources:** `india-compliance/india_compliance/gst_india/print_format/gst_tax_invoice/gst_tax_invoice.json:13` (layout: company GSTIN, place of supply, HSN column, in words, signatory), `frappe/frappe/printing/doctype/print_format/test_print_format.py:18` (`test_print_user`)

### FILE-052 · Draft, pending approval and rejected documents print with a DRAFT watermark
- **Priority:** P1
- **Level:** integration
- **Given** the FILE-051 PO in states `draft`, `pending_approval` and `rejected` **When** each is rendered **Then** the HTML has `<div class="watermark" data-status="draft">DRAFT</div>` (fixed position, rotated, shown on every printed page) and a "DRAFT" badge next to the title. In every one of these states the document has no number yet, so the print shows `Draft 42` where the number goes, and never a `PO/26-27/…` number (spec R1). A rejected PO that is revised back to draft still shows `Draft 42`.
- **Go test:** `TestPrint_DraftWatermark` (table-driven by state)
- **Sources:** `frappe/frappe/templates/print_formats/standard_macros.html:213`, `odoo/addons/account/views/report_invoice.xml:11` ("Draft Invoice")

### FILE-053 · A cancelled document prints with a CANCELLED watermark
- **Priority:** P1
- **Level:** integration
- **Given** the PO in state `cancelled` (numbered `PO/26-27/00001` when it was approved) **When** it is rendered **Then** the response is 200, the number `PO/26-27/00001` is shown, and the HTML has `<div class="watermark" data-status="cancelled">CANCELLED</div>` and a "CANCELLED" badge. Frappe blocks this by default (`test_print_error`); we allow it.
- **Go test:** `TestPrint_CancelledWatermark`
- **Sources:** `frappe/frappe/tests/test_printview.py:175` (`test_print_error`), `frappe/frappe/templates/print_formats/standard_macros.html:218`, `frappe/frappe/printing/doctype/print_settings/print_settings.json:110`

### FILE-054 · Approved and closed documents have no watermark
- **Priority:** P1
- **Level:** integration
- **Given** the PO in `approved` and in `closed` **When** each is rendered **Then** the HTML shows the number `PO/26-27/00001` and contains no `.watermark` element and no "DRAFT" or "CANCELLED" text.
- **Go test:** `TestPrint_NoWatermarkWhenFinal`
- **Sources:** `frappe/frappe/templates/print_formats/standard_macros.html:213`

### FILE-055 · Request parameters can't remove the watermark
- **Priority:** P1
- **Level:** integration
- **Given** draft PO `Draft 42` **When** `/print/purchase-order/{id}?watermark=0&no_draft=1&status=approved` is requested **Then** the DRAFT watermark and `Draft 42` are still shown. The print handler reads state only from the database.
- **Go test:** `TestPrint_WatermarkNotOverridable`
- **Sources:** `frappe/frappe/www/printview.py:440` (`get_allowed_print_settings_override`)

### FILE-056 · The print page is A4 portrait with 15 mm margins
- **Priority:** P1
- **Level:** e2e
- **Given** the FILE-051 PO (the reference PO) **When** chromedp prints the print view to PDF with `preferCSSPageSize: true` **Then** the PDF has one page of 595×842 pt (±1), and the print CSS contains `@page { size: A4 portrait; margin: 15mm; }`.
- **Go test:** `TestE2E_PrintA4` (owned by WP3; FILE's unit test `TestPrintCSS_A4` checks the CSS line without its own case ID, CD-77)
- **Sources:** `frappe/frappe/tests/test_pdf.py:469` (`test_custom_page_size_converted_from_mm`), `odoo/odoo/addons/base/tests/test_ir_actions_report.py:385` (`test_format_A4`)

### FILE-057 · A long table spans pages, repeats its header row and doesn't split rows
- **Priority:** P1
- **Level:** e2e
- **Given** draft PO `Draft 42` with 60 lines, some with two-line descriptions **When** it is printed to PDF **Then** it covers more than one page. Each page's table starts with the header row Sr/Item/HSN/Qty/UoM/Rate/Amount. No row is split across a page break. The totals, amount in words and signature block stay together on the last page.
- **Go test:** `TestE2E_PrintLongTable`
- **Sources:** `odoo/odoo/addons/base/tests/test_ir_actions_report.py:545` (`test_thead_tbody_repeat`), `odoo/odoo/addons/base/tests/test_ir_actions_report.py:453` (`test_report_pdf_page_break`)

### FILE-058 · Each page shows "Page X of Y" and the document number in its footer
- **Priority:** P2
- **Level:** e2e
- **Given** the FILE-057 PO printing to 3 pages **When** the PDF text is extracted **Then** the pages show "Draft 42 · Page 1 of 3", "Page 2 of 3" and "Page 3 of 3" (the PO is a draft, so it has no number; once approved the footer reads "PO/26-27/00001 · Page 1 of 3") (CSS `@page` margin boxes with `counter(page)`/`counter(pages)`).
- **Go test:** `TestE2E_PrintPageNumbers`
- **Sources:** `frappe/frappe/tests/test_pdf.py:175` (`test_print_format_margins_keep_header_and_footer_on_every_page`)

### FILE-059 · User text is escaped in prints
- **Priority:** P1
- **Level:** unit
- **Given** vendor name `<script>alert(1)</script>`, item description `"><img src=x onerror=alert(1)>` and terms containing `{{.Secret}}` **When** the PO renders **Then** the HTML contains `&lt;script&gt;alert(1)&lt;/script&gt;`, contains no `onerror=` attribute, and shows `{{.Secret}}` literally.
- **Go test:** `TestPrint_EscapesUserContent`
- **Sources:** `frappe/frappe/printing/doctype/print_format/test_print_format.py:349` (`test_labels_are_escaped`), `frappe/frappe/tests/test_printview.py:108` (`test_attach_image_signature_barcode_values_are_escaped`)

### FILE-060 · The print page is self-contained and makes no outside requests
- **Priority:** P1
- **Level:** e2e
- **Given** the print view **When** it loads with network logging on **Then** every request goes to the app's own origin (CSS and font are embedded or served by the binary, and the logo comes from the app's own URL or a data URI), there are no third-party requests, and the page's own CSP is `default-src 'self'; img-src 'self' data:`.
- **Go test:** `TestE2E_PrintNoExternal`
- **Sources:** `frappe/frappe/tests/test_pdf.py:350` (`test_report_pdf_blocks_external_requests`), `frappe/frappe/tests/test_pdf.py:415` (`test_private_images_in_pdf`)

### FILE-061 · The print page title, and so the "Save as PDF" file name, is the document number with "/" replaced
- **Priority:** P1
- **Level:** unit
- **Given** approved `PO/26-27/00001`, approved amendment `PO/26-27/00001-1`, and draft PO `Draft 42` **When** they are rendered **Then** the `<title>` values are `PO_26-27_00001`, `PO_26-27_00001-1` and `Draft 42`. Characters `/ \ % ? #` become `_`.
- **Go test:** `TestPrint_TitleIsSafeNumber`
- **Sources:** `frappe/frappe/tests/test_printview.py:12` (`test_print_filename_uses_docname_not_title_field`), `frappe/frappe/www/printview.py:195`, `frappe/frappe/core/doctype/file/utils.py:559`

### FILE-062 · Printing doesn't change the document
- **Priority:** P1
- **Level:** integration
- **Given** approved `PO/26-27/00001` **When** Ravi prints it 3 times **Then** `modified_at` and `version` are unchanged and the audit trail gets no new entries.
- **Go test:** `TestPrint_IsReadOnly`
- **Sources:** `frappe/frappe/www/printview.py:511` (`run_before_print` sets flags only)

### FILE-063 · Prints use the party and address saved on the document, not the current master
- **Priority:** P1
- **Level:** integration
- **Given** approved `PO/26-27/00001` saved with vendor name "Shree Steel Traders" and its Mumbai address (GSTIN `27AABCS1429B1ZU`) **When** the `SHREE` master is renamed to "Shree Steel Industries" with a new address, and the PO is printed **Then** the print still shows "Shree Steel Traders" and the old Mumbai address.
- **Go test:** `TestPrint_UsesDocumentSnapshot`
- **Sources:** `india-compliance/india_compliance/gst_india/print_format/gst_tax_invoice/gst_tax_invoice.json:13` (prints `doc.customer_name` and `doc.address_display`, stored on the document)

### FILE-064 · Optional parts are left out cleanly
- **Priority:** P1
- **Level:** unit
- **Given** a company with no logo, a branch with no GSTIN, the unregistered vendor `LOCAL` Ganesh Hardware (no GSTIN) and a PO with no terms **When** it is rendered **Then** there is no `<img>` tag for the logo and no "GSTIN:" line in the letterhead, the vendor block shows "GSTIN: Unregistered", and there is no empty Terms heading.
- **Go test:** `TestPrint_OptionalPartsOmitted`
- **Sources:** `frappe/frappe/www/printview.py:674` (`has_value`), `india-compliance/india_compliance/gst_india/print_format/gst_tax_invoice/gst_tax_invoice.json:13` (`{% if company_logo != "" %}`)

### FILE-065 · Money, quantities and dates on prints use the NUM formatters
- **Priority:** P1
- **Level:** unit
- **Given** amount 123456789 paise, quantity 1200.000 kg, and date 2026-10-24 **When** rendered **Then** the print shows "₹12,34,567.89", Qty cell "1,200" and UoM cell "kg" (NUM-053 trims trailing zeros; `types.FormatQtyUoM` is for running text only) and "24-10-2026". Approval time 2026-10-24T05:00:00.000Z (the fixture clock) shows as "24-10-2026 10:30".
- **Go test:** `TestPrint_UsesNUMFormatting`
- **Sources:** `frappe/frappe/geo/country_info.json:1259` (`#,##,###.##`), `frappe/frappe/printing/doctype/print_format/test_print_format.py:102` (`test_date_format_overrides_system_format`)

### FILE-066 · Every printable DocType has a template that renders its fixture
- **Priority:** P1
- **Level:** unit
- **Given** the DocType registry (`doctype.All()`, CD-02) and FILE's `printtest` print views for the fixture DocTypes `fixture.Order`, `fixture.Receipt` and `fixture.Claim` (CD-58) **When** the test iterates every DocType marked `Printable` together with those three **Then** each renders its fixture in every state (`draft`, `pending_approval`, `approved`, `rejected`, `closed`, `cancelled`) without error and the approved render matches its golden file (`fixture_order_approved.golden.html`, `fixture_receipt_approved.golden.html`, `fixture_claim_approved.golden.html`). This passes in Wave 1; the module print views (Purchase Order, GRN, Expense) join it in their own plans. A new printable DocType without a template fails the build test with "No print template for DocType <name>."
- **Go test:** `TestPrint_AllPrintableDocTypesHaveTemplates`
- **Sources:** `frappe/frappe/tests/test_printview.py:207` (`test_unresolvable_format_falls_back_to_the_doctype_default`), `frappe/frappe/printing/doctype/print_format/test_print_format.py:286` (`test_malformed_layout_renders_empty_instead_of_erroring`)

### FILE-067 · The GRN print shows the PO reference and received, accepted and rejected quantities
- **Priority:** P2
- **Level:** integration (golden file)
- **Given** approved `GR/26-27/00001` against `PO/26-27/00001` (vendor `SHREE`, site `PUNE-WH`), with line `MS-ANGLE-50` received 1,200.000, accepted 1,180.000, rejected 20.000 kg **When** rendered **Then** it shows the title "Goods Receipt Note", "Against PO: PO/26-27/00001 dated 24-10-2026", vendor challan no. and date, and the columns Received/Accepted/Rejected with those values.
- **Go test:** `TestPrint_GRNGolden`
- **Sources:** `india-compliance/india_compliance/gst_india/print_format/gst_tax_invoice/gst_tax_invoice.json:13` (Purchase Order Reference block)

### FILE-068 · The expense print lists the employee, lines with a receipt marker and the attachment names
- **Priority:** P2
- **Level:** integration (golden file)
- **Given** approved `EXP/26-27/00001` for employee "Ravi Kulkarni (EMP-003)" with 3 lines (2 with receipts) and attachments `taxi.jpg`, `hotel.pdf` **When** rendered **Then** it shows "Expense Claim", the employee, the period, lines with "Receipt: Yes/No", the total in figures and words, and "Attachments: taxi.jpg, hotel.pdf".
- **Go test:** `TestPrint_ExpenseGolden`
- **Sources:** `frappe/frappe/printing/doctype/print_format/test_print_format.py:92` (`test_image_element`)

### FILE-069 · Prints show who approved and when, and amended prints show the original number
- **Priority:** P2
- **Level:** unit
- **Given** `PO/26-27/00001` approved by Ravi Kulkarni at 2026-10-24T05:00:00.000Z, and approved amendment `PO/26-27/00001-1` amended from `PO/26-27/00001` **When** rendered **Then** the first shows "Approved by Ravi Kulkarni on 24-10-2026 10:30", and the second shows "Amends PO/26-27/00001".
- **Go test:** `TestPrint_ApprovalAndAmendLines`
- **Sources:** `india-compliance/india_compliance/gst_india/print_format/gst_tax_invoice/gst_tax_invoice.json:13` ("Generated By / Generated On" block)

### FILE-070 · Print and Print Preview from the form
- **Priority:** P2
- **Level:** e2e
- **Given** Ravi, with read and print on Purchase Order, viewing `PO/26-27/00001` **When** he clicks "Print" **Then** the print view opens in a new tab with a toolbar ("Print / Save as PDF", "Close") that is hidden in `@media print`. The toolbar button calls `window.print()`. A user who can read the PO but has no print right (FILE-050) never sees the Print button, and opening the print URL directly gives 403. (changed by spec R3)
- **Go test:** `TestE2E_PrintButton`
- **Sources:** `frappe/frappe/www/printview.py:911` (print-on-load script)

### FILE-071 · A draft with no lines still renders
- **Priority:** P2
- **Level:** unit
- **Given** draft PO `Draft 42` with no lines **When** rendered **Then** it shows `Draft 42` where the number goes, the lines table shows one row "No items", totals show ₹0.00, words show "Rupees Zero Only", and the DRAFT watermark is present.
- **Go test:** `TestPrint_EmptyDraft`
- **Sources:** `frappe/frappe/printing/doctype/print_format/test_print_format.py:900` (`test_all_rows_filtered_hides_table`)

### Amount in words

### FILE-072 · The canonical example from the brief
- **Priority:** P1
- **Level:** unit
- **Given** `InWordsINR(1411280)` (₹14,112.80) **Then** it returns `"Rupees Fourteen Thousand One Hundred Twelve and Paise Eighty Only"`.
- **Go test:** `TestInWordsINR` (table-driven, row "brief example")
- **Sources:** `frappe/frappe/utils/data.py:1538` (`money_in_words`), `frappe/frappe/tests/test_utils.py:425` (`test_money_in_words`)

### FILE-073 · Zero and paise-only amounts
- **Priority:** P1
- **Level:** unit
- **Given** 0, 1, 5, 10, 50, 80 and 99 paise **Then** the results are `"Rupees Zero Only"`, `"Paise One Only"`, `"Paise Five Only"`, `"Paise Ten Only"`, `"Paise Fifty Only"`, `"Paise Eighty Only"` and `"Paise Ninety Nine Only"`.
- **Go test:** `TestInWordsINR_ZeroAndPaiseOnly`
- **Sources:** `frappe/frappe/utils/data.py:1590` (zero / fraction-only branches), `frappe/frappe/tests/test_utils.py:425` (`(0, "BHD Zero only.")`, `(0.1, "One Hundred Fils only.")`)

### FILE-074 · Whole rupees drop the paise clause
- **Priority:** P1
- **Level:** unit
- **Given** 100, 2100 and 500000 paise **Then** the results are `"Rupees One Only"`, `"Rupees Twenty One Only"` and `"Rupees Five Thousand Only"`.
- **Go test:** `TestInWordsINR_WholeRupees`
- **Sources:** `frappe/frappe/utils/data.py:1600` (`if cint(fraction)`), `odoo/odoo/addons/base/models/res_currency.py:259`

### FILE-075 · Units, teens, tens and hundreds, with no hyphen and no inner "and"
- **Priority:** P1
- **Level:** unit
- **Given** rupee amounts 11, 15, 19, 20, 40, 99, 100, 101, 110, 115, 999 **Then** the words are "Eleven", "Fifteen", "Nineteen", "Twenty", "Forty", "Ninety Nine", "One Hundred", "One Hundred One", "One Hundred Ten", "One Hundred Fifteen", "Nine Hundred Ninety Nine" (each wrapped as `Rupees … Only`). "Forty" is spelled without a "u".
- **Go test:** `TestInWordsINR_BelowThousand`
- **Sources:** `frappe/frappe/utils/data.py:1631` (`.replace("-", " ")`)

### FILE-076 · Thousands
- **Priority:** P1
- **Level:** unit
- **Given** rupees 1000, 1001, 1010, 10000, 99999 **Then** the words are "One Thousand", "One Thousand One", "One Thousand Ten", "Ten Thousand", "Ninety Nine Thousand Nine Hundred Ninety Nine".
- **Go test:** `TestInWordsINR_Thousands`
- **Sources:** `frappe/frappe/utils/data.py:1619` (`in_words`)

### FILE-077 · Lakhs use the Indian scale
- **Priority:** P1
- **Level:** unit
- **Given** rupees 100000, 100001, 110000, 1234567, 9999999 **Then** the words are "One Lakh", "One Lakh One", "One Lakh Ten Thousand", "Twelve Lakh Thirty Four Thousand Five Hundred Sixty Seven", "Ninety Nine Lakh Ninety Nine Thousand Nine Hundred Ninety Nine". "Million" never appears.
- **Go test:** `TestInWordsINR_Lakh`
- **Sources:** `frappe/frappe/utils/data.py:1582` (`in_million = False` for `#,##,###.##`), `frappe/frappe/geo/country_info.json:1259`

### FILE-078 · Crores, including amounts of 100 crore and more
- **Priority:** P1
- **Level:** unit
- **Given** paise 1000000000 (₹1,00,00,000), 1234567890 (₹1,23,45,678.90), 100000000000 (₹100,00,00,000) and 9999999999999 (₹99,99,99,99,999.99) **Then** the results are `"Rupees One Crore Only"`, `"Rupees One Crore Twenty Three Lakh Forty Five Thousand Six Hundred Seventy Eight and Paise Ninety Only"`, `"Rupees One Hundred Crore Only"` and `"Rupees Nine Thousand Nine Hundred Ninety Nine Crore Ninety Nine Lakh Ninety Nine Thousand Nine Hundred Ninety Nine and Paise Ninety Nine Only"`.
- **Go test:** `TestInWordsINR_Crore`
- **Sources:** `frappe/frappe/utils/data.py:1619`

### FILE-079 · Negative and too-large amounts return errors
- **Priority:** P1
- **Level:** unit
- **Given** −100 paise and 100,000 crore rupees (10^14 paise) **When** converted **Then** they return `ErrNegativeAmount` with "Amount in words needs an amount of 0 or more." and `ErrAmountTooLarge` with "Amount is too large to write in words." The largest accepted value is 10^14 − 1 paise.
- **Go test:** `TestInWordsINR_Errors`
- **Sources:** `frappe/frappe/utils/data.py:1555` (negative gives ""), `frappe/frappe/tests/test_utils.py:425` (`("Fail", "")`)

### FILE-080 · Input is integer paise, so nearby values never give the same words
- **Priority:** P1
- **Level:** unit
- **Given** 28 and 29 paise, and 1411279 and 1411280 paise **Then** each pair gives different words ("Paise Twenty Eight Only" / "Paise Twenty Nine Only"). The function signature only accepts `types.Money` (int64 paise).
- **Go test:** `TestInWordsINR_NoFloatDrift`
- **Sources:** `odoo/odoo/addons/base/tests/test_res_currency.py:105` (`test_amount_to_text_10`)

### FILE-081 · Words always have the right shape and convert back to the same amount (fuzz)
- **Priority:** P1
- **Level:** unit (fuzz)
- **Given** any paise in [0, 10^14) **When** converted **Then** the result starts with "Rupees " or "Paise ", ends with " Only", has no double spaces, no hyphen and no lower-case word start, contains " and Paise " at most once, and `ParseWordsINR(result)` (a test-only parser) returns the input.
- **Go test:** `FuzzInWordsINR_RoundTrip`
- **Sources:** `frappe/frappe/utils/data.py:1538`

### FILE-082 · Prints put the payable (rounded) total into words, not the unrounded total or line amounts
- **Priority:** P1
- **Level:** integration
- **Given** an expense whose grand total is ₹14,112.80 and whose NUM rounded payable total is ₹14,113.00 **When** printed **Then** it shows "Rounded Total ₹14,113.00" and "Rupees Fourteen Thousand One Hundred Thirteen Only". With rounding off for that DocType, it shows "Rupees Fourteen Thousand One Hundred Twelve and Paise Eighty Only".
- **Go test:** `TestPrint_WordsUsePayableTotal`
- **Sources:** `erpnext/erpnext/controllers/buying_controller.py:662` (`set_total_in_words`), `erpnext/erpnext/controllers/selling_controller.py:231`

### FILE-083 · Amount in words is computed at render time and not stored
- **Priority:** P2
- **Level:** unit
- **Given** the PO table schema **Then** there is no `in_words` column, and the print view-model's `AmountInWords` equals `InWordsINR(doc.PayableTotalPaise)`.
- **Go test:** `TestPrintModel_WordsDerived`
- **Sources:** `erpnext/erpnext/controllers/buying_controller.py:678` (ERPNext stores `in_words`; we derive it)

## Deferred and excluded
| Scenario | Source | later/never | Reason |
|---|---|---|---|
| Server-side PDF (download/email/bulk merge, password-protected PDF) | `frappe/frappe/tests/test_pdf.py:144` (`test_pdf_encryption`), `:218` (`test_bulk_pdf_with_password_keeps_every_document`), `odoo/odoo/addons/base/tests/test_ir_actions_report.py:124` (`test_merge_pdfs`) | later | v1 prints from the browser (D-FILE-10). Add with a pure-Go library when vendor email arrives. |
| Saving the generated PDF as an attachment on approval and reloading it | `odoo/odoo/addons/base/tests/test_ir_actions_report.py:65` (`test_report_reload_from_attachment`) | later | Needs server-side PDF. Approved data doesn't change. |
| Scanning PDFs for JavaScript | `frappe/frappe/tests/test_pdf.py:428` (`test_pdf_contains_js_detects_document_level_js`) | later | No pure-Go scanner we could verify. Mitigated by sandbox CSP and login-only access (D-FILE-09). |
| Public files and public URLs; "only System Manager may upload public" | `frappe/frappe/core/doctype/file/test_file.py:2502` | never | Internal app. Everything is private (D-FILE-04). |
| Switching a file between private and public | `frappe/frappe/core/doctype/file/test_file.py:445` | never | No public files. |
| Guest uploads and web-form uploads | `frappe/frappe/core/doctype/file/test_file.py:993` | never | No portal (brief). |
| File manager with folders, Home/Attachments, folder copy | `frappe/frappe/core/doctype/file/test_file.py:590` (`test_folder_depth`), `:610` | never | Files belong only to documents and masters. |
| Files that are only a remote URL (link attachments), fetching web images | `frappe/frappe/core/doctype/file/test_file.py:497`, `:1969` | later | No use in v1. SSRF risk. |
| Unzipping uploaded ZIPs | `frappe/frappe/core/doctype/file/test_file.py:816` | never | ZIP isn't an allowed type. |
| Server thumbnails, image optimisation, resizing | `frappe/frappe/core/doctype/file/test_file.py:773`, `:2077`, `odoo/odoo/addons/base/tests/test_ir_attachment.py:140` | later | The browser scales previews. Keeping original bytes is better evidence (D-FILE-08). |
| Converting HEIC to JPEG on the server | none in sources | later | Needs a cgo/LGPL decoder, which breaks the single open-source binary. |
| Old Office formats (XLS/DOC, OLE) | `frappe/frappe/core/doctype/file/test_file.py:129` (`test_ole_xls_content_not_decoded`) | later | Rare. Add if users ask. |
| Chunked/resumable uploads | `frappe/frappe/core/doctype/file/test_file.py:1095` | never | The 10 MB limit fits one request. |
| Images pulled out of rich-text fields | `frappe/frappe/core/doctype/file/test_file.py:1886` (`test_extract_images_from_doc`) | never | No rich-text editor in v1. |
| Full-text indexing of attachment content | `odoo/odoo/addons/base/models/ir_attachment.py:563` (`_index`) | later | Not needed for 20 users. |
| User-editable print templates, print format builder, versions of templates | `frappe/frappe/printing/doctype/print_format/test_print_format.py:977`, `:1002` | later | No runtime metadata in v1 (brief). |
| Several letterheads per company, Letter Head DocType | `frappe/frappe/printing/doctype/letter_head/letter_head.py:58` | later | One company. Branch decides the GSTIN (D-FILE-12). |
| Other page sizes, landscape, custom sizes | `frappe/frappe/tests/test_pdf.py:490` (`test_landscape_orientation_swaps_paper_size`), `odoo/odoo/addons/base/tests/test_ir_actions_report.py:389` | later | A4 portrait only (D-FILE-13). |
| Barcode / QR on prints, UPI payment QR | `frappe/frappe/printing/doctype/print_format/test_print_format.py:196`, `:223`, `gst_tax_invoice.json:13` (UPI QR) | later | Needed for Sales Invoice / e-invoice, not PO/GRN/Expense. |
| GST tax invoice print rules (Rule 46: "Tax Invoice" title, number of 16 characters or fewer, place of supply, reverse charge, HSN-wise tax breakup, e-invoice IRN/QR) | `india-compliance/india_compliance/gst_india/print_format/gst_tax_invoice/gst_tax_invoice.json:13`, `india-compliance/india_compliance/gst_india/constants/__init__.py:1476` | later | Applies to the Sales Invoice and Delivery Challan modules. Write their print tests then. |
| Raw / network printer printing | `frappe/frappe/printing/doctype/network_printer_settings/` | never | Out of scope. |
| Translated prints and other languages for words | `odoo/odoo/addons/base/tests/test_res_currency.py:114` | never (v1) | English only (brief). |
| Showing absolute values for negative amounts (returns / credit notes) | `frappe/frappe/tests/test_printview.py:150` | later | No return documents in v1. |
| Print access by share key or link | `frappe/frappe/www/printview.py:522` (`validate_key`) | never | No sharing outside the app. |
| Logging who printed what | none | later | Not required. Printing is read-only (FILE-062). |

## Interfaces this area needs from others
- **AUTH** provides `auth.Service.Can(ctx, p, doctype, Right, rec)` with rights `read` (download, list), `write` (add or remove files) and `print` (print view), including record-level rules (`auth.ErrNotFound`, the same value as `doctype.ErrNotFound`, → 404 when the record isn't visible; `auth.ErrForbidden` → 403 with the standard text; CD-54), and the session middleware (signed-out requests get a 303 to `/signin` or a 401, CD-17, CD-63). Errors reaching handlers implement `doctype.HTTPError` (CD-44). Handlers read the signed-in user with `auth.PrincipalFrom(ctx)` (CD-12). FILE never decides permission by itself.
- **DOC** provides the facts about a document through `doctype.InfoSource` (CD-05): state (`draft`/`pending_approval`/`rejected`/`approved`/`closed`/`cancelled`, `""` for masters), number, display label (`PO/26-27/00001` or `Draft 42`), DocType label and the record ref used for permission checks. Its `doc.Engine.Do` calls `files.Service.CopyAll` on `amend` and `files.Service.DeleteAll` on `delete`, in the same transaction. FILE writes `attachment_added` / `attachment_removed` through an `audit.Writer` (CD-03).
- **DOC** marks which DocTypes are `Printable` (`doctype.DocType.Printable`, listed by `doctype.All()`, CD-02); each printable DocType registers its view with `print.Register(doctype, view)`. For DOC's test-only `fixture.Order`, `fixture.Receipt` and `fixture.Claim`, FILE's `printtest` provides the views (CD-58).
- **NAM** provides the document number (assigned by `naming.Service.Assign` at first approval, spec R1), or empty before that, in which case prints show `Draft {id}`. The number may contain `/`.
- **APR** provides the approver's display name and approval time for prints (FILE-069).
- **MST** provides the company legal name and logo attachment, branch address, state and optional GSTIN through the `print.Letterhead` interface (`ForBranch(ctx, tx, branchID) (LetterheadInfo, error)`, implemented by `masters/company`, CD-30) with the §5 `LetterheadInfo{ LegalName, LogoURL, BranchName string; Address []string; StateName, StateCode, GSTIN string }` (CD-58), the party name, address and GSTIN as saved on the document, and item HSN codes.
- **NUM** provides (CD-33) `types.FormatINR(m)` with Indian grouping (`₹12,34,567.89`), `FormatQty(q)` (`1,200`), `FormatQtyUoM(q, uom)` (`1,200 kg`), `FormatRate`, `FormatPercent`, `FormatDate(d)` → `24-10-2026`, `FormatIST(t)` → `24-10-2026 10:30`, and the document's `PayableTotalPaise` (rounded total when rounding applies).
- **Ops/backup** (outside these areas) must back up `<data_dir>/app.db` and `<data_dir>/files/` together, and run `app files gc` on a schedule (for example hourly).
