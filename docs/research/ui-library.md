# UI component library: daisyUI 5 on Tailwind CSS v4

Date: 25 October 2026 · Decision: CD-98 (owner choice, 25 Oct 2026) · Replaces the CSS approach in `frontend-tech.md` §1 (hand-written CSS). Everything else in `frontend-tech.md` stands: htmx config, Alpine CSP build, icons, fonts, tokens, overflow rules and test, wizard mechanics, component behaviour.

## 1. Options compared (checked 25 Oct 2026)

| Library | GitHub stars | Latest release | Fit for templ + htmx + Alpine CSP, no Node |
|---|---|---|---|
| **daisyUI** (`saadeghi/daisyui`) | 42,671 | v5.7.50, 13 Oct 2026 | Pure CSS class names, no JavaScript runtime. Builds with the Tailwind standalone executable plus a `daisyui.mjs` file (official "standalone" install) |
| Bootstrap (`twbs/bootstrap`) | 175,016 | v5.3.8, 26 Aug 2025 | Most popular, but 14 months without a release, a dated look, and JS widgets that overlap htmx and Alpine |
| Basecoat (`hunvreus/basecoat`) | 4,399 | 1.1.0, 23 Oct 2026 | shadcn look for plain HTML; Tailwind-based; small community; its interactive parts ship their own JS |
| Franken UI (`franken-ui/ui`) | 2,608 | no GitHub release; last push 14 Jun 2026 | Less active |
| templUI (now `axadrn/shadcn-templ`) | 1,782 | v2.0.0-beta.15 | Only templ-native option, but v2 is still beta |
| Web Awesome (`shoelace-style/webawesome`) | 1,379 | v3.14.0, 24 Sep 2026 | Web components; Shoelace is sunset in its favour |

**Decision: daisyUI 5.7.50 on Tailwind CSS v4.3.3**, the most popular Tailwind component library, pure CSS, actively released, and well known to AI models. It gives every screen the same polished base styles. It does **not** change who owns the UI: all markup still lives in the closed set of templ components in `internal/ui` (design-system §7), and UX behaviour (ARIA, focus, flows, copy) stays ours, because daisyUI only styles.

## 2. Verified in a test build (25 Oct 2026, macOS arm64)

- **Downloads and checksums.** `tailwindcss-macos-arm64` v4.3.3 is 79,826,018 bytes; its sha256 `cdf64670…815dce9d` matches the release's `sha256sums.txt` (other platforms are listed there too). `daisyui.mjs` v5.7.50 is 351,408 bytes (sha256 `d69de07c…6be472`), `daisyui-theme.mjs` is 45,999 bytes (sha256 `e48a35d8…641f251`). No Node is used.
- **Speed and size.** A build with 20 components took 74 ms. Output: 56,083 bytes minified, 9,098 bytes gzip.
- **Light only.** `@plugin "daisyui" { themes: false; }` plus one custom theme with `default: true; prefersdark: false; color-scheme: light` produced one theme on `:where(:root)` and no `prefers-color-scheme: dark` rule. daisyUI still emits an unused `:root:has(input.theme-controller…)` selector; it is harmless.
- **Our tokens are the Tailwind theme.** `@theme { --color-*: initial; --text-*: initial; --radius-*: initial; --shadow-*: initial; }` removes Tailwind's defaults (`bg-red-500` was not generated). `tokens.css` with its first `:root {` changed to `@theme static {`, then imported, emits every token variable (including non-namespace ones such as `--space-4`, `--z-toast`) and generates utilities from them (`bg-canvas`, `text-primary-600`, `text-xs`, `rounded-md`). The plain `@media (min-width: 48rem) { :root { … } }` override in the same file still applies.
- **44 px controls.** daisyUI sizes buttons and inputs as `calc(var(--size-field) * 10)`, so `--size-field: 0.275rem` gives 2.75rem = 44 px (`--target-min`).
- **Sheet and dialog.** `modal modal-bottom md:modal-middle` on a native `<dialog>` gives a bottom sheet on phones and a centred dialog from 48rem, matching design-system §7.
- **Cascade.** Tailwind declares `theme, base, components, utilities`, and daisyUI nests its layers **inside `utilities`** (`utilities.daisyui.l1…`). Plain Tailwind utilities therefore override daisyUI, but anything we put in `@layer components` would lose to it. Our own component CSS is written as Tailwind `@utility` rules (verified: `@utility stepper { … }` compiles and is generated when used).

## 3. How it fits the stack

- **Build.** `internal/ui/cmd/buildassets` (run by `go generate`, CD-45) runs the pinned Tailwind executable on `internal/ui/css/app.css` with `--minify`, then hashes the output as before; esbuild still bundles `app.js`. The executable is downloaded once into `bin/tools/tailwindcss-v4.3.3-<os>-<arch>` (git-ignored by `/bin/`) and checked against `internal/ui/vendor/tailwind-v4.3.3.sha256` (committed copy of the release's `sha256sums.txt`); a mismatch fails the build. `daisyui.mjs` and `daisyui-theme.mjs` v5.7.50 are vendored under `internal/ui/vendor/daisyui/` with their checksums and MIT licence.
- **Scanning.** `@import "tailwindcss" source(none);` then `@source "../../ui/**/*.{templ,go}";` and `@source "../../{kernel,masters,modules}/**/*.templ";` (class names in Go prop-to-class maps must be whole strings; Tailwind cannot see concatenated names).
- **Theme.** `internal/ui/css/theme.css` holds the daisyUI theme block. Its values are copies of token values (it is the second file allowed raw colours), and a test proves each equals its token:

| daisyUI variable | Token |
|---|---|
| `--color-base-100` / `-200` / `-300` / `-content` | surface / canvas / sunken / text |
| `--color-primary`, `--color-accent` | primary-600 |
| `--color-secondary`, `--color-neutral` | text-secondary |
| `--color-info` / `success` / `warning` / `error` | the matching `-700` (solid fills) |
| every `*-content` except base | text-on-fill |
| `--radius-selector` / `-field` / `-box` | radius-sm / radius-md / radius-lg |
| `--size-selector` / `--size-field` | 0.25rem / 0.275rem (44 px controls) |
| `--border`, `--depth`, `--noise` | border-w / 0 / 0 |

- **Breakpoints.** `@theme { --breakpoint-sm: 30rem; }` (Tailwind's md 48rem, lg 64rem and xl 80rem already match design-system §5). Container queries use Tailwind's built-in `@container` / `@md:` variants.

## 4. Allowed and banned daisyUI parts

Allowed (mapped in design-system §7.1): `btn`, `input`, `textarea`, `select`, `fieldset`, `label`, `checkbox`, `radio`, `toggle`, `file-input`, `alert`, `badge`, `card`, `modal`, `collapse`, `menu`, `drawer`, `navbar`, `join`, `loading`, `progress`, `skeleton`, `toast`, `tooltip`, `divider`, `indicator`, `list`, `breadcrumbs`, `kbd`, `link`, `stat`, `status`.

Banned (lint rule `daisy-banned`), each with the reason:
- `countdown`, `radial-progress`, popover `dropdown` with `anchor-name`: need `style=` attributes, which the CSP and lint forbid;
- `table`: no tables outside `print/` (design-system §1.3);
- `carousel`, `dock`, `fab`, `hover-3d`, `hover-gallery`, `text-rotate`, `mockup-*`, `diff`, `swap`, `rating`, `mask`: sideways scrolling, a bottom tab bar (design-system §6 rejects it) or decoration with no job in this app;
- `theme-controller`, `dark:` variants: light only;
- the `*-soft` and `*-dash` style variants: their colours are mixed at runtime, so the contrast test cannot check them; use our tint utilities (`bg-warning-50 text-warning-800`) instead.

Tailwind arbitrary values (`w-[…]`, `[…]:`) are banned outside `internal/ui/css`; spacing and sizes come from tokens.

## 5. Sources

https://github.com/saadeghi/daisyui/releases/tag/v5.7.50 · https://daisyui.com/docs/install/standalone/ · https://daisyui.com/docs/themes/ · https://daisyui.com/llms.txt · https://github.com/tailwindlabs/tailwindcss/releases/tag/v4.3.3 · https://tailwindcss.com/docs/theme · https://tailwindcss.com/docs/detecting-classes-in-source-files · GitHub API repository data for each project in §1 (25 Oct 2026).
