#!/usr/bin/env python3
"""Generate a light-mode-friendly variant of logo.png."""
from PIL import Image
import os, sys

SRC = "internal/web/static/logo.png"
DST = "internal/web/static/logo-light.png"

TEXT_BAND_TOP_PCT = 0.62
TARGET = (16, 30, 64, 255)

def _is_text_pixel(r, g, b, a):
    if a < 240:
        return False
    return r > 220 and g > 220 and b > 220

def main():
    if not os.path.exists(SRC):
        print(f"missing {SRC}", file=sys.stderr); return 1
    img = Image.open(SRC).convert("RGBA")
    w, h = img.size
    px = img.load()
    band_start_y = int(h * TEXT_BAND_TOP_PCT)
    changed = 0
    for y in range(band_start_y, h):
        for x in range(w):
            r, g, b, a = px[x, y]
            if _is_text_pixel(r, g, b, a):
                px[x, y] = TARGET
                changed += 1
    print(f"re-tinted {changed} pixels in bottom band -> {DST}")
    img.save(DST, "PNG", optimize=True)
    return 0

if __name__ == "__main__":
    sys.exit(main())
