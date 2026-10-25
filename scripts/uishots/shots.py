"""Screenshots for UI/UX reviews: each page at phone, tablet and desktop width, with a
horizontal-overflow check (the design system allows none).

Needs Python Playwright (`pip install playwright && playwright install chromium`); it is a
review tool only, not part of the Go build.

Usage: python3 scripts/uishots/shots.py [--base URL] [--out DIR] PATH [PATH ...]
  e.g. python3 scripts/uishots/shots.py /_gallery /_gallery/fields /_gallery/datalist
Exit status 1 when any page scrolls sideways.
"""
import argparse
import pathlib
import sys

from playwright.sync_api import sync_playwright

SIZES = {
    "phone": {"device": "iPhone 13"},
    "tablet": {"device": "iPad (gen 7)"},
    "desktop": {"viewport": {"width": 1280, "height": 900}},
}
MAX_HEIGHT = 4000  # taller pages are cut here so the image stays readable


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="http://localhost:8099")
    ap.add_argument("--out", default=".work/uishots")
    ap.add_argument("paths", nargs="+")
    args = ap.parse_args()
    out = pathlib.Path(args.out)
    out.mkdir(parents=True, exist_ok=True)

    failed = False
    with sync_playwright() as p:
        browser = p.chromium.launch()
        for size, spec in SIZES.items():
            opts = p.devices[spec["device"]] if "device" in spec else {"viewport": spec["viewport"]}
            ctx = browser.new_context(**opts)
            page = ctx.new_page()
            for i, path in enumerate(args.paths, 1):
                page.goto(args.base + path, wait_until="networkidle")
                page.wait_for_timeout(300)
                overflow = page.evaluate(
                    "document.documentElement.scrollWidth - document.documentElement.clientWidth")
                width = page.viewport_size["width"]
                height = min(page.evaluate("document.documentElement.scrollHeight"), MAX_HEIGHT)
                name = path.strip("/").replace("/", "-") or "root"
                page.screenshot(path=str(out / f"{i:02d}-{name}-{size}.png"),
                                clip={"x": 0, "y": 0, "width": width, "height": height})
                failed |= overflow > 0
                print(f"{size:7} {path}: {'OVERFLOW ' + str(overflow) + 'px' if overflow > 0 else 'ok'}")
            ctx.close()
        browser.close()
    print(f"screenshots in {out}")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
