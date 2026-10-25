# Vendored sources

## Fonts (`internal/ui/fonts/`, WP1-UI Task 1)

Subset once by hand with fontTools 4.61.1 (`python3 -m fontTools.*`, with the
`brotli` 1.2.1 module for woff2) on 25 Oct 2026. The outputs are committed;
fontTools is not a build dependency.

### Inter 4.1

- Source: https://github.com/rsms/inter/releases/download/v4.1/Inter-4.1.zip,
  file `InterVariable.ttf` (sha256 `4989b125924991b90d05b2d16e0e388c48f7d5bb8b30539bbf9c755278d0ccaf`).
- Licence: SIL OFL 1.1, `fonts/OFL-Inter.txt` (the zip's `LICENSE.txt`).
- Commands:

```
python3 -m fontTools.varLib.instancer InterVariable.ttf opsz=14 -o inter-opsz14.ttf
python3 -m fontTools.subset inter-opsz14.ttf \
  --unicodes="U+0000-00FF,U+2000-206F,U+20AC,U+20B9,U+2122,U+2190-2193,U+2212,U+2215" \
  --layout-features="kern,liga,calt,tnum,case,zero" \
  --flavor=woff2 --output-file=fonts/inter-var-sub.woff2
```

- Output: `fonts/inter-var-sub.woff2`, 40,696 bytes (`wght` 100–900 kept),
  sha256 `74aa47b0fd04eb36fe512c03d92caeac0b3454c856fdb5f96ce230dfa4101c5e`.

### Noto Sans Devanagari 2.007

- Source: https://github.com/notofonts/devanagari/releases/download/NotoSansDevanagari-v2.007/NotoSansDevanagari-v2.007.zip,
  file `NotoSansDevanagari/unhinted/variable-ttf/NotoSansDevanagari[wdth,wght].ttf`
  (sha256 `9f82923465ef82fb86c6b99cbdd4947ba832fe3335948de815b18adfd75947c7`).
- Licence: SIL OFL 1.1, `fonts/OFL-Noto.txt` (the zip's `OFL.txt`).
- Commands:

```
python3 -m fontTools.varLib.instancer "NotoSansDevanagari[wdth,wght].ttf" wdth=100 -o noto-wdth100.ttf
python3 -m fontTools.subset noto-wdth100.ttf \
  --unicodes="U+0900-097F,U+1CD0-1CF9,U+200C-200D,U+25CC,U+A8E0-A8FF,U+20B9" \
  --layout-features='*' \
  --flavor=woff2 --output-file=fonts/noto-sans-devanagari-sub.woff2
```

- Output: `fonts/noto-sans-devanagari-sub.woff2`, 114,508 bytes (`wght` kept),
  sha256 `ddc5437917face9b84dfbca77384e3619ac7070f2787804e1b862b1f356bc236`.
