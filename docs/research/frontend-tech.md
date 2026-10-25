# Front-end technology and design-token foundation

Status: research recommendation, 24 October 2026. Scope: CSS architecture, tokens, the technical approach for components, and verification for the Go + templ + htmx single-binary app. UX patterns are covered in a separate research file.

Requirements this answers: responsive; **light mode only**; **mobile-first**; **no horizontal scrolling anywhere**; complex forms as wizards with a clear progress bar; open-source only; latest stable versions; prefer well-maintained libraries; no Node.js at build or run time; CSP from `docs/superpowers/plans/2026-10-24-wp0-foundation.md` Task 4: `default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; frame-ancestors 'none'`.

How things were checked: versions came from the npm registry (`registry.npmjs.org/<pkg>`), the Go module proxy (`proxy.golang.org/<module>/@latest`) and the GitHub API (`api.github.com/repos/<repo>` and `/releases/latest`), all queried on 24 Oct 2026. Behaviour claims about htmx, Alpine, templ and chromedp were checked against the **source code of the exact versions**, and line numbers are cited. The fonts were subset and measured locally with fontTools 4.67.0. The contrast ratios were computed with the WCAG 2.x relative-luminance formula. The overflow test and the esbuild CSS build were **run** (see §6 and §1).

---

## 0. Recommendations summary

| Concern | Decision | Version (24 Oct 2026) | Licence | Why |
|---|---|---|---|---|
| CSS approach | Hand-written CSS: custom-property tokens, `@layer` cascade layers, container queries, a small closed set of component classes | n/a | n/a | No external tool, smallest HTML over htmx swaps, design system fully ours, works under CSP with no inline styles |
| CSS bundling and minifying | `esbuild` **Go API** (`github.com/evanw/esbuild/pkg/api`) run by `go generate`; output embedded with `embed` and content-hashed | v0.28.2 (Go proxy, 8 Aug 2026) | MIT | Pure Go, no Node; resolves `@import … layer(x)`, lowers nesting, minifies (verified) |
| Runner-up CSS approach | Tailwind CSS v4 standalone CLI | v4.3.3 (16 Jul 2026) | MIT | Valid if the team prefers utility classes; costs an 80–112 MB per-platform binary in CI and dev |
| Not chosen | Open Props, Pico CSS | 1.7.23 / 2.1.1 | MIT / MIT | See §1 |
| htmx | Stay on **2.0.11** with a strict-CSP config (meta tag, no eval) | 2.0.11 (npm `latest`, 22 Sep 2026) | 0BSD | htmx 4.0.0 was released 28 Aug 2026, but the maintainers deliberately keep npm `latest` on 2.x. Plan a later migration (§2) |
| Alpine.js | **`@alpinejs/csp` build is required** (the standard build uses `new Function`) | 3.17.5 (20 Oct 2026) | MIT | `script-src 'self'` has no `'unsafe-eval'` (§2) |
| Own JS | One small `app.js` (event delegation, < 3 KB) for toasts, dialog open/close, upload progress, Alpine component registration | n/a | n/a | Keeps behaviour out of attributes, so it is CSP-safe |
| Icons | **Lucide**, embedded as inline SVG templ components generated from a curated list | lucide-static 1.61.0 (21 Oct 2026) | ISC (Feather-derived icons MIT) | Most active set, 2,171 icons including `indian-rupee`, consistent 24 px stroke style |
| Font (UI) | **Inter** variable, self-hosted, subset to Latin + ₹ with `tnum`/`zero`/`case`; system-ui fallback | Inter 4.1 (rsms/inter release, Nov 2024) | OFL-1.1 | **41.2 KB** woff2 for all weights 100–900; has ₹ (U+20B9) and tabular figures |
| Font (Devanagari names) | **Noto Sans Devanagari** variable, subset, loaded only via `unicode-range` when Devanagari text is present | 2.007 | OFL-1.1 | 114 KB, downloaded only when needed |
| Tokens | Light-only palette (§9), 4 px spacing grid, stepped type scale with a ≥48rem bump, 44 px minimum touch target | n/a | n/a | Every text pair ≥ 4.5:1, every UI and focus pair ≥ 3:1 (computed) |
| Overflow guard | Global CSS rules (§6.1) + a Go lint test for forbidden patterns + a **chromedp e2e test at 320/360/390/768/1024/1440** | chromedp v0.21.0 (11 Oct 2026) | MIT | The test was run: it catches fixed widths, spilling text, scroll containers and the `overflow-x:hidden` cheat |
| Wizard | Server-held draft saved each step; URL per step through the `HX-Push-Url` **response header**; `historyCacheSize: 0` + `refreshOnHistoryMiss: true`; `<ol>` stepper with `aria-current="step"` and "Step n of N" text; `autofocus` on the step heading | n/a | n/a | Back and forward always show server truth; focus moves on every step change (§7) |
| CSP change (optional, recommended) | Target `style-src 'self'` (drop `'unsafe-inline'`) once nothing emits a `style` attribute | n/a | n/a | Achievable with the rules in §2.4; keep the plan's policy until the e2e CSP check passes |

---

## 1. CSS approach

### Options compared

| Criterion | Hand-written CSS + tokens (recommended) | Tailwind v4 standalone CLI | Open Props | Pico CSS (classless) |
|---|---|---|---|---|
| Node needed | No. esbuild Go API for bundling and minifying | No. Self-contained executable | No (copy the CSS file), but it is just a token library | No |
| Tooling footprint | One Go module dependency (esbuild, MIT) | 79.8 MB (macOS arm64) to 112.5 MB (Windows) per platform, from the v4.3.3 release assets; pin it and check its sha256 in CI | None | None |
| Output size | Only what we write: about 15–25 KB min, about 5–7 KB gzip (estimate for the component set) | Small (only the classes it detects) | Large unless trimmed: it ships hundreds of props | ~80 KB min full build |
| HTML size and htmx swaps | Short semantic classes (`class="btn btn--primary"`) | Long class lists repeated in every swapped fragment | n/a | Minimal classes |
| Consistency enforcement | Tokens + a Go lint test that forbids raw colours and px values outside `tokens.css` (§6.3) | Strong: the theme is the only vocabulary | Weak (tokens only) | Weak once you customise it |
| Custom design system | Full control | Good (`@theme` variables) | Good, as a token source | Poor: opinionated look; overriding it fights the framework |
| templ fit | Classes are plain strings | Class detection treats `.templ` as plain text. Dynamic names must be whole strings ("Tailwind can't interpret string concatenation or interpolation") | n/a | n/a |
| CSP | No inline styles needed | Same | Same | Same |
| AI-assisted maintenance | Good: plain CSS, a small closed component list | Very good: models know Tailwind well | OK | OK |
| Maintenance activity | n/a | Very active: pushed 19 Oct 2026, 99 open issues | npm 1.7.23 (Jan 2026); v2 still in beta (`2.0.0-beta.5`) | Last release v2.1.1 on 15 Mar 2025 (19 months ago), 125 open issues |

**Decision: hand-written CSS with tokens.** All UI comes from a closed set of templ components in `internal/ui/`, so markup and styling live in one place. A utility framework mainly pays off when many hands write ad-hoc markup, which this project forbids. We get semantic class names, small htmx fragments, no 100 MB tool and full control of the design system. Tailwind v4 standalone is a sound runner-up. Choose it only if the team strongly prefers utilities. If you do, use `@import "tailwindcss" source(none); @source "../internal";`, map every variant to a complete class string in Go, and run the CLI from `go generate`.

### Architecture

Use these modern features, all **Baseline widely available** according to MDN browser-compat-data and web-features (queried 24 Oct 2026):

| Feature | Chrome / Safari / Firefox | Baseline |
|---|---|---|
| Cascade layers `@layer` | 99 / 15.4 / 97 | high since 2022-03 |
| Container queries | 105 / 16 / 110 | high since 2023-02 |
| `:has()` | 105 / 15.4 / 121 | high since 2023-12 |
| CSS nesting | 120 / 17.2 / 117 | high since 2023-12 (esbuild lowers it anyway) |
| Dynamic viewport units `dvh` | 108 / 15.4 / 101 | high since 2022-12 |
| `inert` | | high since 2023-04 |

Layer order and files (all in `internal/ui/css/`, embedded):

```css
/* app.css: the only entry point */
@layer reset, tokens, base, layout, components, utilities;
@import "./reset.css"      layer(reset);
@import "./tokens.css"     layer(tokens);   /* §9: the only file allowed raw colours and px values */
@import "./base.css"       layer(base);     /* element defaults, anti-overflow rules (§6.1) */
@import "./layout.css"     layer(layout);   /* app shell, stack, cluster, sidebar, sticky action bar */
@import "./components.css" layer(components); /* .btn, .field, .card, .list, .stepper, .sheet, .toast … */
@import "./utilities.css"  layer(utilities); /* a dozen helpers: .visually-hidden, .num, .truncate */
```

Build. This was verified: esbuild v0.28.2 with `Bundle`, `MinifyWhitespace`, `MinifySyntax` and engines Chrome 111 / Safari 16.4 / Firefox 115 produced `@layer reset,…;@layer tokens{:root{…}}@layer components{.btn{…}.btn:hover{…}…}`. It wrapped the import in its layer and lowered nesting.

```go
//go:generate go run ./internal/ui/cmd/buildcss   // writes internal/ui/static/app.<sha256-8>.css + a Go constant with the name
r := api.Build(api.BuildOptions{
    EntryPoints: []string{"internal/ui/css/app.css"},
    Bundle: true, MinifyWhitespace: true, MinifySyntax: true,
    Engines: []api.Engine{{Name: api.EngineChrome, Version: "111"}, {Name: api.EngineSafari, Version: "16.4"}, {Name: api.EngineFirefox, Version: "115"}},
    Write: false, Outdir: "out",
})
```

Serve hashed static files with `Cache-Control: public, max-age=31536000, immutable`. Use the same mechanism for `app.js`, `htmx.min.js`, the Alpine CSP build and the fonts.

Sources: https://tailwindcss.com/docs/detecting-classes-in-source-files ; https://tailwindcss.com/blog/standalone-cli ; https://github.com/tailwindlabs/tailwindcss/releases/tag/v4.3.3 ; https://github.com/argyleink/open-props ; https://github.com/picocss/pico ; https://pkg.go.dev/github.com/evanw/esbuild/pkg/api ; https://github.com/mdn/browser-compat-data

---

## 2. Scripts under a strict CSP

### 2.1 Alpine.js: the CSP build is required

- Alpine's docs say the standard build "needs to use utilities that violate the 'unsafe-eval' Content Security Policy … it uses Function declarations". Verified in source: `alpinejs@3.17.5/dist/cdn.min.js` contains `new Function` (1 occurrence). `@alpinejs/csp@3.17.5/dist/cdn.min.js` contains **0**.
- The CSP build supports literals, basic operators, ternaries, assignments (`count++`), `x-model` on nested properties and method calls. It does **not** support arrow functions, template literals, spread, destructuring, property assignment such as `user.name = 'x'`, globals (`window`, `document`, `Math`, `JSON`, `console`) or `x-html`.
- Rule: every non-trivial component is registered with `Alpine.data('name', () => ({…}))` in our `app.js` inside `document.addEventListener('alpine:init', …)`, and markup uses `x-data="combobox"`. This is more testable anyway.
- Sizes (measured): CSP build 72.2 KB raw / **23.9 KB gzip**; standard build 55.9 / 19.9 KB.
- Load order: `<script src="/static/app.<hash>.js" defer>`, then `<script src="/static/alpine-csp.<hash>.js" defer>`. Deferred scripts run in document order, so `alpine:init` listeners exist before Alpine starts.
- Pass server data to Alpine with `templ.JSONScript(id, data)`. It writes a `<script type="application/json">` data block (templ `jsonscript.go:57`), which is not executed and not blocked by CSP. Read it with `JSON.parse(el.textContent)` inside `Alpine.data`, because the CSP build cannot call `JSON` inline.

Source: https://alpinejs.dev/advanced/csp ; https://www.npmjs.com/package/@alpinejs/csp

### 2.2 htmx 2.0.11 under `script-src 'self'`

These were verified in `htmx.org@2.0.11/dist/htmx.js`:

- `maybeEval()` (line 4022) gates every eval path on `htmx.config.allowEval`. The `hx-on:*` handlers are compiled with `new Function('event', code)` (line 2958). Trigger filters `[cond]` (line 2227) and `js:` in `hx-vals`/`hx-headers` (line 3997) also use eval. **Under our CSP all of these would throw anyway, so set `allowEval:false` and never use them.** Put behaviour in `app.js` with `addEventListener` (event delegation on `document`).
- `insertIndicatorStyles()` (line 5135) injects a `<style>` element into `<head>`, which needs `style-src 'unsafe-inline'` or a nonce. Set `includeIndicatorStyles:false` and put the two indicator rules in `components.css`.
- `allowScriptTags:false`, so scripts in swapped HTML are never executed.
- Configure it with `<meta name="htmx-config" content='{…}'>`. That is JSON, not script, so it is CSP-safe.

Recommended config, which also serves the wizard (§7):

```html
<meta name="htmx-config" content='{
  "allowEval": false,
  "allowScriptTags": false,
  "includeIndicatorStyles": false,
  "selfRequestsOnly": true,
  "historyCacheSize": 0,
  "refreshOnHistoryMiss": true,
  "defaultFocusScroll": true,
  "responseHandling": [
    {"code":"204","swap":false},
    {"code":"[23]..","swap":true},
    {"code":"422","swap":true,"error":false},
    {"code":"[45]..","swap":false,"error":true}
  ]
}'>
```

- `historyCacheSize: 0` keeps no snapshots of business data (amounts, names) in `localStorage`. The htmx docs list this and `hx-history="false"` for sensitive pages.
- `refreshOnHistoryMiss: true` makes back and forward do a normal full-page GET of the step URL. This sidesteps `historyRestoreAsHxRequest` (default `true`, line 281), which would otherwise send `HX-Request: true` on restore and make a partial-returning handler send a fragment for a full page.
- Server rule: handlers that return fragments for `HX-Request` must send `Vary: HX-Request`.

Sources: https://htmx.org/docs/#security ; https://htmx.org/docs/#config ; https://htmx.org/reference/#config

### 2.3 htmx 4: released, but not yet `latest`

GitHub release v4.0.0 was published 2026-08-28 and is not a prerelease. htmx.org says "htmx 4.0 has been released" and that it is not marked `latest` on npm "so that people using the 2.x line are not accidentally upgraded". npm dist-tags are `latest: 2.0.11`, `next: 4.0.0`. The migration guide lists these breaking changes: no `localStorage` history cache, explicit attribute inheritance (`:inherited`), `allowEval`/`allowScriptTags` removed in favour of an `hx-csp` core extension, renamed events, `fetch()` instead of XHR (so the `htmx:xhr:progress` upload events change), and **4xx/5xx responses are swapped by default**.

**Recommendation: keep 2.0.11** for v1, as the spec pins it and npm `latest` agrees. The config above already behaves like htmx 4 (no history cache, 422 swaps), which makes a later migration small. Revisit at the start of each work package. Under the "latest stable" rule, an argument for 4.0 exists; this is the main version risk to flag to the spec owner.

Sources: https://github.com/bigskysoftware/htmx/releases/tag/v4.0.0 ; https://htmx.org/ ; https://four.htmx.org/migration-guide-htmx-4/

### 2.4 templ and CSP; tightening `style-src`

- **Do not use templ `css` components or `script` templates.** `RenderCSSItems` writes `<style type="text/css">` inline (`runtime.go:359`), and `script` templates and `templ.JSFuncCall` emit inline JS that `script-src 'self'` blocks. All CSS goes in `app.css`. All JS goes in `app.js`.
- `style-src 'unsafe-inline'` is only needed for `style="…"` attributes and `<style>` elements. Setting styles through the CSSOM (`el.style.display = 'none'`, which is what Alpine `x-show` does) is not governed by `style-src`. With the rules here (no `style` attributes, which the lint test enforces; htmx indicator styles off; the progress bar uses `<progress>` or classes, never `style="width:40%"`), the app can move to **`style-src 'self'`**. Prove it with the e2e CSP check (§6.4) before changing the header.

---

## 3. Icons

| Set | Latest | Licence | Last release | Activity | Notes |
|---|---|---|---|---|---|
| **Lucide** (`lucide-static`) | 1.61.0 | ISC; Feather-derived icons MIT (LICENSE file) | 21 Oct 2026 | Pushed 23 Oct 2026; weekly releases | 2,171 SVGs counted in the package; has `indian-rupee`, `camera`; 24×24, `stroke="currentColor"`, stroke-width 2 |
| Tabler Icons (`@tabler/icons`) | 3.49.0 | MIT | 5 Oct 2026 | Active, 87 open issues | Very large set; same stroke style; good alternative |
| Heroicons | 2.2.0 | MIT | 18 Nov 2024 | 23 months without a release | Small set |
| Phosphor (`@phosphor-icons/core`) | 2.1.1 (npm, Mar 2024) | MIT | GitHub release v2.0.8 (Feb 2024) | Low activity | Six weights |

**Decision: Lucide.** Delivery: vendor only the icons we use. A small generator (`go generate`) reads `internal/ui/icons/icons.txt` (one name per line), copies each SVG from a pinned `lucide-static@1.61.0` checkout or tarball, and emits `icons_gen.go` with one function per icon that returns a `templ.Component`. That component writes `<svg class="icon" aria-hidden="true" focusable="false" …>`. Inline SVG needs no extra request, inherits `currentColor`, and is CSP-safe (no `style`). Keep the `@license lucide-static v1.61.0 - ISC` notice in the generated file and add the licence to `THIRD_PARTY_LICENSES`. Icon-only buttons must have a visible label or an `aria-label`.

An external sprite (`<svg><use href="/static/icons.<hash>.svg#camera">`) also works under `default-src 'self'` and suits very long lists that repeat icons. Inline is simpler and is the default.

Sources: https://lucide.dev ; https://github.com/lucide-icons/lucide/blob/main/LICENSE ; https://www.npmjs.com/package/lucide-static ; https://github.com/tabler/tabler-icons ; https://github.com/tailwindlabs/heroicons ; https://github.com/phosphor-icons/core

---

## 4. Font

These were measured locally from the actual font files with fontTools 4.67.0. "₹" means the font maps U+20B9. "tnum" means the GSUB table has the tabular-figures feature.

| Font file | Size (woff2) | ₹ | tnum | Devanagari |
|---|---|---|---|---|
| **Inter 4.x variable, our subset** (measured on the google/fonts build, "Version 4.001"; the rsms v4.1 file should be within a few KB) (Latin-1 + General Punctuation + € ₹ ™ arrows − ∕; features `kern liga calt tnum case zero`; `opsz` pinned at 14, `wght` 100–900 kept) | **41.2 KB** | yes | yes | no |
| Fontsource `@fontsource-variable/inter` 5.3.0 `latin` | 48.3 KB | **no** (₹ is in the `latin-ext` file, 85.1 KB) | yes | no |
| Fontsource Noto Sans `latin` / `latin-ext` | 35.8 / 168.0 KB | no / yes | yes / no | no |
| Fontsource IBM Plex Sans `latin` / `latin-ext` | 45.7 / 31.0 KB | no / yes | **no tnum in the subset** | no |
| **Noto Sans Devanagari variable, our subset** (U+0900–097F, Vedic, ZWJ/ZWNJ, ₹, ◌; all features; `wdth` pinned 100) | **114.3 KB** | yes | n/a | yes |
| System font stack | 0 KB | yes on current iOS (SF), Android (Roboto), Windows (Segoe UI), macOS | yes (`font-variant-numeric: tabular-nums`) | system fallback (Noto / Kohinoor / Nirmala UI) |

Note on Fontsource: its unicode ranges place ₹ in the `latin-ext` subset (`U+20AD-20C0`), so the commonly copied "latin" file alone **renders ₹ in a fallback font**. That is a real gotcha for an INR app.

**Decision.**
1. **UI font: Inter, self-hosted, our own subset (41.2 KB, one file for all weights).** Get the source from the official `rsms/inter` v4.1 release (`InterVariable.ttf`, OFL-1.1). Subsetting is a one-off step done with `pyftsubset` (fontTools, MIT) and the woff2 output is committed. That is a dev-time tool, not a build dependency. Why Inter over the system stack: identical metrics on every phone and on the A4 print view (the spec uses browser print for PDF), a slashed-zero option (`zero`) for codes such as GSTIN/PAN/PO numbers, and tabular figures. Fallback stack: `Inter, system-ui, -apple-system, "Segoe UI", Roboto, "Noto Sans", sans-serif`.
2. **Money and quantity columns:** `.num { font-variant-numeric: tabular-nums; text-align: end; }`. Codes: `.code { font-feature-settings: "zero" 1, "tnum" 1; overflow-wrap: anywhere; }`.
3. **Devanagari names:** declare Noto Sans Devanagari with `unicode-range: U+0900-097F, U+1CD0-1CF9, U+200C-200D, U+25CC, U+A8E0-A8FF`. The browser downloads the 114 KB file **only on pages that contain Devanagari text**. ₹ is deliberately left out of its range so ₹ always comes from Inter.
4. `font-display: swap`. Preload Inter: `<link rel="preload" as="font" type="font/woff2" crossorigin href="/static/inter.<hash>.woff2">`.

```css
@font-face { font-family: Inter; src: url("/static/inter-var-sub.woff2") format("woff2"); font-weight: 100 900; font-style: normal; font-display: swap; }
@font-face { font-family: "Noto Sans Devanagari"; src: url("/static/noto-sans-devanagari-sub.woff2") format("woff2"); font-weight: 100 900; font-display: swap;
  unicode-range: U+0900-097F, U+1CD0-1CF9, U+200C-200D, U+25CC, U+A8E0-A8FF; }
```

Sources: https://github.com/rsms/inter/releases/tag/v4.1 ; https://github.com/google/fonts/tree/main/ofl/inter ; https://github.com/google/fonts/tree/main/ofl/notosansdevanagari ; https://fontsource.org/fonts/inter ; https://fonttools.readthedocs.io/en/latest/subset/

---

## 5. Design tokens: rationale

The complete token file is in §9.

- **Colour.** Light mode only: no `prefers-color-scheme` branch, and `color-scheme: light` on `:root` so form controls and scrollbars stay light even when the OS is dark. Three surfaces: `canvas` #F5F6F8 (page), `surface` #FFFFFF (cards and inputs), `sunken` #EDEFF2 (wells and table headers). **All text and UI colours were checked against all three surfaces** (table in §9). Status colours have a 700 shade (text, solid buttons), a 600 shade (icons and borders, ≥ 3:1) and a 50 shade (tinted backgrounds) with 800 text. Never use colour alone: pair status with an icon and a word.
- **Focus indicator.** 2 px solid `--color-focus` (#1F4FD1) outline with a 2 px offset, so the ring sits on the surface (6.78:1 on white, 5.89:1 on sunken). This meets WCAG 2.2 SC 1.4.11 (3:1) and the size and contrast intent of 2.4.13. SC 2.4.11 *Focus Not Obscured* means the sticky header and action bar must not cover the focused field. Use `scroll-padding-block: var(--header-h) var(--actionbar-h)` on `html`.
- **Type.** Body is 16 px (1rem). It is never smaller inside inputs, because iOS Safari zooms on focus below 16 px. Stepped scale (`--text-xs` 13 px … `--text-3xl` 30 px). Headings step up once at `min-width: 48rem`. Stepped beats fluid `clamp()` here: values are predictable for testing and match print. Line heights are 1.5 for body and 1.25 for headings. Font-size tokens are in `rem` so user font scaling works.
- **Spacing.** 4 px base: `--space-1` 4 px … `--space-16` 64 px. Page gutter 16 px on mobile, 24 px at ≥ 48rem.
- **Touch targets.** `--target-min: 2.75rem` (44 px) for every interactive element, and 48 px for primary actions in the mobile action bar. WCAG 2.2 SC 2.5.8 AA only asks for 24×24. 44 px follows Apple's HIG and approximates Material's 48 dp.
- **Breakpoints.** Mobile-first `min-width` only: 30rem (480), 48rem (768), 64rem (1024), 80rem (1280). Custom properties cannot be used inside `@media`, so these are constants documented in `tokens.css`. Prefer **container queries** for components (a card list switches to rows when *its container* is ≥ 48rem, not the viewport).
- **Elevation.** Three subtle shadows plus the bottom-sheet shadow. Borders do most of the separation, and shadows are never the only cue.
- **Z-index.** Named scale: base 0, raised 1, sticky 100, header 200, dropdown 300, overlay 400, toast 500. Native `<dialog>.showModal()` uses the browser top layer and needs no z-index.
- **Motion.** 120/200/300 ms with standard easings. Under `prefers-reduced-motion: reduce`, durations drop to near zero and smooth scrolling is turned off.
- **Safe areas.** `viewport-fit=cover` plus `env(safe-area-inset-*)` padding on the header, the action bar and sheets. The viewport meta is `width=device-width, initial-scale=1, viewport-fit=cover, interactive-widget=resizes-content`. That last value lets the on-screen keyboard shrink the layout viewport in Chrome Android, so the sticky action bar stays above the keyboard. Safari ignores it and still works acceptably. **Never** set `maximum-scale` or `user-scalable=no`.

Sources: https://www.w3.org/TR/WCAG22/ (1.4.3, 1.4.11, 2.4.11, 2.4.13, 2.5.8) ; https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum ; https://developer.apple.com/design/human-interface-guidelines/accessibility ; https://m3.material.io/foundations/designing/structure

---

## 6. No horizontal overflow

### 6.1 Global CSS rules (in `base.css` and `layout.css`)

```css
*, *::before, *::after { box-sizing: border-box; }
html { -webkit-text-size-adjust: 100%; text-size-adjust: 100%; }
body { margin: 0; overflow-wrap: anywhere; }            /* long GSTINs, emails, PO numbers, URLs break instead of overflowing */
:where(img, svg, video, canvas, iframe) { max-inline-size: 100%; block-size: auto; }
:where(input, select, textarea, button) { font: inherit; max-inline-size: 100%; min-inline-size: 0; }
:where(pre, code) { white-space: pre-wrap; overflow-wrap: anywhere; }
/* Flex and grid children default to min-width:auto, which refuses to shrink below their content. */
:where(.stack, .cluster, .grid, .row, .card, .field) > * { min-inline-size: 0; }
.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 16rem), 1fr)); }  /* min(100%, …) prevents overflow at 320 px */
.truncate { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }  /* the only allowed nowrap, single-line labels only */
```

Rules for authors, enforced by lint (§6.3): no fixed `width`/`min-width` in length units except via `min(100%, X)`; no `100vw` (it includes the scrollbar); no `overflow-x: auto|scroll` and no `overflow-x: hidden` on `html`/`body` (that hides the bug instead of fixing it); no `<table>` outside the `ui.DataTable` component; no `white-space: nowrap` except `.truncate`; no `style=` attributes.

### 6.2 Data lists without tables on small screens

- **One markup, two layouts.** Each record is an `<li>` in `<ul class="list" role="list">`. Inside it, a heading link (number + party) and a `<dl class="kv">` of label/value pairs (date, status badge, amount). On phones this renders as cards.
- With `container-type: inline-size` on the list wrapper, `@container (min-width: 48rem)` switches each `<li>` to `display: grid; grid-template-columns: var(--cols)`, with column templates such as `minmax(0,1.2fr) minmax(0,2fr) 7rem 8rem`. A header row with `aria-hidden="true"` shows column titles. The `<dt>` elements become `.visually-hidden` in row mode, so screen readers still hear "Amount ₹1,20,000". Every track is `minmax(0, …)` or `min(…)`, so nothing can force overflow.
- **Column priority.** Low-priority fields (created by, branch) get `data-priority="low"` and are hidden below 64rem. They remain on the detail page.
- Amounts are right-aligned with `.num`. Long codes use `.code` (breaks anywhere).
- **Line items (PO/GRN lines, up to 500).** On phones, each line is a card with *Edit* opening a bottom sheet. At ≥ 64rem it is an editable grid (the Alpine keyboard component that the spec reserves Alpine for). Even then, the grid fits the width: columns shrink and the description wraps. There is no sideways scroll.
- The print view (A4) is separate and may use a real `<table>`. It is excluded from the overflow test through its own route prefix.

### 6.3 Lint for forbidden patterns (a Go test, no Node)

Stylelint would need Node. Use a table-driven Go test, `internal/ui/lint_test.go`, that walks `**/*.templ` and `internal/ui/css/*.css` and fails with `file:line` on any of the following.

| Pattern (regexp, case-insensitive) | Files | Why |
|---|---|---|
| `\bstyle\s*=` | `.templ` | No inline styles (CSP, consistency) |
| `<script(?![^>]*\bsrc=)(?![^>]*type="application/json")` | `.templ` | Inline JS is blocked by CSP |
| `\bhx-on[:-]` , `js:` inside `hx-vals`/`hx-headers`, `hx-trigger="[^"]*\[` | `.templ` | Eval features are disabled |
| `^\s*(css\|script)\s+\w+\(` | `.templ` | templ inline CSS/JS components |
| `(?<!max-)(?<!min\()\b(width\|min-width\|inline-size\|min-inline-size)\s*:\s*\d+(\.\d+)?(px\|rem\|em)` | `.css` except `tokens.css` | Fixed widths cause overflow |
| `100vw` | `.css` | Includes the scrollbar, so it overflows |
| `overflow-x\s*:\s*(auto\|scroll)` ; `(html\|body)[^{]*\{[^}]*overflow-x\s*:\s*hidden` | `.css` | Sideways scrolling, or masking it |
| `white-space\s*:\s*nowrap` outside the `.truncate` rule | `.css` | Forces overflow |
| `#[0-9a-f]{3,8}\b`, `rgb\(`, `hsl\(` | `.css` except `tokens.css` | Colours only via tokens |
| `<table` | `.templ` except `datatable.templ`, `print/*.templ` | Lists use the card/row pattern |

Go's RE2 has no look-behind, so implement the two look-behind rows as "match `width:` then reject if preceded by `max-`". Keep the rule list in one slice so adding a rule is a one-line change.

### 6.4 Automated e2e check (chromedp v0.21.0), run and verified

**Important API note.** chromedp v0.21.0 has a **generic API**: `Action[T]`, `chromedp.Do(ctx, steps...)` for void steps, and `chromedp.Run(ctx, chromedp.Evaluate[T](expr))` returns `(T, error)` (`action.go:31,59,78`, `eval.go:37`). Older examples that use `chromedp.Run(ctx, chromedp.Evaluate(expr, &res))` will not compile.

**Important measurement note.** The brief suggested asserting `scrollWidth <= window.innerWidth`. **That assertion is unsafe.** With mobile emulation (`EmulateMobile`), Chrome widens the layout viewport to fit overflowing content. In our run of a broken page at 320 px, `innerWidth` reported **677**, equal to `scrollWidth`, so the check would pass. Compare against `document.documentElement.clientWidth` instead.

Verified run: a scratch module with Go 1.25.5, chromedp v0.21.0 and local Chrome served four test pages.

| Page | Result |
|---|---|
| `/bad` (`width:400px` box + unbroken 60-character GSTIN-like string) | **Failed** at 320 px: `document scrollWidth 677 > clientWidth 320 (innerWidth 677)`, `outside viewport: div.box [8,408]`, `content wider than box: p scrollWidth 669 > 304` |
| `/clip` (same box with `html{overflow-x:hidden}`) | **Failed**: `scrollWidth 408 > 320`. The cheat is caught |
| `/scroller` (`overflow-x:auto` wrapper around a wide table) | **Failed**: `content wider than box: div.t … overflow-x:auto` |
| `/good` (`overflow-wrap:anywhere`, `max-width:100%`) | **Passed** at every width |

```go
//go:build e2e

// e2e/overflow_test.go: checks every screen at every width for horizontal overflow.
var widths = []int64{320, 360, 390, 768, 1024, 1440}

const overflowJS = `(() => {
  const de = document.documentElement, vw = de.clientWidth, out = [];
  if (de.scrollWidth > vw) out.push("document scrollWidth " + de.scrollWidth + " > clientWidth " + vw + " (innerWidth " + innerWidth + ")");
  const name = el => el.tagName.toLowerCase() + (el.id ? "#" + el.id : "") +
    (typeof el.className === "string" && el.className.trim() ? "." + el.className.trim().split(/\s+/).join(".") : "");
  for (const el of document.querySelectorAll("body *")) {
    if (el.closest("[data-allow-overflow]")) continue;          // escape hatch: must be reviewed (e.g. none in v1)
    const cs = getComputedStyle(el);
    if (cs.display === "none" || cs.display === "contents") continue;
    const r = el.getBoundingClientRect();
    if (r.width === 0 && r.height === 0) continue;
    if (r.right > vw + 0.5 || r.left < -0.5) { out.push("outside viewport: " + name(el) + " [" + Math.round(r.left) + "," + Math.round(r.right) + "]"); continue; }
    if (el.scrollWidth > el.clientWidth + 1 && el.clientWidth > 0 && cs.textOverflow !== "ellipsis" && !(el instanceof SVGElement))
      out.push("content wider than box: " + name(el) + " scrollWidth " + el.scrollWidth + " > " + el.clientWidth + " overflow-x:" + cs.overflowX);
  }
  return out;
})()`

func TestNoHorizontalOverflow(t *testing.T) {
	ctx := e2ectx.Browser(t)                    // NewExecAllocator(DefaultExecAllocatorOptions) + NewContext, signed in as the fixture admin
	for _, s := range screens.All(t) {          // see "every screen" below
		for _, w := range widths {
			t.Run(fmt.Sprintf("%s@%d", s.Name, w), func(t *testing.T) {
				opts := []chromedp.EmulateViewportOption{}
				if w < 768 { opts = append(opts, chromedp.EmulateMobile, chromedp.EmulateTouch) }
				if err := chromedp.Do(ctx, chromedp.EmulateViewport(w, 800, opts...), chromedp.Navigate(s.URL), chromedp.WaitReady("body")); err != nil { t.Fatal(err) }
				if err := s.Setup(ctx); err != nil { t.Fatal(err) } // optional: open a dialog/sheet, trigger a 422, go to wizard step n
				probs, err := chromedp.Run(ctx, chromedp.Evaluate[[]string](overflowJS))
				if err != nil { t.Fatal(err) }
				if len(probs) > 0 { t.Errorf("%d problems:\n%s", len(probs), strings.Join(probs, "\n")) }
			})
		}
	}
}
```

**"Every screen".** Build the list from the router with `chi.Walk`, taking every `GET` route outside `/static`, `/print` and `/api`. Fill path parameters from the shared fixtures (`00-fixtures.md`): the longest party name, the longest GSTIN/email and 500-line documents. Add `Setup` variants for each wizard step, each dialog and sheet, a validation-error state (long error messages) and an empty list. The test fails when a registered GET route has no screen entry, so new screens cannot skip the check. Seed fixtures with worst-case strings (60-character unbroken codes, 100-character names, ₹99,99,99,999.99).

**CSP check in the same run.** v0.21.0 removed `ListenTarget`. Subscribe first with the iterator API, `entries := chromedp.Events(ctx, log.EntryAdded)` (cdproto `log/log.go:51`; `action.go:141`, see `ExampleEvents_consoleLog`). Enable the Log domain, then fail on any entry with `Source == log.EntrySourceSecurity` or text containing "Content Security Policy". A simpler and more robust alternative: add `report-uri /csp-report` to the policy in e2e builds and assert that the handler received no reports. This is the gate for dropping `style-src 'unsafe-inline'`.

Sources: https://pkg.go.dev/github.com/chromedp/chromedp@v0.21.0 ; https://github.com/chromedp/chromedp/releases/tag/v0.21.0 ; https://developer.mozilla.org/en-US/docs/Web/API/Element/scrollWidth ; https://developer.mozilla.org/en-US/docs/Web/CSS/overflow-wrap

---

## 7. Wizard mechanics with htmx

1. **State lives on the server.** The wizard edits a `draft` document (F5). Creating the wizard immediately saves a draft with `version`. Every *Next* POSTs that step's fields, validates the step, saves the draft (optimistic `version` check) and returns the next step. Nothing important lives in the browser. Reloading, switching devices or losing the network loses at most the step being typed.
2. **One URL per step.** `GET /po/{id}/edit/{step}` (`step` is a slug: `party`, `lines`, `terms`, `review`). Without `HX-Request` it renders the full page; with it, only `#wizard`. Send `Vary: HX-Request`.
3. **Submitting a step.** `<form hx-post="/po/{id}/edit/lines" hx-target="#wizard" hx-swap="outerHTML" hx-disabled-elt="find button[type=submit]">`. Without JS, the same form posts normally and gets a 303 redirect. The server decides navigation with **response headers**:
   - Success → `200`, body = next step, header `HX-Push-Url: /po/{id}/edit/terms`. Use the response header, not the `hx-push-url` attribute, so a failed validation never pushes a URL.
   - Validation error → `422`, body = **the same step re-rendered from the POSTed values** (not the saved draft), with `aria-invalid="true"` and `aria-describedby` on each bad field and an error summary at the top. `responseHandling` swaps 422 (§2.2).
   - Version conflict → `409` handled by the global error toast, which offers "Reload".
4. **Back and forward.** With `historyCacheSize: 0` and `refreshOnHistoryMiss: true`, the browser's Back on step 3 does a full GET of step 2's URL, rendered from the saved draft. The page is always correct and never a stale snapshot. The in-page *Back* button is a plain link (`<a href=".../party" hx-get hx-target="#wizard" hx-push-url="true">`) that does **not** save. Saving a step happens only on *Next* and *Save draft*. If unsaved edits on a step should survive *Back*, make *Back* a submit button with `name="nav" value="back"` that saves without validating.
5. **Steps can be revisited.** Completed steps in the stepper are links. Steps ahead of the furthest-reached step are not links. On submit for approval, the server re-validates every step and jumps to the first invalid one.
6. **Progress indicator markup.** It is accessible, has no inline style, and fits at 320 px.

```html
<nav class="stepper" aria-label="Purchase order steps">
  <p class="stepper__count" id="step-count">Step 2 of 4</p>
  <ol class="stepper__list">
    <li class="stepper__step is-done"><a href="/po/42/edit/party"><span class="visually-hidden">Completed: </span>Party</a></li>
    <li class="stepper__step is-current" aria-current="step"><span>Lines</span></li>
    <li class="stepper__step"><span>Terms</span></li>
    <li class="stepper__step"><span>Review</span></li>
  </ol>
  <progress class="stepper__bar" max="4" value="2" aria-labelledby="step-count">2 of 4</progress>
</nav>
```

   On phones, `.stepper__list` shows only the current step's label (the others collapse to dots with `flex: 1 1 0; min-width: 0`). "Step 2 of 4" plus the `<progress>` bar is always visible. The labels show at ≥ 30rem. Style `<progress>` with `appearance: none` and `::-webkit-progress-value` / `::-moz-progress-bar` using `--color-primary-600` on `--color-progress-track` (4.83:1, §9).
7. **Focus after a step change.** Each step's `<h1 id="step-title" tabindex="-1" autofocus>Lines</h1>`. htmx calls `.focus()` on the first `[autofocus]` in swapped content (`processFocus`, `htmx.js:1587-1592`), and full page loads honour native `autofocus`. On a 422 response, put `autofocus` on the error summary (`role="alert"` not needed once focused; `tabindex="-1"`) instead of the heading, so the user hears the errors. `defaultFocusScroll: true` scrolls the focused element into view, and `scroll-padding` keeps it clear of the sticky bars.
8. **Sticky action bar.** It is the last child of the form: `<div class="actionbar">` with *Back* (secondary) and *Next* / *Submit for approval* (primary), `position: sticky; inset-block-end: 0`, plus `padding-bottom: max(var(--space-3), env(safe-area-inset-bottom))`. Use `<button type="submit">`, so pressing Enter in a field moves to the next step.

Sources: https://htmx.org/headers/hx-push-url/ ; https://htmx.org/docs/#history ; https://htmx.org/docs/#response-handling ; https://www.w3.org/WAI/ARIA/apg/ (aria-current) ; https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Elements/progress

---

## 8. Component catalogue: technical approach

Legend: **None** = HTML + CSS only; **htmx** = server round trip; **app.js** = our own delegated script (CSP-safe); **Alpine** = registered `Alpine.data` component with the CSP build.

| Component | JS | Notes |
|---|---|---|
| Button, link-button, badge, card, field, alert | None | Every interactive element has `min-block-size: var(--target-min)` |
| Accordion / disclosure | None | `<details><summary>`. Baseline widely available. Exclusive groups via `<details name="x">` (Baseline 2024-09, newly available) |
| Sticky action bar | None | §7.8 |
| Stepper / progress | None | §7.6 |
| Form submit, validation, inline field check | htmx | `hx-post` + 422 swap. Per-field check: `hx-post="/validate/gstin" hx-trigger="change" hx-target="next .field__msg"` |
| Toasts | htmx + app.js | **Content:** the server appends a toast with an out-of-band swap `<div id="toasts" hx-swap-oob="beforeend">…</div>` into a `role="status" aria-live="polite"` region, with errors in a separate `role="alert"` region. Templ escapes the text, so no client-side HTML building. **Events:** use `HX-Trigger: {"toast":{"level":"success","text":"Saved"}}` only where no fragment is returned (e.g. 204). app.js listens with `document.body.addEventListener("toast", …)` and builds the node with `textContent`. app.js also auto-dismisses success toasts after 5 s (pause on hover/focus; never auto-dismiss errors) |
| Dialog / confirm | app.js | Native `<dialog>` (Baseline since Mar 2022) with `showModal()`. Background is inert, Esc closes, focus is restored, top layer. Opener: `<button data-open-dialog="confirm-cancel">` and a delegated click handler in app.js. **Invoker commands** (`commandfor`/`command="show-modal"`) would remove the JS, but are only Baseline *newly* available since Dec 2025 (Chrome 135, Firefox 144, Safari 26.2). Older iPhones lack them, so use app.js now and switch later. `closedby="any"` (light dismiss) is not in Safari yet, so add an explicit close button and backdrop click in app.js. Server-loaded content: `hx-get` into the dialog body, then `HX-Trigger: {"dialog:open":"id"}` |
| Bottom sheet | app.js | The same `<dialog>`, styled `.sheet`: `margin-block-end: 0; inline-size: 100%; max-block-size: 85dvh; border-radius: var(--radius-xl) var(--radius-xl) 0 0`, with safe-area padding. At ≥ 48rem it renders as a centred dialog. `@starting-style` transitions are disabled under reduced motion |
| Searchable picker / combobox | htmx + Alpine | WAI-ARIA 1.2 combobox pattern: `<input role="combobox" aria-expanded aria-controls="lb" aria-autocomplete="list" aria-activedescendant>` + `<ul role="listbox" id="lb">` of `role="option"`. **htmx** fetches options: `hx-get="/pick/party" hx-trigger="input changed delay:250ms, focus once" hx-target="#lb"`. The server returns at most 20 matches, searching code + name + GSTIN. **Alpine** (`x-data="combobox"`) handles ↑/↓/Enter/Esc, `aria-activedescendant` and writes the chosen ID into a hidden `<input name="party_id">`. The visible input stays a free-text search. On phones (< 48rem) the picker opens as a full-height sheet, so the on-screen keyboard does not cover the list. Don't use `<datalist>`: it is not Baseline, cannot carry IDs and its mobile UI is inconsistent. Keep the pattern accessible without Alpine: a normal `<select>` fallback for lists under 50 items (UoM, tax code) |
| Line grid (desktop editing) | Alpine | Spec-reserved Alpine use: keyboard navigation between cells, add/remove row. Each change posts with htmx for server-calculated totals (GST split) |
| File upload with camera | None + app.js (progress) | Two controls: **Take photo** `<input type="file" accept="image/*" capture="environment">` and **Choose file** `<input type="file" accept="image/*,application/pdf" multiple>`. With `capture`, Android opens the camera directly (no gallery), hence two buttons. MDN BCD lists `capture` in Chrome Android 25 and Safari iOS 10, ignored on desktop. Form: `hx-post hx-encoding="multipart/form-data"`. A progress bar uses the htmx 2 `htmx:xhr:progress` event, set as `progress.value` in app.js (no inline style). The server checks size and type (`mimetype`), as the spec requires |
| Data list (card/row) | None (htmx for paging/filter) | §6.2. Filters are a `<form hx-get hx-push-url="true" hx-trigger="change, submit">`, so the filtered URL is shareable |
| Loading indicator | None | `.htmx-indicator` rules in `components.css` (htmx's own injection is off). Use `hx-disabled-elt` on submit buttons to stop double submits |

Sources: https://www.w3.org/WAI/ARIA/apg/patterns/combobox/ ; https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Elements/dialog ; https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Attributes/capture ; https://htmx.org/attributes/hx-swap-oob/ ; https://htmx.org/headers/hx-trigger/ ; https://htmx.org/events/#htmx:xhr:progress ; https://github.com/web-platform-dx/web-features (invoker-commands, dialog-closedby, details-name)

---

## 9. Proposed token file (`internal/ui/css/tokens.css`, light mode only)

```css
/* tokens.css: the ONLY file allowed raw colours and raw lengths (lint §6.3). Light mode only.
   Breakpoints (constants; custom properties can't be used in @media):
     --bp-sm 30rem (480px) · --bp-md 48rem (768px) · --bp-lg 64rem (1024px) · --bp-xl 80rem (1280px)
   Always write mobile-first: base styles, then @media (min-width: …). Prefer @container for components. */

:root {
  color-scheme: light;

  /* ---------- Neutrals ---------- */
  --color-canvas:          #F5F6F8;  /* page background */
  --color-surface:         #FFFFFF;  /* cards, inputs, sheets, dialogs */
  --color-sunken:          #EDEFF2;  /* wells, list header row, read-only fields */
  --color-text:            #141A23;  /* body and headings */
  --color-text-secondary:  #46505E;  /* labels, meta */
  --color-text-muted:      #5E6878;  /* help text, placeholders (still ≥4.5:1) */
  --color-text-disabled:   #8A93A1;  /* disabled controls only (exempt from 1.4.3) */
  --color-text-on-fill:    #FFFFFF;  /* text on solid primary/status fills */
  --color-border:          #7C8696;  /* input, checkbox and radio edges: UI component, ≥3:1 */
  --color-border-subtle:   #DCE0E6;  /* dividers and card outlines (decorative) */
  --color-progress-track:  #D5DAE1;  /* unfilled progress/stepper track */
  --color-overlay:         rgb(20 26 35 / 0.5); /* dialog::backdrop */

  /* ---------- Primary (actions, links, focus) ---------- */
  --color-primary-50:  #EEF3FF;  /* selected row, primary-tinted backgrounds */
  --color-primary-600: #1F4FD1;  /* solid buttons, links, active stepper */
  --color-primary-700: #1A3FA8;  /* hover */
  --color-primary-800: #16348A;  /* active/pressed; text on primary-50 */
  --color-focus:       #1F4FD1;

  /* ---------- Status: 50 = tint background, 600 = icon/border, 700 = text/solid fill, 800 = text on tint ---------- */
  --color-success-50:  #EAF7EE; --color-success-600: #1E8A42; --color-success-700: #167033; --color-success-800: #0F5326;
  --color-warning-50:  #FFF6E5; --color-warning-600: #B86200; --color-warning-700: #985000; --color-warning-800: #7A3F00;
  --color-danger-50:   #FDEEEC; --color-danger-600:  #D0342A; --color-danger-700:  #B3261E; --color-danger-800:  #8F1F18;
  --color-info-50:     #EAF4FB; --color-info-600:    #0C78BF; --color-info-700:    #0A64A0; --color-info-800:    #084C79;

  /* ---------- Typography ---------- */
  --font-sans: Inter, system-ui, -apple-system, "Segoe UI", Roboto, "Noto Sans", "Noto Sans Devanagari", sans-serif;
  --font-mono: ui-monospace, "SF Mono", Menlo, Consolas, "Liberation Mono", monospace;
  --text-xs:   0.8125rem; /* 13px: captions, badges only */
  --text-sm:   0.875rem;  /* 14px: meta, table secondary */
  --text-base: 1rem;      /* 16px: body, ALL inputs (prevents iOS zoom) */
  --text-lg:   1.125rem;  /* 18px: card titles, h3 */
  --text-xl:   1.25rem;   /* 20px: h2 */
  --text-2xl:  1.5rem;    /* 24px: h1 on mobile */
  --text-3xl:  1.875rem;  /* 30px: h1 at ≥48rem */
  --leading-tight: 1.25;
  --leading-snug:  1.375;
  --leading-body:  1.5;
  --weight-regular: 400;
  --weight-medium:  500;
  --weight-semibold: 600;
  --weight-bold:    700;
  --tracking-tight: -0.011em;  /* headings */
  --h1: var(--text-2xl);
  --h2: var(--text-xl);
  --h3: var(--text-lg);

  /* ---------- Spacing (4px grid) ---------- */
  --space-0: 0;
  --space-1: 0.25rem;  /*  4 */
  --space-2: 0.5rem;   /*  8 */
  --space-3: 0.75rem;  /* 12 */
  --space-4: 1rem;     /* 16 */
  --space-5: 1.25rem;  /* 20 */
  --space-6: 1.5rem;   /* 24 */
  --space-8: 2rem;     /* 32 */
  --space-10: 2.5rem;  /* 40 */
  --space-12: 3rem;    /* 48 */
  --space-16: 4rem;    /* 64 */
  --gutter: var(--space-4);          /* page side padding */
  --content-max: 72rem;              /* max content width on desktop; always via max-inline-size */
  --form-max: 40rem;                 /* single-column form/wizard width */

  /* ---------- Sizing ---------- */
  --target-min: 2.75rem;    /* 44px minimum touch target (WCAG 2.5.8 AA asks 24px) */
  --target-primary: 3rem;   /* 48px primary actions in the mobile action bar */
  --control-h: 2.75rem;     /* input/select/button height */
  --icon-sm: 1rem;
  --icon-md: 1.25rem;
  --icon-lg: 1.5rem;
  --logo-header-w: 10rem;  --logo-header-h: 2rem;     /* app header logo slot (CD-99) */
  --logo-signin-w: 15rem;  --logo-signin-h: 4rem;     /* sign-in page logo slot */
  --header-h: 3.5rem;
  --actionbar-h: 4.5rem;

  /* ---------- Borders and radii ---------- */
  --border-w: 1px;
  --border-w-strong: 2px;
  --radius-sm: 0.25rem;  /*  4: badges */
  --radius-md: 0.5rem;   /*  8: inputs, buttons */
  --radius-lg: 0.75rem;  /* 12: cards */
  --radius-xl: 1rem;     /* 16: sheets, dialogs */
  --radius-pill: 999px;

  /* ---------- Elevation (light only, subtle) ---------- */
  --shadow-1: 0 1px 2px rgb(20 26 35 / 0.06), 0 1px 1px rgb(20 26 35 / 0.04);   /* cards */
  --shadow-2: 0 4px 12px rgb(20 26 35 / 0.08), 0 1px 3px rgb(20 26 35 / 0.06);  /* dropdowns, sticky bars */
  --shadow-3: 0 12px 32px rgb(20 26 35 / 0.14), 0 2px 6px rgb(20 26 35 / 0.08); /* dialogs */
  --shadow-sheet: 0 -8px 24px rgb(20 26 35 / 0.12);                              /* bottom sheet */

  /* ---------- Focus ---------- */
  --focus-ring-w: 2px;
  --focus-ring-offset: 2px;
  --focus-ring: var(--focus-ring-w) solid var(--color-focus);

  /* ---------- Z-index (native <dialog> uses the top layer, no z-index) ---------- */
  --z-base: 0;
  --z-raised: 1;
  --z-sticky: 100;     /* sticky action bar, sticky list header */
  --z-header: 200;     /* app header */
  --z-dropdown: 300;   /* combobox listbox (non-modal) */
  --z-overlay: 400;    /* non-dialog overlays */
  --z-toast: 500;

  /* ---------- Motion ---------- */
  --dur-fast: 120ms;
  --dur-base: 200ms;
  --dur-slow: 300ms;
  --ease-standard: cubic-bezier(0.2, 0, 0, 1);
  --ease-enter:    cubic-bezier(0, 0, 0, 1);
  --ease-exit:     cubic-bezier(0.3, 0, 1, 1);

  /* ---------- Safe areas ---------- */
  --safe-top: env(safe-area-inset-top, 0px);
  --safe-bottom: env(safe-area-inset-bottom, 0px);
  --safe-left: env(safe-area-inset-left, 0px);
  --safe-right: env(safe-area-inset-right, 0px);
}

@media (min-width: 48rem) {
  :root {
    --gutter: var(--space-6);
    --h1: var(--text-3xl);
    --h2: var(--text-2xl);
    --h3: var(--text-xl);
  }
}

@media (prefers-reduced-motion: reduce) {
  :root { --dur-fast: 0.01ms; --dur-base: 0.01ms; --dur-slow: 0.01ms; }
  html { scroll-behavior: auto; }
}

/* Applied in base.css (shown here for completeness) */
/* :focus-visible { outline: var(--focus-ring); outline-offset: var(--focus-ring-offset); }
   html { scroll-padding-block: calc(var(--header-h) + var(--space-2)) calc(var(--actionbar-h) + var(--space-2)); } */
```

### Contrast table (computed with the WCAG 2.x relative-luminance formula)

Text needs 4.5:1 (normal) and UI components, focus indicators and large text need 3:1. "Surface" = #FFFFFF, "Canvas" = #F5F6F8, "Sunken" = #EDEFF2.

**Foreground on the three backgrounds**

| Token | Hex | on Surface | on Canvas | on Sunken | Required | Result |
|---|---|---|---|---|---|---|
| `--color-text` | #141A23 | 17.47 | 16.16 | 15.17 | 4.5 | Pass |
| `--color-text-secondary` | #46505E | 8.17 | 7.56 | 7.09 | 4.5 | Pass |
| `--color-text-muted` | #5E6878 | 5.63 | 5.21 | 4.89 | 4.5 | Pass |
| `--color-border` (input edges) | #7C8696 | 3.68 | 3.40 | 3.19 | 3.0 | Pass |
| `--color-primary-600` (links, ghost buttons) | #1F4FD1 | 6.78 | 6.27 | 5.89 | 4.5 | Pass |
| `--color-focus` (focus ring) | #1F4FD1 | 6.78 | 6.27 | 5.89 | 3.0 | Pass |
| `--color-success-700` text | #167033 | 6.18 | 5.71 | 5.36 | 4.5 | Pass |
| `--color-warning-700` text | #985000 | 6.03 | 5.58 | 5.23 | 4.5 | Pass |
| `--color-danger-700` text | #B3261E | 6.54 | 6.04 | 5.67 | 4.5 | Pass |
| `--color-info-700` text | #0A64A0 | 6.28 | 5.81 | 5.45 | 4.5 | Pass |

**Solid fills, tints and component pairs**

| Pair | Ratio | Required | Result |
|---|---|---|---|
| White on primary-600 #1F4FD1 (primary button) | 6.78 | 4.5 | Pass |
| White on primary-700 #1A3FA8 (hover) | 9.05 | 4.5 | Pass |
| White on primary-800 #16348A (pressed) | 11.10 | 4.5 | Pass |
| White on success-700 #167033 | 6.18 | 4.5 | Pass |
| White on warning-700 #985000 | 6.03 | 4.5 | Pass |
| White on danger-700 #B3261E (destructive button) | 6.54 | 4.5 | Pass |
| White on danger-800 #8F1F18 (hover) | 8.83 | 4.5 | Pass |
| White on info-700 #0A64A0 | 6.28 | 4.5 | Pass |
| primary-800 on primary-50 (#16348A / #EEF3FF) | 9.99 | 4.5 | Pass |
| primary-700 on primary-50 | 8.15 | 4.5 | Pass |
| success-800 on success-50 (#0F5326 / #EAF7EE) | 8.33 | 4.5 | Pass |
| warning-800 on warning-50 (#7A3F00 / #FFF6E5) | 7.71 | 4.5 | Pass |
| danger-800 on danger-50 (#8F1F18 / #FDEEEC) | 7.83 | 4.5 | Pass |
| info-800 on info-50 (#084C79 / #EAF4FB) | 8.12 | 4.5 | Pass |
| text on primary-50 / on warning-50 | 15.73 / 16.28 | 4.5 | Pass |
| text-secondary on sunken | 7.09 | 4.5 | Pass |
| success-600 icon/border on surface (#1E8A42) | 4.40 | 3.0 | Pass |
| warning-600 icon/border on surface (#B86200) | 4.39 | 3.0 | Pass |
| danger-600 icon/border on surface (#D0342A) | 4.99 | 3.0 | Pass |
| info-600 icon/border on surface (#0C78BF) | 4.71 | 3.0 | Pass |
| success-600 border on success-50 | 3.99 | 3.0 | Pass |
| warning-600 border on warning-50 | 4.09 | 3.0 | Pass |
| danger-600 border on danger-50 | 4.43 | 3.0 | Pass |
| info-600 border on info-50 | 4.23 | 3.0 | Pass |
| Progress fill primary-600 on track #D5DAE1 | 4.83 | 3.0 | Pass |
| Disabled text #8A93A1 on surface | 3.10 | exempt (inactive UI, 1.4.3) | n/a |
| border-subtle #DCE0E6 / track #D5DAE1 / canvas #F5F6F8 / primary-50 vs surface | 1.33 / 1.41 / 1.08 / 1.11 | decorative only | Never the sole cue: selected rows also get a 3 px primary-600 leading bar plus `aria-selected`/`aria-current`; cards on canvas also get `--color-border-subtle` + `--shadow-1` |

Usage rules that follow from the table:
- `--color-text-muted` is the lightest colour allowed for any readable text, including placeholders. Never put muted text on tinted (`*-50`) backgrounds; use the 800 shade there.
- Status text uses the 700 shade. The 600 shade is only for icons, borders and focus-adjacent indicators (≥ 3:1, not for body text).
- Warning solid fills use 700 (#985000) with white text. Never use yellow fills with white text.

---

## 10. Open points for the spec owner

1. **htmx 4.0.0** has been released; npm `latest` is still 2.0.11 by maintainer choice. Recommend staying on 2.0.11 for v1 (§2.3), but confirm this against the "latest stable" rule.
2. **CSP `style-src`**: recommend tightening to `'self'` once the e2e CSP check passes (§2.4, §6.4). This would amend WP0 Task 4.
3. **Router-driven screen list** for the overflow test needs the fixture set to include worst-case long strings (§6.4). Add them to `00-fixtures.md`.
4. The spec stack table (§2) should add: esbuild v0.28.2 (MIT, build-time Go module), `@alpinejs/csp` 3.17.5 (in place of the standard Alpine build), lucide-static 1.61.0 (ISC, vendored SVGs), Inter 4.1 and Noto Sans Devanagari 2.007 (OFL-1.1, vendored subsets).
