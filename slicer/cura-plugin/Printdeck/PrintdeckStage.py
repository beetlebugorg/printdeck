# The Printdeck tab of Cura: the deck UI, live. The deck draws a session of its UI for Cura, and the
# tab draws its frames and sends the clicks (deckscreen). The stream runs only while the tab shows.
# While it connects, the tab shows the start screen of the deck. PrintdeckPlugin gives it the
# address, the API key, and the answers of the Printdeck.

import os
import shutil
import tempfile
import time
import urllib.parse

from UM.Application import Application
from UM.Logger import Logger
from cura.Stages.CuraStage import CuraStage

try:
    from PyQt6.QtCore import QTimer, QUrl, pyqtProperty, pyqtSignal, pyqtSlot
except ImportError:  # Cura 4 (Cura LulzBot Edition)
    from PyQt5.QtCore import QTimer, QUrl, pyqtProperty, pyqtSignal, pyqtSlot

from . import deckscreen

# The time after the start of Cura before the tab says that Cura has no Printdeck: the OctoPrint
# connection plugin finds the printer first.
FIND_SECONDS = 30
# The wait before a new connection after the stream ended with an error, in milliseconds.
RETRY_MS = 3000


class PrintdeckStage(CuraStage):
    statusChanged = pyqtSignal()
    screenChanged = pyqtSignal()  # the view, the size of the deck UI, and the fonts
    frameChanged = pyqtSignal()

    def __init__(self, parent=None):
        super().__init__(parent)
        self._status = None
        self._base = ""
        self._key = ""
        self.showPrint = None  # set by PrintdeckPlugin: opens the file of the print in the Preview
        self._started = time.monotonic()
        # The deck UI.
        self._stream = None  # the DeckStream, or None
        self._active = False  # the tab shows
        self._live = False  # the stream has a session and a frame
        self._refused = False  # the deck has no UI for slicers, or the key is wrong
        self._frame = ""
        self._screen = deckscreen.WIDE  # the size of the deck UI
        self._area = (0, 0)  # the size of the area of the tab for the UI
        self._folder = tempfile.mkdtemp(prefix="printdeck-")  # the images and the fonts of the UI
        self._retry = QTimer()
        self._retry.setSingleShot(True)
        self._retry.timeout.connect(self._update)
        app = Application.getInstance()
        app.engineCreatedSignal.connect(self._onEngineCreated)
        app.applicationShuttingDown.connect(self._shutdown)

    def _onEngineCreated(self):
        path = Application.getInstance().getPluginRegistry().getPluginPath(self.getPluginId())
        if path:
            self.addDisplayComponent("menu", os.path.join(path, "qml", "PrintdeckMenu.qml"))
            self.addDisplayComponent("main", os.path.join(path, "qml", "PrintdeckMain.qml"))
        Application.getInstance().getController().activeStageChanged.connect(self._onStageChanged)

    def _shutdown(self):
        if self._stream:
            self._stream.stop()
        shutil.rmtree(self._folder, ignore_errors=True)

    def setStatus(self, status, base, key):
        base, key = base or "", key or ""
        if (base, key) != (self._base, self._key):
            self._refused = False  # another printer or key: try the deck UI again
            if self._stream:
                self._stream.stop()
        self._status, self._base, self._key = status, base, key
        self.statusChanged.emit()
        self.screenChanged.emit()
        self._update()

    # The deck UI.

    def _onStageChanged(self):
        stage = Application.getInstance().getController().getActiveStage()
        self._active = stage is self
        self._update()

    def _update(self):
        """Starts the stream when the tab shows, and stops it when it does not."""
        want = self._active and self._base and self._key and not self._refused
        if want and not self._stream and not self._retry.isActive():
            self._screen = deckscreen.wanted_size(*self._area)
            stream = None

            def later(fn, *args):
                Application.getInstance().callLater(fn, stream, *args)

            stream = deckscreen.DeckStream(
                self._base, self._key, self._screen, self._folder,
                on_session=lambda w, h: later(self._onSession, w, h),
                on_frame=lambda text: later(self._onFrame, text),
                on_end=lambda error: later(self._onEnd, error))
            self._stream = stream
            stream.start()
        elif not want and self._stream:
            self._stream.stop()

    def _onSession(self, stream, w, h):
        if stream is self._stream:
            self._screen = (w, h)
            self.screenChanged.emit()

    def _onFrame(self, stream, text):
        if stream is not self._stream:
            return
        self._frame = text
        if not self._live:
            self._live = True
            self.screenChanged.emit()
        self.frameChanged.emit()

    def _onEnd(self, stream, error):
        if stream is not self._stream:
            return
        self._stream = None
        self._live = False
        if error is not None:
            Logger.log("w", "Printdeck: the screen stream ended: %r", error)
        if isinstance(error, deckscreen.Refused):
            self._refused = True
        self.screenChanged.emit()
        if error is not None and not self._refused:
            self._retry.start(RETRY_MS)
        else:
            self._update()

    @pyqtProperty(str, notify=screenChanged)
    def view(self):
        """"live" (the deck UI), "connecting", "refused" (an older deck, or a wrong key), or "none" (no
        Printdeck)."""
        if self._live:
            return "live"
        if self._refused:
            return "refused"
        if self._base or time.monotonic() - self._started < FIND_SECONDS:
            return "connecting"
        return "none"

    @pyqtProperty(str, notify=screenChanged)
    def host(self):
        """The address of the Printdeck, for example printdeck.local."""
        return urllib.parse.urlsplit(self._base).hostname or "" if self._base else ""

    @pyqtProperty(int, notify=screenChanged)
    def screenWidth(self):
        return self._screen[0]

    @pyqtProperty(int, notify=screenChanged)
    def screenHeight(self):
        return self._screen[1]

    @pyqtProperty(str, notify=frameChanged)
    def frame(self):
        """The drawing commands of the newest frame, as JSON."""
        return self._frame

    @pyqtProperty(str, constant=True)
    def imageBase(self):
        """The folder of the images of the deck UI, as a URL with a "/" at the end."""
        return QUrl.fromLocalFile(os.path.join(self._folder, "img")).toString() + "/"

    @pyqtProperty(str, constant=True)
    def fontBase(self):
        """The folder of the fonts of the deck UI, as a URL with a "/" at the end."""
        return QUrl.fromLocalFile(self._folder).toString() + "/"

    @pyqtProperty(bool, notify=screenChanged)
    def fontsReady(self):
        return os.path.exists(os.path.join(self._folder, "bold.ttf"))

    @pyqtSlot(float, float)
    def setArea(self, w, h):
        """The size of the area of the tab for the deck UI. A new shape asks for a new session."""
        self._area = (w, h)
        want = deckscreen.wanted_size(w, h)
        if self._stream and (want[0] != self._screen[0] or abs(want[1] - self._screen[1]) > 8):
            self._stream.stop()  # _onEnd starts a session of the new size

    @pyqtSlot(str, float, float)
    def touch(self, kind, x, y):
        if self._stream:
            self._stream.touch(kind, x, y)

    @pyqtProperty(str, notify=statusChanged)
    def state(self):
        """"printing", "paused", or "ready"."""
        return ((self._status or {}).get("job") or {}).get("state") or "ready"

    @pyqtSlot()
    def openInPreview(self):
        if self.showPrint:
            self.showPrint()
