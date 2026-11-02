# Shell, masters and admin review: uiv2-review-shell

VERDICT: REQUEST CHANGES

BLOCKER 1 · HIGH 8 · MEDIUM 15 · LOW 10

Reviewed on main 0a90e96c at http://localhost:8081, on 3 Nov 2026. Measured against docs/ux/2026-11-01-ui-v2.md, docs/ux/v2-mocks/ and docs/specs/2026-10-24-design-system.md. Line references are where I read the cause; "likely" means I did not trace it fully.

**In short:** the shell's layout is solid. There is no overflow, the small-target audit found one page, and axe found nothing. The damage is in what is missing or broken behind it: nobody can add a user, the master date chips blank the field, failure states disappear or lose work, and masters look like an earlier product than the document modules.

## BLOCKER

### B1. No screen can add a user, set roles, reset a password, unlock or delete
- **Screen:** /users and /users/{id}. **Width:** all.
- **Shots:** <shots>/sweep/users-1280.png, <shots>/sweep/users_1-1280.png, <shots>/sweep/users-390.png
- **Wrong:** /users is a read-only list with no New button, and /users/new is a 404. /users/1 shows only a name, an email and "Site and project access". The POST routes for create, roles, password, unlock and delete exist but no form calls them. `views/user.templ` is 25 lines and `views/users.templ` is 32, and neither holds a form. The app cannot get past one user. The sign-in help line ("Ask your administrator to reset it. They can do it from Users in a minute.") is untrue, and so is the setup follow-up "Next, add your users".
- **Expected:** §5.4 sends the admin to add users, §5.3 sends users to Users for a reset, and §2 has Anil and Kavya managing users weekly. The list needs New user. The user page needs roles, password reset, unlock and delete, in the v2 form pattern.
- **Likely files:** internal/kernel/auth/web/views/users.templ, user.templ. Handlers are already in internal/kernel/auth/web/users.go.
- **Note:** read from source and the running pages only. I posted nothing to these routes, as the brief keeps users read only.

## HIGH

### H1. Today and Yesterday on every master date field blank the field
- **Screen:** Employee (joining, birth, relieving), Project (start, end). **Width:** all (tested at 390).
- **Shot:** <shots>/masters/today-joining_date.png
- **Wrong:** the chips render `data-set-date=""`, so a tap writes an empty value, and it also clears a date already typed. Date of joining is required, so a user who taps Today moves on and is sent back at the end with "Date of joining is required." Business settings has correct values (03-11-2026), so the fault is the masters' wiring.
- **Expected:** §5.7, A-15. Chips set the date and echo the weekday.
- **Likely files:** internal/masters/mstkit/web/web.go:1039 passes `h.deps` to `docscreen.FieldInput`, and internal/ui/docscreen/inputs.go:98 skips the dates when `d.Today` is nil. Only internal/app/routes.go:266 sets it. Also consider DateInput refusing to draw chips with no value, so this can't recur silently.

### H2. A session that expires during a simple-master Save gives a bare white page and loses the entry
- **Screen:** every single-page master form (reproduced on /m/uom/new). **Width:** 390.
- **Shots:** <shots>/auth/expired-post-real-390.png, <shots>/failure/csrf-plain-390.png
- **Wrong:** the page is unstyled black serif text, "Your session has expired. Sign in again.", with no link, no shell and no form. Back goes to sign-in, and the typed values are gone after signing in. A stale CSRF token gives the same kind of page: "Your form has expired. Reload the page and try again."
- **Expected:** §5.14, DS rule 9 ("Nothing typed is ever lost") and CD-382's return path. A plain POST should at least reach sign-in with `next` set to its page and the reason shown.
- **Likely files:** internal/kernel/auth/web/guard.go:134 (`web.Error` for a non-htmx, non-GET request) and the CSRF path at guard.go:95. Master forms post plain: internal/masters/mstkit/web/screens.templ uses `components.Form` without `HX`. Giving these forms HX would also fix the second half of H3.
- **How tested:** I signed out a twin of my own session, leaving a real revoked cookie. No one else's session was touched. Natural idle timeout was not waited for.

### H3. A network or server failure on a scrolled phone form shows nothing, and the offered action loses typing
- **Screen:** party wizard step 1 (shared `#network`, so probably every long form). **Width:** 390.
- **Shots:** <shots>/failure/network-banner-viewport-390.png, <shots>/failure/plain-post-drop-390.png
- **Wrong, three parts:**
  1. `#network` sits in page flow at the top. With the form scrolled, the banner rendered at y = -426, entirely off-screen. The button returned to idle and nothing else changed, so the tap looks ignored. It is not focused, and `aria-live` is the only signal.
  2. The text is "Your change may have been saved. Reload to check before trying again." for dropped network, 500 and 503 alike. On a master wizard that says "Nothing is saved until the last step" this is untrue, and Reload threw away typed values (Name and PAN came back empty).
  3. A simple-master Save (plain POST) never reaches the banner at all: a dropped connection lands on Chrome's "This site can't be reached".
- **Expected:** §3.3, §5.14, A-28. "Couldn't reach the server. Check your connection. Nothing you typed is lost." with Try again, values kept, and always visible. CD-346's "unsure" text is deliberate for a version-less POST, but it should not apply to a wizard step that only writes the session.
- **Likely files:** internal/ui/layout/page.templ:50, internal/ui/js/app.js (the network handler), and the CSS for `#network` (make it sticky or fixed, or scroll to and focus it).
- **How tested:** Playwright route abort or fulfil on one POST. Document wizards were not retested here.

### H4. Home is the Approvals page, and the sidebar has no Home
- **Screen:** bottom nav, sidebar, 404 "Go to Home". **Width:** all.
- **Shots:** <shots>/shell/home-tab-390.png, <shots>/shell/account-1280.png
- **Wrong:** `/` redirects to /approvals, so two of the five bottom tabs are one page. Tapping Home highlights Approvals, and Home can never be current. For a fresh user the landing screen is "Nothing is waiting for your approval." with no next step. The sidebar's Work group has no Home item, so A-2's listed group is short.
- **Expected:** §5.2 lists Home (`/`) and Approvals as separate items, with Home first in Work. §3.1: "see what is waiting for me and start a new document in one tap".
- **Likely files:** internal/app/routes.go:386 (`home`), routes.go:528 (nav). **This needs a ruling:** CD-231 predates v2 and sends `/` to the inbox, so either build a Home or formally drop the tab.

### H5. Branch State is a free text box that refuses the state's name
- **Screen:** /m/branch/new. **Width:** all (tested at 390).
- **Shot:** <shots>/masters/x-branch-state-word.png
- **Wrong:** typing "Maharashtra" (or "MH") answers "State is required. Pick the state where this branch is located.", about a filled field with nothing to pick. Only "27" works, and no hint says so. A valid GSTIN echoes "Maharashtra (27)" yet doesn't fill State. Party addresses already use a state select with names.
- **Expected:** §5.5 item 8 (a Select) and §5.4 ("the system should work them out"). Use the state select and derive it from the GSTIN.
- **Likely file:** internal/masters/company/branch.go:54 (`Type: doctype.Text`), and the message at branch.go:98.

### H6. Import masters and the stuck and overdue approvals pages have no link anywhere
- **Screen:** sidebar and drawer, Admin group. **Width:** all.
- **Shots:** <shots>/shell/drawer-768.png, <shots>/import/2-upload.png
- **Wrong:** a search of the code finds no link to /m/import/choose, /admin/approvals/stuck or /admin/approvals/overdue. Only someone who knows the URL gets there, and the plain /m/import is a 404. The import wizard itself works well (see "Checked and fine"), so this hides finished work.
- **Expected:** §5.2 Admin group lists Import and Stuck approvals.
- **Likely file:** internal/app/routes.go:583.

### H7. Desktop users can't reach Change password or My sessions
- **Screen:** header account chip. **Width:** 1280.
- **Shot:** <shots>/shell/account-1280.png
- **Wrong:** the menu holds only Sign out. The account links exist only in the Me sheet, whose trigger is hidden from 64rem, so all of them report not visible. The menu also stays open on Esc and on an outside click.
- **Expected:** §5.2: the account chip opens the account menu, matching Me (My requests, My sessions, Change password, Sign out). Esc closes it.
- **Likely file:** internal/ui/layout/header.templ:60.

### H8. Masters don't feel like the same product as the documents
- **Screen:** all master forms. **Width:** all.
- **Shots:** <shots>/sweep/m_item_new-390.png, <shots>/sweep/m_party_new-390.png, <shots>/masters/item-new-1280.png, compare docs/ux/v2-mocks/party-form.html
- **Wrong:** forms are bare label-and-box stacks on the canvas: no section cards, no icon tiles, no one-line descriptions, no examples in any input, almost no hints and no info tips. Code, GSTIN, HSN and the money field all start blank. The Item form is one 12-field scroll on a phone. Party puts PAN on step 1, but the state and GSTIN that could supply it are three screens away. This is what the owner means by "rudimentary", and the party mock shows the target (`e.g. SHREE01`, `e.g. Shree Steel Traders`, `e.g. AABCS1429B`).
- **Expected:** §5.5 items 1, 4 and 5: cards with icon and description, placeholders as examples, tips for the why. Money shows `0.00` (Standard Rate doesn't).
- **Likely files:** internal/masters/mstkit/web/sections.go, web.go, screens.templ, plus each master's field declarations.

## MEDIUM

**M1. Party and Employee wizards pass step 1 with required fields blank.** /m/party/new, /m/employee/new, 390. Code* and Name* blank still continue (Party needs only the role; Employee continues with nothing), and the error arrives only at Check's Save, which throws the user back to step 1. Expected: DS §8.3, a 422 on the failing step. Shots: <shots>/masters/pw390-a-role-only.png, <shots>/masters/pz-d-save-blank.png. Likely file: internal/masters/mstkit/web/wizard.go:166.

**M2. Master wizards say "Save and continue" under "Nothing is saved until the last step."** 390 and 1280. §5.6 says `Continue` in master wizards (setup and import already do). Shot: <shots>/masters/party-1-open.png. Likely file: internal/ui/wizard/frame.templ:40.

**M3. Start over drops the whole draft on one tap with no confirmation.** Party and employee wizards, 390. It is a ghost button just below the footer, and it posts straight to /start-over. Expected: §7 "Destructive actions confirm", as × already does. Shot: <shots>/masters/pf390-4-check-with-blank-addr.png. Likely file: internal/masters/mstkit/web/wizard.templ:49.

**M4. Two exits stacked at the top of every master wizard.** "‹ Parties" sits above the wizard head's ×, and it scrolls while × sticks. Only × goes through the leave dialog as far as I saw. Expected: §5.6, one wizard head. Shot: <shots>/sweep/m_party_new-390.png. Likely cause: `Back` passed alongside wizard `Close`.

**M5. The master list header pushes content down and adds a second primary.** 320 and 390. Label, search, label, sort, Export and New stack to about 360 px before the first row at 320. §5.12 and §7 put New in the bottom nav on phones and Export in the Filters sheet. There is no search icon or placeholder, no Filters button and no quick chips, and search runs only on Enter with no visible button. Shots: <shots>/lists/m_party-320.png, <shots>/lists/m_party-768.png.

**M6. Master list rows run code and name together and show nothing else.** All widths. Examples: "BAG Bags", "ZRP2 Shree…". The party list has no city, GSTIN or vendor/customer role, no disabled marker in the row, and no header row from 48rem. Expected: §5.12 rows and DS rule 10, recognise rather than recall. Shots: <shots>/lists/m_party-1280.png, <shots>/lists/m_uom-1280.png.

**M7. Sign-in gives no reason after expiry.** 390. A GET or htmx request that hits expiry lands on an ordinary sign-in page; the kernel's message goes in a 401 body nobody sees. The return itself works (CD-382): GET and htmx both came back to the right page. But a wizard user just sees sign-in appear. Show "Your session has expired. Sign in again." on the page. Shot: <shots>/auth/expired-wizard-390.png. Likely file: internal/kernel/auth/web/guard.go:129.

**M8. Sessions cards use the raw user agent as their title.** 390. "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit…" fills four lines, and `deviceName` returns it unchanged. Show "Chrome on Mac". There is no paging either: this page reached about 40,000 px with 122 sessions, though review traffic inflates that. The page title also says "Sessions" while the link says "My sessions". Shot: <shots>/sweep/account_sessions-390.png. Likely file: internal/kernel/auth/web/views/sessions.templ:48.

**M9. Roles is not the designed page.** 390 and 1280. Every role card carries a name input, Rename, a red Delete and Copy rights, so the list is long and dangerous to scroll on a phone, and "Add role" is a primary at the top. Expected: DS §8.6, roles list then role page. The rights page itself is good. Shots: <shots>/sweep/roles-1280.png, <shots>/sweep/roles-390.png.

**M10. Focus misses on two shell surfaces.** Bottom nav Search leaves focus on body at every phone width (§5.2: focuses the search box on load), so a phone user taps twice. The drawer focuses its container, not the current item (§5.2, DL §5.1). The drawer also has no brand row or user area. Shot: <shots>/shell/drawer-320.png.

**M11. Purchase home is short of the design.** 390 and 1280. No greeting (§5.12 item 1). The caught-up state has no second line (`New documents and approvals will show here.`) and uses the inbox icon, not `circle-check`. Note that A-24 passes on the words alone. Shot: <shots>/sweep/modules_purchase-390.png. Likely file: internal/ui/layout/modulehome.templ:30.

**M12. Numbering is a list of plain text lines.** 768 and 1280. Rows are unstyled, read as text not links, and at 20 px tall miss the 44 px target (5 links at both widths). They show raw `EA/{FY}/{#####}` with no card. Shot: <shots>/sweep/settings_numbering-1280.png.

**M13. A date typed as digits is refused.** Any date field, 390. `31032026` answers "is not a valid date", though the field opens a numeric keyboard where the dash is the hardest key to reach. Auto-insert the dashes or accept the form. Shared component, so this also reaches the document modules. Also noticed: because the digits stay as typed, tapping a chip and typing can produce `310320262-11-2026`. Likely file: internal/ui/js/field.js or date.js.

**M14. Party address and contact rows are weak.** 390. All four switches start off with no explanation of Primary billing or Primary shipping, and a first address should default to billing and primary. State is required for an Indian address but has no asterisk. An empty address row disappears silently on continue. Shots: <shots>/masters/pz-addr-form.png, <shots>/masters/pz-contact-form.png.

**M15. Error text doesn't always match what's on screen.** 390.
- "Enter a short code such as SHREE" appears on items, UoMs, tax codes and branches.
- "Stock UoM is required" sits under a field labelled UoM, and "Item type" under Type.
- "Tick Vendor, Customer or both." and "Tick Billing, Shipping or both." sit under a segmented control and switches, which nobody ticks.
- Payment term row errors list before Code, out of page order.
Shots: <shots>/masters/item-blank-390.png, <shots>/masters/payment-terms-blank-390.png. Owning areas change these texts (CD-147).

## LOW

- **L1. Mixed names and case.** Page titles "Expense Types", "Payment Terms", "Tax Codes", "UoMs" (nav says Units); labels "Standard Rate", "Tax Code"; nav "Companies" over a page titled "Company". DS §9 is sentence case. <shots>/lists/m_uom-390.png
- **L2. The ₹ prefix is 12 px beside a 16 px value,** below the 13 px minimum and hard to read. Shared. <shots>/masters/x-item-conversion.png, class `field__pre`.
- **L3. Sign-in polish.** Sign in is 44 px, not 48 (§5.3 item 5). The company legal name isn't shown though a company exists (§5.3 item 1). The banner shows a visible "Error:" prefix (§5.3 has the text alone). A signed-in user at /signin gets the form, not a redirect. Sign-out gives no confirmation. <shots>/auth/signin-fail-390.png, <shots>/auth/signed-out-390.png
- **L4. Input focus ring is near-black (rgb 20,26,35),** not the accent ring with accent-100 halo of §5.1 and §7. Visible and compliant, but off-design. <shots>/auth/signin-1280.png
- **L5. Empty sections are blank cards.** Payment Terms on a party page and Addresses and Contacts on Check show a heading and nothing. Say "Not set". <shots>/lists/party-disabled-390.png
- **L6. Every empty state uses the inbox icon.** §5.14 asks for an object icon. <shots>/sweep/m_item-390.png
- **L7. Primary buttons stretch full width on desktop** on Roles and Approval rules. <shots>/sweep/admin_approval-rules-1280.png
- **L8. Site Type uses radios while Item Type uses Segmented,** both two short options (§5.5 item 8). <shots>/masters/site-blank-390.png
- **L9. Pickers say "Type to search"** in place of §5.8's `Name or code`, and an empty master answers `No matches for "".` on open. <shots>/masters/pw390-b-payterm-picker-open.png
- **L10. The 404 tab title is the whole sentence,** "Error: This record doesn't exist or you don't have access to it. · Paperweight", and focus stays on body. <shots>/sweep/nonexistent-xyz-390.png

## Checked and fine

- **Overflow:** none on any of the 43 swept routes at 320, 360, 390, 768 and 1280. List and record pages were also checked at all five with a party name of about 100 characters and a long item name.
- **Targets:** every interactive element met 44 px except the Numbering rows (M12).
- **axe-core** (WCAG 2.2 AA tags): zero violations on 27 pages at 390 and 1280.
- **Sign-in:** a failed attempt shows the right text, clears and focuses the password, marks no field invalid, and sets the `Error:` title. A-5 and A-6 geometry holds, and the eye sits inside the field.
- **`?next=https://evil.example/` is refused,** landing on /approvals. Expiry return works for GET and htmx (CD-382).
- **Shell:** New sheet and Me sheet focus, Esc return, backdrop close, the `/` shortcut, skip link and tab order all work.
- **Loading bar and busy state:** the bar appears after 300 ms and the button shows "Saving…" under throttled network. A triple tap on a plain Save sent one POST.
- **Master errors:** summary focused, title prefixed, each link reaches its field, values kept. GSTIN, PAN, HSN and GST-rate messages are excellent, for example "GST rate 17% is not a notified rate. Use one of 0.1, 0.25, 1, 1.5, 3, 5, 6, 7.5, 12, 18, 28, 40."
- **Live commas** on Standard Rate (1,50,000.5, then 1,50,000.50 on blur).
- **Other good screens:** 404 page, party edit, Disable and Enable, and the rights page at 360. The import wizard ran end to end including a bad row, an error download link and a real import.

## Could not reach or not tested

- **403 page:** needs a non-admin user, none exists, and B1 means one can't be added through the UI. A-4's per-role New sheet and the non-admin views are untested for the same reason.
- **Toasts:** I found no trigger in these screens. Masters use flash banners; the callers are list exports, uploads and expense receipts.
- **First-run setup:** another reviewer finished it around 03:56 while I worked. I saw step 1 only, in <shots>/sweep/home-390.png.
- **Empty states with no company:** only the first sweep saw these.
- **Native calendar sheet:** Playwright can't drive it. The Choose date button exists.
- **Real phones and real keyboards:** all phone widths were Chromium emulation, so M13's keyboard point is reasoned, not observed.
- **Natural session timeout:** simulated by revocation (H2).
- **Delete on masters:** only the link was seen; I did not delete. **Save and save-list paths:** Project and Site were not saved, and Employee stopped at the H1 refusal.
- **Interactive depth by width:** all routes were swept at all five widths, but flows were driven mainly at 390 and 1280, with shell checks at 320, 360 and 768.
- **Known items** (five banner redraws without focus, Segmented word splits, Select text under the chevron) were seen, and none is worse than described. Project Status at 320 shows the word splits.

## Records left in the shared data

Party ZRP2 (renamed "ZR Edited Party", left disabled), item ZRI-1, branch ZRB, expense types ZRX1 and ZRIMP2. Sign-outs affected only my own sessions; "End all other sessions" was never used. Scripts are in <shots>/tools/, built on the approach of scripts/uishots/shots.py but not a copy of it. No project file was changed.
