#!/usr/bin/env python3
"""Validate WCAG contrast of the design-token palette in input.css.

Parses the dark (:root) and light ([data-theme="light"]) token blocks and
checks the pairings the UI actually renders (docs/design/ui-overhaul/02 §1).
Exits non-zero on any failure so it can gate CI or a pre-release checklist.

Usage: python3 script/check-contrast.py
"""
import re
import sys
from pathlib import Path

CSS = Path(__file__).resolve().parent.parent / "server/webui/static/src/input.css"

# (foreground, background, minimum ratio, context)
# 4.5 = AA normal text; 3.0 = AA large text / UI components.
PAIRS = [
    ("--text", "--bg", 4.5, "body text on content backdrop"),
    ("--text", "--bg-raised", 4.5, "text on shell (sidebar/topbar)"),
    ("--text", "--surface", 4.5, "text on panels/cards"),
    ("--text-secondary", "--surface", 4.5, "secondary text on cards"),
    ("--text-muted", "--surface", 3.0, "muted text (large/annotation) on cards"),
    ("--text-on-accent", "--accent", 4.5, "label on filled accent (buttons/pills)"),
    ("--accent", "--surface", 3.0, "accent as UI component on cards"),
    ("--accent", "--bg", 3.0, "accent as UI component on backdrop"),
    ("--success", "--surface", 3.0, "success glyph/badge on cards"),
    ("--warning", "--surface", 3.0, "warning glyph/badge on cards"),
    ("--error", "--surface", 3.0, "error glyph/badge on cards"),
    ("--info", "--surface", 3.0, "info glyph/badge on cards"),
]


def parse_blocks(css: str):
    def block(start_re):
        m = re.search(start_re, css)
        if not m:
            sys.exit(f"check-contrast: cannot find token block {start_re!r}")
        depth, i = 0, m.end() - 1
        for j in range(i, len(css)):
            if css[j] == "{":
                depth += 1
            elif css[j] == "}":
                depth -= 1
                if depth == 0:
                    return css[i : j + 1]
        sys.exit("check-contrast: unbalanced braces")

    dark = block(r":root\s*\{")
    light = block(r':root\[data-theme="light"\]\s*\{')
    return dark, light


def tokens(block: str):
    out = {}
    for name, val in re.findall(r"(--[\w-]+)\s*:\s*([^;]+);", block):
        out[name] = val.strip()
    return out


def to_rgb(value: str, table: dict, seen=None):
    seen = seen or set()
    value = value.strip()
    if value.startswith("var("):
        ref = value[4:-1].strip()
        if ref in seen:
            sys.exit(f"check-contrast: circular var {ref}")
        seen.add(ref)
        return to_rgb(table[ref], table, seen)
    m = re.match(r"#([0-9a-fA-F]{6})$", value)
    if m:
        h = m.group(1)
        return tuple(int(h[i : i + 2], 16) for i in (0, 2, 4))
    return None  # rgba()/shadows/fonts — not checked as solid pairs


def luminance(rgb):
    def lin(c):
        c = c / 255.0
        return c / 12.92 if c <= 0.04045 else ((c + 0.055) / 1.055) ** 2.4

    r, g, b = (lin(c) for c in rgb)
    return 0.2126 * r + 0.7152 * g + 0.0722 * b


def contrast(fg, bg):
    l1, l2 = luminance(fg), luminance(bg)
    hi, lo = max(l1, l2), min(l1, l2)
    return (hi + 0.05) / (lo + 0.05)


def check(theme: str, table: dict, base: dict) -> int:
    merged = {**base, **table}
    failures = 0
    for fg_name, bg_name, minimum, ctx in PAIRS:
        fg = to_rgb(merged[fg_name], merged)
        bg = to_rgb(merged[bg_name], merged)
        if fg is None or bg is None:
            print(f"  SKIP {theme} {fg_name} on {bg_name} (non-solid value)")
            continue
        ratio = contrast(fg, bg)
        status = "OK  " if ratio >= minimum else "FAIL"
        if ratio < minimum:
            failures += 1
        print(
            f"  {status} {theme:5s} {fg_name} on {bg_name}: "
            f"{ratio:.2f}:1 (need {minimum}) — {ctx}"
        )
    return failures


def main():
    css = CSS.read_text()
    dark_block, light_block = parse_blocks(css)
    dark, light = tokens(dark_block), tokens(light_block)
    print("check-contrast: dark theme")
    failures = check("dark", dark, dark)
    print("check-contrast: light theme (dark values as fallback base)")
    failures += check("light", light, dark)
    if failures:
        sys.exit(f"check-contrast: {failures} pairing(s) below WCAG minimum")
    print("check-contrast: all pairings pass")


if __name__ == "__main__":
    main()
