#!/usr/bin/env python3
"""Render TestVisualReview cell JSON as a standalone SVG (standard library only)."""
import argparse
import html
import json
from pathlib import Path

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("cells", type=Path)
parser.add_argument("size", help="Frame name, e.g. 120x30")
parser.add_argument("output", type=Path)
args = parser.parse_args()
rows = json.loads(args.cells.read_text())[args.size]
width, height = len(rows[0]) * 10, len(rows) * 20
parts = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" height="{height}" viewBox="0 0 {width} {height}" role="img" aria-label="Systemdoc terminal fixture">']
parts.append('<title>Systemdoc — illustrative terminal fixture</title>')
# Draw all backgrounds before glyphs so wide characters are not overwritten.
for y, row in enumerate(rows):
    for x, cell in enumerate(row):
        bg = cell["BG"] if cell["BG"] >= 0 else 0x070B14
        parts.append(f'<rect x="{x*10}" y="{y*20}" width="10" height="20" fill="#{bg:06x}"/>')
for y, row in enumerate(rows):
    for x, cell in enumerate(row):
        if not cell["Rune"].strip():
            continue
        fg = cell["FG"] if cell["FG"] >= 0 else 0xEDF5FF
        weight = 700 if cell.get("Bold") else 400
        glyph = html.escape(cell["Rune"])
        parts.append(f'<text x="{x*10}" y="{y*20+15}" font-family="JetBrains Mono,JetBrainsMono Nerd Font,monospace" font-size="16" font-weight="{weight}" fill="#{fg:06x}">{glyph}</text>')
parts.append('</svg>')
args.output.write_text(''.join(parts))
