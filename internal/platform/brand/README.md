# Brand

Branding is one folder applied at build time (design-system §5.1, CD-99). Changing the
logo or colours is a config change and a rebuild, not development.

## Rebrand

1. Replace the files in `assets/` (or add new ones).
2. Edit `brand.json`.
3. Run `make generate && make build`.
4. Deploy the new binary.

`make generate` fails with a message saying what to change if a colour or file breaks a rule below.

## brand.json keys

Unknown keys are an error.

| Key | Meaning | Rules |
|---|---|---|
| `app_name` | Name in the header, page titles and sign-in page | Required |
| `primary` | Main brand colour: buttons, links, focus ring, selected state | Required, `#RRGGBB`. White text on it ≥ 4.5:1; as text on `#FFFFFF`, `#F5F6F8` and `#EDEFF2` ≥ 4.5:1 |
| `primary_700` | Hover shade | Optional, derived from `primary` (15% towards black in OKLab). White text on it ≥ 4.5:1 |
| `primary_800` | Pressed shade, text on tint | Optional, derived (30% towards black). On `primary_50` ≥ 4.5:1 |
| `primary_50` | Tint | Optional, derived (93% towards white) |
| `document_accent` | Accent on printed documents | Optional, defaults to `primary`. On white ≥ 4.5:1 |
| `app_logo` | Logo in the app header and sign-in page | SVG (with a `viewBox`) or PNG, at most 200 KB |
| `document_logo` | Logo on printed documents | PNG or JPEG, at most 1 MB |
| `favicon` | Browser tab icon | PNG, square, at least 32 × 32 |

Logos are shown inside fixed boxes and scaled to fit (app header 160 × 32 px, sign-in
page 240 × 64 px, letterhead 60 × 20 mm), so their width:height must be between 1:2 and
8:1. The default files are placeholders.

## Generated code

`go generate` runs `./gen`, which checks the config and writes `brand_gen.go` with the
content-hashed URLs (`AppLogoURL`, `DocumentLogoURL`, `FaviconURL`) and the logos' sizes
(`AppLogoWidth`, `AppLogoHeight`, `DocumentLogoWidth`, `DocumentLogoHeight`). Commit it.
`Static()` serves the images under those URLs.
