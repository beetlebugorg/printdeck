#!/usr/bin/env python3
"""Show the current Spotify track on a black and white Inky wHAT."""

import io
import os
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import parse_qs

import requests
from PIL import Image, ImageDraw, ImageFont

# The Inky wHAT is in landscape position in the top of the case.
WIDTH, HEIGHT = 400, 300
ART_SIZE = 200
# Rotation of the image for the display. Use 0 or 180.
ROTATE = int(os.environ.get("INKY_ROTATE", "0"))
MARGIN = 10
FONT_DIR = "/usr/share/fonts/truetype/dejavu/"
PORT = int(os.environ.get("NOWPLAYING_PORT", "8765"))
# A full refresh is slow. Wait for this time so that fast skips cause one refresh.
SETTLE_SECONDS = 2.0

title_font = ImageFont.truetype(FONT_DIR + "DejaVuSans-Bold.ttf", 22)
text_font = ImageFont.truetype(FONT_DIR + "DejaVuSans.ttf", 18)

pending = None
pending_lock = threading.Lock()
pending_event = threading.Event()


def fit(draw, text, font, max_width):
    """Cut the text until it fits in max_width."""
    if draw.textlength(text, font=font) <= max_width:
        return text
    while text and draw.textlength(text + "…", font=font) > max_width:
        text = text[:-1]
    return text.rstrip() + "…"


def render(track):
    canvas = Image.new("L", (WIDTH, HEIGHT), 255)
    draw = ImageDraw.Draw(canvas)

    if track.get("cover"):
        try:
            data = requests.get(track["cover"], timeout=10).content
            art = Image.open(io.BytesIO(data)).convert("L")
            art = art.resize((ART_SIZE, ART_SIZE), Image.LANCZOS)
            canvas.paste(art, ((WIDTH - ART_SIZE) // 2, MARGIN))
        except Exception as error:
            print(f"cover download failed: {error}", file=sys.stderr)

    y = MARGIN + ART_SIZE + 8
    max_width = WIDTH - 2 * MARGIN
    for text, font in (
        (track.get("name", ""), title_font),
        (track.get("artists", ""), text_font),
        (track.get("album", ""), text_font),
    ):
        line = fit(draw, text, font, max_width)
        x = (WIDTH - draw.textlength(line, font=font)) // 2
        draw.text((x, y), line, font=font, fill=0)
        y += font.size + 6

    # Dither to 1 bit. Then map to the Inky palette: 0 is white, 1 is black.
    canvas = canvas.rotate(ROTATE, expand=True)
    bits = canvas.convert("1").convert("L").point(lambda v: 0 if v > 127 else 1)
    image = Image.frombytes("P", bits.size, bits.tobytes())
    image.putpalette([255, 255, 255, 0, 0, 0] + [0, 0, 0] * 254)
    return image


def show(track):
    from inky import InkyWHAT

    display = InkyWHAT("black")
    display.set_border(display.WHITE)
    display.set_image(render(track))
    display.show()


def worker():
    global pending
    while True:
        pending_event.wait()
        time.sleep(SETTLE_SECONDS)
        with pending_lock:
            track, pending = pending, None
            pending_event.clear()
        if track:
            try:
                show(track)
            except Exception as error:
                print(f"display update failed: {error}", file=sys.stderr)


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        global pending
        length = int(self.headers.get("Content-Length", 0))
        fields = parse_qs(self.rfile.read(length).decode("utf-8"))
        track = {key: values[0] for key, values in fields.items()}
        with pending_lock:
            pending = track
            pending_event.set()
        self.send_response(204)
        self.end_headers()

    def log_message(self, *args):
        pass


def main():
    if "--test" in sys.argv:
        show({"name": "Speakerdeck", "artists": "Display test", "album": "Inky wHAT"})
        return
    threading.Thread(target=worker, daemon=True).start()
    HTTPServer(("127.0.0.1", PORT), Handler).serve_forever()


if __name__ == "__main__":
    main()
