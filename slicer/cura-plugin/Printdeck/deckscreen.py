# The deck UI in Cura: the client of the remote screen of the deck (deck/internal/remote). The deck
# starts a session of its UI for Cura and sends the drawing commands of each frame as Server-Sent
# Events (GET /ui/events). Cura draws them on a QML Canvas, and sends the clicks back (POST
# /ui/touch). The API key of the OctoPrint connection opens the /ui/ requests. This module needs no
# Cura, so the tests can use it.

import http.client
import json
import os
import queue
import socket
import threading
import urllib.parse

# The sizes of the deck UI, as in page.html of the deck: a wide area gets the wide UI, 800 x 480. A
# tall area gets the tall UI, 480 wide, and a height from 800 to 1200 for the shape of the area.
WIDE = (800, 480)
TALL_W, MIN_H, MAX_H = 480, 800, 1200

# The time out of a read of the stream, in seconds. The deck sends a ping each 30 seconds.
READ_TIMEOUT = 75
CONNECT_TIMEOUT = 5


def wanted_size(w, h):
    """The size of the deck UI for an area of w x h."""
    if w <= 0 or h <= 0 or w > h:
        return WIDE
    return TALL_W, max(MIN_H, min(MAX_H, round(TALL_W * h / w)))


def image_ids(ops):
    """The numbers of the images that the drawing commands of a frame use."""
    return {op[5] for op in ops if op and op[0] in ("c", "i") and len(op) > 5}


class SSEParser:
    """Turns the lines of a stream of Server-Sent Events into events (name, data)."""

    def __init__(self):
        self._event, self._data = "message", []

    def feed(self, line):
        """Takes one line, without its line end. Returns an event at the end of an event, else None."""
        if line == "":
            if not self._data:
                self._event = "message"
                return None
            out = (self._event, "\n".join(self._data))
            self._event, self._data = "message", []
            return out
        if line.startswith(":"):
            return None  # a comment: the ping of the deck
        field, _, value = line.partition(":")
        if value.startswith(" "):
            value = value[1:]
        if field == "event":
            self._event = value
        elif field == "data":
            self._data.append(value)
        return None


class Refused(Exception):
    """The deck refused the stream: an old deck with no API key on /ui/, or a wrong key."""


class DeckStream:
    """One session of the deck UI. The callbacks run on the threads of the stream; the caller moves
    them to its main thread.

    - on_session(width, height): the session started, with the size of the UI.
    - on_frame(text): a frame, the JSON text of its drawing commands. Its images are in the folder
      of the images at that time.
    - on_end(error): the stream ended. error is None after stop(), a Refused, or another error.
    """

    def __init__(self, base, key, size, folder, on_session, on_frame, on_end):
        u = urllib.parse.urlsplit(base)
        self._https = u.scheme == "https"
        self._host = u.hostname or ""
        self._port = u.port
        self._path = u.path.rstrip("/")
        self._key = key
        self._size = size
        self.folder = folder
        self._on_session, self._on_frame, self._on_end = on_session, on_frame, on_end
        self._session = ""
        self._stopped = threading.Event()
        self._sock = None  # the socket of the stream, to end it from another thread
        self._touches = queue.Queue()

    # The folders of the images and the fonts.

    def image_path(self, n):
        return os.path.join(self.folder, "img", "%d.png" % n)

    def font_path(self, name):
        return os.path.join(self.folder, name)

    def start(self):
        threading.Thread(target=self._run, name="Printdeck screen", daemon=True).start()
        threading.Thread(target=self._send_touches, name="Printdeck touch", daemon=True).start()

    def stop(self):
        """Ends the stream. on_end gets None."""
        self._stopped.set()
        self._touches.put(None)
        sock = self._sock
        if sock:
            try:
                sock.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass

    def touch(self, kind, x, y):
        """Sends a touch: kind "down", "move" or "up", and x, y from 0 to 1 on the UI."""
        self._touches.put((kind, min(1.0, max(0.0, x)), min(1.0, max(0.0, y))))

    # The requests.

    def _connection(self, timeout):
        cls = http.client.HTTPSConnection if self._https else http.client.HTTPConnection
        return cls(self._host, self._port, timeout=timeout)

    def _get(self, path):
        """GET a file of the deck. Returns its bytes, or None."""
        conn = self._connection(CONNECT_TIMEOUT * 2)
        try:
            conn.request("GET", self._path + path, headers={"X-Api-Key": self._key})
            r = conn.getresponse()
            body = r.read()
            return body if r.status == 200 else None
        except (OSError, http.client.HTTPException):
            return None
        finally:
            conn.close()

    def _save(self, path, data):
        os.makedirs(os.path.dirname(path), exist_ok=True)
        tmp = path + ".part"
        with open(tmp, "wb") as f:
            f.write(data)
        os.replace(tmp, path)

    def _fonts(self):
        for name in ("regular.ttf", "bold.ttf"):
            path = self.font_path(name)
            if not os.path.exists(path):
                data = self._get("/ui/font/" + name)
                if data:
                    self._save(path, data)

    def _images(self, ops):
        for n in image_ids(ops):
            path = self.image_path(n)
            if not os.path.exists(path):
                data = self._get("/ui/img/%d" % n)
                if data:
                    self._save(path, data)

    def _run(self):
        error = None
        try:
            self._fonts()
            self._stream()
        except Exception as e:  # the deck is off, or the network failed
            error = e
        if self._stopped.is_set():
            error = None
        self._touches.put(None)
        self._on_end(error)

    def _stream(self):
        conn = self._connection(CONNECT_TIMEOUT)
        try:
            conn.connect()
            conn.sock.settimeout(READ_TIMEOUT)
            self._sock = conn.sock
            if self._stopped.is_set():
                return
            conn.request("GET", "%s/ui/events?w=%d&h=%d" % (self._path, self._size[0], self._size[1]),
                         headers={"X-Api-Key": self._key, "Accept": "text/event-stream"})
            r = conn.getresponse()
            if r.status in (401, 403, 404):
                raise Refused("the deck answered %d" % r.status)
            if r.status != 200:
                raise OSError("the deck answered %d" % r.status)
            parser = SSEParser()
            while not self._stopped.is_set():
                raw = r.readline()
                if not raw:
                    raise OSError("the deck closed the stream")
                ev = parser.feed(raw.decode("utf-8", "replace").rstrip("\r\n"))
                if not ev:
                    continue
                name, data = ev
                if name == "session":
                    parts = data.split()
                    self._session = parts[0]
                    w, h = (int(parts[1]), int(parts[2])) if len(parts) >= 3 else self._size
                    self._on_session(w, h)
                elif name == "message":
                    self._images(json.loads(data))
                    self._on_frame(data)
        finally:
            conn.close()

    def _send_touches(self):
        while True:
            t = self._touches.get()
            if t is None or self._stopped.is_set():
                return
            # A move waits only for the newest position.
            while t[0] == "move":
                try:
                    nxt = self._touches.get_nowait()
                except queue.Empty:
                    break
                if nxt is None:
                    return
                if nxt[0] != "move":
                    self._post(t)
                t = nxt
            self._post(t)

    def _post(self, t):
        if not self._session:
            return
        conn = self._connection(CONNECT_TIMEOUT)
        try:
            body = json.dumps({"kind": t[0], "x": t[1], "y": t[2]})
            conn.request("POST", "%s/ui/touch?s=%s" % (self._path, self._session), body=body,
                         headers={"X-Api-Key": self._key, "Content-Type": "application/json"})
            conn.getresponse().read()
        except (OSError, http.client.HTTPException):
            pass
        finally:
            conn.close()
