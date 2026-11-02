# Makes the cover of the project (docs/img/cover.png) from a render: 4:3, with a transparent
# background. Run: uv run --no-project --with pillow python lulzbot/cover.py docs/img/slate-v4.png docs/img/cover.png
import sys
from PIL import Image, ImageDraw
src, out = sys.argv[1], sys.argv[2]
im = Image.open(src).convert("RGBA")
w, h = im.size
# The background: fill from the corners with a transparent color.
for xy in ((0, 0), (w - 1, 0), (0, h - 1), (w - 1, h - 1)):
    ImageDraw.floodfill(im, xy, (0, 0, 0, 0), thresh=12)
# The holes (inside the base): the pure white pixels. The shaded faces are darker.
px = im.load()
for y in range(h):
    for x in range(w):
        r, g, b, a = px[x, y]
        if a and min(r, g, b) >= 250:
            px[x, y] = (0, 0, 0, 0)
box = im.getbbox()
im = im.crop(box)
cw, ch = im.size
H = int(max(ch, cw * 3 / 4) * 1.12)
W = int(H * 4 / 3)
canvas = Image.new("RGBA", (W, H), (0, 0, 0, 0))
canvas.paste(im, ((W - cw) // 2, (H - ch) // 2))
canvas = canvas.resize((1200, 900), Image.LANCZOS)
canvas.save(out, optimize=True)
