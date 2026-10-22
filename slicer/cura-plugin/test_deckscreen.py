# Tests of the deck UI client: python3 -m unittest test_deckscreen
import http.server
import json
import os
import queue
import tempfile
import threading
import unittest

from Printdeck import deckscreen

KEY = "key1"
FRAME = [["f", 0, 0, 800, 480, 1184273], ["c", 0, 0, 10, 10, 7], ["t", 5, 20, "READY", 1, 18, 16777215]]


class FakeDeck(http.server.ThreadingHTTPServer):
    """The /ui/ requests of the deck: a session, one frame, an image, the fonts, and the touches."""

    def __init__(self):
        self.touches = queue.Queue()
        self.size = None
        self.hosts = set()
        super().__init__(("127.0.0.1", 0), Handler)


class Handler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):
        pass

    def _send(self, code, body=b"", kind="application/octet-stream"):
        self.send_response(code)
        self.send_header("Content-Type", kind)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        self.server.hosts.add(self.headers.get("Host"))
        if self.headers.get("X-Api-Key") != KEY:
            return self._send(401)
        if self.path.startswith("/ui/events"):
            q = self.path.split("?", 1)[1]
            self.server.size = dict(p.split("=") for p in q.split("&"))
            # Chunks, as the deck sends them, with a cut in the middle of the frame.
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Transfer-Encoding", "chunked")
            self.end_headers()
            frame = b"data: " + json.dumps(FRAME).encode() + b"\n\n"
            for part in (b": ping\n\nevent: session\ndata: s1 800 480\n\n", frame[:20], frame[20:]):
                self.wfile.write(b"%x\r\n%s\r\n" % (len(part), part))
            self.wfile.flush()
            self.server.hold.wait(5)  # the stream stays open
        elif self.path == "/ui/img/7":
            self._send(200, b"PNG7", "image/png")
        elif self.path.startswith("/ui/font/"):
            self._send(200, b"TTF", "font/ttf")
        else:
            self._send(404)

    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
        if self.path == "/ui/touch?s=s1" and self.headers.get("X-Api-Key") == KEY:
            self.server.touches.put(json.loads(body))
            return self._send(204)
        self._send(410)


class TestDeckScreen(unittest.TestCase):
    def test_wanted_size(self):
        self.assertEqual(deckscreen.wanted_size(1000, 600), (800, 480))
        self.assertEqual(deckscreen.wanted_size(480, 900), (480, 900))
        self.assertEqual(deckscreen.wanted_size(480, 2000), (480, 1200))
        self.assertEqual(deckscreen.wanted_size(600, 700), (480, 800))
        self.assertEqual(deckscreen.wanted_size(0, 0), (800, 480))

    def test_sse(self):
        p = deckscreen.SSEParser()
        lines = [": ping", "", "event: session", "data: a 1 2", "", "data: [1]", "data: [2]", ""]
        events = [e for e in map(p.feed, lines) if e]
        self.assertEqual(events, [("session", "a 1 2"), ("message", "[1]\n[2]")])

    def test_image_ids(self):
        self.assertEqual(deckscreen.image_ids(FRAME + [["i", 0, 0, 1, 1, 9, 1]]), {7, 9})

    def _deck(self):
        deck = FakeDeck()
        deck.hold = threading.Event()
        threading.Thread(target=deck.serve_forever, daemon=True).start()
        self.addCleanup(deck.server_close)
        self.addCleanup(deck.shutdown)
        self.addCleanup(deck.hold.set)
        return deck

    def _stream(self, deck, key):
        events = queue.Queue()
        folder = tempfile.mkdtemp()
        s = deckscreen.DeckStream("http://127.0.0.1:%d" % deck.server_address[1], key, (800, 480), folder,
                                  on_session=lambda w, h: events.put(("session", w, h)),
                                  on_frame=lambda t: events.put(("frame", t)),
                                  on_end=lambda e: events.put(("end", e)))
        return s, events, folder

    def test_stream(self):
        deck = self._deck()
        s, events, folder = self._stream(deck, KEY)
        s.start()
        self.assertEqual(events.get(timeout=5), ("session", 800, 480))
        kind, text = events.get(timeout=5)
        self.assertEqual((kind, json.loads(text)), ("frame", FRAME))
        self.assertEqual(deck.size, {"w": "800", "h": "480"})
        # The image and the fonts are in the folder before the frame.
        with open(s.image_path(7), "rb") as f:
            self.assertEqual(f.read(), b"PNG7")
        self.assertTrue(os.path.exists(os.path.join(folder, "bold.ttf")))
        s.touch("down", 0.5, 1.5)
        s.touch("up", 0.5, 0.5)
        self.assertEqual(deck.touches.get(timeout=5), {"kind": "down", "x": 0.5, "y": 1.0})
        self.assertEqual(deck.touches.get(timeout=5)["kind"], "up")
        s.stop()
        self.assertEqual(events.get(timeout=5), ("end", None))

    def test_direct(self):
        self.assertEqual(deckscreen.direct("http://localhost:8080/api?x=1"),
                         ("http://127.0.0.1:8080/api?x=1", {"Host": "localhost:8080"}))
        self.assertEqual(deckscreen.direct("http://127.0.0.1/api"), ("http://127.0.0.1/api", {}))
        self.assertEqual(deckscreen.direct("https://deck.example/api"), ("https://deck.example/api", {}))

    def test_stream_by_name(self):
        # The stream connects to the address, and sends the name as the Host.
        deck = self._deck()
        port = deck.server_address[1]
        events = queue.Queue()
        s = deckscreen.DeckStream("http://localhost:%d" % port, KEY, (800, 480), tempfile.mkdtemp(),
                                  on_session=lambda w, h: events.put("session"), on_frame=lambda t: events.put("frame"),
                                  on_end=lambda e: events.put(("end", e)))
        s.start()
        self.assertEqual(events.get(timeout=5), "session")
        self.assertEqual(events.get(timeout=5), "frame")
        s.stop()
        self.assertEqual(deck.hosts, {"localhost:%d" % port})

    def test_wrong_key(self):
        deck = self._deck()
        s, events, _ = self._stream(deck, "wrong")
        s.start()
        kind, error = events.get(timeout=5)
        self.assertEqual(kind, "end")
        self.assertIsInstance(error, deckscreen.Refused)


if __name__ == "__main__":
    unittest.main()
