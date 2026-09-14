#!/usr/bin/env python3
"""Convert a tmux capture-pane -e dump into the cell JSON that render-preview.py draws.

tmux stores what the application drew, including 24-bit colours, so a
capture of the real binary against real data renders through the same SVG
path as the fixture-based TestVisualReview export. Only the SGR sequences
tmux emits are interpreted: reset, bold, 16/256-colour and truecolor.
"""
import argparse
import json
import re
import sys
from pathlib import Path

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("dump", type=Path, help="output of tmux capture-pane -e -p (use - for stdin)")
parser.add_argument("width", type=int)
parser.add_argument("height", type=int)
parser.add_argument("output", type=Path)
parser.add_argument("--background", default="070B14", help="hex colour for cells without a background")
args = parser.parse_args()

text = sys.stdin.read() if str(args.dump) == "-" else args.dump.read_text(errors="replace")
BASIC = [0x000000, 0xCD0000, 0x00CD00, 0xCDCD00, 0x0000EE, 0xCD00CD, 0x00CDCD, 0xE5E5E5,
         0x7F7F7F, 0xFF0000, 0x00FF00, 0xFFFF00, 0x5C5CFF, 0xFF00FF, 0x00FFFF, 0xFFFFFF]


def xterm256(index):
    if index < 16:
        return BASIC[index]
    if index < 232:
        index -= 16
        steps = [0, 95, 135, 175, 215, 255]
        r, g, b = steps[index // 36], steps[(index // 6) % 6], steps[index % 6]
        return (r << 16) | (g << 8) | b
    grey = 8 + (index - 232) * 10
    return (grey << 16) | (grey << 8) | grey


SGR = re.compile(r"\x1b\[([0-9;:]*)m")
OTHER = re.compile(r"\x1b\[[0-9;?]*[A-Za-z]|\x1b[()][A-Za-z0-9]|\x1b[=>]")
rows = []
for line in text.split("\n")[: args.height]:
    fg, bg, bold = -1, -1, False
    cells = []
    i = 0
    while i < len(line) and len(cells) < args.width:
        m = SGR.match(line, i)
        if m:
            params = [p for p in re.split(r"[;:]", m.group(1)) if p != ""] or ["0"]
            j = 0
            while j < len(params):
                p = int(params[j])
                if p == 0:
                    fg, bg, bold = -1, -1, False
                elif p == 1:
                    bold = True
                elif p == 22:
                    bold = False
                elif 30 <= p <= 37:
                    fg = BASIC[p - 30]
                elif 90 <= p <= 97:
                    fg = BASIC[p - 90 + 8]
                elif 40 <= p <= 47:
                    bg = BASIC[p - 40]
                elif 100 <= p <= 107:
                    bg = BASIC[p - 100 + 8]
                elif p == 39:
                    fg = -1
                elif p == 49:
                    bg = -1
                elif p in (38, 48) and j + 1 < len(params):
                    mode = int(params[j + 1])
                    colour = -1
                    if mode == 5 and j + 2 < len(params):
                        colour = xterm256(int(params[j + 2]))
                        j += 2
                    elif mode == 2 and j + 4 < len(params):
                        r, g, b = (int(v) for v in params[j + 2 : j + 5])
                        colour = (r << 16) | (g << 8) | b
                        j += 4
                    if p == 38:
                        fg = colour
                    else:
                        bg = colour
                j += 1
            i = m.end()
            continue
        m = OTHER.match(line, i)
        if m:
            i = m.end()
            continue
        ch = line[i]
        i += 1
        if ch == "\r":
            continue
        cells.append({"Rune": ch, "FG": fg, "BG": bg, "Bold": bold})
    while len(cells) < args.width:
        cells.append({"Rune": " ", "FG": -1, "BG": -1, "Bold": False})
    rows.append(cells)
while len(rows) < args.height:
    rows.append([{"Rune": " ", "FG": -1, "BG": -1, "Bold": False} for _ in range(args.width)])
background = int(args.background, 16)
for row in rows:
    for cell in row:
        if cell["BG"] < 0:
            cell["BG"] = background
args.output.write_text(json.dumps({f"{args.width}x{args.height}": rows}))
