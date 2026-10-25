# Identity & permissions (AUTH)

## Scope
AUTH owns users, sign-in, sign-out, sessions, CSRF, password rules and hashing, failed-login lockout, the first administrator, roles, the role × DocType rights matrix (read, create, write, submit, cancel, amend, delete, approve, print, export, import), record scopes (own, team and all), and per-user record restrictions by Project and Site. It also owns two admin screens built from UI components: the user restrictions editor and the read-only "Who can…" view (CD-49).
It exposes one permission service (`auth.Service.Can` for one record, `auth.Service.Scope` for a SQL filter) that every channel must use: screens, lists, counts, link pickers, prints, PDF, CSV export/import, attachments and JSON endpoints.
It relies on DOC for the state machine (AUTH only answers "may this user attempt this transition") and for the DocType registry it reads rights metadata from, APR for amount rules and self-approval, MST for the Employee, Project and Site masters and the reporting lines behind team scope (`auth.Directory`), NUM for the list query builder and CSV pipeline, FILE for print and attachment handlers, and DOC's audit trail (F6) for changes to users, roles and rights.

## Decisions
| ID | Question | ERPNext/Frappe | Odoo | Decision | Why |
|---|---|---|---|---|---|
| D-AUTH-01 | What identifies a user at sign-in? | Email by default. Username and mobile number can be turned on. Email is stored lowercased and trimmed (`frappe/frappe/core/doctype/user/user.py:208`, `:954`) | Separate `login` field (`odoo/odoo/addons/base/models/res_users.py:857`) | Email only. Trimmed, lowercased, unique. The user's primary key is an integer ID (F3), so an administrator can change the email later. | One identifier means one lockout key and one place to check. 20 people all have company email addresses. |
| D-AUTH-02 | Password hashing | passlib `pbkdf2_sha256` by default, also verifies `argon2`, rehashes on login when the hash is out of date (`frappe/frappe/utils/password.py:19`, `:37`) | passlib `pbkdf2_sha512`, at least 600,000 rounds, old schemes marked deprecated so they get rehashed (`odoo/odoo/addons/base/models/res_users.py:90`, `:1305`) | **argon2id** from `golang.org/x/crypto/argon2` (BSD-3-Clause): memory 19 MiB (19456 KiB), 2 iterations, parallelism 1, 16-byte random salt, 32-byte key, stored as a PHC string (`$argon2id$v=19$m=19456,t=2,p=1$…`). On a successful sign-in, a hash with old parameters is replaced. | These are the OWASP minimum argon2id settings. The algorithm is memory-hard and is in the Go x/crypto library, so we need no extra dependency. Both reference systems rehash on sign-in, so we do too. |
| D-AUTH-03 | Password policy | zxcvbn score of at least 2 when the policy is on. Maximum 512 characters. Text is NFC-normalised. The new password can't equal the current one (`frappe/frappe/utils/password.py:17`, `:27`, `frappe/frappe/core/doctype/user/user.py:1094`) | Password must not be empty (`res_users.py:1039`). Minimum length comes from the `auth_password_policy` add-on, which isn't in this clone | At least 10 and at most 256 characters, NFC-normalised before hashing. Rejected if it's on an embedded common-password list, contains the local part of the user's email, or equals the current password. No composition rules and no periodic expiry. | Follows NIST 800-63B. I couldn't confirm that a Go zxcvbn port is maintained, so we don't depend on one. A fixed list plus a minimum length is easy to test and easy to explain to users. |
| D-AUTH-04 | Lockout after failed sign-ins | Counts failures per account and per IP, and counts unknown names the same way. The counter expires a lock interval after the first failure. Defaults are 10 attempts and 60 s (`frappe/frappe/auth.py:278`, `:525`, `:561`, `system_settings.json:331`, `:342`) | Counts per IP: after 5 failures, a 60 s cooldown follows each further failure (`res_users.py:1326`, `:1394`) | **Per account** (normalised email, known or unknown): 5 failures inside 15 minutes lock the account for 15 minutes, counted from the first failure. **Per IP**: 50 failures inside 15 minutes block that IP for 15 minutes. A success resets the account counter. An administrator can unlock an account. Counters live in SQLite and survive a restart. | A per-account limit stops password guessing. A per-IP limit stops spraying one password across many accounts. Frappe's lock-from-first-failure window means a counter can never stay forever. |
| D-AUTH-05 | What does a failed sign-in say? | The same "Invalid login credentials" for an unknown user, a wrong password and a locked unknown user. A dummy hash is checked when the user is unknown. "User disabled" is shown only after a correct password (`auth.py:297`, `:304`, `:308`, `utils/password.py:136`) | `AccessDenied` with no detail | "Invalid email or password." for an unknown email, a wrong password and a user with no password. The disabled message is shown only after the correct password. The lockout message is the same for known and unknown emails. A dummy argon2id verify runs when there is no stored hash. | Nobody can find out which emails exist, and staff still get a helpful message when their account is disabled. |
| D-AUTH-06 | Session model and expiry | Sessions are stored server side under SHA-256 of the session ID. The session expires after it has been idle for longer than `session_expiry` (170 h in the settings default, 240 h in the code fallback) (`frappe/frappe/sessions.py:30`, `:413`, `:563`) | Session token is an HMAC over the user's ID, login, password hash and active flag, so changing any of them kills every session (`res_users.py:949`) | Server-side `sessions` table holding SHA-256 of a 32-byte random token. Sessions expire after **12 h idle** or **7 days** in total, whichever comes first. The cookie is `HttpOnly`, `SameSite=Lax`, `Secure` when served over HTTPS, has the host-only `__Host-` prefix when served over HTTPS, and has no expiry date (a browser-session cookie; the server enforces both limits, CD-14). Changing your own password ends your other sessions. An administrator resetting your password, disabling you or deleting you ends all your sessions. | A token is useless if the database leaks (Frappe). Ending sessions on a credential change copies Odoo's protection. 12 h covers a working day. |
| D-AUTH-07 | Several sessions for one user | Allowed unless `deny_multiple_sessions` is on (default off) (`frappe/frappe/auth.py:261`) | Allowed. A user can "log out from all devices" (`test_res_users.py:896`) | Allowed. A user can list their own sessions and end any of them, or all others. | Staff use a phone and a desktop. |
| D-AUTH-08 | Superuser / administrator model | A hard-coded `Administrator` user bypasses every check and can't be disabled or deleted (`permissions.py:109`, `user.py:395`, `:650`) | An inactive superuser (`test_res_users.py:837`). At least one admin must exist (`res_users.py:645`). You can't deactivate yourself (`res_users.py:686`) | No hidden user. An `Administrator` **role** grants every right with no record restrictions. At least one enabled user must hold it. You can't disable yourself or remove your own Administrator role. Holding the role never skips lifecycle rules (DOC) or approval rules (APR). | Odoo's model is safer: there's no shared admin password and every action belongs to a named person. |
| D-AUTH-09 | Creating the first administrator | The setup wizard sets the Administrator password at install | An admin is created along with the database | When the `users` table is empty: `app admin create --email … --name …` on the server prompts for a password, or the `/setup` screen is used. `/setup` answers 404 once any user exists. The two paths are serialised so only one admin can be created. | One-time and race-free, and needs no default password. |
| D-AUTH-10 | Shape of the rights model | Each role × DocType row (DocPerm) holds 14 rights. Rights are combined with OR across roles. Custom rows replace the standard rows (`permissions.py:14`, `:289`, `test_permissions.py:889`) | `ir.access` rows give CRUD access per group with an optional domain. Group permissions are combined with OR (`ir_access.py:496`) | 11 rights per role × DocType: read, create, write, submit, cancel, amend, delete, approve, print, export, import. Rights are combined with **OR across a user's roles**. The matrix is stored in the database, seeded from Go defaults, and editable by an Administrator. Every edit is audited. Actions map to rights (changed by spec R4): save → `create` (new) / `write` (existing); submit, withdraw, close → `submit`; approve, reject → `approve`; revise → `write`; cancel → `cancel`; amend → `amend`; delete → `delete`; print → `print`. There is no `reject` or `close` right. | The rights list matches the business verbs in F5. Small companies rename and reshape roles, so the matrix must be editable. The set of DocTypes stays fixed in code. |
| D-AUTH-11 | Rights that don't fit a DocType | Submit is zeroed when the DocType isn't submittable, and import when imports aren't allowed (`permissions.py:160`, `:258`) | – | The matrix rejects submit, cancel, amend and approve on masters, and import on DocTypes that don't allow import. Every row that grants any other right must also grant read. | Stops a meaningless matrix from being saved. |
| D-AUTH-12 | "Only own documents" and "my team's documents" | `if_owner` on a DocPerm row: the list shows only the user's own rows, every right except create applies to own rows only, and a role without `if_owner` overrides it (`permissions.py:324`–`345`, `:261`). ERPNext's expense approver is set per claim and given access by sharing (`hrms/hrms/hr/utils.py:785`) | Domain rules such as `employee_id.user_id = user.id` (`odoo/addons/hr_expense/security/ir.access.csv:10`). The Team Approver group sees expenses of employees they manage (`odoo/addons/hr_expense/security/ir.access.csv:4`, `odoo/addons/hr_expense/models/hr_employee.py:45`) | Every granted right on a role × DocType row has scope **own**, **team** or **all** (CD-11). **Own** means `created_by` is me, or, on DocTypes with an Employee field, that Employee is linked to me. **Team** means own, plus records whose Employee reports **directly** to me and is active. Team is offered only on DocTypes with an Employee field. Create isn't affected. The widest scope any of my roles grants wins (all > team > own). | This is Frappe's model plus Odoo's employee link, which expense claims need when an accountant keys one in for an employee, plus Odoo's team approver so a reporting manager can see and approve the claims of their direct reports. Direct reports only (Odoo also includes indirect ones) keeps the rule easy to state. |
| D-AUTH-13 | Record restrictions by Project / Site | User Permissions list the allowed values for a link DocType and apply them to every link field of that type. When "strict" is off (the default), documents with an empty link stay visible. Restrictions on different DocTypes combine with AND. An owner keeps `if_owner` rights despite a restriction (`permissions.py:358`, `:373`, `:423`, `:271`) | Restrictions are `ir.access` rows without a group, combined with AND (`ir_access.py:129`, `:496`) | A user can have a list of allowed **Projects** and a list of allowed **Sites**. An empty list means no restriction. The restriction applies to every DocType with that link field, in the header or on any line. **Strict**: a restricted user doesn't see documents where that field is empty. Restrictions combine with AND, with each other and with "own". They never grant anything. When the list has exactly one value, it's the default on new documents. | Strict is safer, and makes the rule easy to state: "you only see Site PUNE-WH". Odoo's AND is simpler than Frappe's owner exception. |
| D-AUTH-14 | Who may print | Read **or** print is enough (`frappe/frappe/www/printview.py:479`) | Print reports need read access | Needs the **print** right and the document must be visible. A PDF download is the same check. | Some roles may view on screen but must not produce formal POs to send to vendors. |
| D-AUTH-15 | Export | Needs the export right. An owner-only export is filtered to the user's own rows (`frappe/frappe/desk/reportview.py:560`, `test_permissions.py:107`) | Export follows read access | Needs the **export** right. Rows are filtered exactly as the list is: own scope, restrictions and filters. | No back door around list filtering. |
| D-AUTH-16 | Response when access is denied | `PermissionError` naming the missing access or the blocking link (`permissions.py:147`–`157`, `test_permissions.py:172`) | `AccessError` listing the groups that would allow it (`ir_access.py:326`) | A missing right on a DocType or record you can see gives **403** with a plain message naming the action and DocType. A record hidden by own scope or a restriction gives **404**, the same as a record that doesn't exist. | Lists and detail screens agree: if a record isn't in your list, it doesn't exist for you. Document numbers from other sites aren't confirmed. |
| D-AUTH-17 | Remove a user | Delete is allowed, and owner fields are swept to a new name (`user.py:650`). Disabling ends sessions (`user.py:395`) | Users are archived. The superuser can't be activated | **Disable, don't delete**, once the user has signed in or is referenced anywhere (F4 analogue). A user who never signed in and isn't referenced may be deleted. | Audit trail and `created_by` must keep pointing at a real user. |
| D-AUTH-18 | When role or right changes take effect | Caches are cleared, and the change applies on the next check (`test_permissions.py:651`) | `has_group` cache is cleared on write (`test_res_users_has_group.py:322`) | On the user's **next request**, with no sign-out needed. Rights are read per request, with a cache keyed on a global permissions version. | A role taken away at 10:00 must stop working at 10:00. |
| D-AUTH-19 | Forgotten password | Emailed single-use link that expires. The response doesn't reveal whether the user exists (`user.py:1202`, `:1342`) | Identity-check wizard; reset by email | v1: an Administrator sets a temporary password, and the user **must change it at next sign-in**. Self-service email reset comes later. | v1 has no email inbox, and outgoing mail isn't in scope yet. The admin is in the same office. |
| D-AUTH-20 | CSRF | Each session gets a CSRF token, required on unsafe methods by header or form field (`frappe/frappe/auth.py:93`, `sessions.py:241`) | CSRF token on forms | A token per session, required on every POST, PUT, PATCH and DELETE: from HTMX in the `X-CSRF-Token` header, from plain forms in a hidden field. The sign-in form uses a pre-session token. | HTMX sends requests carrying the session cookie, and `SameSite=Lax` alone isn't enough. |
| D-AUTH-21 | Role hierarchy | Flat roles, plus role profiles (`user.py:285`) | Groups imply other groups (`test_res_users_has_group.py:226`) | Flat roles. A user can hold several roles. | Easier to reason about for about 6 roles. |
| D-AUTH-22 | Default role for new users | None. Saving a user without roles shows a warning (`user.py:995`) | Default groups are added (`test_res_users.py:407`) | New users get **Employee** by default (own expense claims). The administrator may remove it. A warning shows if a user ends up with no roles. | Every staff member files expenses. |

## Test cases

Fixtures used below come from `00-fixtures.md` (company: Vardhan Infra Projects Pvt Ltd, PAN `AAFCV2449G`; head office branch `PUNE`, GSTIN `27AAFCV2449G1Z7`, Maharashtra). The rights listed are the seeded matrix rows this file relies on:

| User | Roles | Rights this file relies on | Restrictions |
|---|---|---|---|
| `admin@vardhan.test` (Anil Kapoor) | Administrator | every applicable right | – |
| `ravi@vardhan.test` (Ravi Kulkarni) | Purchase Manager, Employee | Purchase Manager: PO read, approve, cancel, amend, print, export (all) | – |
| `priya@vardhan.test` (Priya Shah) | Purchase User, Employee | Purchase User: PO read, create, write, submit, print, export (all); delete (own) | Projects: `PRJ-METRO` |
| `suresh@vardhan.test` (Suresh Patil) | Stores, Employee | Stores: GRN read, create, write, submit, print (all); PO read (all) | Sites: `PUNE-WH`, `METRO-SITE` |
| `deepak@vardhan.test` (Deepak Nair) | Employee | Employee: Expense Claim create, write, submit, print (own); read, approve (team) | – |
| `old@vardhan.test` (Former Staff) | Employee | disabled user | – |

The Employee role held by Ravi, Priya, Suresh and Kavya adds Expense Claim rights: own, plus read and approve over the claims of their direct reports (team). Reporting lines: Priya manages Deepak; Ravi manages Priya and Suresh; Meena manages Ravi and Kavya. Meena (Director) and Kavya (Masters Manager) are in the seed but are used only where a case names them. Any other user or role is created in the case that needs it.

The password of every fixture user is `Correct-Horse-42`. The clock is fixed at 2026-10-24 10:30 IST (`2026-10-24T05:00:00.000Z`, FY 2026-27). Sites are `PUNE-WH` (warehouse, branch PUNE), `METRO-SITE` (branch PUNE, project PRJ-METRO) and `TECH-SITE` (branch BLR, project PRJ-TECHPARK). Projects are `PRJ-METRO` (Pune Metro Depot) and `PRJ-TECHPARK` (Whitefield Tech Park). POs in this file are for Project `PRJ-METRO` unless a case says otherwise, so Priya can see them. Draft, pending and rejected documents have no number and show as `Draft {id}` (spec R1); approved, closed and cancelled ones show numbers such as `PO/26-27/00001`.

### AUTH-001 · Correct email and password signs the user in
- **Priority:** P1
- **Level:** integration
- **Given** an enabled user `priya@vardhan.test` with password `Correct-Horse-42` **When** Priya POSTs `/signin` with those credentials and a valid CSRF token **Then** the response is 303 to `/`, a `session` cookie is set (`HttpOnly`, `SameSite=Lax`, no `Expires`), one row exists in `sessions` for Priya's user ID, `users.last_login_at` is set to the current UTC time (`2026-10-24T05:00:00.000Z` on the fixture clock), and an auth-log row `login_success` is written with Priya's user ID and IP.
- **Go test:** `TestLogin_ValidCredentials`
- **Sources:** `frappe/frappe/auth.py:158` (`LoginManager.login`), `frappe/frappe/tests/test_auth.py:169` (`test_allow_login_using_mobile`), `odoo/odoo/addons/base/models/res_users.py:857` (`_login`)

### AUTH-002 · Email is matched case-insensitively and trimmed
- **Priority:** P1
- **Level:** unit
- **Given** user `priya@vardhan.test` **When** Priya signs in as `"  Priya@Vardhan.TEST "` with the right password **Then** sign-in succeeds as the same user ID.
- **Go test:** `TestLogin_EmailCaseAndSpaceInsensitive`
- **Sources:** `frappe/frappe/core/doctype/user/user.py:208` (`autoname` lowercases and strips the email), `frappe/frappe/utils/password.py:167` (comment "lettercase agnostic")

### AUTH-003 · Wrong password is rejected with a generic message
- **Priority:** P1
- **Level:** integration
- **Given** user `priya@vardhan.test` **When** Priya signs in with `Correct-Horse-43` **Then** the response is 401, the form shows "Invalid email or password.", no session is created, and an auth-log row `login_failed` is written without the password text.
- **Go test:** `TestLogin_WrongPassword`
- **Sources:** `frappe/frappe/auth.py:302` (`authenticate`), `frappe/frappe/auth.py:343` (`fail` writes the authentication log), `frappe/frappe/tests/test_auth.py:539` (`test_failed_api_key_auth_is_logged_without_the_credentials`)

### AUTH-004 · Unknown email gets exactly the same response as a wrong password
- **Priority:** P1
- **Level:** integration
- **Given** no user `nobody@vardhan.test` **When** someone signs in with it **Then** the status (401), the body text "Invalid email or password." and the response headers equal those for AUTH-003, apart from the CSRF token.
- **Go test:** `TestLogin_UnknownEmailSameResponse`
- **Sources:** `frappe/frappe/auth.py:297`, `frappe/frappe/tests/test_auth.py:104` (`test_locked_account_response_matches_unknown_account`), `frappe/frappe/tests/test_auth.py:141` (`test_unknown_account_login_without_lock_interval`)

### AUTH-005 · Unknown email and missing password still run one hash verification
- **Priority:** P1
- **Level:** unit
- **Given** a spy wrapped around the argon2id verifier **When** sign-in is attempted (a) for an unknown email and (b) for a user who has no password set **Then** the verifier is called exactly once in each case, against a fixed dummy hash, and sign-in fails with "Invalid email or password."
- **Go test:** `TestLogin_UnknownUserSpendsHashTime`
- **Sources:** `frappe/frappe/tests/test_auth.py:152` (`test_unknown_account_checks_a_password_hash`), `frappe/frappe/tests/test_auth.py:157` (`test_user_without_a_password_checks_a_password_hash`), `frappe/frappe/utils/password.py:136` (`verify_against_placeholder`)

### AUTH-006 · Empty email or password is rejected before any lookup
- **Priority:** P1
- **Level:** unit
- **Given** the sign-in form **When** it is posted with email `""` or password `""` **Then** the response is 400 with "Enter your email and password.", no user lookup runs, and no failure is counted.
- **Go test:** `TestLogin_IncompleteDetails`
- **Sources:** `frappe/frappe/auth.py:284` (`authenticate`: "Incomplete login details"), `odoo/odoo/addons/base/models/res_users.py:940` (`_check_uid_passwd`: an empty password is refused)

### AUTH-007 · Disabled user with the correct password is refused with a clear message
- **Priority:** P1
- **Level:** integration
- **Given** `old@vardhan.test` (Former Staff) is disabled **When** they sign in with their correct password `Correct-Horse-42` **Then** the response is 403 with "Your account is disabled. Ask your administrator to enable it.", no session is created, and the account's failure counter goes up by 1.
- **Go test:** `TestLogin_DisabledUserCorrectPassword`
- **Sources:** `frappe/frappe/auth.py:305` (disabled branch, "User disabled or missing"), `odoo/odoo/addons/base/models/res_users.py:944` (`_check_uid_passwd`: inactive user refused)

### AUTH-008 · Disabled user with a wrong password gets the generic message
- **Priority:** P1
- **Level:** unit
- **Given** `old@vardhan.test` is disabled **When** they sign in with a wrong password **Then** the message is "Invalid email or password.", so the disabled status isn't revealed.
- **Go test:** `TestLogin_DisabledUserWrongPasswordGeneric`
- **Sources:** `frappe/frappe/auth.py:302`–`308` (password is checked before the enabled flag)

### AUTH-009 · Account locks after 5 failures within 15 minutes
- **Priority:** P1
- **Level:** integration
- **Given** `priya@vardhan.test` with no recent failures and the fixture clock at 2026-10-24 10:30:00 IST **When** Priya fails 5 times between 10:30 and 10:34 and then tries a 6th time with the **correct** password at 10:35 **Then** attempts 1–5 return "Invalid email or password." and attempt 6 returns 429 with "Too many failed sign-in attempts. Try again in 15 minutes, or ask your administrator to unlock your account." No session is created.
- **Go test:** `TestLockout_LocksAfterFiveFailures`
- **Sources:** `frappe/frappe/tests/test_auth.py:573` (`test_account_lock`), `frappe/frappe/auth.py:545` (lock message), `odoo/odoo/addons/base/models/res_users.py:1383` (`_assert_can_auth`)

### AUTH-010 · Lock is lifted 15 minutes after the first failure
- **Priority:** P1
- **Level:** unit
- **Given** the lock from AUTH-009, with the first failure at 10:30:00 IST **When** the correct password is tried at 10:44:59 and again at 10:45:01 **Then** the first try is still locked and the second succeeds and clears the counter.
- **Go test:** `TestLockout_UnlocksAfterWindow`
- **Sources:** `frappe/frappe/tests/test_auth.py:591` (`test_account_unlock`), `frappe/frappe/auth.py:600` (`add_failure_attempt`: the window starts at the first failure), `odoo/odoo/addons/base/models/res_users.py:1394` (`_on_login_cooldown`)

### AUTH-011 · A successful sign-in resets the failure counter
- **Priority:** P1
- **Level:** unit
- **Given** 4 failures for `priya@vardhan.test` **When** Priya signs in correctly and then fails 4 more times **Then** Priya is not locked (the count is 4, not 8).
- **Go test:** `TestLockout_SuccessResetsCounter`
- **Sources:** `frappe/frappe/auth.py:606` (`add_success_attempt`), `frappe/frappe/utils/password.py:182` (`delete_login_failed_cache`), `odoo/odoo/addons/base/models/res_users.py:1392`

### AUTH-012 · Failure counters always expire
- **Priority:** P1
- **Level:** unit
- **Given** any failure row for an account key **When** it is written **Then** it has an `expires_at` of no more than 15 minutes after the first failure. A cleanup job removes expired rows, and a row with no expiry can't be written (NOT NULL constraint).
- **Go test:** `TestLockout_CounterAlwaysExpires`
- **Sources:** `frappe/frappe/tests/test_auth.py:610` (`test_failure_counter_always_expires`), `frappe/frappe/auth.py:553` (comment: "a counter can never exist without an expiry")

### AUTH-013 · Concurrent failures are all counted
- **Priority:** P1
- **Level:** integration
- **Given** an account key with no failures **When** 20 goroutines record a failure at once against the SQLite database (WAL mode) **Then** the stored count is exactly 20 and the account is locked.
- **Go test:** `TestLockout_ConcurrentFailuresCounted`
- **Sources:** `frappe/frappe/tests/test_auth.py:634` (`test_concurrent_failures_are_all_counted`)

### AUTH-014 · Unknown emails lock at the same attempt as real ones
- **Priority:** P1
- **Level:** integration
- **Given** the threshold is 5 **When** 6 attempts are made for `ghost@vardhan.test` (unknown) and 6 for `priya@vardhan.test` with wrong passwords **Then** both sequences return identical status codes and messages, and both lock at attempt 6. Unknown keys are stored as SHA-256 of the normalised email, never as the plain email.
- **Go test:** `TestLockout_UnknownEmailIndistinguishable`
- **Sources:** `frappe/frappe/tests/test_auth.py:104` (`test_locked_account_response_matches_unknown_account`), `frappe/frappe/auth.py:802` (`UnknownUserLoginAttemptTracker` hashes the name), `frappe/frappe/tests/test_auth.py:637`

### AUTH-015 · Per-IP block after 50 failures across accounts
- **Priority:** P2
- **Level:** integration
- **Given** IP `203.0.113.7` **When** it makes 50 failed attempts in 15 minutes, each against a different email **Then** attempt 51 from that IP returns 429 with "Too many failed sign-in attempts from your network. Try again in 15 minutes.", even for a correct password. A request from `203.0.113.8` is unaffected.
- **Go test:** `TestLockout_PerIPBlock`
- **Sources:** `frappe/frappe/auth.py:291` (`ip_tracker`), `odoo/odoo/addons/base/models/res_users.py:1363` (cooldown keyed on `remote_addr`)

### AUTH-016 · Administrator can unlock a locked account
- **Priority:** P2
- **Level:** integration
- **Given** `priya@vardhan.test` is locked **When** `admin@vardhan.test` clicks "Unlock" on Priya's user record **Then** Priya's failure counter is cleared, Priya can sign in at once, and the audit trail records "unlocked by Anil Kapoor".
- **Go test:** `TestLockout_AdminUnlock`
- **Sources:** `frappe/frappe/auth.py:606` (`add_success_attempt` clears the counter); Odoo has no equivalent (decision D-AUTH-04)

### AUTH-017 · Over-long password is rejected the same way for known and unknown users
- **Priority:** P1
- **Level:** unit
- **Given** a password of 257 characters **When** it is used to sign in (a) for `priya@vardhan.test` and (b) for `nobody@vardhan.test` **Then** both return 400 with "Password must be at most 256 characters." and no hash is computed.
- **Go test:** `TestLogin_OversizedPasswordSameError`
- **Sources:** `frappe/frappe/tests/test_password.py:107` (`test_oversized_password`), `frappe/frappe/utils/password.py:32` (`validate_password_size`)

### AUTH-018 · Sign-in redirect only goes to a local path
- **Priority:** P1
- **Level:** unit
- **Given** the sign-in URL `/signin?next=X` **When** X is `/po/123` **Then** a successful sign-in redirects to `/po/123`. **When** X is `https://evil.example/`, `//evil.example`, `/\evil.example` or `javascript:alert(1)` **Then** it redirects to `/`.
- **Go test:** `TestLogin_NextParamLocalOnly`
- **Sources:** `frappe/frappe/www/login.py:203` (`sanitize_redirect`)

### AUTH-019 · Sign-in events are logged without secrets
- **Priority:** P2
- **Level:** integration
- **Given** one successful and one failed sign-in **When** the auth log is read **Then** each row has a UTC time, the user ID (or the hashed email for an unknown user), the IP, the user agent, and an event of `login_success` or `login_failed`. The password text, the session token and the CSRF token appear nowhere in the log or the application logs.
- **Go test:** `TestAuthLog_NoSecrets`
- **Sources:** `frappe/frappe/tests/test_auth.py:539` (`test_failed_api_key_auth_is_logged_without_the_credentials`), `frappe/frappe/auth.py:343` (`fail`), `odoo/odoo/addons/base/models/res_users.py:878` (login logged with IP)

### AUTH-020 · Passwords are stored only as argon2id hashes
- **Priority:** P1
- **Level:** unit
- **Given** the password `Correct-Horse-42` is set for `priya@vardhan.test` **When** the `users` row is read **Then** `password_hash` starts with `$argon2id$v=19$m=19456,t=2,p=1$`, doesn't contain the password text, and verifies against `Correct-Horse-42`.
- **Go test:** `TestPassword_StoredAsArgon2id`
- **Sources:** `frappe/frappe/tests/test_password.py:65` (`test_hashed_password`), `odoo/odoo/addons/base/models/res_users.py:1305` (`_crypt_context`)

### AUTH-021 · Same password gets a different hash each time (random salt)
- **Priority:** P1
- **Level:** unit
- **Given** two users with the same password `Correct-Horse-42` **When** both are hashed **Then** the two stored hashes differ and both verify.
- **Go test:** `TestPassword_UniqueSalt`
- **Sources:** `frappe/frappe/utils/password.py:200` (`passlibctx.hash` salts every hash)

### AUTH-022 · Old password stops working after a change
- **Priority:** P1
- **Level:** integration
- **Given** `priya@vardhan.test` changes the password from `Correct-Horse-42` to `Chai-Break-Ledger-7` **When** Priya signs in with the old password **Then** Priya gets "Invalid email or password.", and the new password works.
- **Go test:** `TestPassword_OldPasswordRejectedAfterChange`
- **Sources:** `frappe/frappe/tests/test_password.py:65` (`test_hashed_password`), `odoo/odoo/addons/base/tests/test_res_users.py:853` (`test_change_password`)

### AUTH-023 · Hashes with outdated parameters are upgraded at sign-in
- **Priority:** P2
- **Level:** unit
- **Given** a stored hash `$argon2id$v=19$m=8192,t=1,p=1$…` for `Correct-Horse-42` **When** the user signs in successfully **Then** the stored hash is replaced by one with `m=19456,t=2,p=1`. A failed sign-in leaves it unchanged.
- **Go test:** `TestPassword_RehashOnLogin`
- **Sources:** `frappe/frappe/utils/password.py:37` (`verify_password` returns `needs_rehash`), `frappe/frappe/utils/password.py:176`, `odoo/odoo/addons/base/models/res_users.py:1318` (`deprecated=['auto']`)

### AUTH-024 · Unicode passwords are NFC-normalised before hashing
- **Priority:** P2
- **Level:** unit
- **Given** the password `Café-Ledger-43` set in NFD form (`e` followed by a combining accent) **When** the user signs in with the NFC form, and again with the NFD form **Then** both succeed, and `Cafe-Ledger-43` fails.
- **Go test:** `TestPassword_UnicodeNFC`
- **Sources:** `frappe/frappe/tests/test_password.py:88` (`test_unicode_password_normalized`), `frappe/frappe/utils/password.py:27` (`normalize_password`)

### AUTH-025 · Password policy: minimum length
- **Priority:** P1
- **Level:** unit
- **Given** a password being set **When** it is `Short-123` (9 characters) **Then** it's rejected with "Password must be at least 10 characters." **When** it is `Short-1234` (10 characters, not on the common list) **Then** it's accepted.
- **Go test:** `TestPasswordPolicy_MinLength`
- **Sources:** `frappe/frappe/core/doctype/user/test_user.py:289` (`test_password_strength`), `odoo/odoo/addons/base/models/res_users.py:1039` (`_change_password` rejects empty)

### AUTH-026 · Password policy: maximum length
- **Priority:** P1
- **Level:** unit
- **Given** a password being set **When** it has 256 characters **Then** it's accepted. **When** it has 257 **Then** it's rejected with "Password must be at most 256 characters."
- **Go test:** `TestPasswordPolicy_MaxLength`
- **Sources:** `frappe/frappe/utils/password.py:17` (`MAX_PASSWORD_SIZE`), `frappe/frappe/tests/test_password.py:107`

### AUTH-027 · Password policy: common passwords rejected
- **Priority:** P1
- **Level:** unit
- **Given** the embedded common-password list **When** the password is `password123`, `Password@123` or `qwertyuiop` (matched case-insensitively) **Then** it's rejected with "This password is too common. Choose a longer or less predictable one."
- **Go test:** `TestPasswordPolicy_CommonList`
- **Sources:** `frappe/frappe/core/doctype/user/test_user.py:289` (the password `"password"` is rejected on save), `frappe/frappe/utils/password_strength.py:158` ("This is a top-10 common password.")

### AUTH-028 · Password policy: must not contain the user's email name
- **Priority:** P2
- **Level:** unit
- **Given** user `priya@vardhan.test` **When** Priya sets `Priya-2026-Secure` **Then** it's rejected with "Password must not contain your name or email."
- **Go test:** `TestPasswordPolicy_NotContainingEmail`
- **Sources:** `frappe/frappe/utils/password_strength.py:20` (`test_password_strength` takes `user_inputs`), `frappe/frappe/utils/password_strength.py:174` ("Names and surnames by themselves are easy to guess.")

### AUTH-029 · New password must differ from the current one
- **Priority:** P1
- **Level:** unit
- **Given** Priya's current password is `Correct-Horse-42` **When** Priya sets `Correct-Horse-42` again **Then** it's rejected with "New password must be different from your current password."
- **Go test:** `TestPasswordPolicy_NotSameAsCurrent`
- **Sources:** `frappe/frappe/core/doctype/user/user.py:1094` (`is_password_reused`), `frappe/frappe/utils/password.py:113`

### AUTH-030 · Changing your own password needs the current password
- **Priority:** P1
- **Level:** integration
- **Given** `priya@vardhan.test` is signed in **When** Priya submits the change-password form with current `wrong-current-1`, new `Chai-Break-Ledger-7` and confirm `Chai-Break-Ledger-7` **Then** it's rejected with "Current password is incorrect.", the password is unchanged, and the attempt counts towards Priya's lockout.
- **Go test:** `TestChangePassword_RequiresCurrent`
- **Sources:** `odoo/odoo/addons/base/models/res_users.py:1019` (`change_password`), `frappe/frappe/core/doctype/user/user.py:1202` (`_get_user_for_update_password` with `old_password`)

### AUTH-031 · New password and its confirmation must match
- **Priority:** P1
- **Level:** unit
- **Given** the change-password form **When** new is `Chai-Break-Ledger-7` and confirm is `Chai-Break-Ledger-8` **Then** it's rejected with "New password and confirmation do not match."
- **Go test:** `TestChangePassword_ConfirmMismatch`
- **Sources:** `odoo/odoo/addons/base/models/res_users.py:1585` (`_check_password_confirmation`)

### AUTH-032 · Changing your own password ends your other sessions
- **Priority:** P1
- **Level:** integration
- **Given** `priya@vardhan.test` has sessions A (desktop) and B (phone) **When** Priya changes the password from session A **Then** session A stays valid, and session B's next HTMX request gets 401 "Your session has expired. Sign in again." (a plain browser GET from B gets 303 to `/signin?next=…`, CD-17).
- **Go test:** `TestChangePassword_EndsOtherSessions`
- **Sources:** `frappe/frappe/utils/password.py:188` (`update_password(logout_all_sessions=…)`), `frappe/frappe/core/doctype/user/user.py:1107` (`logout_on_password_reset`), `odoo/odoo/addons/base/models/res_users.py:949` (`password` is part of the session token)

### AUTH-033 · Administrator sets a temporary password; the user must change it at next sign-in
- **Priority:** P1
- **Level:** integration
- **Given** `admin@vardhan.test` sets the password of `suresh@vardhan.test` to `Temp-Godown-2026` **When** Suresh signs in with it **Then** Suresh is sent to `/account/password` and every other URL redirects there until Suresh sets a new valid password. All of Suresh's earlier sessions were ended at the reset.
- **Go test:** `TestAdminResetPassword_ForcesChange`
- **Sources:** `frappe/frappe/auth.py:316` (`force_user_to_reset_password`), `frappe/frappe/tests/test_auth.py:249` (`test_forced_password_reset_does_not_leak_reset_key`), `frappe/frappe/core/doctype/user/user.py:1371` (`change_password` by an admin with `logout_all_sessions=1`)

### AUTH-034 · Only an Administrator can set another user's password
- **Priority:** P1
- **Level:** integration
- **Given** `priya@vardhan.test` (not an admin) **When** Priya POSTs `/users/{suresh_id}/password` **Then** the response is 403 with "You don't have permission to change other users' passwords." and nothing changes.
- **Go test:** `TestAdminResetPassword_AdminOnly`
- **Sources:** `frappe/frappe/core/doctype/user/user.py:1371` (`change_password`: `check_permission("write")`), `odoo/odoo/addons/base/tests/test_res_users.py:688` (`test_user_writeable_fields`)

### AUTH-035 · Session token is random, and only its hash is stored
- **Priority:** P1
- **Level:** unit
- **Given** a sign-in **When** the cookie value and the `sessions` row are compared **Then** the cookie holds 32 random bytes (base64url), the row holds SHA-256 of them, and the raw token isn't found in any table or log.
- **Go test:** `TestSession_TokenHashedAtRest`
- **Sources:** `frappe/frappe/tests/test_auth.py:680` (`test_raw_sid_is_never_stored`), `frappe/frappe/tests/test_auth.py:685` (`test_session_is_stored_under_its_hash`), `frappe/frappe/sessions.py:30` (`hash_sid`)

### AUTH-036 · Presenting the raw token resumes the session; its hash doesn't
- **Priority:** P1
- **Level:** unit
- **Given** a valid session **When** a request carries the raw token **Then** it is signed in as that user. **When** a request carries the stored hash as the cookie **Then** it is treated as signed out.
- **Go test:** `TestSession_HashNotReplayable`
- **Sources:** `frappe/frappe/tests/test_auth.py:710` (`test_raw_cookie_resumes_the_session`), `frappe/frappe/tests/test_auth.py:714` (`test_sid_hash_follows_sid`)

### AUTH-037 · Idle session expires after 12 hours
- **Priority:** P1
- **Level:** unit
- **Given** a session last used at the fixture time, 2026-10-24 10:30:00 IST **When** a request arrives at 22:29:59 the same day **Then** it's accepted and the last-used time moves to 22:29:59. **When** the next request arrives at 10:29:59 on 2026-10-25 (12 h after the last use) **Then** it's accepted. **When** one arrives 12 h 0 m 1 s after the last use **Then** it is treated as signed out (401 for HTMX, API and non-GET requests; 303 to `/signin?next=…` for a browser GET, CD-17) and the row is deleted.
- **Go test:** `TestSession_IdleExpiry`
- **Sources:** `frappe/frappe/tests/test_auth.py:725` (`test_session_expires`), `frappe/frappe/sessions.py:413` (idle check), `frappe/frappe/sessions.py:563` (`get_expiry_period`)

### AUTH-038 · Session ends after 7 days even when in constant use
- **Priority:** P2
- **Level:** unit
- **Given** a session created at the fixture time, 24 Oct 2026 10:30 IST, and used every hour **When** an HTMX request arrives on 31 Oct 2026 at 10:30:01 IST **Then** it gets 401 "Your session has expired. Sign in again."
- **Go test:** `TestSession_AbsoluteExpiry`
- **Sources:** `frappe/frappe/sessions.py:415` (`session_end` hard end), `frappe/frappe/auth.py:182` (`post_login(session_end=…)`)

### AUTH-039 · Expired session on an HTMX request leaks no data and redirects to sign-in
- **Priority:** P1
- **Level:** integration
- **Given** an expired session **When** HTMX GETs `/po/list?status=draft` **Then** the response is 401 "Your session has expired. Sign in again." with `HX-Redirect: /signin?next=%2Fpo%2Flist%3Fstatus%3Ddraft`, and the body has no PO data and no stack trace. A normal browser GET gets 303 to the same URL (CD-17).
- **Go test:** `TestSession_ExpiredHTMXRedirect`
- **Sources:** `frappe/frappe/tests/test_auth.py:748` (`test_expired_session_answers_401_without_leaking_method`)

### AUTH-040 · Sign-out deletes the session server side
- **Priority:** P1
- **Level:** integration
- **Given** `priya@vardhan.test` is signed in **When** Priya POSTs `/signout` with a CSRF token (CD-63) **Then** the response is 303 to `/signin`, Priya's `sessions` row is deleted, the cookie is cleared (`Max-Age=0`), and replaying the old cookie on an HTMX request gets 401 (a plain browser GET gets 303 to `/signin?next=%2F`).
- **Go test:** `TestLogout_DeletesSession`
- **Sources:** `frappe/frappe/auth.py:388` (`logout` calls `delete_session`), `frappe/frappe/sessions.py:98` (`delete_session`)

### AUTH-041 · Sign-out is POST-only
- **Priority:** P2
- **Level:** integration
- **Given** a signed-in user **When** a `GET /signout` is made (for example from an image tag on another site) **Then** the response is 405 and the session stays.
- **Go test:** `TestLogout_PostOnly`
- **Sources:** `frappe/frappe/tests/test_auth.py:332` (`test_login_with_email_link`: a POST to a GET-only auth route is refused, which shows auth routes are method-restricted)

### AUTH-042 · Disabling a user ends all their sessions at once
- **Priority:** P1
- **Level:** integration
- **Given** `deepak@vardhan.test` has two active sessions **When** `admin@vardhan.test` disables Deepak **Then** both sessions are deleted and Deepak's next HTMX request gets 401 "Your session has expired. Sign in again."
- **Go test:** `TestDisableUser_EndsSessions`
- **Sources:** `frappe/frappe/core/doctype/user/user.py:395` (`check_enable_disable` logs the user out), `odoo/odoo/addons/base/models/res_users.py:949` (`active` is in the session token)

### AUTH-043 · A session whose user no longer exists is invalid
- **Priority:** P1
- **Level:** unit
- **Given** a session row whose `user_id` points to a user deleted under AUTH-055 **When** a request uses it **Then** it is treated as signed out (401 for HTMX, API and non-GET requests; 303 to `/signin?next=…` for a browser GET) and the row is removed.
- **Go test:** `TestSession_DeletedUserInvalid`
- **Sources:** `odoo/odoo/addons/base/tests/test_res_users.py:310` (`test_session_non_existing_user`)

### AUTH-044 · User can list and end their own sessions
- **Priority:** P2
- **Level:** integration
- **Given** `priya@vardhan.test` has sessions on "Chrome on Windows" and "Safari on iPhone" **When** Priya opens "My sessions" **Then** each session shows when it started and was last used in the one timestamp format (`24-10-2026 10:30` on the fixture clock, CD-33). **When** Priya clicks "Sign out all other sessions" **Then** only the current session remains, and Priya can't see or end other users' sessions (`/account/sessions/{other_id}` gives 404).
- **Go test:** `TestSessions_UserRevokesOthers`
- **Sources:** `odoo/odoo/addons/base/tests/test_res_users.py:896` (`test_revoke_all_devices`), `frappe/frappe/core/doctype/user/user.py:168` (`active_sessions`), `frappe/frappe/core/doctype/user/user.py:1701` (`clear_session`)

### AUTH-045 · Several sessions per user are allowed
- **Priority:** P2
- **Level:** integration
- **Given** `priya@vardhan.test` signs in from browser A, then from browser B **When** browser A makes a request **Then** it's still signed in.
- **Go test:** `TestSessions_MultipleAllowed`
- **Sources:** `frappe/frappe/auth.py:261` (`clear_active_sessions` only runs when `deny_multiple_sessions` is on), `frappe/frappe/tests/test_auth.py:221` (`test_deny_multiple_login`, the opposite setting)

### AUTH-046 · State-changing requests need a valid CSRF token
- **Priority:** P1
- **Level:** integration
- **Given** a signed-in session **When** HTMX POSTs `/po/123/save` (a) with no `X-CSRF-Token`, (b) with another session's token, (c) with the right token **Then** (a) and (b) get 403 with "Your form has expired. Reload the page and try again." and change nothing, and (c) succeeds.
- **Go test:** `TestCSRF_RequiredOnUnsafeMethods`
- **Sources:** `frappe/frappe/auth.py:93` (`validate_csrf_token`), `frappe/frappe/sessions.py:241` (token generated per session)

### AUTH-047 · Sign-in form is protected against login CSRF
- **Priority:** P2
- **Level:** integration
- **Given** no session **When** `/signin` is POSTed without the pre-session CSRF token from the sign-in page **Then** the response is 403 and no session is created.
- **Go test:** `TestCSRF_LoginForm`
- **Sources:** `frappe/frappe/auth.py:93` (same check); the choice to cover the pre-session sign-in form is D-AUTH-20

### AUTH-048 · Session cookie attributes
- **Priority:** P1
- **Level:** unit
- **Given** the app is served over HTTPS **When** a session cookie is set **Then** it has `HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/`, no `Domain`, the name `__Host-session`, and no `Expires` or `Max-Age` (a browser-session cookie; the server enforces the 12 h idle and 7-day limits, CD-14). Over plain HTTP on localhost it is named `session` and has no `Secure` flag.
- **Go test:** `TestSession_CookieAttributes`
- **Sources:** `frappe/frappe/auth.py:417` (`CookieManager.set_cookie`), `frappe/frappe/tests/test_auth.py:98` (`test_cookie_manager_without_a_request_uses_non_secure_cookie`), `frappe/frappe/tests/test_auth.py:358` (`test_correct_cookie_expiry_set`)

### AUTH-049 · First administrator via the CLI when no users exist
- **Priority:** P1
- **Level:** integration
- **Given** an empty database after `goose up` **When** `app admin create --email admin@vardhan.test --name "Anil Kapoor"` runs and the password `Correct-Horse-42` is entered twice **Then** user 1 exists, enabled, with the Administrator role, and its creation is in the audit trail as "system". Running the command again fails with "Users already exist. Sign in as an administrator to add more users." This wording is used everywhere, including WP3's app test (CD-66).
- **Go test:** `TestBootstrap_CLIFirstAdmin`
- **Sources:** `odoo/odoo/addons/base/models/res_users.py:645` (`_check_at_least_one_administrator`); Frappe creates `Administrator` at install

### AUTH-050 · `/setup` works only while there are no users, and only once
- **Priority:** P1
- **Level:** integration
- **Given** an empty database **When** two `/setup` POSTs arrive at the same time with different emails **Then** exactly one admin is created and the other request gets 409 with "Setup is already complete. Sign in instead." After that, `GET /setup` returns 404.
- **Go test:** `TestBootstrap_SetupOnceRaceFree`
- **Sources:** `odoo/odoo/addons/base/models/res_users.py:645` (at least one admin); decision D-AUTH-09

### AUTH-051 · Email must be valid and unique
- **Priority:** P1
- **Level:** unit
- **Given** user `priya@vardhan.test` exists **When** an admin creates a user with `PRIYA@vardhan.test` **Then** it's rejected with "A user with email priya@vardhan.test already exists." **When** the email is `priya@` **Then** "Enter a valid email address, such as name@company.in."
- **Go test:** `TestUser_EmailValidUnique`
- **Sources:** `frappe/frappe/core/doctype/user/user.py:208` (email lowercased as the name), `frappe/frappe/core/doctype/user/user.py:760` (`validate_email_type`)

### AUTH-052 · New user gets the Employee role by default; no roles gives a warning
- **Priority:** P2
- **Level:** integration
- **Given** an admin creates a new user `kiran@vardhan.test` (Kiran Joshi) in this test without choosing roles **When** the user is saved **Then** the user has the Employee role. **When** the admin then removes every role and saves **Then** the save succeeds, with the warning "Kiran Joshi has no roles and will see nothing after signing in."
- **Go test:** `TestUser_DefaultRoleAndNoRoleWarning`
- **Sources:** `odoo/odoo/addons/base/tests/test_res_users.py:407` (`test_default_groups`), `frappe/frappe/core/doctype/user/user.py:995` (`check_roles_added`)

### AUTH-053 · You can't disable yourself or remove your own Administrator role
- **Priority:** P1
- **Level:** unit
- **Given** `admin@vardhan.test` is signed in **When** Anil unticks "Enabled" on Anil's own user **Then** "You can't disable your own account. Ask another administrator." **When** Anil removes Anil's own Administrator role **Then** "You can't remove your own Administrator role. Ask another administrator."
- **Go test:** `TestUser_CannotDisableOrDemoteSelf`
- **Sources:** `odoo/odoo/addons/base/models/res_users.py:686` ("You cannot deactivate the user you're currently logged in as.")

### AUTH-054 · At least one enabled Administrator must always exist
- **Priority:** P1
- **Level:** unit
- **Given** Anil (`admin@vardhan.test`) and an extra Administrator `nisha@vardhan.test` (Nisha Menon, created in this test) are both enabled Administrators **When** Nisha removes Anil's Administrator role **Then** it succeeds. **When** the user service is then asked to disable Nisha, remove Nisha's Administrator role, or delete Nisha, from any path (screen, CSV import or the CLI `app user disable nisha@vardhan.test`) **Then** it refuses with "At least one enabled user must have the Administrator role." and nothing changes.
- **Go test:** `TestUser_AtLeastOneAdmin`
- **Sources:** `odoo/odoo/addons/base/models/res_users.py:645` (`_check_at_least_one_administrator`), `frappe/frappe/core/doctype/user/user.py:395` (Administrator can't be disabled)

### AUTH-055 · Users who are referenced are disabled, not deleted
- **Priority:** P1
- **Level:** integration
- **Given** `priya@vardhan.test` created `PO/26-27/00001`, and an extra user `kiran@vardhan.test` (Kiran Joshi), created in this test, never signed in and is referenced nowhere **When** an admin deletes Priya **Then** "Priya Shah has created or changed records, so they can't be deleted. Disable them instead." (CD-19) **When** the admin deletes Kiran **Then** it succeeds.
- **Go test:** `TestUser_DeleteOnlyIfUnreferenced`
- **Sources:** `frappe/frappe/core/doctype/user/user.py:650` (`on_trash`), `frappe/frappe/core/doctype/user/test_user.py:263` (`test_delete_user`), `odoo/odoo/addons/base/models/res_users.py:745` (`_unlink_except_master_data`)

### AUTH-056 · A disabled user's name stays on their documents
- **Priority:** P2
- **Level:** integration
- **Given** `priya@vardhan.test` created `PO/26-27/00001` and is now disabled **When** Ravi opens the PO and the audit trail **Then** both still show "Priya Shah (disabled)". Disabled users don't appear in user pickers such as approver lists.
- **Go test:** `TestUser_DisabledStillShownOnHistory`
- **Sources:** `frappe/frappe/core/doctype/user/user.py:1652` (`get_enabled_users`); decision D-AUTH-17

### AUTH-057 · Users can edit their own profile but not their email, roles or restrictions
- **Priority:** P1
- **Level:** integration
- **Given** `priya@vardhan.test` (not an admin) **When** Priya changes Priya's own mobile to `+91 98200 12345` **Then** it's saved. **When** Priya submits changes to Priya's own email, roles, enabled flag or allowed sites **Then** each gets 403 "Only an administrator can change this." **When** Priya edits Ravi's mobile **Then** 403.
- **Go test:** `TestUser_SelfEditLimitedFields`
- **Sources:** `odoo/odoo/addons/base/tests/test_res_users.py:688` (`test_user_writeable_fields`), `odoo/odoo/addons/base/tests/test_res_users.py:298` (`test_user_self_update`), `frappe/frappe/tests/test_permissions.py:297` (`test_not_allowed_to_remove_user_permissions`)

### AUTH-058 · Only Administrators can assign roles
- **Priority:** P1
- **Level:** integration
- **Given** `ravi@vardhan.test` (Purchase Manager) **When** Ravi POSTs a request adding the Administrator role to Ravi's own user, or the Purchase Manager role to Priya **Then** both get 403 "Only an administrator can change roles." and nothing changes.
- **Go test:** `TestRoles_OnlyAdminAssigns`
- **Sources:** `frappe/frappe/core/doctype/user/test_user.py:959` (`test_bulk_role_endpoints_require_write_permission`), `odoo/odoo/addons/base/models/res_users.py:652` (`_has_field_access`: `group_erp_manager` needed)

### AUTH-059 · Changing an email keeps the user's documents and history
- **Priority:** P2
- **Level:** integration
- **Given** `priya@vardhan.test` (user ID 4) created `PO/26-27/00001` **When** an admin changes Priya's email to `priya.shah@vardhan.test` **Then** Priya signs in with the new email, the PO still shows Priya as creator (`created_by = 4`), the old email no longer signs in, and Priya's other sessions end.
- **Go test:** `TestUser_EmailChangeKeepsIdentity`
- **Sources:** `frappe/frappe/core/doctype/user/test_user.py:410` (`test_user_rename`), `odoo/odoo/addons/base/tests/test_res_users.py:392` (`test_change_user_login`), `odoo/odoo/addons/base/models/res_users.py:949` (`login` is in the session token)

### AUTH-060 · Role or right changes take effect on the next request
- **Priority:** P1
- **Level:** integration
- **Given** `priya@vardhan.test` is signed in and can open `/po/list` **When** an admin removes Priya's Purchase User role **Then** Priya's very next request to `/po/list` gets 403, without signing out. Adding the role back restores access on the next request.
- **Go test:** `TestRoles_ChangeAppliesNextRequest`
- **Sources:** `frappe/frappe/tests/test_permissions.py:651` (`test_if_owner_permission_on_get_list`: role removed, list changes), `odoo/odoo/addons/base/tests/test_res_users_has_group.py:322` (`test_has_group_cleared_cache_on_write`)

### AUTH-061 · A role that is still assigned can't be deleted
- **Priority:** P2
- **Level:** unit
- **Given** the role Stores is assigned to Suresh and to two extra users `store1@vardhan.test` and `store2@vardhan.test` created in this test (3 users) **When** an admin deletes it **Then** "Role Stores is assigned to 3 users. Remove it from them before deleting the role." Once it's unassigned, deleting it also deletes its matrix rows.
- **Go test:** `TestRoles_DeleteBlockedWhileAssigned`
- **Sources:** `frappe/frappe/core/doctype/user/test_user.py:180` (`test_delete`: `LinkExistsError` while the role is assigned)

### AUTH-062 · Role names are unique and the Administrator role is fixed
- **Priority:** P2
- **Level:** unit
- **Given** the role "Stores" exists **When** an admin creates "stores" **Then** "A role named Stores already exists." **When** an admin renames, deletes or edits the rights of "Administrator" **Then** "The Administrator role can't be changed."
- **Go test:** `TestRoles_UniqueAndAdminFixed`
- **Sources:** `frappe/frappe/permissions.py:37` (`ADMIN_ROLE`), `frappe/frappe/permissions.py:109` (admin is allowed everything)

### AUTH-063 · Rights from several roles combine with OR
- **Priority:** P1
- **Level:** unit
- **Given** `suresh@vardhan.test` has Stores (no PO approve), and Purchase Manager (PO approve) is added to Suresh in this test **When** `auth.Service.Can(ctx, suresh, "PurchaseOrder", approve, po)` runs **Then** it returns nil (allowed). After Purchase Manager is removed again, it returns `auth.ErrForbidden`.
- **Go test:** `TestRights_UnionAcrossRoles`
- **Sources:** `frappe/frappe/permissions.py:332` (`any(p.get(ptype) …)`), `odoo/odoo/addons/base/models/ir_access.py:500` (docstring: "permissions are OR-ed together")

### AUTH-064 · A user with no matching role gets nothing
- **Priority:** P1
- **Level:** integration
- **Given** `deepak@vardhan.test` (Employee only) **When** Deepak GETs `/po/list`, `/po/123`, `/grn/list` and `/po/export.csv` **Then** each returns 403 with "You don't have permission to read Purchase Order. Ask your administrator for a role that allows it." (or GRN). The PO and GRN menu items aren't shown.
- **Go test:** `TestRights_NoRoleNoAccess`
- **Sources:** `frappe/frappe/permissions.py:178` ("does not have doctype access via role permission"), `frappe/frappe/tests/test_permissions.py:882` (`test_get_doctypes_with_read`), `odoo/odoo/addons/base/models/ir_access.py:48` (`ACCESS_ERROR_MESSAGE`)

### AUTH-065 · Matrix: a row needs read before any other right
- **Priority:** P1
- **Level:** unit
- **Given** an admin edits the role Stores on Purchase Order **When** they keep print but clear read **Then** the save fails with "Read is needed for any other right on Purchase Order. Tick Read or clear the other rights."
- **Go test:** `TestMatrix_ReadRequired`
- **Sources:** `frappe/frappe/tests/test_permissions.py:84` (`test_select_permission`: with no read, nothing works), `frappe/frappe/permissions.py:219`

### AUTH-066 · Matrix: rights that don't apply to a DocType can't be granted
- **Priority:** P1
- **Level:** unit
- **Given** the masters Item and Party **When** an admin grants submit, cancel, amend or approve on Item, or import on a DocType that doesn't allow import **Then** "Submit does not apply to Item. Clear it." (wording follows the right and DocType). `auth.Service.Can(…, "Item", submit, …)` always returns `auth.ErrForbidden`, even for an Administrator.
- **Go test:** `TestMatrix_InapplicableRightsRejected`
- **Sources:** `frappe/frappe/permissions.py:160` ("Document Type is not submittable"), `frappe/frappe/permissions.py:164` ("Document Type is not importable"), `frappe/frappe/permissions.py:255`

### AUTH-067 · Matrix is seeded from code defaults; admin edits replace them; "reset" restores them
- **Priority:** P2
- **Level:** integration
- **Given** a fresh install **When** the matrix is read **Then** it equals the Go-defined defaults for each role. **When** an admin removes Purchase User → PO → export **Then** Priya loses export. **When** the admin clicks "Reset to defaults" for Purchase Order **Then** export comes back. An upgrade that adds a new DocType seeds its default rows without touching rows the admin edited.
- **Go test:** `TestMatrix_SeedEditReset`
- **Sources:** `frappe/frappe/tests/test_permissions.py:889` (`test_overrides_work_as_expected`), `frappe/frappe/permissions.py:755` (`reset_perms`)

### AUTH-068 · Changes to the matrix, roles and restrictions are audited
- **Priority:** P2
- **Level:** integration
- **Given** an admin grants Stores → PO → print and adds the Site `TECH-SITE` to Suresh **When** the audit trail is read **Then** it has two entries, each with who (Anil Kapoor), when (UTC, `2026-10-24T05:00:00.000Z` on the fixture clock), what (`role=Stores, doctype=Purchase Order, print: none→all`; `user=Suresh Patil, sites: [PUNE-WH, METRO-SITE]→[PUNE-WH, METRO-SITE, TECH-SITE]`).
- **Go test:** `TestMatrix_ChangesAudited`
- **Sources:** `odoo/odoo/addons/base/models/res_users.py:886` (`_log_group_changes`), `frappe/frappe/core/doctype/user/user.py:992` (`get_permission_log_options`)

### AUTH-069 · Administrator role has every applicable right and no restrictions
- **Priority:** P1
- **Level:** unit
- **Given** `admin@vardhan.test` with no matrix rows **When** `auth.Service.Can` is called for every DocType × applicable right on records from every Site and Project **Then** it returns nil. `auth.Service.Scope` returns an empty predicate (no filter).
- **Go test:** `TestRights_AdminAll`
- **Sources:** `frappe/frappe/permissions.py:109` (Administrator allowed everything), `frappe/frappe/permissions.py:311` (`allow_everything`)

### AUTH-070 · Administrator still follows lifecycle and approval rules
- **Priority:** P1
- **Level:** integration
- **Given** `PO/26-27/00003` is `approved` **When** `admin@vardhan.test` tries to edit its lines or delete it **Then** DOC refuses with its normal message (for example "Approved documents can't be edited. Cancel and amend instead."). AUTH doesn't bypass the state machine.
- **Go test:** `TestRights_AdminCannotBypassLifecycle`
- **Sources:** `frappe/frappe/tests/test_permissions.py:341` (`test_dont_change_standard_constants`: constants are enforced regardless of the user); F5

### AUTH-071 · Read right: list and detail need read
- **Priority:** P1
- **Level:** integration
- **Given** Ravi has PO read **When** Ravi opens `/po/list` and `/po/123` **Then** both return 200. **When** an admin removes Ravi's read (with every other PO right) **Then** both return 403 "You don't have permission to read Purchase Order. Ask your administrator for a role that allows it."
- **Go test:** `TestRights_Read`
- **Sources:** `frappe/frappe/tests/test_permissions.py:80` (`test_basic_permission`), `frappe/frappe/model/db_query.py:155` (list checks read)

### AUTH-072 · Create right
- **Priority:** P1
- **Level:** integration
- **Given** Ravi has PO read but not create **When** Ravi GETs `/po/new` or POSTs `/po` **Then** both return 403 "You don't have permission to create Purchase Order. Ask your administrator for a role that allows it." The "New" button isn't shown. Priya (who has create) gets 200 and 303.
- **Go test:** `TestRights_Create`
- **Sources:** `frappe/frappe/tests/test_permissions.py:413` (`test_insert_if_owner_with_user_permissions`: insert raises `PermissionError`), `odoo/odoo/addons/base/models/ir_access.py:51`

### AUTH-073 · Write right
- **Priority:** P1
- **Level:** integration
- **Given** draft PO `Draft 4` and Ravi (no PO write) **When** Ravi POSTs a change to its delivery date **Then** 403 "You don't have permission to change Purchase Order. Ask your administrator for a role that allows it." and the record is unchanged. The form opens read-only for Ravi. **When** Ravi clicks Revise on rejected PO `Draft 13` (revise needs `write`, spec R4) **Then** the same 403, and it stays `rejected`.
- **Go test:** `TestRights_Write`
- **Sources:** `frappe/frappe/tests/test_permissions.py:84` (`test_select_permission`: save raises `PermissionError`), `odoo/odoo/addons/base/models/ir_access.py:50`

### AUTH-074 · Submit right is separate from write
- **Priority:** P1
- **Level:** integration
- **Given** a new role `Purchase Clerk` with PO read, create and write but not submit, held only by an extra user `clerk@vardhan.test` (both created in this test), draft PO `Draft 5`, pending PO `Draft 9` and approved `PO/26-27/00009` **When** the clerk saves `Draft 5` **Then** that succeeds. **When** the clerk submits `Draft 5`, withdraws `Draft 9`, or closes `PO/26-27/00009` (submit, withdraw and close all need `submit`, spec R4) **Then** each gets 403 "You don't have permission to submit Purchase Order. Ask your administrator for a role that allows it." and no state changes. **When** Priya (submit) withdraws `Draft 9` **Then** AUTH allows it.
- **Go test:** `TestRights_Submit`
- **Sources:** `frappe/frappe/permissions.py:20` (`submit` in `std_rights`), `hrms/hrms/hr/doctype/expense_claim/expense_claim.json:540` (the Employee role has write but no submit)

### AUTH-075 · Approve right governs approve and reject
- **Priority:** P1
- **Level:** integration
- **Given** PO `Draft 6` is `pending_approval` **When** Priya (no approve) POSTs approve or reject **Then** each gets 403 "You don't have permission to approve Purchase Order. Ask your administrator for a role that allows it." (reject needs `approve`; there is no `reject` right, spec R4). **When** Ravi (approve) approves **Then** AUTH allows it and APR decides the rest (amount limits, self-approval). AUTH's own test checks this through `auth.Service.Can(…, approve, rec)`. In the app, DOC doesn't call `Can(approve)` for approve and reject: it checks visibility first (hidden → 404), then calls APR's `CheckApprover`, which owns the approve-right and eligibility messages (CD-52). So through the app Priya still gets 403, with APR's message.
- **Go test:** `TestRights_Approve`
- **Sources:** `odoo/odoo/addons/hr_expense/security/ir.access.csv:4` (team approver group); Frappe has no `approve` right (it uses workflow roles), so this is decision D-AUTH-10

### AUTH-076 · Cancel right
- **Priority:** P1
- **Level:** integration
- **Given** `PO/26-27/00007` is `approved` **When** Priya (no cancel) POSTs cancel **Then** 403 "You don't have permission to cancel Purchase Order. Ask your administrator for a role that allows it." **When** Ravi cancels **Then** AUTH allows it (DOC checks for linked documents).
- **Go test:** `TestRights_Cancel`
- **Sources:** `frappe/frappe/permissions.py:21` (`cancel`), `hrms/hrms/hr/doctype/expense_claim/expense_claim.json:518` (cancel is granted only to manager roles)

### AUTH-077 · Amend right
- **Priority:** P1
- **Level:** integration
- **Given** `PO/26-27/00007` is `cancelled` **When** Priya (no amend) POSTs amend **Then** 403 "You don't have permission to amend Purchase Order. Ask your administrator for a role that allows it." **When** Ravi amends **Then** a new draft is created (shown as `Draft {id}`; it gets `PO/26-27/00007-1` only when approved) and Ravi becomes its `created_by`. DOC owns the engine's `created_by` assertion (CD-67); AUTH's test checks the right and its probe.
- **Go test:** `TestRights_Amend`
- **Sources:** `frappe/frappe/permissions.py:22` (`amend`)

### AUTH-078 · Delete right (drafts only, enforced by DOC)
- **Priority:** P1
- **Level:** integration
- **Given** draft PO `Draft 8` created by Priya **When** Ravi (read but no delete) deletes it **Then** 403 "You don't have permission to delete Purchase Order. Ask your administrator for a role that allows it." **When** Priya (delete, own scope) deletes a draft Priya created **Then** it is deleted.
- **Go test:** `TestRights_Delete`
- **Sources:** `frappe/frappe/tests/test_permissions.py:681` (`test_if_owner_permission_on_delete`), `odoo/odoo/addons/base/models/ir_access.py:52`

### AUTH-079 · Print right: print view and PDF
- **Priority:** P1
- **Level:** integration
- **Given** a new role `Purchase Viewer` with PO read but not print, held only by an extra user `viewer@vardhan.test` (both created in this test) **When** the viewer GETs `/po/123/print` and `/po/123/pdf` for a PO they can see **Then** both return 403 "You don't have permission to print Purchase Order. Ask your administrator for a role that allows it." and the Print button isn't shown. Read alone is not enough (spec R3). Priya (print) gets 200 and an `application/pdf` response. For a PO outside Priya's Project (`PRJ-TECHPARK`), Priya's print and PDF requests return 404.
- **Go test:** `TestRights_Print`
- **Sources:** `frappe/frappe/www/printview.py:479` (`validate_print_permission`, which accepts read OR print: we differ, see D-AUTH-14)

### AUTH-080 · Export right
- **Priority:** P1
- **Level:** integration
- **Given** Suresh (PO read, no export) **When** Suresh GETs `/po/export.csv` **Then** 403 "You don't have permission to export Purchase Order. Ask your administrator for a role that allows it." and the Export button isn't shown. Priya (export) gets a CSV.
- **Go test:** `TestRights_Export`
- **Sources:** `frappe/frappe/desk/reportview.py:560` (`_export_query`: "You are not allowed to export {} doctype"), `frappe/frappe/permissions.py:662` (`can_export`)

### AUTH-081 · Import right, plus create (and write for updates) on every row
- **Priority:** P1
- **Level:** integration
- **Given** the Item master allows import, and Priya has Item read and create but not import **When** Priya uploads `items.csv` **Then** 403 "You don't have permission to import Item. Ask your administrator for a role that allows it." **When** the Purchase User row for Item is changed in this test to read and import without create, and Priya uploads it **Then** every row is rejected with "Row 2: you don't have permission to create Item."
- **Go test:** `TestRights_Import`
- **Sources:** `frappe/frappe/core/doctype/data_import/data_import.py:179` (import permission), `frappe/frappe/core/doctype/data_import/value_mapping.py:96` (create permission), `frappe/frappe/permissions.py:653` (`can_import`)

### AUTH-082 · Lines (child rows) follow the parent document's rights
- **Priority:** P1
- **Level:** integration
- **Given** Ravi (PO read, no write) **When** Ravi POSTs `/po/123/lines` to add a line, or `/po/123/lines/7/delete` **Then** both return 403 "You don't have permission to change Purchase Order. Ask your administrator for a role that allows it." Line endpoints for a PO Ravi can't see return 404.
- **Go test:** `TestRights_LinesFollowParent`
- **Sources:** `frappe/frappe/tests/test_permissions.py:752` (`test_child_permissions`), `frappe/frappe/tests/test_permissions.py:787` (`test_child_permission_error_reports_parent_doctype`), `frappe/frappe/permissions.py:822` (`has_child_permission`)

### AUTH-083 · `created_by` and `created_at` can't be set or changed by the client
- **Priority:** P1
- **Level:** unit
- **Given** Priya POSTs a new PO with the form fields `created_by=3` and `created_at=2026-04-01T00:00:00Z` **When** it is saved **Then** `created_by` is Priya's ID and `created_at` is the server time (`2026-10-24T05:00:00.000Z` on the fixture clock). A later edit posting `created_by=3` leaves it unchanged. AUTH's share is that its route guard drops these fields from every form post; DOC owns the engine's assertions (DOC-003, CD-67).
- **Go test:** `TestRights_OwnerNotClientSettable`
- **Sources:** `frappe/frappe/tests/test_permissions.py:326` (`test_set_standard_fields_manually`), `frappe/frappe/tests/test_permissions.py:341` (`test_dont_change_standard_constants`)

### AUTH-084 · UI only shows actions the user is allowed
- **Priority:** P2
- **Level:** e2e
- **Given** PO `Draft 6` in `pending_approval` **When** Priya opens it **Then** there are no Approve or Reject buttons, and a Withdraw button is shown (Priya has `submit`, spec R2). **When** Ravi opens it **Then** Approve and Reject are shown, with no Edit, Delete or Withdraw (Ravi has no `submit`). Buttons come from the same `auth.Service.Can` call that the handlers use. Owned by WP3 (`e2e/`, CD-40); AUTH's share is `TestAllowedRights_MatchesCan`.
- **Go test:** `TestE2E_ButtonsMatchRights`
- **Sources:** `frappe/frappe/tests/test_permissions.py:107` (`test_owner_only_export_stays_in_can_export`: boot permissions drive the UI)

### AUTH-085 · Own scope: the list shows only my documents
- **Priority:** P1
- **Level:** integration
- **Given** Expense Claims `EX/26-27/00001` (created by Deepak) and `EX/26-27/00002` (created by Suresh), with the seeded Employee rights (Deepak has no direct reports, so team scope adds nothing) **When** Deepak opens `/expense/list` **Then** only `EX/26-27/00001` is listed, the count shows "1", and the total amount sums only Deepak's claims.
- **Go test:** `TestOwnScope_ListFiltered`
- **Sources:** `frappe/frappe/tests/test_permissions.py:651` (`test_if_owner_permission_on_get_list`), `frappe/frappe/tests/test_permissions.py:568` (an empty list rather than an error), `odoo/odoo/addons/hr_expense/security/ir.access.csv:10`

### AUTH-086 · Own scope: another person's document is "not found"
- **Priority:** P1
- **Level:** integration
- **Given** Deepak (own) **When** Deepak GETs `/expense/{id of EX/26-27/00002}`, its print and its PDF **Then** each returns 404 "This record doesn't exist or you don't have access to it."
- **Go test:** `TestOwnScope_OthersNotFound`
- **Sources:** `frappe/frappe/tests/test_permissions.py:623` (`test_if_owner_permission_on_getdoc`)

### AUTH-087 · Own scope: write and delete only own; create unaffected
- **Priority:** P1
- **Level:** integration
- **Given** Priya has PO delete (own) and PO create (all), and draft POs `Draft 10` (Priya's) and `Draft 11` (Ravi's) **When** Priya deletes `Draft 11` **Then** 403 "You can only delete Purchase Orders you created." **When** Priya deletes `Draft 10` **Then** it's deleted. Creating a new PO is still allowed.
- **Go test:** `TestOwnScope_WriteDeleteOwnOnly`
- **Sources:** `frappe/frappe/tests/test_permissions.py:568` (`test_if_owner_permission_overrides_properly`), `frappe/frappe/tests/test_permissions.py:681` (`test_if_owner_permission_on_delete`), `frappe/frappe/permissions.py:339` (create is excluded from `if_owner`)

### AUTH-088 · Own scope includes documents for my linked Employee
- **Priority:** P1
- **Level:** integration
- **Given** Employee `EMP-007 Deepak Nair` is linked to user `deepak@vardhan.test`, and `admin@vardhan.test` (Anil Kapoor) keyed in `EX/26-27/00003` for employee `EMP-007` **When** Deepak opens `/expense/list` **Then** `EX/26-27/00003` is listed and Deepak can open it, even though `created_by` is Anil.
- **Go test:** `TestOwnScope_LinkedEmployee`
- **Sources:** `odoo/odoo/addons/hr_expense/security/ir.access.csv:10` (`employee_id.user_id = user.id`), `erpnext/erpnext/setup/doctype/employee/employee.py:222` (`update_user_permissions` links an employee to a user)

### AUTH-089 · "All" from any role beats "own" from another
- **Priority:** P1
- **Level:** unit
- **Given** Deepak has Employee (Expense Claim read: team, which for Deepak, with no direct reports, means own), and Director (Expense Claim read: all) is added to Deepak in this test **When** Deepak's Expense Claim list is built **Then** it has no owner filter. When Director is removed again, the owner filter returns.
- **Go test:** `TestOwnScope_AllWins`
- **Sources:** `frappe/frappe/permissions.py:324` (`has_permission_without_if_owner_enabled`), `frappe/frappe/tests/test_permissions.py:651` (adding the Website Manager role widens the list)

### AUTH-090 · Own scope: export contains only my rows
- **Priority:** P1
- **Level:** integration
- **Given** the Employee role is given Expense Claim export (own) in this test **When** Deepak exports `/expense/export.csv` **Then** the CSV has only claims Deepak created or that are for `EMP-007`, and the row count matches Deepak's list.
- **Go test:** `TestOwnScope_ExportFiltered`
- **Sources:** `frappe/frappe/tests/test_permissions.py:107` (`test_owner_only_export_stays_in_can_export`), `frappe/frappe/desk/reportview.py:560`

### AUTH-091 · Restriction by Site: list shows only allowed sites
- **Priority:** P1
- **Level:** integration
- **Given** GRNs `GR/26-27/00001` (Site `PUNE-WH`) and `GR/26-27/00002` (Site `TECH-SITE`), and Suresh restricted to `PUNE-WH` and `METRO-SITE` **When** Suresh opens `/grn/list` **Then** only `GR/26-27/00001` is listed, the count is "1", and `/grn/{id of GR/26-27/00002}` returns 404.
- **Go test:** `TestRestriction_SiteList`
- **Sources:** `frappe/frappe/tests/test_permissions.py:184` (`test_user_permissions_in_report`), `frappe/frappe/tests/test_permissions.py:159` (`test_user_permissions_in_doc`)

### AUTH-092 · Restriction by Project
- **Priority:** P1
- **Level:** integration
- **Given** POs `PO/26-27/00020` (Project `PRJ-METRO`) and `PO/26-27/00021` (Project `PRJ-TECHPARK`), and Priya restricted to `PRJ-METRO` **When** Priya lists or exports POs **Then** only `PO/26-27/00020` appears, and `PO/26-27/00021` returns 404.
- **Go test:** `TestRestriction_ProjectList`
- **Sources:** `frappe/frappe/tests/test_permissions.py:243` (`test_user_link_match_doc`), `odoo/odoo/addons/project/security/ir.access.csv:3` (project member rule)

### AUTH-093 · Restriction is strict: a document with an empty field is hidden
- **Priority:** P1
- **Level:** integration
- **Given** `PO/26-27/00022` with no Project, and Priya restricted to `PRJ-METRO` **When** Priya lists POs **Then** `PO/26-27/00022` isn't shown. Ravi (unrestricted) sees it.
- **Go test:** `TestRestriction_StrictEmptyHidden`
- **Sources:** `frappe/frappe/tests/test_permissions.py:479` (`test_strict_user_permissions`), `frappe/frappe/permissions.py:373`

### AUTH-094 · Restriction applies to line-level links as well as the header
- **Priority:** P1
- **Level:** integration
- **Given** `PO/26-27/00023` with header Site `PUNE-WH` and a line with delivery Site `TECH-SITE`, and Suresh restricted to `PUNE-WH` and `METRO-SITE` **When** Suresh opens it **Then** 404, because every Site link on the document must be allowed.
- **Go test:** `TestRestriction_ChildLinksChecked`
- **Sources:** `frappe/frappe/permissions.py:481` (child rows are checked), `frappe/frappe/permissions.py:453` (message naming the row and field)

### AUTH-095 · Restrictions on Project and Site combine with AND
- **Priority:** P1
- **Level:** unit
- **Given** Suresh, with the fixture Sites `[PUNE-WH, METRO-SITE]` and a Project restriction `[PRJ-METRO]` added in this test **When** Suresh's PO scope is built **Then** it is `(project_id IN (<PRJ-METRO id>)) AND (site_id IN (<PUNE-WH id>, <METRO-SITE id>))`. A PO for `PRJ-METRO` at `TECH-SITE` is hidden.
- **Go test:** `TestRestriction_DimensionsAnd`
- **Sources:** `frappe/frappe/tests/test_permissions.py:193` (`test_user_permissions_in_report_with_multiple_link_fields`), `frappe/frappe/tests/test_permissions.py:389` (`test_user_permission_doctypes`), `odoo/odoo/addons/base/models/ir_access.py:500` ("restrictions are AND-ed")

### AUTH-096 · Restrictions never grant access
- **Priority:** P1
- **Level:** unit
- **Given** Deepak (no PO rights) is given Site `PUNE-WH` **When** Deepak opens a PO for `PUNE-WH` **Then** 403. A restriction only narrows what roles grant.
- **Go test:** `TestRestriction_DoesNotGrant`
- **Sources:** `frappe/frappe/tests/test_permissions.py:523` (`test_user_permission_is_not_applied_if_user_roles_does_not_have_permission`)

### AUTH-097 · Restriction and own scope combine with AND
- **Priority:** P2
- **Level:** unit
- **Given** Priya with Expense Claim (own, from Employee) restricted to `PRJ-METRO`, and Priya's own claim `EX/26-27/00009` for `PRJ-TECHPARK` **When** Priya lists claims **Then** `EX/26-27/00009` isn't shown.
- **Go test:** `TestRestriction_AndOwnScope`
- **Sources:** `frappe/frappe/permissions.py:271` (Frappe keeps owner rights despite a restriction; we follow Odoo's AND, see D-AUTH-13), `odoo/odoo/addons/base/models/ir_access.py:496`

### AUTH-098 · Restricted users can only pick allowed values in link pickers
- **Priority:** P1
- **Level:** integration
- **Given** Suresh restricted to `PUNE-WH` and `METRO-SITE` **When** Suresh opens the Site picker on a new GRN **Then** only `PUNE-WH` and `METRO-SITE` are offered, not `TECH-SITE`. The Project picker offers every enabled project, because Suresh has no Project restriction.
- **Go test:** `TestRestriction_LinkPickerFiltered`
- **Sources:** `frappe/frappe/desk/search.py:103` (search checks read), `frappe/frappe/desk/search.py:699`, `frappe/frappe/tests/test_permissions.py:829` (`test_select_user`)

### AUTH-099 · Saving a document with a disallowed value is rejected
- **Priority:** P1
- **Level:** integration
- **Given** Suresh restricted to `PUNE-WH` and `METRO-SITE` **When** Suresh POSTs a new GRN with Site `TECH-SITE`, made up in the request **Then** 403 "You can only use Sites METRO-SITE, PUNE-WH. Choose one of your allowed sites." and nothing is saved.
- **Go test:** `TestRestriction_SaveDisallowedValue`
- **Sources:** `frappe/frappe/tests/test_permissions.py:413` (`test_insert_if_owner_with_user_permissions`: insert refused, then allowed after the permission is added), `frappe/frappe/tests/test_permissions.py:455` (`test_ignore_user_permissions_if_missing`)

### AUTH-100 · A single allowed value becomes the default on new documents
- **Priority:** P2
- **Level:** integration
- **Given** Suresh's Site list is cut to exactly one Site, `PUNE-WH`, in this test **When** Suresh opens `/grn/new` **Then** Site is prefilled with `PUNE-WH`. With Suresh's two fixture Sites (`PUNE-WH`, `METRO-SITE`) it is left empty and is required.
- **Go test:** `TestRestriction_SingleValueDefault`
- **Sources:** `frappe/frappe/tests/test_permissions.py:212` (`test_default_values`), `frappe/frappe/core/doctype/user_permission/test_user_permission.py:51` (`test_default_user_permission`)

### AUTH-101 · Restricted users can't link to a document they can't see
- **Priority:** P1
- **Level:** integration
- **Given** approved `PO/26-27/00030` for Site `TECH-SITE`, and Suresh restricted to `PUNE-WH` and `METRO-SITE` **When** Suresh creates a GRN and posts `purchase_order_id` of `PO/26-27/00030` **Then** 403 "You can't use Purchase Order PO/26-27/00030. It isn't available to you." The PO picker on the GRN doesn't list it.
- **Go test:** `TestRestriction_LinkedDocumentMustBeVisible`
- **Sources:** `frappe/frappe/permissions.py:423` (`check_user_permission_on_link_fields`), `frappe/frappe/tests/test_permissions.py:311` (`test_user_permissions_if_applied_on_doc_being_evaluated`)

### AUTH-102 · Masters themselves are filtered by the restriction
- **Priority:** P2
- **Level:** integration
- **Given** Suresh restricted to `PUNE-WH` and `METRO-SITE` **When** Suresh opens `/site/list` **Then** only `PUNE-WH` and `METRO-SITE` are listed. **When** Priya (restricted to `PRJ-METRO`) opens `/project/list` **Then** only `PRJ-METRO` is listed. This works because the Site and Project masters declare `SiteFields`/`ProjectFields` `["id"]` (CD-64).
- **Go test:** `TestRestriction_MasterSelf`
- **Sources:** `frappe/frappe/tests/test_permissions.py:284` (`test_read_if_explicit_user_permissions_are_set`), `frappe/frappe/permissions.py:390` (step 1: restriction on the DocType itself)

### AUTH-103 · Only Administrators manage restrictions
- **Priority:** P1
- **Level:** integration
- **Given** Suresh (not an admin) **When** Suresh POSTs to add `TECH-SITE` to Suresh's own Sites, or to remove `PUNE-WH` **Then** both return 403 "Only an administrator can change site and project access."
- **Go test:** `TestRestriction_AdminOnly`
- **Sources:** `frappe/frappe/tests/test_permissions.py:272` (`test_not_allowed_to_set_user_permissions`), `frappe/frappe/tests/test_permissions.py:297` (`test_not_allowed_to_remove_user_permissions`)

### AUTH-104 · Removing all restrictions on a dimension lifts it
- **Priority:** P2
- **Level:** integration
- **Given** Suresh restricted to `PUNE-WH` and `METRO-SITE` **When** an admin clears Suresh's Site list **Then** on the next request Suresh sees GRNs for every site.
- **Go test:** `TestRestriction_ClearLifts`
- **Sources:** `frappe/frappe/tests/test_permissions.py:723` (`test_clear_user_permissions`), `frappe/frappe/tests/test_permissions.py:455`

### AUTH-105 · A disabled Site or Project stays valid in a restriction list
- **Priority:** P2
- **Level:** unit
- **Given** Suresh restricted to `PUNE-WH` and `METRO-SITE` **When** `PUNE-WH` is disabled (F4) **Then** Suresh still sees its old GRNs, the picker doesn't offer `PUNE-WH` for new documents, and the admin screen marks the restriction "PUNE-WH (disabled)".
- **Go test:** `TestRestriction_DisabledMasterKept`
- **Sources:** `frappe/frappe/core/doctype/user_permission/test_user_permission.py:30` (`test_default_user_permission_validation`); F4

### AUTH-106 · Every channel uses the same scope: list, count, search, picker, print, export, JSON
- **Priority:** P1
- **Level:** integration
- **Given** Suresh restricted to `PUNE-WH` and `METRO-SITE`, and GRNs `GR/26-27/00001` (`PUNE-WH`) and `GR/26-27/00002` (`TECH-SITE`) **When** the same query is made through (a) `/grn/list`, (b) the list count badge, (c) the global search for "GR/26-27/00002", (d) the GRN picker on a stock form, (e) `/grn/{id}/print`, (f) `/grn/export.csv`, (g) `/api/grn?site=TECH-SITE` **Then** none of them returns or confirms `GR/26-27/00002`: (a)–(d) and (f) leave it out, and (e) and (g) return 404 or an empty set. This is a table-driven test over every registered route.
- **Go test:** `TestScope_AllChannelsConsistent`
- **Sources:** `frappe/frappe/model/db_query.py:155` (one query path checks read), `frappe/frappe/desk/search.py:103`, `frappe/frappe/www/printview.py:479`, `frappe/frappe/desk/reportview.py:560`

### AUTH-107 · Every route declares its permission check (route inventory test)
- **Priority:** P1
- **Level:** unit
- **Given** the router's list of registered routes **When** the test walks it **Then** every route other than `/signin`, `/setup`, `/static/*` and `/healthz` is wrapped by the auth middleware and declares a (DocType, right), "self" or "admin" requirement, or, only for the generic `/doc/{doctype}/…` routes, the "checked in handler" mode (guard `CheckedInHandler`: the handler calls `auth.Service.Can` itself, CD-46). A new route without one, or a "checked in handler" route outside `/doc/`, fails the build.
- **Go test:** `TestRoutes_AllDeclarePermission`
- **Sources:** `frappe/frappe/tests/test_auth.py:748` (non-whitelisted methods refused even when the session expired); Frappe's `@frappe.whitelist` model

### AUTH-108 · Guessing record IDs reveals nothing
- **Priority:** P1
- **Level:** integration
- **Given** Deepak (own scope) **When** Deepak requests `/expense/1` … `/expense/50` **Then** IDs Deepak owns return 200, and IDs that exist but aren't Deepak's return the same 404 body as IDs that don't exist, with matching response size apart from the CSRF token.
- **Go test:** `TestScope_IDGuessingNoLeak`
- **Sources:** `frappe/frappe/tests/test_permissions.py:623` (`test_if_owner_permission_on_getdoc`); decision D-AUTH-16

### AUTH-109 · Attachments follow the parent document's permission
- **Priority:** P1
- **Level:** integration
- **Given** a private attachment `invoice_8812.pdf` on `GR/26-27/00002` (Site `TECH-SITE`) **When** Suresh (GRN read, restricted to `PUNE-WH` and `METRO-SITE`) GETs its download URL **Then** 404. **When** Deepak (no GRN rights) GETs it **Then** 403 "You don't have permission to read GRN. Ask your administrator for a role that allows it." **When** `admin@vardhan.test` GETs it **Then** the file is returned.
- **Go test:** `TestScope_AttachmentsFollowParent`
- **Sources:** `frappe/frappe/core/doctype/file/file.py:1003` (`has_permission` follows the attached document); detailed cases belong to FILE

### AUTH-110 · List filters and sorting can't widen the scope
- **Priority:** P1
- **Level:** integration
- **Given** Suresh restricted to `PUNE-WH` and `METRO-SITE` **When** Suresh requests `/grn/list?site=TECH-SITE`, `?created_by=1` or `?sort=site` **Then** the results are still only `PUNE-WH` and `METRO-SITE` GRNs (an empty set for the first). The user's filters are added with AND to the scope and never replace it.
- **Go test:** `TestScope_FiltersCannotWiden`
- **Sources:** `frappe/frappe/model/db_query.py:786` (`build_filter_conditions` alongside the match conditions)

### AUTH-111 · Reports and totals respect scope
- **Priority:** P2
- **Level:** integration
- **Given** Priya restricted to `PRJ-METRO`, with POs worth ₹4,72,000.00 on `PRJ-METRO` and ₹12,35,500.00 on `PRJ-TECHPARK` **When** Priya opens the "PO value by vendor" report **Then** the grand total is ₹4,72,000.00.
- **Go test:** `TestScope_ReportTotals`
- **Sources:** `frappe/frappe/tests/test_permissions.py:193` (`get_user_match_filters` for reports)

### AUTH-112 · JSON endpoints use the same permission service as screens
- **Priority:** P1
- **Level:** integration
- **Given** the JSON endpoint `/api/po/{id}` and the screen `/po/{id}` **When** each enabled fixture user calls both for each fixture PO **Then** the allow/deny result is identical for every (user, PO) pair (a table-driven test over 7 users × 8 POs).
- **Go test:** `TestScope_APIEqualsUI`
- **Sources:** `frappe/frappe/permissions.py:82` (one `has_permission` used by every caller), `odoo/odoo/addons/base/models/ir_access.py:283` (one access table for every RPC)

### AUTH-113 · Permission check fuzzing: scope SQL and `Can` agree
- **Priority:** P2
- **Level:** unit
- **Given** random users (random roles, own/team/all scopes, random reporting lines and employee status, 0–3 Projects, 0–3 Sites) and random documents **When** `auth.Service.Can(ctx, user, dt, read, doc)` is compared with "doc is returned by a query using `auth.Service.Scope(ctx, user, dt, read)`" **Then** they always agree.
- **Go test:** `FuzzScope_CanMatchesSQL`
- **Sources:** `odoo/odoo/addons/base/models/ir_access.py:523` (permission domain filtering must agree with the record check); `frappe/frappe/permissions.py:358` versus `db_query` match conditions

### AUTH-114 · Permission denials explain the fix without leaking data
- **Priority:** P2
- **Level:** unit
- **Given** each denial type **When** its message is produced **Then** the message names the action and the DocType (for example "You don't have permission to approve Purchase Order. Ask your administrator for a role that allows it."), never another user's data, and never a stack trace or SQL. Every AUTH error that reaches a handler implements `doctype.HTTPError` with the status it is sent with (CD-44), and errors are written through `web.ErrorFrom`, so an unexpected error shows the standard 500 page without its details (CD-76, CD-81).
- **Go test:** `TestDenial_Messages`
- **Sources:** `frappe/frappe/tests/test_permissions.py:172` (`test_user_permission_denial_is_explained`), `odoo/odoo/addons/base/models/ir_access.py:326` (`_make_model_access_error`)

### AUTH-115 · Signed-out users can reach only the public routes
- **Priority:** P1
- **Level:** integration
- **Given** no session **When** GET `/`, `/po/list`, `/api/po/1`, `/users` and `/static/app.css` **Then** the first four redirect (303) to `/signin?next=…` (or return 401 for the API and HTMX, CD-17), and `/static/app.css` returns 200.
- **Go test:** `TestGuest_OnlyPublicRoutes`
- **Sources:** `frappe/frappe/tests/test_auth.py:91` (`test_session_without_a_request_uses_guest_sid`), `frappe/frappe/tests/test_permissions.py:857` (`test_automatic_permissions`: Guest has only the Guest role)

### AUTH-116 · Team scope: a manager sees and can approve a direct report's claim
- **Priority:** P1
- **Level:** integration
- **Given** Deepak (`EMP-007`, reports directly to Priya, `EMP-004`) submitted Expense Claim `Draft 40` for `EMP-007`, now `pending_approval`, and Suresh (`EMP-005`, reports to Ravi) submitted `Draft 41`. Priya holds Employee (Expense Claim read and approve: team) **When** Priya opens `/expense/list`, opens `Draft 40` and approves it **Then** `Draft 40` is listed and `Draft 41` isn't, `Draft 40` opens (200), and `auth.Service.Can(ctx, priya, "fixture.Claim", approve, claim)` returns nil (AUTH's own tests use the stand-in `authtest.Claim`, CD-65), so the approve goes ahead. In the app the approve goes through DOC's visibility check and then APR's `CheckApprover`, which decides the rest (CD-52). `Draft 41` returns 404. `auth.Service.UsersWithRight(ctx, "fixture.Claim", approve, Draft 40)` is Anil (Administrator), Meena (Director, all), Priya (team) and Deepak (team includes own). APR's self-approval rule, not AUTH, keeps Deepak from approving the claim.
- **Go test:** `TestTeamScope_DirectReportVisibleAndApprovable`
- **Sources:** `odoo/addons/hr_expense/security/hr_expense_security.xml:9` (`group_hr_expense_team_approver`), `odoo/addons/hr_expense/models/hr_employee.py:45` (`parent_id in employees`: the employees a team approver manages), `odoo/addons/hr_expense/models/hr_employee.py:54` (`_get_expense_managers` includes `parent_id.user_id`), `hrms/hrms/hr/doctype/expense_claim/expense_claim.json:118` (`expense_approver`), `hrms/hrms/hr/utils.py:785` (`share_doc_with_approver` gives the approver access to the claim); decision CD-11

### AUTH-117 · Team scope covers direct reports only, not indirect ones
- **Priority:** P1
- **Level:** integration
- **Given** Deepak's pending claim `Draft 40` from AUTH-116, and the reporting line Deepak → Priya → Ravi → Meena **When** Ravi (Employee: team; Priya's manager) opens `/expense/list`, `/expense/{id of Draft 40}` and posts approve **Then** the claim isn't listed and both requests return 404 "This record doesn't exist or you don't have access to it." (visibility is checked before anything about approval, CD-52). **When** Meena's roles are cut to Employee only in this test **Then** Meena gets the same 404s. **When** Director is given back to Meena **Then** Meena opens it (200) through Director's `all`, not through team.
- **Go test:** `TestTeamScope_IndirectReportExcluded`
- **Sources:** `odoo/addons/hr_expense/security/ir.access.csv:4` (Odoo's Team Approver rule also matches indirect reports with `child_of`; we don't, see D-AUTH-12), `odoo/addons/hr_expense/models/hr_employee.py:45` (`parent_id`: the direct manager); decision CD-11

### AUTH-118 · A report who has left drops out of the manager's team
- **Priority:** P1
- **Level:** integration
- **Given** `EMP-008` (Former Staff, status Left) is set in this test to report to Priya (`EMP-004`), Anil keyed in claim `Draft 42` for `EMP-008`, and Suresh (`EMP-005`, Active, reports to Ravi) has claim `Draft 43` **When** Priya opens `/expense/list` and `/expense/{id of Draft 42}` **Then** `Draft 42` isn't listed and returns 404. **When** Ravi opens `Draft 43` **Then** 200. **When** Suresh's employee status is changed to Left **Then** Ravi's very next request for `Draft 43` returns 404 and the list no longer shows it, without Ravi signing out. Setting Suresh back to Active restores it.
- **Go test:** `TestTeamScope_LeftEmployeeIgnored`
- **Sources:** `erpnext/erpnext/setup/doctype/employee/employee.py:299` (`validate_status`: reporting lines count only Active employees), `hrms/hrms/hr/doctype/department_approver/department_approver.py:32` (`get_approvers` offers enabled users only); decisions CD-11, CD-13 (`DirectReports` returns active employees only)

### AUTH-119 · Team scope is offered only on DocTypes with an Employee field
- **Priority:** P1
- **Level:** unit
- **Given** an admin editing the rights matrix **When** the admin sets Employee → Purchase Order → read to Team **Then** the save fails with "Team does not apply to Purchase Order. Choose Own or All." and nothing changes. **When** the admin sets Employee → Expense Claim → read and approve to Team **Then** it saves. The scope choices are None, Own, Team and All for Expense Claim, and None, Own and All for Purchase Order.
- **Go test:** `TestMatrix_TeamOnlyWithEmployeeField`
- **Sources:** `frappe/frappe/permissions.py:160` (a right that doesn't fit the DocType is refused, the same idea as D-AUTH-11), `odoo/addons/hr_expense/security/ir.access.csv:4` (the team rule is defined only for `hr.expense`, which has `employee_id`); decision CD-11

### AUTH-120 · "Who can…" shows the roles and users that hold a right
- **Priority:** P2
- **Level:** integration
- **Given** the seeded matrix and users **When** `admin@vardhan.test` opens Who can… for Purchase Order and approve **Then** it lists Administrator (every right: Anil Kapoor), Director · All (Meena Iyer) and Purchase Manager · All (Ravi Kulkarni). **When** Anil picks Expense Claim and approve **Then** it lists Administrator (Anil Kapoor), Director · All (Meena Iyer) and Employee · Team (Deepak Nair, Kavya Rao, Priya Shah with "Projects: PRJ-METRO", Ravi Kulkarni, Suresh Patil with "Sites: PUNE-WH, METRO-SITE"). Former Staff, who is disabled, isn't listed. For Item the right list doesn't offer Submit, Cancel, Amend or Approve; asking for Item and approve directly gives 400 "Approve does not apply to Item. Clear it." **When** Priya opens the page **Then** 403 "Only an administrator can see who has which rights." The view is read-only, uses no table markup (design system §8.6), and is built from UI components after the UI work package merges (CD-49).
- **Go test:** `TestWhoCan_RolesAndUsers`
- **Sources:** `frappe/frappe/core/page/permission_manager/permission_manager.py:81` (`get_permissions` for a DocType or role), `frappe/frappe/core/page/permission_manager/permission_manager.py:214` (`get_users_with_role`); decision CD-49, design system §8.6

### AUTH-121 · Administrators edit a user's Site and Project access on one screen
- **Priority:** P2
- **Level:** integration
- **Given** Suresh restricted to `PUNE-WH` and `METRO-SITE` and no Project restriction **When** `admin@vardhan.test` opens Suresh's "Site and project access" page **Then** the Sites section lists `PUNE-WH` and `METRO-SITE`, each with "Remove", and the Projects section says "All projects (no restriction)". **When** Anil removes `METRO-SITE` **Then** the page reloads (303) listing only `PUNE-WH`, with the banner "Site and project access for Suresh Patil saved." (CD-80). **When** Anil adds `TECH-SITE` with the site picker **Then** it lists `PUNE-WH` and `TECH-SITE`. **When** `PUNE-WH` is disabled **Then** the page shows "PUNE-WH (disabled)" (AUTH-105). **When** Suresh opens the page **Then** 403 "Only an administrator can change site and project access." The page uses no table markup and is built from UI components after the UI work package merges (CD-49).
- **Go test:** `TestAccessEditor_ShowsAddsRemoves`
- **Sources:** `frappe/frappe/core/doctype/user_permission/user_permission.py:97` (`get_user_permissions`: one place lists a user's permissions), `frappe/frappe/core/doctype/user_permission/user_permission.py:252` (`clear_user_permissions`), `frappe/frappe/tests/test_permissions.py:723` (`test_clear_user_permissions`); decision CD-49

## Deferred and excluded
| Scenario | Source | later/never | Reason |
|---|---|---|---|
| Two-factor sign-in (TOTP, email or SMS code) | `frappe/frappe/tests/test_twofactor.py`, `frappe/frappe/tests/test_auth.py:275` | later | Valuable once the app is reachable from the internet. Needs a QR enrolment flow and recovery codes. |
| Self-service "forgot password" email link (single use, expiring, generic response) | `frappe/frappe/core/doctype/user/user.py:1202`, `:1342`, `frappe/frappe/core/doctype/user/test_user.py:789`, `:802` | later | v1 has no outgoing-mail module. An admin resets passwords (AUTH-033). |
| Sign-in by username or mobile number | `frappe/frappe/tests/test_auth.py:169`, `:198`, `:211` | never | Email only (D-AUTH-01). |
| Sign-in by emailed one-time link | `frappe/frappe/tests/test_auth.py:332` | later | Needs outgoing mail. |
| Turn off password sign-in (SSO only) | `frappe/frappe/tests/test_auth.py:241` | later | SSO is out of scope for v1. |
| Only one session per user (`deny_multiple_sessions`) | `frappe/frappe/tests/test_auth.py:221` | never | We allow several sessions and let users end them (D-AUTH-07). |
| Sign-in allowed only between set hours | `frappe/frappe/auth.py:355` | later | No demand at 20 people. |
| Restrict a user to certain IP addresses | `frappe/frappe/auth.py:485`, `frappe/frappe/tests/test_auth.py:458` | later | Office IPs change, and the risk of locking people out outweighs the gain for now. |
| Impersonate another user | `frappe/frappe/core/doctype/user/test_user.py:970`, `frappe/frappe/auth.py:381` | later | Useful for support, but needs careful audit and notification design. |
| Public sign-up | `frappe/frappe/core/doctype/user/test_user.py:593` | never | Internal app. Only admins create users. |
| Website or portal users | `odoo/odoo/addons/base/tests/test_res_users.py:176`, `test_res_users_has_group.py:71` | never | Website/portal is out of scope (brief). |
| API keys and OAuth for third-party clients | `frappe/frappe/tests/test_auth.py:539`, `odoo/odoo/addons/base/tests/test_res_users.py:935` | later | The Zoho Books sync is outbound. No inbound API clients in v1. |
| General API rate limiting | `frappe/frappe/tests/test_rate_limiter.py:18` | later | Sign-in lockout covers the v1 risk. |
| Forced password change every N days | `frappe/frappe/auth.py:316`, `frappe/frappe/tests/test_auth.py:306` | never | NIST 800-63B advises against periodic expiry. A forced change happens only after an admin reset. |
| zxcvbn strength score | `frappe/frappe/utils/password_strength.py:20` | later | I couldn't verify a maintained open-source Go port. Length plus a common-password list covers v1. |
| Field-level rights (`permlevel`, for example hiding rates from store staff) | `frappe/frappe/tests/test_permissions.py:787`, `frappe/frappe/core/doctype/user/test_user.py:229` | later | Real need, likely next ("GRN without rates"), but it doubles the matrix. |
| Share one document with one user | `frappe/frappe/permissions.py:185` | later | Restrictions and own scope cover v1 needs. |
| Role profiles (bundles of roles) and bulk role assignment | `frappe/frappe/core/doctype/user/user.py:285`, `frappe/frappe/core/doctype/user/test_user.py:893` | later | 20 users, about 6 roles. |
| Role hierarchy (implied groups) | `odoo/odoo/addons/base/tests/test_res_users_has_group.py:274` | never | Flat roles (D-AUTH-21). |
| Disable a role without deleting it | `frappe/frappe/core/doctype/role/test_role.py:10` | later | Removing the role from users is enough for v1. |
| Restriction by Branch | `frappe/frappe/tests/test_permissions.py:538` (`test_contextual_user_permission`) | later | Most branch-level control comes through Site. Add Branch as a third dimension when needed. |
| A restriction that applies only to some DocTypes (`applicable_for`) | `frappe/frappe/tests/test_permissions.py:538`, `frappe/frappe/core/doctype/user_permission/test_user_permission.py:96` | later | v1 restrictions apply to every DocType with the field. |
| Tree restrictions (`hide_descendants`) | `frappe/frappe/core/doctype/user_permission/test_user_permission.py:178` | never | Project and Site aren't trees in v1. |
| Multi-company record rules | `odoo/odoo/addons/purchase/security/ir.access.csv:6` | never | Single company (F1). |
| Approval rules by amount and self-approval | `odoo/odoo/addons/hr_expense/security/ir.access.csv:4` | — | Owned by APR. AUTH supplies only the `approve` right. |

## Interfaces this area needs from others
- **DOC** `doc.Engine.Do` calls `auth.Service.Can(ctx, p, doctype, right, rec)` before every action except approve and reject, with the spec R4 mapping (changed by spec R2/R4): save → `create` (new) / `write` (existing); submit, withdraw, close → `submit`; revise → `write`; cancel → `cancel`; amend → `amend`; delete → `delete`. For approve and reject (right `approve`), DOC first checks visibility with `read` (hidden → 404), then calls APR's `CheckApprover`, then its from-state guard (CD-52). DOC owns the `created_by` assertions: clients can't set it, and the amender becomes `created_by` (CD-67). DOC enforces state rules (F5, spec §3.2) on its own, including for Administrators. On amend, DOC sets `created_by` to the amending user. On save and import, DOC calls `auth.Service.CheckValues` for proposed Project and Site values and `auth.Service.CheckLink` for each linked document (CD-12).
- **DOC** owns the **DocType registry** (`doctype.Register(doctype.DocType{…})` in `internal/kernel/doctype`, CD-02). Each DocType declares `Submittable`, `Approvable`, `Importable`, `OwnerField`, `EmployeeField`, `ProjectFields` and `SiteFields` (header columns, and line columns as `lines.col`) and `DefaultRights` (role → right → scope), CD-09. AUTH reads the registry for the matrix validator (AUTH-065, AUTH-066, AUTH-119), the defaults (AUTH-067) and the scope filter. There is no AUTH-side registry.
- **DOC** implements `doctype.InfoSource` (CD-05). AUTH uses it in `CheckLink` to get a linked record's ref and display label (`PO/26-27/00030` or `Draft 42`).
- **DOC** provides the append-only audit API: AUTH takes an `audit.Writer` (`audit.Default` in production, `auditfake` in tests, CD-03) and writes user, role, matrix, restriction, unlock and password-reset events to it. Sign-in events go to AUTH's own `auth_log`.
- **APR** `approval.Service.CheckApprover` owns the approve-right and eligibility messages for approve and reject (CD-52); it uses `auth.Service.Can` for visibility and the `approve` right, then applies its amount limits and self-approval rules. APR can list possible approvers with `auth.Service.UsersWithRight(ctx, doctype, approve, rec)`, which respects team scope and restrictions and excludes disabled users. APR uses the same `auth.Directory` for reporting managers (CD-13).
- **MST** implements `auth.Directory` (CD-13; every method takes `tx`): `EmployeeForUser` and `UserForEmployee` (own scope, `auth.RecordRef.EmployeeID`), `ManagerChain`, `DirectReports` (active employees only; team scope), and `ProjectCodes` / `SiteCodes` (restriction messages). `UserForEmployee` and `DirectReports` skip employees who have left (CD-54). Until MST merges, tests use WP0's shared fake `authfake.NewDirectory()` (CD-56). MST registers its masters as DocTypes with a `MasterLookup` (CD-21); the Site master declares `SiteFields: ["id"]` and the Project master `ProjectFields: ["id"]` (CD-64), so restrictions apply to the masters themselves (AUTH-102) and AUTH can mark a disabled value (AUTH-105).
- **NUM** list, search, count, report and CSV-export builders (`list.Build(dt, q, scope)`) accept the `auth.Predicate` from `auth.Service.Scope(ctx, p, doctype, right)`, whose columns are qualified with the DocType's table name and whose line conditions use `EXISTS (… parent_id …)`; `SQL == ""` means no filter (CD-16). They always combine it with AND, never alias the main table, and never let a user filter replace it. CSV import asks `auth.Service.Can` for `import` once and for `create`/`write` per row.
- **FILE** print and PDF views (registered with `print.Register`) call `auth.Service.Can(…, print, rec)`: the `print` right plus visibility, read alone is not enough (changed by spec R3). Attachment downloads (`files.Service.Open`) check `read` on the parent. Out-of-scope records give `auth.ErrNotFound` (404); a missing right on a visible record gives `auth.ErrForbidden` (403).
- **NAM** uses `auth.Principal` on its numbering admin screen (CD-28). A number is assigned by `naming.Service.Assign` only inside the transaction that first enters `approved` (changed by spec R1), after AUTH has allowed the transition.
- **Platform**: one SQLite connection pool with `busy_timeout`, used for the `sessions`, `login_attempts` and `auth_log` tables. A clock interface (`clock.Clock`, UTC, shown in Asia/Kolkata, F7) that tests can fake for expiry and lockout. `platform/web` takes AUTH's `*scs.SessionManager` and middleware (CD-14). WP0 declares the one not-found sentinel `doctype.ErrNotFound` (`auth.ErrNotFound` is the same value, CD-54) with AUTH's wording, "This record doesn't exist or you don't have access to it." (CD-76), maps errors to responses with `web.ErrorFrom` (including `db.ErrBusy` → 503), and provides `web.Flash` for banners after a redirect (CD-80).
- **UI** supplies the components (`internal/ui`) AUTH builds its restrictions editor and "Who can…" view from (CD-49); those screens are built after UI merges. UI's generic `/doc/{doctype}/…` routes use AUTH's "checked in handler" guard mode (`CheckedInHandler`) and call `auth.Service.Can` themselves (CD-46). UI maps errors only through `doctype.HTTPError`, which every AUTH error implements (CD-44).
- **WP3** wires `auth/cli` (`AdminCreate`, `UserDisable`) into `cmd/app` (CD-18), owns AUTH-084 (`TestE2E_ButtonsMatchRights`) under `e2e/` (CD-40), and switches `testkit`'s user seeding to AUTH's tables (CD-67).
