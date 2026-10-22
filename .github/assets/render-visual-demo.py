#!/usr/bin/env python3
"""Render a four-scene, code-drawn motion demo. Requires Pillow; no network or model."""

import argparse
import json
import math
from itertools import pairwise
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

W, H, FPS, STEPS = 1200, 760, 10, 45
BG, INK, MUTED, LINE = "#f7f7f3", "#172a32", "#68767b", "#dce2df"
GREEN = "#258268"
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--preview-dir", type=Path)
args = parser.parse_args()
ROOT = Path(__file__).resolve().parents[2]
CFG = json.loads((ROOT / ".github/assets/demo/story.json").read_text())
ACCENT = CFG["accent"]
for evidence in CFG.get("evidence", []):
    if evidence["contains"] not in (ROOT / evidence["path"]).read_text():
        raise ValueError("Demo evidence changed: " + evidence["path"])


def font(size, bold=False):
    paths = [
        Path("/System/Library/Fonts/Supplemental")
        / ("Arial Bold.ttf" if bold else "Arial.ttf"),
        Path("/usr/share/fonts/truetype/dejavu")
        / ("DejaVuSans-Bold.ttf" if bold else "DejaVuSans.ttf"),
    ]
    for p in paths:
        if p.exists():
            return ImageFont.truetype(str(p), size)
    raise SystemExit("Install Arial or DejaVu Sans to render the demo.")


def ease(t):
    t = min(1, max(0, t))
    return t * t * (3 - 2 * t)


def mix(a, b, t):
    t = min(1, max(0, t))
    a, b = tuple(bytes.fromhex(a.lstrip("#"))), tuple(bytes.fromhex(b.lstrip("#")))
    return tuple(round(x + (y - x) * t) for x, y in zip(a, b))


def label(d, xy, value, size=22, color=INK, bold=False, center=False, width=None):
    f = font(size, bold)
    if width:
        while d.textlength(value, font=f) > width and size > 15:
            size -= 1
            f = font(size, bold)
        if d.textlength(value, font=f) > width:
            raise ValueError("Text does not fit: " + value)
    d.text(xy, value, font=f, fill=color, anchor="mm" if center else "lt")


def panel(d, box, fill="white", outline=LINE, radius=18, stroke=2):
    d.rounded_rectangle(box, radius, fill=fill, outline=outline, width=stroke)


def badge(d, x, y, value, color=None, width=150):
    color = color or ACCENT
    panel(
        d,
        (x - width / 2, y - 19, x + width / 2, y + 19),
        fill=mix("#ffffff", color, 0.10),
        outline=mix("#ffffff", color, 0.2),
    )
    label(d, (x, y), value, 17, color, True, True, width - 18)


def icon(d, x, y, kind="doc", color=None, scale=1):
    c, s = color or ACCENT, scale
    if kind == "check":
        d.line(
            [(x - 13 * s, y), (x - 3 * s, y + 10 * s), (x + 17 * s, y - 13 * s)],
            fill=c,
            width=max(2, int(4 * s)),
        )
    elif kind == "person":
        d.ellipse((x - 9 * s, y - 21 * s, x + 9 * s, y - 3 * s), outline=c, width=3)
        d.arc(
            (x - 20 * s, y - 3 * s, x + 20 * s, y + 30 * s), 180, 360, fill=c, width=3
        )
    elif kind == "chart":
        for i, h in enumerate((14, 27, 39)):
            d.rounded_rectangle(
                (
                    x - 22 * s + i * 17 * s,
                    y + 19 * s - h * s,
                    x - 12 * s + i * 17 * s,
                    y + 19 * s,
                ),
                3,
                fill=c,
            )
    elif kind == "search":
        d.ellipse((x - 20 * s, y - 22 * s, x + 8 * s, y + 6 * s), outline=c, width=3)
        d.line((x + 5 * s, y + 5 * s, x + 22 * s, y + 22 * s), fill=c, width=4)
    else:
        d.rounded_rectangle(
            (x - 18 * s, y - 24 * s, x + 18 * s, y + 24 * s), 5, outline=c, width=3
        )
        for offset in (-10, 0, 10):
            d.line(
                (x - 9 * s, y + offset * s, x + 9 * s, y + offset * s), fill=c, width=2
            )


def wire(d, start, end, t, color=None, offset=0):
    c = color or ACCENT
    d.line((start, end), fill=mix("#ffffff", c, 0.22), width=3)
    p = (t * 1.2 + offset) % 1
    x, y = start[0] + (end[0] - start[0]) * p, start[1] + (end[1] - start[1]) * p
    d.ellipse((x - 5, y - 5, x + 5, y + 5), fill=c)


def flow(d, points, t, color=None):
    c = color or ACCENT
    d.line(points, fill=mix("#ffffff", c, 0.25), width=4, joint="curve")
    lengths = [math.dist(a, b) for a, b in pairwise(points)]
    travel = (t % 1) * sum(lengths)
    for i, length in enumerate(lengths):
        if travel <= length:
            f = travel / length
            a, b = points[i : i + 2]
            x, y = a[0] + (b[0] - a[0]) * f, a[1] + (b[1] - a[1]) * f
            d.ellipse((x - 6, y - 6, x + 6, y + 6), fill=c)
            return
        travel -= length


def brain(d, x, y, r, t):
    pulse = 4 * math.sin(t * math.pi * 2)
    d.ellipse(
        (
            x - r - 16 - pulse,
            y - r - 16 - pulse,
            x + r + 16 + pulse,
            y + r + 16 + pulse,
        ),
        outline=mix("#ffffff", ACCENT, 0.22),
        width=2,
    )
    d.ellipse((x - r, y - r, x + r, y + r), fill=ACCENT)
    pts = [
        (-0.43, -0.28),
        (-0.05, -0.48),
        (0.40, -0.23),
        (-0.43, 0.22),
        (0.02, 0.04),
        (0.42, 0.30),
        (-0.04, 0.48),
    ]
    edges = [
        (0, 1),
        (1, 2),
        (0, 3),
        (0, 4),
        (1, 4),
        (2, 4),
        (2, 5),
        (3, 4),
        (3, 6),
        (4, 5),
        (4, 6),
        (5, 6),
    ]
    for a, b in edges:
        p, q = pts[a], pts[b]
        d.line(
            (x + p[0] * r, y + p[1] * r, x + q[0] * r, y + q[1] * r),
            fill=mix("#ffffff", ACCENT, 0.38),
            width=3,
        )
    for i, (a, b) in enumerate(pts):
        rr = 5 + (2 if int(t * 14) % 7 == i else 0)
        d.ellipse(
            (x + a * r - rr, y + b * r - rr, x + a * r + rr, y + b * r + rr),
            fill="white",
        )


def tile(d, x, y, title, kind="doc", width=175):
    panel(d, (x - width / 2, y - 48, x + width / 2, y + 48))
    icon(d, x - width / 2 + 32, y, kind, scale=0.65)
    label(d, (x - width / 2 + 59, y - 11), title, 21, bold=True, width=width - 72)


def doc(d, x, y, w=270, h=290, title="Draft", tag=None, color=None):
    panel(d, (x, y, x + w, y + h))
    color = color or ACCENT
    icon(d, x + 36, y + 40, color=color, scale=0.65)
    label(d, (x + 65, y + 31), title, 23, bold=True, width=w - 85)
    for i, frac in enumerate((0.78, 0.92, 0.61, 0.86, 0.69)):
        yy = y + 92 + i * 29
        d.rounded_rectangle(
            (x + 25, yy, x + 25 + (w - 50) * frac, yy + 7),
            3,
            fill=mix("#ffffff", INK, 0.14),
        )
    if tag:
        badge(d, x + w / 2, y + h - 35, tag, color, w - 44)


def rank_row(d, y, name, score, index, highlight=False):
    panel(
        d,
        (285, y, 920, y + 92),
        outline=ACCENT if highlight else LINE,
        stroke=3 if highlight else 2,
    )
    label(d, (320, y + 32), str(index), 23, MUTED, True)
    d.ellipse((365, y + 22, 413, y + 70), fill=mix("#ffffff", ACCENT, 0.12))
    label(d, (389, y + 46), name[0], 22, ACCENT, True, True)
    label(d, (440, y + 32), name, 26, bold=True)
    label(d, (855, y + 46), str(score), 35, ACCENT, True, True)


def draw_ranking(d, scene, t):
    names = CFG["names"]
    if scene == 0:
        doc(d, 85, 300, 270, 300, "Your leads", "8 sample leads")
        wire(d, (355, 450), (555, 450), t)
        brain(d, 555, 450, 65, t)
        label(d, (555, 557), "Your rubric", 24, bold=True, center=True)
        wire(d, (620, 450), (790, 450), t)
        for i, (name, score) in enumerate(zip(names[:3], [65, 60, 50])):
            y = 280 + i * 115
            panel(d, (790, y, 1125, y + 88))
            label(d, (815, y + 21), name, 22, bold=True, width=225)
            label(d, (1085, y + 45), str(score), 29, ACCENT, True, True)
            d.rounded_rectangle(
                (815, y + 59, 815 + 200 * ease(t) * score / 80, y + 65), 3, fill=ACCENT
            )
    elif scene == 1:
        label(d, (300, 405), "65", 112, ACCENT, True, True)
        label(d, (300, 510), names[0], 31, bold=True, center=True)
        badge(d, 300, 575, "Call list", GREEN, 160)
        for i, (name, points) in enumerate(
            zip(["Company fit", "Region", "Role"], [40, 10, 15])
        ):
            yy = 300 + i * 115
            label(d, (600, yy), name, 26, bold=True)
            label(d, (1040, yy), "+" + str(points), 30, ACCENT, True)
            d.rounded_rectangle((600, yy + 45, 1080, yy + 57), 5, fill=LINE)
            d.rounded_rectangle(
                (600, yy + 45, 600 + 480 * points / 40 * ease(t), yy + 57),
                5,
                fill=ACCENT,
            )
    elif scene == 2:
        badge(d, 600, 265, "Warm path: +10 → +30", ACCENT, 310)
        move = ease((t - 0.20) / 0.50)
        first_y = 345 + move * 130
        second_y = 475 - move * 130
        rank_row(d, first_y, names[0], 65, 2 if move > 0.5 else 1)
        rank_row(
            d, second_y, names[1], round(60 + 20 * move), 1 if move > 0.5 else 2, True
        )
        badge(d, 600, 650, "Your priorities, made explicit", GREEN, 340)
    else:
        for i, (name, count) in enumerate(
            zip(["Call list", "Nurture", "Excluded"], [5, 1, 2])
        ):
            y = 280 + i * 125
            wire(d, (235, 440), (650, y + 30), t, offset=i / 3)
            panel(d, (650, y - 12, 1085, y + 85))
            icon(
                d,
                695,
                y + 35,
                "doc" if i < 2 else "check",
                GREEN if i < 2 else MUTED,
                0.7,
            )
            label(d, (740, y + 19), name, 28, bold=True)
            label(d, (1025, y + 36), str(count), 36, ACCENT, True, True)
        d.ellipse((155, 360, 315, 520), fill=ACCENT)
        label(d, (235, 423), "8", 58, "white", True, True)
        label(d, (235, 477), "leads", 23, "white", False, True)
        badge(d, 845, 675, "CSV exports · no CRM push", GREEN, 330)


DRAW = draw_ranking


def render(scene, t):
    im = Image.new("RGB", (W, H), BG)
    d = ImageDraw.Draw(im)
    label(d, (52, 32), CFG["brand"], 18, ACCENT, True)
    label(d, (52, 101), CFG["titles"][scene], 49, bold=True, width=1095)
    for i in range(4):
        d.ellipse(
            (1050 + i * 26, 37, 1058 + i * 26, 45), fill=ACCENT if i == scene else LINE
        )
    DRAW(d, scene, t)
    d.line((52, 712, 1148, 712), fill=LINE, width=1)
    label(d, (52, 731), CFG["footers"][scene], 15, MUTED, width=1095)
    return im


# All four keyframes contribute to one palette, keeping colors stable during motion.
keys = [render(i, 1) for i in range(4)]
atlas = Image.new("RGB", (W, H * 4))
for i, im in enumerate(keys):
    atlas.paste(im, (0, H * i))
palette = atlas.quantize(colors=128)
frames = []
for scene in range(4):
    previous = keys[(scene - 1) % 4]
    for step in range(STEPS):
        frame = render(scene, step / (STEPS - 1))
        if scene and step < 4:
            frame = Image.blend(previous, frame, (step + 1) / 4)
        frames.append(frame.quantize(palette=palette, dither=Image.Dither.NONE))
out = ROOT / ".github/assets/demo.gif"
frames[0].save(
    out,
    save_all=True,
    append_images=frames[1:],
    duration=1000 // FPS,
    loop=0,
    optimize=True,
    disposal=1,
)
if args.preview_dir:
    args.preview_dir.mkdir(parents=True, exist_ok=True)
    for i, im in enumerate(keys):
        im.save(args.preview_dir / f"scene-{i + 1}.png")
print(
    f"{out}: {len(frames)} frames, {len(frames) / FPS:.1f}s, {out.stat().st_size} bytes"
)
