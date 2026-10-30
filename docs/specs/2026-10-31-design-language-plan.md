# Design language, iconography, motion and per-deployment theming: plan

Date: 31-10-2026 · Status: draft plan for the owner · Runs **after** PO, GRN and Expense ship · No code, template, CSS, spec or test is changed by this document.

Screenshots: `docs/ux/design-audit/` (390, 768 and 1440 px, Chromium via Playwright, signed in as the owner on the local test instance).

## 0. What this plan does, and what it replaces

The owner asked for a proper design system and iconography, smooth and polished motion, and (added 31 Oct) **per-deployment theming**: each company runs its own instance, puts its logo and accent colours in a config file at deploy time, and the whole app follows. Nothing changes now. The plan is executed as work package **WP-DL** once the modules are done.

The current spec (`docs/specs/2026-10-24-design-system.md`) is sound: tokens with checked contrast, CSP-safe components, mobile first, no sideways scrolling. Most of what the owner saw comes from **gaps** in that spec (no icon rules, no navigation design, motion is just three durations, theming is build-time only), not from wrong decisions. This plan **extends** the spec. When it is approved, the director folds it into a new spec version, `docs/specs/<date>-design-system-v2.md`, with a CD entry. It supersedes these parts:

| Current spec section | What happens |
|---|---|
| §3 Icons row, §7 `Icon` row | Replaced by §4 here (semantic icon registry, Lucide re-pinned) |
| §5 Tokens: Motion row, State badge table, Z-index row | Replaced by §3 and §6 here |
| §5.1 Branding (build-time `brand.json`, CD-99) | Replaced by §3.6–3.9 here: the same keys plus new ones, read **at startup** from a deployment folder |
| §6 App shell: navigation | Extended by §5.1 (groups, states, brand and user area) |
| §7.1 rows `Button` (danger), `StateBadge`, `EmptyState`, `Toast`, `Dialog/Sheet`, App shell | Amended by §5 |
| §10 Enforcement | Extended by §7 |

Everything else stays as it is: product rules §1, people §2, stack §3 (except icons), philosophy §4, layout primitives, screen patterns §8, copy rules §9 and out-of-scope §11. Light mode only stays (§3.10 gives the reasons).

---

## 1. Audit: what the app looks like today

### 1.1 Problems, by screen

| # | Screen and file | Problem | Cause in code |
|---|---|---|---|
| A1 | Nav drawer, `m-party-navopen-390.png`, `home-navopen-390.png` | All 11 masters use the same generic `file` icon. The 4 admin items use mixed metaphors: `pencil` for Numbering (it means "edit"), `lock` for Roles (it means "locked/closed", and is also the **Closed** state icon), `check-circle` for Approval rules (it is also the **Approved** state icon) | `internal/app/routes.go:363` hard-codes `Icon: "file"` for every master and `"file-text"` for every document; admin icons at `:373–376` |
| A2 | Same | No grouping. Work, masters and admin run as one flat list of 17 items, even though `ui.NavItem` already has a `Group` field (`Work`, `Documents`, `Masters`, `Administration`) | `layout/nav.templ` `navList` ignores `Group` |
| A3 | Same, `m-party-1440.png` | The active item is a dark slate fill (`rgb(70,80,94)` = `--color-neutral`, white text). Hover is light grey (`Items` in the 1440 shot). They look unrelated, and neither uses the brand colour | daisyUI `menu-active` uses `--menu-active-bg`, which falls back to `neutral`. `theme.css` maps `neutral` to text-secondary |
| A4 | Same | No brand or user context in the drawer: no logo, no app name, no signed-in user, no sign-out | `navSheet` only renders the close button and the list |
| A5 | Same | Close button: the owner's iPhone showed an oversized X with a heavy dark border. In Chromium it is a 44 × 44 ghost square with a 24 px glyph (measured), the same size as the 24 px nav icons, so it competes with the list. I could not reproduce the border without WebKit. The likely cause is the focus ring when `showModal()` focuses the first control on iOS Safari, because the base `:focus-visible` ring is 2 px solid | `navSheet` puts the close button first, unlabelled by any header; icon size is the SVG default 24 px (no `.icon` size rule exists) |
| A6 | Header, `m-party-768.png`, `signin-390.png`, `m-party-1440.png` | The brand name shows twice ("Paperweight Paperweight"). The placeholder `logo.svg` contains the word "Paperweight" in an SVG `<text>`, and the header also prints `brand-name`. The logo hard-codes `fill="#1F4FD1"`, so it ignores any theme | `platform/brand/assets/logo.svg`, `layout/header.templ` |
| A7 | Every page, `m-party-390.png`, `approvals-390.png`, `settings-numbering-390.png` | The grey canvas stops after the content and the rest of the viewport is white | daisyUI sets `:root { background-color: var(--root-bg) }` (= base-100, white). That stops `body`'s canvas background from propagating to the viewport. Measured: `html` white, `body` `#F5F6F8` |
| A8 | Phone header, `approvals-390.png`, `settings-numbering-390.png` | The menu button stays grey-filled after a tap (sticky hover/focus on touch). Seen in emulation. Not checked on a real phone | `btn-ghost` hover/focus background with no `@media (hover: hover)` guard on our side |
| A9 | Lists, `m-party-390.png`, `m-party-1440.png` | The empty state is a plain sentence plus a **second** "New party" primary button beside the toolbar one: two primaries on one screen (breaks §4.1). "Show disabled parties" is centred bold text with no affordance. The Export button has a heavy 1 px dark outline, the same visual weight as the primary | `components.EmptyState` has no icon or structure; `btn-outline` borders take `currentColor` (text colour) |
| A10 | Wizard, `m-party-new-390.png` | The title shows three times (header "Party details", step label "Party details", h1 "Party details") with **two** progress indicators: the segmented `<ol>` plus a separate `<progress>` bar | Stepper renders both segments and `<progress>`. Spec §8.3 asked for "a segmented bar", not both |
| A11 | Wizard and forms, `m-party-new-390.png`, `m-uom-new-422-390.png` | The sticky action bar is a white box inset from the edges, floating over the form. Content shows on both sides and under it ("Registered Composition" is cut). No top edge or shadow says it is a bar | `.actionbar` sits inside the gutter, not full-bleed; no separator |
| A12 | Validation, `m-uom-new-422-390.png` | The focused error summary gets the blue focus ring around a red box: two competing frames | ErrorSummary takes focus (CD-159, correct). The ring styling isn't adapted for it |
| A13 | Roles, `roles-390.png` | Every role is expanded at once with its rename form. A solid red "Delete" sits beside an outline "Copy rights" and a full-width outline "Rename". No cards, so there is no visual grouping. Headings are small | Screen composed from bare fields and buttons; danger is solid fill everywhere |
| A14 | Numbering (company not set up), `settings-numbering-390.png` | The page is empty under its heading: no explanation, no next step | No empty state used |
| A15 | Sign-in, `signin-390.png` | "Show password" is a bare text button beside a shortened input, and the header repeats the brand (A6) | Password component layout |
| A16 | Booleans, `m-party-new-390.png` | "Vendor (optional)" plus a checkbox labelled "Yes": a question in the label, an answer beside the box | docscreen Bool field rendering |
| A17 | Motion | Durations are tokenised (120/200/300 ms), but daisyUI's own transitions ignore them. The nav sheet's `modal-box` transition measured `translate 0.3s ease-out, scale 0.3s ease-out, opacity 0.2s ease-out 0.05s`, and the drawer's built-in 0.3 s `ease-out` has no reduced-motion handling (vendored daisyUI 5.7.50). Only `.dialog` and the htmx indicator use our tokens | `components/dialog.css` covers `.dialog` only; `.sheet` (nav) inherits daisyUI |
| A18 | Theming | Branding is build-time (`brand.json` embedded, `brand.CSS()` appended to the CSS bundle by `cmd/buildassets`). A company has to rebuild the binary to change colour or logo. The company logo uploaded in the app (CD-209, `files.Store.LatestURL`) is used only on print | `platform/brand`, `ui/cmd/buildassets/build/build.go:177` |
| A19 | CSP | `style-src 'self' 'unsafe-inline'` is still in force (`platform/web/middleware.go:13`). The spec's target is `style-src 'self'` | Not yet dropped |

### 1.2 Good, keep it

- **Tokens and contrast.** Every text pair is at least 4.5:1 and checked in tests (`contrast_test.go`, `TestTheme_MatchesTokens`). Recomputed today: text on canvas 16.16, secondary on sunken 7.09, muted on sunken 4.89, border on surface 3.68.
- **CSP discipline and the lint test** (`internal/ui/lint_test.go`, 25 rules with fixtures). This is the right enforcement model. §7 adds rules to it, not a new tool.
- **One icon pipeline**: vendored Lucide SVGs → `icons.txt` → generated `icons_gen.go`, `aria-hidden`, `currentColor`, no `style`. This plan builds on it.
- **Component ownership**: screens compose `internal/ui/components`, daisyUI only inside `internal/ui`.
- **Field and error pattern** (`m-uom-new-422-390.png`): labels above, "(optional)", error under the field with an icon, summary with links. Good.
- **Type**: Inter, 16 px inputs (no iOS zoom), clear sizes.
- **44 px targets** everywhere (nav items measured 44 px).
- **The dialog's `@starting-style` entry** in `dialog.css` is already the pattern §6 generalises.
- **The brand contrast checker** (`platform/brand/colour.go`: OKLab mix, WCAG ratio) is the seed of §3.7.
- **No overflow at 390, 768 or 1440** on any captured screen.

---

## 2. Principles (each one testable)

| # | Principle | Test that proves it |
|---|---|---|
| P1 | **Tokens only.** No raw colour, length, duration, easing, shadow or z-index outside `tokens.css`, `theme.css` and the generated theme file | Lint rules `raw-colour` (exists), `raw-duration`, `raw-easing`, `raw-shadow`, `raw-z` (§7) |
| P2 | **One meaning per icon, one icon per meaning.** A glyph is used for exactly one concept app-wide, and screens name icons by meaning, not by drawing | Registry test: no glyph appears under two semantic names unless listed as an allowed alias (§4.7) |
| P3 | **Brand is accent, never meaning.** The theme colours actions, links, selection and focus. It never carries status, and status colours never change per company | Theme test: generated theme CSS sets only themeable variables; status variables are absent from it (§3.6) |
| P4 | **Any accent stays accessible.** Every derived colour passes WCAG AA against every surface it can sit on, or the app adjusts it and says so | Property test over random accents (§3.7, §7) |
| P5 | **Motion explains, then gets out of the way.** Only transform and opacity animate; nothing over 300 ms in the UI; everything is instant under reduced motion; no motion blocks input | Lint `transition-property` allow-list; e2e `getAnimations()` check under normal and reduced-motion emulation (§7) |
| P6 | **Same job, same look.** One active, hover, pressed and focus treatment for every selectable thing (nav, list row, radio row, segmented control) | Gallery page "states" with a chromedp check that the computed styles of the four states match the tokens |
| P7 | **Phone first, thumb first.** 44 px targets (48 px in the action bar), primary action nearest the thumb, nothing hidden behind hover | Existing e2e target-size gate plus a `@media (hover: hover)` lint for hover-only styles |

---

## 3. Tokens

### 3.1 Colour roles

Fixed (never themeable): neutrals, status, danger, overlay. Themeable: the **accent** ramp (and optionally a second **document accent** for print). `primary` keeps its name in code, because daisyUI and the current tokens use it.

| Role | Token | Default value | Use | Themeable |
|---|---|---|---|---|
| Canvas | `--color-canvas` | `#F5F6F8` | Page background, **also on `html`** (fixes A7) | No |
| Surface | `--color-surface` | `#FFFFFF` | Cards, inputs, sheets, drawer, header, action bar | No |
| Sunken | `--color-sunken` | `#EDEFF2` | Wells, read-only, hover on neutral rows | No |
| Text / secondary / muted / disabled | `--color-text…` | `#141A23` / `#46505E` / `#5E6878` / `#8A93A1` | As today | No |
| Border / subtle | `--color-border`, `--color-border-subtle` | `#7C8696` / `#DCE0E6` | Input edges (3.68:1) / dividers | No |
| **Accent 600** | `--color-primary-600` | `#1F4FD1` | Primary button fill, links, focus ring, current step, active nav bar | **Yes** |
| **Accent 700** | `--color-primary-700` | `#1A3FA8` | Hover on fills, link hover | **Yes (derived)** |
| **Accent 800** | `--color-primary-800` | `#16348A` | Pressed fill; text and icon on accent tints | **Yes (derived)** |
| **Accent 100** (new) | `--color-primary-100` | derived | Pressed background on tinted rows; selected chip | **Yes (derived)** |
| **Accent 50** | `--color-primary-50` | `#EEF3FF` | Active nav item, selected row, tinted panels | **Yes (derived)** |
| On-accent | `--color-text-on-fill` | `#FFFFFF` | Text on accent and status fills | No (always white; the accent is darkened until white passes) |
| Focus | `--color-focus` | = accent 600 | 2 px ring, 2 px offset | Follows accent (§3.5) |
| Status ×4 | `--color-{success,warning,danger,info}-{50,600,700,800}` | As today | States, banners, field errors | **No** |
| Overlay | `--color-overlay` | `rgb(20 26 35 / 0.5)` | Dialog and drawer backdrop | No |
| Document accent | `--color-doc-accent` | = accent 600 | Print headings, rules, totals band | Yes |

**State colours** (each also has a word and an icon, rule §4.6):

| State | Background | Text | Icon (semantic → Lucide) | Text/background contrast |
|---|---|---|---|---|
| Draft | sunken `#EDEFF2` | secondary `#46505E` | `state-draft` → `circle-dashed` | 7.09 |
| Pending approval | warning-50 `#FFF6E5` | warning-800 `#7A3F00` | `state-pending` → `clock` | 7.71 |
| Approved | success-50 `#EAF7EE` | success-800 `#0F5326` | `state-approved` → `circle-check` | 8.33 |
| Rejected | danger-50 `#FDEEEC` | danger-800 `#8F1F18` | `state-rejected` → `circle-x` | 7.83 |
| Cancelled | sunken | secondary | `state-cancelled` → `ban` | 7.09 |
| Closed | sunken `#EDEFF2` | text `#141A23` | `state-closed` → `circle-stop` | 15.17 |

Change from today: **Closed moves off the accent tint** (`primary-50`/`primary-800`) to a neutral, because a themed accent must not carry meaning (P3), and because a red or orange company accent would make Closed look like Rejected or Pending. Draft, Cancelled and Closed are all neutral and are told apart by their icon and word, which are always shown.

**PO receipt status** (filter chips, not lifecycle states): `receipt-none` → `package`, `receipt-part` → `package-open`, `receipt-full` → `package-check`, all neutral chips.

Status icons on their own surface (non-text, needs 3:1): warning-600 4.39, danger-600 4.99, success-600 4.40, info-600 4.71 on white. All pass.

### 3.2 Type scale (unchanged values, named roles added)

| Role | Size / line height / weight | Where |
|---|---|---|
| `display` (new) | 30/1.25/600 at ≥48rem, 24 on phones | h1 |
| `title` | 20/1.25/600 | h2, dialog title, drawer brand name |
| `heading` | 18/1.375/600 | card titles, h3 |
| `body` | 16/1.5/400 | text, **every input** |
| `label` | 16/1.375/500 | field labels, nav items, buttons |
| `meta` | 14/1.5/400 | metadata, nav group headings (uppercase off; 600 weight, secondary colour) |
| `caption` | 13/1.375/500 | badges, counts only |
| Figures | `tnum` + `.num` | money, quantities |

Rule: at most 3 sizes on a phone screen besides the h1. The **phone h1 stays 24**, but the **header title is hidden when the page h1 is visible**. The header title then appears only after the h1 has scrolled out of view, using an `IntersectionObserver` in `app.js` that toggles a class. That fixes A10's repeated title.

### 3.3 Spacing, sizing, radius, elevation, z-index

- **Spacing**: 4 px grid as today. New named gaps: `--gap-field` 20 px (between fields), `--gap-section` 32 px, `--gap-inline` 8 px.
- **Icon sizes** (new, see §4.3): `--icon-xs` 14, `--icon-sm` 16, `--icon-md` 20, `--icon-lg` 24. **Every `.icon` gets a size from a token**; the bare 24 px SVG default is no longer relied on (A5).
- **Radius**: today's 4/8/12/16/pill. Themeable as a **preset**, not free values (§3.6): `compact` 2/4/6/10, `default` 4/8/12/16, `round` 6/12/16/24. daisyUI's `--radius-selector/-field/-box` follow the preset.
- **Elevation** (light, subtle), named by job: `--shadow-card` (= shadow-1), `--shadow-raised` (sticky action bar top edge, user menu), `--shadow-overlay` (dialog, drawer), `--shadow-sheet`. Cards are flat with a subtle border at rest. Shadow appears only on things that float over content.
- **Z-index**: sticky 100, header 200, dropdown 300, overlay 400, toast 500. `<dialog>` (drawer, sheets, dialogs) uses the top layer. New rule: **no other z-index values** (lint `raw-z`).

### 3.4 Interaction states (one recipe everywhere, P6)

| State | Neutral item (nav, list row, ghost button) | Accent fill (primary button) |
|---|---|---|
| Rest | transparent | accent-600 |
| Hover (only inside `@media (hover: hover)`, fixes A8) | sunken `#EDEFF2` | accent-700 |
| Pressed (`:active`) | accent-100 (accent-50 inside the drawer) + `scale(0.98)` for 100 ms | accent-800 |
| Selected / current | accent-50 background, accent-800 text and icon, 3 px accent-600 bar at the inline start, weight 600 | n/a |
| Focus-visible | 2 px focus ring, 2 px offset (inset −2 px inside the drawer list so it isn't clipped) | same |
| Disabled | text-disabled, no hover | 40 % opacity is **not** used: sunken fill with text-disabled |

This replaces daisyUI's `menu-active` slate (A3). Selected now reads as the hover state taken further in the same hue family, so the two look related.

### 3.5 Focus ring

2 px solid `--color-focus`, 2 px offset (WCAG 2.4.13 asks for at least a 2 px perimeter and 3:1 change). `--color-focus` equals accent-600, which the theme check (§3.7) guarantees is at least 4.5:1 on canvas, surface and sunken. That is above the 3:1 needed. Exception: an element that already has a coloured border of its own (ErrorSummary, A12; banners) gets the ring in `--color-text` instead, so two coloured frames never compete.

### 3.6 Theming model: what a deployment may change

| Input (config key) | Required | Drives | Fixed alternatives |
|---|---|---|---|
| `accent` | Yes (default `#1F4FD1`) | accent 600 and every derived step (700, 800, 100, 50), focus, daisyUI `--color-primary` and `--color-accent` | none |
| `accent_700`, `accent_800`, `accent_50`, `accent_100` | No | Manual override of a derived step, still contrast-checked | derived |
| `document_accent` | No | Print accent | = accent 600 |
| `radius` | No: `compact` / `default` / `round` | Radius tokens and daisyUI radii | `default` |
| `app_name`, logos, favicon | §3.9 | Header, drawer, sign-in, print, tab | embedded defaults |

**Not themeable, and why**: neutrals (text contrast is computed against them, and tinted neutrals make status tints harder to read), status colours and danger (they carry meaning, P3), focus independently of the accent (it follows the accent, so one less thing to check), fonts (a new font is a dependency and changes metrics, which can break the overflow gate), the second brand colour (`secondary`; one accent is calmer, and daisyUI's `secondary` stays mapped to the neutral). A deployment that wants these needs a design change, not a config change.

### 3.7 Deriving the palette from one colour, with automatic checks

Algorithm (in `internal/platform/brand`, pure Go, stdlib only, extends `colour.go`'s OKLab code to OKLCH):

1. Parse `accent` (#RRGGBB). Convert to OKLCH (L, C, H).
2. **600** = the input if it passes all of: white text on it ≥ 4.5; it as text on surface, canvas and sunken ≥ 4.5. Otherwise lower L in steps of 0.005, keeping H and C (reducing C only to stay inside sRGB), until it passes. Record "adjusted".
3. **700** = L(600) − 0.07, same H and C. **800** = L(600) − 0.14, C × 0.9.
4. **50** = L 0.97, C = min(C × 0.25, 0.03). **100** = L 0.93, C = min(C × 0.35, 0.05).
5. Check every pair the UI uses: white on 600 and 700 ≥ 4.5; 600 on surface, canvas and sunken ≥ 4.5 (link text, focus); 800 on 50 and 100 ≥ 4.5 (active nav, selected row); 600 on 50 ≥ 3 (active bar and icon); `document_accent` on white ≥ 4.5.
6. **Manual overrides** (`accent_700`…) are checked by the same pairs and are **never adjusted**: a failing override refuses to start, with the pair named.
7. **Status distance warning**: if the OKLab distance from 600 to any status-700 colour is below 0.10, log a warning (not a refusal). The default blue is already 0.099 from info-700, so the mitigations hold for every theme anyway. Status never uses the accent; every state has an icon and a word; a solid danger fill appears only on a confirm page (§5.2).

Policy on failure:

| Failure | Behaviour | Message (exact form) |
|---|---|---|
| `accent` fails a contrast pair | Darkened automatically. Startup logs a warning; `app brand check` prints it | `brand: accent #00897B fails "white text on accent" (4.32:1, needs 4.5:1); using #007A6E (5.24:1). Set "accent" to #007A6E to silence this.` |
| `accent` fails and `"strict": true` | Refuses to start | same text, ending `Refusing to start because "strict" is true.` |
| An override fails | Refuses to start | `brand: accent_800 #5A7BD6 on accent_50 #EEF3FF is 3.62:1, needs 4.5:1. Remove accent_800 to derive it, or pick a darker colour.` |
| Status distance | Warning only | `brand: accent #AA5403 is close to the warning colour #985000 (distance 0.04). States stay readable because they always show an icon and a word, but consider a different accent.` |

### 3.8 Sample themes (computed with the algorithm above; contrast is WCAG 2 relative luminance)

| | **Default (Paperweight blue)** | **Teal company** (input `#00897B`) | **Saffron company** (input `#F57C00`) | **Maroon company** (input `#8B1E3F`) |
|---|---|---|---|---|
| Input passes? | yes | **no**: white on it 4.32, on sunken 3.75 | **no**: white on it 2.70, on sunken 2.35 | yes |
| 600 | `#1F4FD1` | `#007A6E` (adjusted) | `#AA5403` (adjusted) | `#8B1E3F` |
| 700 | `#1A3FA8`\* | `#006459` | `#8C4502` | `#73002D` |
| 800 | `#16348A`\* | `#014E45` | `#703601` | `#55001F` |
| 100 | `#DDE8FE` | `#D1EFEA` | `#FEE1D0` | `#FFDFE3` |
| 50 | `#EEF3FF`\* | `#E5FBF6` | `#FEF3EB` | `#FEF1F3` |
| White on 600 | 6.78 | 5.24 | 5.28 | 8.92 |
| White on 700 | 9.05 | 7.08 | 7.08 | 11.89 |
| 600 on surface / canvas / sunken | 6.78 / 6.27 / 5.89 | 5.24 / 4.84 / 4.55 | 5.28 / 4.88 / 4.58 | 8.92 / 8.25 / 7.74 |
| 800 on 50 (active nav text) | 9.99 | 8.94 | 8.68 | 13.58 |
| 600 on 50 (active bar, icon ≥ 3) | 6.10 | 4.85 | 4.83 | 8.11 |
| Distance to nearest status-700 | info 0.10 | success 0.08 ⚠ | warning 0.04 ⚠, danger 0.09 ⚠ | danger 0.10 |

\* The default keeps today's hand-set 700/800/50 from `brand.json` (derivation would give `#0D37B9` / `#042597` / `#F0F5FF`, also passing). Saffron shows the adjusted case clearly: the company's bright orange is too light for white text, so buttons use a burnt orange. The logo keeps its true colour, because logos are images and are not recoloured. Script used: `docs/ux/design-audit/dl-palette.py`. It becomes the Go test table in task DL-4.

### 3.9 Configuration file and logos

**Location and loading.** Today `brand.json` is embedded and checked at `go generate`. New: `app serve --brand <dir>` (default `<data>/brand`, so `/var/lib/paperweight/brand` under `deploy/app.service`, which is already readable). It is read **once at startup**:

- Folder or `brand.json` absent: embedded defaults; log `brand: no brand folder at /var/lib/paperweight/brand; using the built-in Paperweight look.`
- Present and invalid: **refuse to start**, with one message per problem (unknown key, bad colour, missing file, logo rule), in the same style as today's `Parse` messages. systemd's `Restart=on-failure` makes the error visible in `journalctl -u paperweight`.
- `app brand check <dir>` runs the same validation and prints the computed palette and contrast table (the §3.8 rows), so a deploy is checked before restart.
- Changes need a restart. No live reload: that keeps hashing and caching simple.

**Format** (JSON, unknown keys refused, as today; documented in `internal/platform/brand/README.md`):

```json
{
  "app_name": "Shree Infra",
  "accent": "#8B1E3F",
  "document_accent": "#8B1E3F",
  "radius": "default",
  "strict": false,
  "app_logo": "logo.svg",
  "app_mark": "mark.svg",
  "document_logo": "letterhead.png",
  "favicon": "favicon.png"
}
```

`primary`, `primary_700`, `primary_800` and `primary_50` are accepted as aliases for one release, so existing files keep working, then removed.

**Logo slots** (all shown inside fixed boxes, `object-fit: contain`, real `width`/`height` on the `<img>`, as CD-99):

| Slot | Where | Box | Formats and limits | Fallback chain |
|---|---|---|---|---|
| `app_logo` (wordmark) | Desktop header (≥48rem), sign-in page, drawer top on phones | header 160 × 32, sign-in 240 × 64, drawer 160 × 32 | SVG with `viewBox`, or PNG ≥ 2× box size; ≤ 200 KB; ratio 1:2–8:1 | built-in Paperweight logo |
| `app_mark` (square, new) | Phone header (beside the title, optional), drawer when no wordmark fits, generated favicon, Apple touch icon | 32 × 32 (header), 180 × 180 (touch icon) | SVG or PNG ≥ 180 × 180, square ±5 % | `favicon` → generated monogram (first letter of `app_name`, white on accent-600, an SVG made at startup and served as a file) |
| `favicon` | Browser tab | 32 × 32 and 180 × 180 | PNG square ≥ 32 (≥ 180 recommended) | `app_mark` → monogram |
| `document_logo` | Print/PDF letterhead | 60 × 20 mm | PNG or JPEG ≤ 1 MB, ≥ 709 px wide (300 dpi at 60 mm) | **Company logo uploaded in the app (CD-209 `Store.LatestURL`)** → `document_logo` → `app_logo` → company legal name as text |

Rules: the **logo must not contain the app name as well as being followed by it**. If `app_logo` is a wordmark, set `"show_name": false` (new key, default true) so the header doesn't repeat it (A6). Logos are images and keep their own colours. Our own placeholder logo is redrawn to use no hard-coded accent, with a neutral mark and no wordmark. SVG logos are refused if they contain `<script>`, `<foreignObject>`, event attributes or external `href`s. They are only ever shown through `<img>`, where scripts don't run.

The CD-209 company logo stays the letterhead source (owner decision D3), because admins can change it in the app without a redeploy.

### 3.10 How the theme reaches the browser under CSP

1. At startup `brand.Theme(cfg)` returns CSS text: `:root{--color-primary-600:#8B1E3F;…;--color-primary:…;--color-primary-content:#FFFFFF;--color-accent:…;--radius-field:…;…}`, plus the print variables. It sets **only** themeable variables (P3, tested).
2. The bytes are hashed (sha256, first 8 hex characters) and served at `/static/theme.<hash>.css` with `Cache-Control: public, max-age=31536000, immutable`, through the same handler as the other static assets. Logos, the mark and the favicon are served the same way, under hashed names.
3. `layout.Page` links it **after** `app.css`, so it wins without `!important`. Print links `/print/assets/theme.<hash>.css`.
4. `cmd/buildassets` **stops appending `brand.CSS`**. The CSS bundle then no longer depends on the brand, so `TestAssets_UpToDate` is independent of any deployment.
5. `brand_gen.go` constants (`AppLogoURL`, sizes) become fields of a runtime `brand.Active` value, set once in `app serve` and read by templates through `ui.Shell`.

No inline `<style>`, no `style=`. This works with today's CSP and with the target `style-src 'self'` (A19).

**Light only, confirmed.** A dark theme doubles every contrast check and every screenshot gate, and adds a second set of status tints and a second brand derivation per company. Staff use the app in offices and on sites in daylight, so the benefit is small. Keep `color-scheme: light`.

### 3.11 Mapping to Tailwind `@theme` and daisyUI

| daisyUI / Tailwind variable | Source | Set in |
|---|---|---|
| `--color-primary`, `--color-accent` | accent-600 | `theme.css` (default), theme file (runtime) |
| `--color-primary-content`, `--color-accent-content` | text-on-fill | `theme.css` |
| `--color-base-100/200/300/-content` | surface / canvas / sunken / text | `theme.css` (fixed) |
| `--color-secondary`, `--color-neutral` | text-secondary | `theme.css` (fixed) |
| `--color-info/success/warning/error` | status-700 | `theme.css` (fixed) |
| `--menu-active-bg`, `--menu-active-fg` | accent-50 / accent-800 (fixes A3) | `components/nav.css` utility |
| `--radius-selector/-field/-box` | radius preset | `theme.css` default; theme file |
| `--root-bg` | canvas (fixes A7) | `base.css` |
| `--depth`, `--noise` | 0 | `theme.css` |
| Tailwind `@theme static` tokens (`--color-primary-600` …) | `tokens.css` | runtime theme file overrides the themeable ones |

---

## 4. Iconography

### 4.1 Source and dependency

**Recommendation: stay on Lucide** (ISC; icons that came from Feather are MIT; 2015 icons per lucide.dev; already vendored). Re-pin to the latest stable `lucide-static` (1.65.0 on npm today, checked). No new dependency. Lucide's rules are the base of §4.2. Today's set uses names Lucide has since renamed (`alert-circle` → `circle-alert`, `check-circle` → `circle-check`, `x-circle` → `circle-x`, `more-horizontal` → `ellipsis`). The re-pin moves to the canonical names.

Alternatives considered (**not** recommended, listed for the record): Phosphor (MIT; six weights including Fill and Duotone; designed on a 16 px grid) would give true filled variants for selection, but it's a second style and a migration. Material Symbols (Apache 2.0; variable FILL/wght/GRAD/opsz axes) is delivered as a font, which means a font dependency, ligature text in the DOM, and a different look. If the owner wants filled "selected" icons, the no-dependency route is §4.4: draw a filled twin for the few nav icons under our own rules.

### 4.2 Grid and drawing rules (every icon, bought or drawn)

| Rule | Value | Source |
|---|---|---|
| Canvas | 24 × 24, `viewBox="0 0 24 24"` | Lucide guide |
| Live area | 22 × 22: at least 1 px from every edge | Lucide guide |
| Stroke | 2 px, centred, never mixed within or across icons; `stroke="currentColor"`, `fill="none"` | Lucide guide; Apple HIG ("consistent… stroke thickness") |
| Caps and joins | round / round | Lucide guide |
| Corner radius | 2 px on 90° corners of shapes ≥ 8 px; 1 px on smaller shapes; 2.41 px where diagonals meet at a right angle; corners where more than two lines meet are not rounded | Lucide guide |
| Spacing | ≥ 2 px between separate elements and inside gaps | Lucide guide |
| Perspective | flat, front-on, no 3D, no shading | Apple HIG |
| Colour | `currentColor` only. No fills with colour, no gradients, no hard-coded hex, so any theme works (P3) | ours |
| Optical sizes | Drawn at 24. Rendered at 16, 20 or 24 only (`--icon-sm/md/lg`). 14 px (`--icon-xs`) only inside 13 px badges. At 16 px the stroke **stays 2 px in viewBox units** (renders 1.33 px), which suits 14–16 px text | Carbon (16/20 next to 14/16 text) |
| Size pairing | 16 with 13–14 px text (badges, meta); 20 with 16 px text (buttons, nav, fields); 24 alone (icon-only buttons, empty states use 48 = 2× of 24) | Carbon, Apple ("match the weights… of adjacent text") |
| Vertical alignment | centred on the text's line box, never on the baseline | Carbon |
| Touch target | icon-only control ≥ 44 × 44 regardless of glyph size | Carbon, WCAG 2.5.5 |
| Contrast | icon colour ≥ 3:1 against its background (1.4.11). Icons used as the only label of a control ≥ 4.5:1 (Carbon's stricter rule) | WCAG, Carbon |

### 4.3 Sizes in use (replaces "24 everywhere", A5)

| Context | Size | Colour |
|---|---|---|
| Nav item | 20 | text-secondary at rest; accent-800 when current |
| Drawer close, header menu, inbox, account | 20 glyph in a 44 × 44 ghost button | text |
| Button with label | 20 (16 in `btn-sm`) | inherits |
| Field error, badge, chip | 16 (14 in badges) | inherits |
| Banner, toast | 20 | status-600 (on 50 tint ≥ 3:1) |
| Empty state | 48 in a 72 px sunken circle | text-muted |

### 4.4 Outline vs filled

- **Outline everywhere** by default (Apple HIG: outline "works well in toolbars, lists… alongside text").
- **Selection is shown by the row**, not by swapping the glyph: the accent-50 background, the 3 px bar, accent-800 colour and 600 weight (§3.4). Lucide has no filled variants, and drawing fills for 30 nav icons is work with little payoff.
- Filled shapes are allowed only for **status dots** (a 8 px circle in a chip) and the **count badge**.
- If the owner wants filled selected nav icons (Apple's tab-bar convention), that is owner decision D5. The cost is a filled twin for each nav glyph, drawn under §4.6.

### 4.5 Metaphor rules

1. Name icons by **what they mean in this app** (`party`, `approve`), map to a glyph once (registry), and never call a glyph directly from a screen.
2. One glyph, one meaning (P2). Explicit exceptions live in the registry with a reason (for example `x` for both "close" and "dismiss", which are the same meaning).
3. **Objects** (nav, document types, masters) use nouns: a thing you can picture (cart, truck, receipt). **Actions** use verbs drawn as motion or change (plus, send, undo). **States** share a **circle silhouette** (dashed circle, clock, check circle, x circle, slashed circle, stop circle), so a state icon is recognisable as a state at a glance.
4. Prefer the metaphor Indian office staff already know from phone apps (WhatsApp, UPI, Tally): `indian-rupee` for money, `receipt` for a bill, `truck` for delivery.
5. Never use a status glyph for navigation (today's `check-circle` Approval rules, `lock` Roles).
6. Never use a glyph that implies an action for a place (today's `pencil` Numbering).
7. Icon + label always, except the standard icon-only controls: menu, close, back, inbox, account, more, remove-line. Each of those has an `aria-label`.

### 4.6 Icon map (every nav item, document type, action and state)

Semantic name → Lucide glyph (all names checked against `lucide-static@1.65.0`).

**Navigation: Work**

| Item | Semantic | Glyph |
|---|---|---|
| Home (if added) | `nav-home` | `house` |
| Approvals (inbox) | `nav-approvals` | `inbox` |
| My requests | `nav-my-requests` | `file-clock` |
| Search | `search` | `search` |

**Documents**

| DocType | Semantic | Glyph |
|---|---|---|
| Purchase Orders | `doc-purchase-order` | `shopping-cart` |
| Goods Receipts | `doc-goods-receipt` | `truck` |
| Expense Claims | `doc-expense-claim` | `receipt` |
| Employee Advances | `doc-employee-advance` | `hand-coins` |
| Pending items report (and reports) | `nav-reports` | `chart-column` |

**Masters**

| Master | Semantic | Glyph |
|---|---|---|
| Branches | `master-branch` | `store` |
| Companies | `master-company` | `building-2` |
| Employees | `master-employee` | `id-card` |
| Expense Types | `master-expense-type` | `tags` |
| Items | `master-item` | `package` |
| Parties | `master-party` | `handshake` |
| Payment Terms | `master-payment-terms` | `calendar-clock` |
| Projects | `master-project` | `folder-kanban` |
| Sites | `master-site` | `map-pin` |
| Tax Codes | `master-tax-code` | `percent` |
| UoMs | `master-uom` | `ruler` |
| Addresses (sub-records) | `master-address` | `map` |
| Contacts (sub-records) | `master-contact` | `contact` |

**Administration**

| Item | Semantic | Glyph |
|---|---|---|
| Users | `admin-users` | `users` |
| Roles | `admin-roles` | `shield` |
| Numbering | `admin-numbering` | `hash` |
| Approval rules | `admin-approval-rules` | `list-checks` |
| Business settings | `admin-settings` | `settings` |
| CSV import | `admin-import` | `file-spreadsheet` |
| Stuck approvals | `admin-stuck` | `hourglass` |
| My sessions | `account-sessions` | `monitor-smartphone` |
| Account menu | `account` | `circle-user` |
| Sign out | `sign-out` | `log-out` |

**Actions**

| Action | Semantic | Glyph |
|---|---|---|
| New / add line | `add` | `plus` |
| Edit / change | `edit` | `pencil` |
| Delete draft / remove line | `delete` | `trash-2` |
| Submit / submit for approval | `submit` | `send` |
| Withdraw | `withdraw` | `undo-2` |
| Revise | `revise` | `pencil-line` |
| Approve | `approve` | `check` |
| Reject | `reject` | `x` (alias of `close`, allowed: both mean "no") |
| Cancel document | `cancel-doc` | `ban` (alias of `state-cancelled`, allowed: same meaning) |
| Close document | `close-doc` | `circle-stop` (alias of `state-closed`) |
| Amend | `amend` | `copy-plus` |
| Print | `print` | `printer` |
| Export | `export` | `download` |
| Import / upload | `upload` | `upload` |
| Attach | `attach` | `paperclip` |
| Take photo | `camera` | `camera` |
| Choose file | `file-choose` | `file-up` |
| Image / thumbnail placeholder | `image` | `image` |
| Filter | `filter` | `list-filter` |
| Sort | `sort` | `arrow-up-down` |
| More actions | `more` | `ellipsis` |
| Back / previous | `back` | `chevron-left` |
| Next / open row | `forward` | `chevron-right` |
| Expand disclosure | `expand` | `chevron-down` |
| Move up / down | `move-up`, `move-down` | `arrow-up`, `arrow-down` |
| Receive all pending | `receive-all` | `check-check` |
| Copy rights | `copy` | `copy` |
| Reset to defaults | `reset` | `rotate-ccw` |
| Reload (409) | `reload` | `refresh-cw` |
| Close dialog / dismiss | `close` | `x` |
| Menu | `menu` | `menu` |
| Show / hide password | `show`, `hide` | `eye`, `eye-off` |
| Enable / disable master | `enable`, `disable` | `toggle-right`, `toggle-left` |
| Date | `date` | `calendar` |
| Money | `money` | `indian-rupee` |

**States and feedback**

| Meaning | Semantic | Glyph |
|---|---|---|
| Draft | `state-draft` | `circle-dashed` |
| Pending approval | `state-pending` | `clock` |
| Approved | `state-approved` | `circle-check` |
| Rejected | `state-rejected` | `circle-x` |
| Cancelled | `state-cancelled` | `ban` |
| Closed | `state-closed` | `circle-stop` |
| Not / part / fully received | `receipt-none/part/full` | `package` / `package-open` / `package-check` |
| Success banner / toast | `feedback-success` | `circle-check` (alias of `state-approved`, allowed: both mean "done, good") |
| Info | `feedback-info` | `info` |
| Warning | `feedback-warning` | `triangle-alert` |
| Error (field, summary, banner) | `feedback-error` | `circle-alert` |

Total: about 75 semantic names on about 70 glyphs, against 33 glyphs today.

### 4.7 The registry (how the pipeline changes)

- `internal/ui/icons/icons.txt` becomes two columns: `semantic-name  lucide-glyph  [# alias-of: name, reason]`. Glyph SVGs stay vendored in `icons/lucide/`, plus a new `icons/custom/` folder for our own drawings (§4.8), each with the same header format.
- `gen` emits a **typed constant per semantic name** (`icons.MasterParty`, type `icons.Name`), and `Icon(name icons.Name)`. A misspelt icon is then a **compile error**, not a render panic. `NavItem.Icon` and `ButtonProps.Icon` become `icons.Name`.
- Nav icons per DocType live in one `map[string]icons.Name` in `internal/app`, keyed by DocType name. They don't go on `doctype.DocType`, because the kernel must not import `internal/ui`. A test fails when any registered DocType has no entry, which fixes A1 for every future module.
- `gen` validates each SVG: 24 viewBox, stroke-width 2, round caps and joins, no `fill` other than `none`/`currentColor`, no colour hex, no `style`, no `transform` with scale. Any failure stops `go generate` with the file and the rule.

### 4.8 Making a new icon

1. **Brief** (in the task or UX doc): meaning in one sentence, where it appears, sizes, the nearest existing icons, and why none of them works.
2. **Search Lucide first** (lucide.dev, 2015 icons). If one fits, add a registry line. Done.
3. **Otherwise draw it** on the 24 grid (§4.2) in any vector editor. Start from the nearest Lucide glyph, export plain SVG, and hand-clean it: one `<path>` or a few primitives, no groups, no ids, no transforms, coordinates on 0.5 px or better.
4. **Review checklist** (reviewer ticks every line):
   - [ ] reads at 16 px on a phone (screenshot at 16, 20, 24 on surface and on accent-50)
   - [ ] stroke 2, round caps and joins, 1 px clear of the edges, ≥ 2 px gaps
   - [ ] same visual weight as its neighbours in the nav or toolbar (side-by-side screenshot)
   - [ ] metaphor follows §4.5; no clash with an existing meaning
   - [ ] states use the circle silhouette; objects are nouns; actions are verbs
   - [ ] no text, digits or letters in the drawing (they don't translate or scale)
   - [ ] `currentColor` only
5. **Naming**: the semantic name is kebab-case by meaning (`doc-<doctype>`, `master-<master>`, `admin-<page>`, `state-<state>`, verbs for actions). The file name in `custom/` describes the drawing (Lucide's convention: name by what it shows).
6. **Add** the line to `icons.txt` and run `make generate`.
7. **Gallery**: the `/_gallery/icons` page lists every registry entry by group, at 16/20/24, with its glyph name. It is generated from the registry, so nothing is added by hand.
8. **Tests**: `gen`'s validation; `TestIcons_GalleryCoversRegistry`; `TestIcons_NoDuplicateGlyph` (aliases must name a reason); the DocType coverage test.

### 4.9 An icon must never

- carry colour of its own, or a brand colour;
- be the only carrier of meaning (state chips always have a word);
- be the only label on a control without an `aria-label`;
- animate in a loop (except the busy spinner, which carries words: "Saving…");
- mean two things, or share a glyph with a state when it isn't that state;
- be resized off the four sizes, or mixed with another icon style;
- include text or numerals;
- be decorative filler (an icon beside every heading or field label is noise).

---

## 5. Components and layout

### 5.1 App shell and navigation

**Information architecture** (groups come from the existing `NavItem.Group`; empty groups are not rendered):

| Group heading | Items (rights decide which show) |
|---|---|
| (none, top) | Approvals (with count), My requests |
| Documents | Purchase Orders, Goods Receipts, Expense Claims, Employee Advances, Reports |
| Masters | Parties, Items, Projects, Sites, Employees, then a **"More masters"** disclosure for Branches, Companies, Expense Types, Payment Terms, Tax Codes, UoMs |
| Administration | Users, Roles, Approval rules, Numbering, Business settings, Import, Stuck approvals |

The order inside Masters follows **frequency** (parties and items daily; UoMs set up once), not the alphabet. The disclosure stays open when the current page is inside it. Group headings use `meta` 14/600, text-secondary, 24 px above, `<h2>` inside `<nav>` with `aria-labelledby` on each `<ul>`.

**Phone drawer** (the existing `<dialog id="nav-sheet">`, which works without JS on CSS-only fallback, stays a dialog):

```
┌──────────────────────────────┐
│ [mark] Shree Infra       [×] │  56 px header row: logo/mark + name; × is a
│                              │  20 px glyph in a 44 px ghost button, end-aligned
├──────────────────────────────┤
│  ▣ Approvals             3   │  count = caption chip, accent-50/accent-800
│  ◷ My requests               │
│ DOCUMENTS                    │
│▍🛒 Purchase Orders           │  current: accent-50 bg, 3 px accent bar,
│  🚚 Goods Receipts           │  accent-800 text and icon, 600 weight
│  …                           │
│ MASTERS                      │
│  …  More masters  ⌄          │
│ ADMINISTRATION               │
│  …                           │
├──────────────────────────────┤
│ (◉) Priya Shah               │  user area, sticky bottom: name + role,
│     Purchase User    Sign out│  "My sessions", "Sign out" (44 px each)
└──────────────────────────────┘
```

- Width `min(100%, 20rem)`; full height; safe-area padding.
- **Focus on open goes to the current item** (or the first item), not the close button. That stops the iOS focus ring landing on ×, the likely cause of A5. `app.js` calls `focus({focusVisible:false})` where supported; elsewhere the ring stays (correct for keyboard users).
- Closes on ×, Esc, a backdrop tap, and after navigating.
- Items: 44 px tall (48 px on phones, matching the action bar, because staff may wear gloves), 12 px inline padding, 12 px gap between icon and label, radius-md.
- **Desktop sidebar** (≥64rem): the same `navList`, 16rem wide, brand at the top of the header, user area in the header account menu. Collapsing to icons only (spec §6 "collapsible") is **dropped**: icon-only nav needs tooltips and memorisation. Owner decision D6.

**Header**: phone shows menu, title (only once the h1 has scrolled away, §3.2), inbox and account. From 48rem: logo (and name unless `show_name: false`), search from 64rem, inbox, account. The duplicate brand (A6) is fixed by `show_name` and the redrawn placeholder.

### 5.2 Buttons

| Variant | Look | Use |
|---|---|---|
| Primary | accent-600 fill, white | the one main action per screen |
| Secondary | surface, **1 px border token `--color-border`** (not `currentColor`, fixes A9's heavy outline), text colour | other actions |
| Ghost | no border, text colour, sunken on hover | toolbars, icon buttons, "Change" |
| Danger | **secondary look with danger-700 text and `delete` icon** | destructive triggers ("Delete draft", "Delete role") |
| Danger confirm | danger-700 fill, white | only on the confirm step ("Delete role Stores") |

Danger is a solid fill only where the user confirms. That keeps a red or saffron company accent from ever sitting next to a red solid button with a different meaning, and it calms screens like Roles (A13). Minimum 44 px (48 in the action bar); label + optional 20 px icon at the start; busy state swaps the label to "Saving…" with the spinner.

### 5.3 Lists (DataList)

- Phone card: as spec §8.1. The **whole card is the link**. Pressed state: accent-100 + `scale(0.99)`. Chevron `forward` 16 px at the end, text-muted.
- Toolbar on a phone: search full width; **Filter** and **Sort** as two equal secondary buttons on one row (sort select inside a sheet on phones); "New …" moves into the **action bar** on phones (thumb reach), and stays at the toolbar end from 48rem. The empty state then has **no second primary** (A9).
- "Show disabled …" becomes a secondary filter chip with `eye` / a checked state, left-aligned under the count.

### 5.4 Empty, loading and error states

| State | Pattern |
|---|---|
| Empty (never had data) | 48 px icon of the list's object in a 72 px sunken circle, `heading` "No parties yet", one line of help, **one** action (secondary on phones when the action bar already has the primary) |
| Empty (filtered) | Same shape, icon `search`, "No parties match "shree"", button "Clear filters" |
| Empty page section (A14) | Short sentence on why and what to do: "Numbering starts after you set up your company. Set up your company" (link) |
| Loading (htmx) | Under 400 ms: nothing. After 400 ms (htmx indicator, delayed with a CSS `transition-delay`, no JS): the busy button label and a 2 px indeterminate bar under the header. Lists that re-filter get skeleton rows (daisyUI `skeleton`, allowed) only on first load of a region |
| Validation (422) | As today, with the ring rule of §3.5 (A12) |
| Conflict (409) | Warning banner + "Reload" button with `reload` |
| Server error | Danger banner in `#alerts`, never a toast |

### 5.5 Forms and wizards

- Field gap 20 px; section gap 32 px with a `heading`.
- Booleans (A16): a **toggle row** ("Vendor" label, help text, toggle at the end, the whole row the target) in place of "Vendor (optional) / ☐ Yes". daisyUI `toggle` is allowed.
- **Stepper (A10)**: one indicator only. Phone: "Step 2 of 4 · Terms" in `meta`, a segmented bar (done = accent-600, current = accent-600 with a 2 px accent-800 underline, future = progress-track). The `<progress>` element stays for assistive tech but is visually hidden. The h1 is the step name. The header title stays hidden (§3.2).
- **Action bar (A11)**: full-bleed surface, 1 px border-subtle top edge plus `--shadow-raised`, safe-area bottom padding; primary fills the width on phones (secondary beside it when there are two). Content gets bottom padding equal to the bar, so nothing hides behind it. From 64rem it becomes an inline footer, as spec §7.

### 5.6 Document and approval pages

- Top block (phone): state chip + document number on one row; amount in `display` with `tnum`; amount in words in `meta`; "Needs your approval: Purchase Manager, ₹50,000 or more" as an info banner on approval pages.
- Sections are cards with `heading` and a `Change` ghost button where editable. Approval history and audit are Disclosures with a `history` icon.
- Approval action bar: **Approve** (primary, `approve` icon) and **Reject** (secondary, `reject` icon), as spec §8.5. Approve has no confirm.
- After approve: success banner + "Next request" (`forward`) link, with the list row leaving the inbox (motion §6.4).

### 5.7 Toasts and banners

Toast: bottom centre on phones (above the action bar, `bottom: actionbar-h + 8 px`), bottom end from 48rem; surface with a 3 px status-600 bar at the start, 20 px status icon, text, dismiss (`close`). 5 s auto-dismiss, paused on hover/focus (as today). At most 1 visible on phones (a new one replaces the old), 3 stacked on desktop.

### 5.8 Touch and keyboard

- 44 × 44 targets with 8 px gaps; 48 px in the action bar and phone nav.
- Hover styles only inside `@media (hover: hover)` (lint, P7). `:active` gives touch feedback.
- `-webkit-tap-highlight-color: transparent` on interactive elements, **only** because every one of them has a designed `:active` state (tested in the gallery).
- Keyboard: drawer and dialogs trap focus (native `<dialog>`); Esc closes; focus returns to the opener; skip link stays; `/` focuses search from 64rem (later, optional).

---

## 6. Motion

### 6.1 Tokens

Based on Material 3's duration scale and easing set (our current three easings already equal M3's standard, standard-decelerate and standard-accelerate) and on Carbon's productive/expressive split. This is a work tool, so **productive** motion only.

| Token | Value | Source | Use |
|---|---|---|---|
| `--dur-instant` | 50 ms | M3 short1 | colour and background changes on press |
| `--dur-fast` | 100 ms | M3 short2 (was 120) | hover, focus ring, pressed scale, checkbox and toggle |
| `--dur-base` | 200 ms | M3 short4 | toasts, chips, small panels, row highlight, exits of larger things |
| `--dur-slow` | 300 ms | M3 medium2 | drawer and sheet enter, dialog enter, wizard step, page transition |
| `--dur-exit-ratio` | exits use the next shorter token | Alpine's defaults (150 in, 75 out) follow the same idea; no single primary source states it as a rule | drawer exit 200, toast exit 100 |
| `--ease-standard` | `cubic-bezier(0.2, 0, 0, 1)` | M3 standard | on-screen changes (move, resize) |
| `--ease-enter` | `cubic-bezier(0, 0, 0, 1)` | M3 standard-decelerate | entering elements |
| `--ease-exit` | `cubic-bezier(0.3, 0, 1, 1)` | M3 standard-accelerate | leaving elements |
| `--ease-emphasized-enter` | `cubic-bezier(0.05, 0.7, 0.1, 1)` | M3 emphasized-decelerate | **only** drawer and sheet enter (a big surface needs a softer landing) |
| `--distance-sm` / `--distance-md` | 8 px / 24 px | ours | slide distance for small things / sheets and steps (never a full-screen slide on content) |
| `--scale-press` | 0.98 | ours | pressed buttons and cards |
| `--stagger` | 30 ms, max 5 items | ours | list first-render stagger (optional, D7) |

**Ceiling: 300 ms** for any UI motion. Nothing in the app needs the M3 long or extra-long tokens.

### 6.2 What animates and what never does

Animates: drawer and sheets, dialogs, toasts, the user menu panel, disclosures (chevron rotation only), pressed and hover states, wizard step change, a list row insert or remove after an htmx action, a field error appearing, the save-state label, the indeterminate loading bar, the skeleton shimmer (reduced under reduced motion), page navigation (cross-fade only, where supported).

**Never** animates: text content and numbers (no counting-up totals), money and quantities, layout properties (`width`, `height`, `top`, `left`, `margin`, `padding`), scroll position (no smooth scroll on focus moves: `defaultFocusScroll` stays instant), error summaries (they appear at once and take focus), the h1 on a step change (it is the focus target), anything on first page load except the drawer if it opens, anything in print, and nothing loops except the busy spinner and the loading bar while a request is in flight.

### 6.3 Performance rules

- Animate `transform` (`translate`, `scale`, `rotate` as individual properties) and `opacity` only. Lint enforces a `transition-property` allow-list (P5).
- No `transition: all`. No `will-change` except on the drawer box while open.
- No animation that delays input: a button is clickable during its pressed animation, and a dialog accepts input during its entry.
- Exit animations never hold up an htmx swap by more than `--dur-fast` (100 ms `swap:` modifier maximum).
- No blur animations (Apple: avoid animating into and out of blurs).

### 6.4 Patterns and exactly how each is built

Browser support (MDN browser-compat data): `@starting-style` Chrome 117, Firefox 129, Safari 17.5. `transition-behavior: allow-discrete` Chrome 117, Safari 17.4, Firefox 129 partial (can't transition `display`). `overlay` Chrome only. Same-document View Transitions Chrome 111, Firefox 144, Safari 18. Cross-document `@view-transition` Chrome 126, Safari 18.2, not Firefox. `interpolate-size` Chrome 129 only. Our floor is Safari 16.4, so **every pattern is progressive**: without support it appears instantly and still works. Nothing depends on motion.

| Pattern | Motion | Implementation under our constraints |
|---|---|---|
| **Drawer** (phone nav, `<dialog>`) | Enter: `translate: -100% 0` → 0, 300 ms emphasized-enter; backdrop opacity 0 → 1, 200 ms. Exit: → `-100%`, 200 ms exit; backdrop 200 ms | CSS only in a new `@utility sheet-start` (ours, overriding daisyUI's hard-coded 0.3 s ease-out, A17): `.modal-box { transition: translate var(--dur-slow) var(--ease-emphasized-enter), opacity …; }`, `[open] .modal-box { @starting-style { translate: -100% 0; } }`, `::backdrop` with its own `@starting-style`. Exit uses `transition-behavior: allow-discrete` on `display` and `overlay` (Chrome; Safari and Firefox close instantly, acceptable) |
| **Bottom sheet** (picker, phones) | Enter `translate: 0 100%` → 0, 300 ms emphasized-enter; exit 200 ms | Same as drawer, on `.sheet` |
| **Dialog** (≥48rem) | Enter: opacity 0 → 1 and `scale: 0.96` → 1, 200 ms enter; exit 100 ms | Extend today's `dialog.css` (already uses `@starting-style`) |
| **User menu** (`<details>`) | Panel opacity and `translate: 0 -4px` → 0, 100 ms | `details[open] > .user-menu-panel { @starting-style {…} }` |
| **Disclosure** | Chevron rotates 180°, 200 ms standard; content appears without a height animation (height auto isn't animatable outside Chrome) | CSS on `details[open] summary .icon` |
| **Toast** | Enter from `translate: 0 var(--distance-sm)` + opacity, 200 ms enter; exit opacity + `scale: 0.98`, 100 ms; others shift with no animation | `toast.js` already adds and removes nodes. Enter via `@starting-style` on `.toast > .alert`. Exit: `toast.js` adds a `leaving` class, waits for `transitionend` (fallback timer = token), then removes the node |
| **List row insert** (htmx swap after add or save) | New row: background accent-50 → transparent over 1 s (a highlight, colour only) + opacity 0 → 1 in 200 ms | htmx adds `htmx-added` to new content and removes it after settle (default `settleDelay` 20 ms): `.htmx-added { opacity: 0 }` and a transition on the row. The highlight is a CSS `@keyframes` on `.row-new`, a class the server sets on the inserted row |
| **List row remove** (remove line, approve from inbox) | Opacity → 0 and `scale: 0.98`, 200 ms exit, then the swap | `hx-swap="outerHTML swap:200ms"`, using htmx's `htmx-swapping` class on the old content: `.htmx-swapping { opacity: 0; scale: .98; transition: … var(--dur-base) var(--ease-exit) }`. The swap delay token is mirrored in a Go constant used in templ attributes (lint checks they match) |
| **Wizard step change** | Old step fades out 100 ms; new step fades in and `translate: var(--distance-sm) 0` → 0, 200 ms enter (direction reversed on Back). Stepper segment fills, colour 200 ms | htmx `transition:true` on the step forms' `hx-swap` (View Transitions where supported: Chrome 111, Safari 18, Firefox 144). CSS: `::view-transition-old(wizard)`, `::view-transition-new(wizard)` with our durations; `#wizard { view-transition-name: wizard }`. Direction from a `data-nav="back"` class on `<html>`, set by `app.js` on `htmx:beforeTransition`. No support: instant swap |
| **Page navigation** (full loads) | Cross-fade 150 ms, header stays put | Cross-document `@view-transition { navigation: auto; }` in CSS (Chrome 126, Safari 18.2). `.app-header { view-transition-name: header }` so it doesn't fade. Firefox: instant. **Not** `htmx.config.globalViewTransitions` (it would apply to every small swap, including inline validation) |
| **htmx swaps in general** | None by default; opt-in per pattern above | `htmx-added` / `htmx-settling` / `htmx-swapping` classes; no `hx-on`, no inline script |
| **Validation error (live, htmx)** | Message: opacity 0 → 1 and `translate: 0 -4px` → 0, 100 ms; border colour 100 ms. **No shake** | The error region is swapped (never the input, per lessons §9), and it gets `htmx-added` |
| **Save state** ("Saving…" → "Saved") | Label cross-fade 100 ms | `busy.js` already swaps labels; add the class transition |
| **Pressed** | `scale: var(--scale-press)` 100 ms standard; release 100 ms | CSS `:active` on `.btn`, card links |
| **Loading bar** | Indeterminate 2 px bar, `translate` loop 1.2 s linear, shown after 400 ms | `.htmx-request .loading-bar` with `transition-delay: 400ms` on opacity |
| **Skeleton** | daisyUI `skeleton` shimmer | Allowed component; reduced motion turns the shimmer off (static sunken block) |
| **Alpine-driven bits** (picker listbox, row grid) | Opacity + `translate` 100 ms | Alpine CSP build supports `x-transition` (in the vendored 3.17.5). Use the **class form** (`x-transition:enter="motion-enter"`, `enter-start`, `enter-end`, `leave…`), whose classes are our `@utility` rules. The helper form sets inline styles through `el.style.setProperty`; MDN says CSSOM style changes are not blocked by `style-src`, but `setProperty` isn't named explicitly, so we don't rely on it |

Choreography: one thing moves at a time. Container first, content second (a sheet lands, then its list appears with no extra animation). Exits are faster than entries. Related elements move in the same direction (wizard forward = from the end side). A user action always gets feedback within 100 ms (the pressed state), even if the response takes longer.

### 6.5 Reduced motion

`@media (prefers-reduced-motion: reduce)` (supported Chrome 74, Firefox 63, Safari 10.1 / iOS 10.3):

- All `--dur-*` tokens become `0.01ms` (as today), **except** opacity cross-fades, which stay at `--dur-fast` (100 ms). Apple's guidance is to replace motion along the x/y/z axes with fades, not to remove all feedback.
- `--distance-*` and `--scale-press` become 0 and 1, so anything still transitioning only fades.
- View transitions: `::view-transition-group(*) { animation: none }`.
- Skeleton shimmer and the loading-bar sweep stop (static bar, plus the words "Loading…").
- WCAG 2.3.3 (motion from interaction can be disabled) is then met. 2.2.2 (pause moving content) is met because nothing auto-moves for more than 5 s except the busy indicators while a request runs.

---

## 7. Enforcement: keeping the whole app on the system

| Check | Where | Fails on |
|---|---|---|
| `raw-colour` (exists) | `lint_test.go` | any hex/rgb/hsl/oklch outside `tokens.css`, `theme.css`, `brand` |
| `raw-duration`, `raw-easing` (new) | `lint_test.go` (CSS + templ) | `\d+m?s` or `cubic-bezier(` outside `tokens.css`; Tailwind `duration-*`/`ease-*` classes outside `internal/ui/css` |
| `transition-property` (new) | `lint_test.go` | `transition` or `transition-property` naming anything other than `opacity, translate, scale, rotate, transform, background-color, color, border-color, outline-color, box-shadow, display, overlay`; `transition: all` |
| `raw-shadow`, `raw-z` (new) | `lint_test.go` | `box-shadow:`/`z-index:` with literal values outside tokens |
| `hover-guard` (new) | `lint_test.go` | `:hover` rules not inside `@media (hover: hover)` in our CSS |
| `swap-delay-token` (new) | `lint_test.go` | `swap:\d+ms` in templ not equal to the Go constant |
| Icon names | compiler (typed `icons.Name`) | unknown icon |
| Icon registry | `icons_test.go` | duplicate glyph without an alias reason; SVG off-grid (gen checks); gallery missing an entry |
| DocType icon coverage | `internal/app` test | a registered DocType with no nav icon |
| Theme scope | `brand` test | generated theme CSS sets a non-themeable variable |
| Palette property test | `brand` test | for 10,000 random accents, any derived pair below its threshold, or the derivation not terminating |
| Sample-theme table | `brand` test | §3.8's four themes don't give the listed values |
| **Computed-colour sweep** (new e2e) | chromedp, every screen in the overflow sweep × 3 themes (default, saffron, maroon) | any rendered `color`, `background-color`, `border-color` or `outline-color` not in the active theme's token set (catches raw colours that slip past lint, for example from daisyUI defaults) |
| axe-core contrast | existing e2e gate, now × 3 themes | any WCAG AA contrast violation |
| Motion e2e (new) | chromedp: on the gallery motion page, `document.getAnimations()` durations ≤ 300 ms; with `Emulation.setEmulatedMedia(prefers-reduced-motion: reduce)` none has a transform keyframe | a slow or moving animation |
| States gallery (new) | `/_gallery/states`: nav item, list row, radio row, button × rest/hover/pressed/current/focus | computed styles differ from §3.4 |
| Visual snapshots (human review, not a gate) | `scripts/uishots/shots.py` gains `--login` and `--theme <dir>` options and a fixed screen list; output goes into the UI reviewer's brief for every DL task | reviewer judgement |
| UX docs | protocol §9 (CD-97) | a changed screen without an approved design |

Pixel-diff snapshots are **not** a gate: font rasterisation differs between machines and would fail for no reason. The computed-colour sweep, axe and the states gallery catch the same regressions deterministically.

---

## 8. Rollout (WP-DL, after PO, GRN and Expense ship)

Tiers per `docs/superpowers/plans/2026-10-25-execution-protocol.md` §3. UI work defaults to Light. Brand config loading is Standard. Logo file validation (uploads, SVG content) is **High** (upload trigger). The palette maths gets a property test and is Standard, because it touches no money.

| Task | What | Files (main) | Tier | Tests | UX doc | Effort (worker days) | Depends on |
|---|---|---|---|---|---|---|---|
| DL-0 | Spec v2 + CD entry + owner decisions recorded; UX docs for shell and states | `docs/specs/<date>-design-system-v2.md`, `docs/specs/contract-changes.md`, `docs/ux/shell-v2.md` | director | n/a | yes: shell, states, empty states | 1.5 | owner approval |
| DL-1 | Tokens v2: motion, icon sizes, radius presets, elevation names, state recipe, `--root-bg` fix (A7), Closed → neutral | `ui/css/tokens.css`, `theme.css`, `base.css`, `components/badge.css` | Light | `TestTheme_MatchesTokens` extended; contrast table rows | no (no layout change) | 1 | DL-0 |
| DL-2 | Lint rules §7 (six new rules with fixtures) | `ui/lint_test.go`, `ui/testdata/lint/*` | Standard | each rule's bad/good fixture | no | 1.5 | DL-1 |
| DL-3 | Icon registry: two-column `icons.txt`, typed names, gen validation, re-pin Lucide 1.65.0, `icons/custom/`, gallery icons page, DocType icon map in `internal/app` | `ui/icons/**`, `ui/gallery/*`, `internal/app/routes.go`, every `Icon(` call site | Standard | registry, gallery coverage, DocType coverage, SVG validation | no | 2.5 | DL-1 |
| DL-4 | Palette derivation (OKLCH), contrast checks, adjust/refuse policy, status-distance warning, sample-theme table | `platform/brand/colour.go`, `brand.go` | Standard | property test (10k accents), §3.8 table, each message | no | 1.5 | DL-0 |
| DL-5 | Runtime brand: `--brand` flag, startup load, defaults, `app brand check`, hashed `theme.<hash>.css` and logo serving, `brand.Active` replaces `brand_gen.go` constants, `buildassets` stops appending brand CSS, print theme file, `show_name`, `app_mark`, monogram, letterhead fallback chain (CD-209) | `platform/brand/**`, `cmd/app/main.go`, `ui/cmd/buildassets/build/build.go`, `ui/layout/page.templ`, `header.templ`, `kernel/print/web/print.go`, `deploy/app.service`, `docs/operations.md` | Standard | startup with absent, valid and invalid folders; hash changes with config; served headers; fallback chain; e2e page links the theme file | no | 3 | DL-4 |
| DL-6 | Logo validation: SVG refusal rules, PNG/JPEG decode limits, ratios, sizes | `platform/brand/logo.go` | **High** | test-writer first: hostile SVGs (script, foreignObject, external href, huge viewBox), oversize and corrupt images | no | 1.5 | DL-5 |
| DL-7 | Motion base: `motion.css` utilities, daisyUI modal and drawer overrides (A17), reduced-motion rules, htmx swap classes, toast exit, view-transition CSS, Alpine class-form utilities | `ui/css/components/motion.css`, `dialog.css`, `toast.css`, `ui/js/toast.js`, `ui/js/app.js` | Standard | motion e2e (§7), reduced-motion e2e, gallery motion page | no | 2 | DL-1, DL-2 |
| DL-8 | Shell: grouped nav, states, drawer header and user area, close-button scale, focus-on-open, header title on scroll, sidebar (A1–A6, A8) | `ui/layout/nav.templ`, `header.templ`, `page.templ`, `layout.css`, new `components/nav.css`, `ui/js/dialog.js` | Light (batch) | layout tests for groups, active state and focus; states gallery; overflow; target size | **yes** (DL-0) | 2 | DL-3, DL-5, DL-7 |
| DL-9 | Components pass 1: Button variants (danger), secondary border, badges with state icons, toggle rows, empty-state component, loading bar and skeleton | `ui/components/button.templ`, `badge.templ`, `empty.templ`, `field.templ`, CSS | Light (batch) | component tests; gallery; axe | yes (states) | 2 | DL-3, DL-7 |
| DL-10 | Components pass 2: Stepper single indicator (A10), full-bleed action bar (A11), ErrorSummary ring (A12), toast placement, dialog/sheet heads | `stepper`, `actionbar`, `errorsummary`, `toast`, `dialog` | Light (batch) | existing wizard tests unchanged; browser tests for the bar not covering fields | yes (wizard) | 2 | DL-9 |
| DL-11 | Screens: masters lists and forms (A9, A16), Roles (A13), Numbering empty state (A14), sign-in (A15) | `internal/masters/**/web`, `kernel/auth` screens, `ui/docscreen` | Light (batch of ≤5) | page tests; overflow; axe × 3 themes | **yes, one per screen family** | 3 | DL-10 |
| DL-12 | Screens: PO, GRN, Expense, approvals, document pages (motion for row remove and next request) | `internal/modules/**/web`, `ui/docscreen` | Light, except anything touching CD-95 grid saves (High) | page tests; browser tests | **yes, one per module** | 4 | DL-11 |
| DL-13 | Enforcement e2e: computed-colour sweep and axe × 3 themes, states gallery check, uishots `--login`/`--theme` | `internal/ui/uibrowser/*`, `scripts/uishots/shots.py`, `ui/testdata/themes/*` | Standard | the gates themselves, each with a planted failure | no | 2 | DL-5, DL-8 |
| DL-14 (optional) | Accent suggestion from the logo: `app brand suggest <logo>` prints three candidates (dominant high-chroma colours, k-means in OKLab over opaque pixels) already run through DL-4's check; the admin copies one into `brand.json`. PNG/JPEG only (stdlib `image`); for SVG it reads explicit `fill`/`stroke` hex values | `platform/brand/suggest.go`, `cmd/app/main.go` | Light | fixtures: three logos give stable candidates | no | 1 | DL-4 |
| DL-15 | Lessons and CLAUDE.md lines (motion and icon rules that bite), PROGRESS | `docs/engineering-lessons.md` §9, `CLAUDE.md` | director | n/a | n/a | 0.5 | all |

**Order and parallelism**: DL-0 → {DL-1, DL-4} in parallel → {DL-2, DL-3, DL-5} in parallel → {DL-6, DL-7} → DL-8 and DL-9 in parallel → DL-10 → DL-11 → DL-12, with DL-13 any time after DL-8, and DL-14 any time after DL-4. Lanes that touch `internal/ui/css` regenerate the hashed CSS: one CSS lane at a time, or rebase and `make generate` per lessons.

**Effort**: about 30 worker days in total (DL-14 adds 1). With 2–3 parallel lanes, that is about 2.5–3 weeks of calendar time including reviews.

**Risks**

| Risk | Mitigation |
|---|---|
| Runtime theme breaks the "CSS bundle is generated and committed" model | The bundle stays committed and brand-free; only the small theme file is runtime. `TestAssets_UpToDate` then gets simpler |
| A company's colour is adjusted and they complain the buttons "aren't our colour" | `app brand check` shows the adjustment before go-live; the logo keeps the true colour; the message names the colour that passes |
| View Transitions behave differently in Safari 18 vs Chrome | Used only for wizard steps and page cross-fade, both safe to drop; browser tests run without them (Chromium flag) as well as with them |
| Icon re-pin changes glyph shapes slightly | Gallery screenshots before and after in the DL-3 review |
| Typed icon names touch many call sites | Mechanical; the compiler finds every one |
| Renamed nav order surprises current users | Few users today; the order follows observed frequency; owner reviews in DL-0 |
| Tailwind picks up words from new comments ("menu", "toggle") | Lessons §9 rule; reviewers check bundle size deltas |
| daisyUI upgrades change their built-in transitions again | Our overrides are `@utility` rules (they win, CD-98), and the motion e2e catches slow ones |

**What stays unchanged until WP-DL starts**: everything. Module work continues on today's components and tokens. To keep the later migration cheap, the director's briefs for PO, GRN and Expense should only add (no code changes now): use `components.*` for every button, badge and empty state; pick icons only from today's `icons.txt`; add no colours, durations or z-index values; and note each new DocType's intended icon from §4.6 in its UX doc.

---

## 9. Owner decisions (each with a recommendation)

| # | Decision | Recommendation |
|---|---|---|
| D1 | Theme config read at startup from `<data>/brand` (restart to change), not edited in the app | **Yes.** A file is auditable, deploys with the instance, and needs no new rights or screens. An in-app theme editor is a later add-on |
| D2 | Failing accent: adjust automatically with a warning (default), or refuse to start | **Adjust**, with `"strict": true` available for anyone who wants a hard stop |
| D3 | Letterhead logo source | **Company logo uploaded in the app (CD-209) first**, then the deployment's `document_logo`, then `app_logo`, then the name as text |
| D4 | Closed state moves from blue tint to neutral | **Yes**, so no company accent can make a state look like another (P3) |
| D5 | Filled icons for the selected nav item | **No.** Selection shows through the row (tint, bar, weight). It avoids drawing about 30 filled twins |
| D6 | Desktop sidebar collapse to icons only (spec §6) | **Drop it.** Icon-only nav needs memorisation and tooltips; the 16rem sidebar fits from 64rem |
| D7 | List first-render stagger (30 ms per row, max 5) | **No** for v1. It is decoration on screens people open dozens of times a day |
| D8 | Radius preset as a theme option | **Yes, three presets only**, no free values |
| D9 | Icon set | **Stay on Lucide** (ISC, no new dependency), re-pinned to 1.65.0. Phosphor or Material Symbols only if the owner wants filled variants app-wide |
| D10 | Optional accent-from-logo suggestion (DL-14) | **Build it last, if time allows** (1 day, stdlib only). It helps non-designers pick a passing accent |

---

## 10. Sources

- Lucide icon design guide: https://lucide.dev/contribute/icon-design-guide · naming: https://lucide.dev/contribute/icons/naming-conventions · licence: https://lucide.dev/license · `lucide-static` 1.65.0, ISC (npm registry, checked 31-10-2026)
- Phosphor: https://github.com/phosphor-icons/homepage
- Material Symbols: https://developers.google.com/fonts/docs/material_symbols
- Apple HIG icons: https://developer.apple.com/design/human-interface-guidelines/icons · SF Symbols: https://developer.apple.com/design/human-interface-guidelines/sf-symbols · Motion: https://developer.apple.com/design/human-interface-guidelines/motion · Accessibility: https://developer.apple.com/design/human-interface-guidelines/accessibility
- Carbon icons: https://carbondesignsystem.com/elements/icons/usage/ · motion: https://carbondesignsystem.com/elements/motion/overview/
- Material 3 motion tokens: https://raw.githubusercontent.com/material-components/material-web/main/tokens/versions/v0_192/_md-sys-motion.scss
- View Transitions support: https://bcd.developer.mozilla.org/bcd/api/v0/current/api.Document.startViewTransition.json · https://bcd.developer.mozilla.org/bcd/api/v0/current/css.at-rules.view-transition.json · https://developer.chrome.com/docs/web-platform/view-transitions
- `@starting-style`: https://developer.mozilla.org/en-US/docs/Web/CSS/@starting-style · https://bcd.developer.mozilla.org/bcd/api/v0/current/css.at-rules.starting-style.json
- CSP `style-src` and CSSOM: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Security-Policy/style-src
- htmx: https://htmx.org/docs/ (and the vendored `htmx.min.js` 2.0.11 defaults: `defaultSettleDelay` 20, `globalViewTransitions` false, `includeIndicatorStyles` true)
- Alpine CSP build: https://alpinejs.dev/advanced/csp · transitions: https://alpinejs.dev/directives/transition
- daisyUI themes: https://daisyui.com/docs/themes/ · drawer: https://daisyui.com/components/drawer/ · menu: https://daisyui.com/components/menu/
- WCAG 2.2: https://www.w3.org/TR/WCAG22/ · 2.3.3: https://www.w3.org/WAI/WCAG22/Understanding/animation-from-interactions.html · 2.5.8: https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html · 2.5.5: https://www.w3.org/WAI/WCAG22/Understanding/target-size-enhanced.html · 2.4.13: https://www.w3.org/WAI/WCAG22/Understanding/focus-appearance.html
- Repo: `docs/specs/2026-10-24-design-system.md`, `docs/research/ui-library.md`, `internal/ui/css/*`, `internal/ui/icons/*`, `internal/ui/layout/*`, `internal/app/routes.go:331–379`, `internal/platform/brand/*`, `internal/masters/company/web/logo.go`, `internal/kernel/files/read.go:95–110`, `internal/platform/web/middleware.go:13`, `deploy/app.service`
