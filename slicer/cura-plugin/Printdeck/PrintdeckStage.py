# The Printdeck tab of Cura: the screen of the deck, the print, the roll, and the keys Pause, Resume
# and Cancel, in the style of the deck. PrintdeckPlugin gives it the answers of the Printdeck.

import datetime
import os

from UM.Application import Application
from cura.Stages.CuraStage import CuraStage

try:
    from PyQt6.QtCore import pyqtProperty, pyqtSignal, pyqtSlot
except ImportError:  # Cura 4 (Cura LulzBot Edition)
    from PyQt5.QtCore import pyqtProperty, pyqtSignal, pyqtSlot


class PrintdeckStage(CuraStage):
    statusChanged = pyqtSignal()

    def __init__(self, parent=None):
        super().__init__(parent)
        self._status = None
        self._base = ""
        self._key = ""
        self.command = None  # set by PrintdeckPlugin: sends a job command to the Printdeck
        self.showPrint = None  # set by PrintdeckPlugin: opens the file of the print in the Preview
        Application.getInstance().engineCreatedSignal.connect(self._onEngineCreated)

    def _onEngineCreated(self):
        path = Application.getInstance().getPluginRegistry().getPluginPath(self.getPluginId())
        if path:
            self.addDisplayComponent("menu", os.path.join(path, "qml", "PrintdeckMenu.qml"))
            self.addDisplayComponent("main", os.path.join(path, "qml", "PrintdeckMain.qml"))

    def setStatus(self, status, base, key):
        self._status, self._base, self._key = status, base or "", key or ""
        self.statusChanged.emit()

    def _job(self):
        return (self._status or {}).get("job") or {}

    def _spool(self):
        return (self._status or {}).get("spool") or {}

    @pyqtProperty(bool, notify=statusChanged)
    def connected(self):
        return self._status is not None

    @pyqtProperty(str, notify=statusChanged)
    def state(self):
        """"printing", "paused", or "ready"."""
        return self._job().get("state") or "ready"

    @pyqtProperty(str, notify=statusChanged)
    def file(self):
        return (self._job().get("file") or "").split("/")[-1]

    @pyqtProperty(int, notify=statusChanged)
    def layer(self):
        return int(self._job().get("layer") or 0)

    @pyqtProperty(int, notify=statusChanged)
    def layers(self):
        return int(self._job().get("layers") or 0)

    @pyqtProperty(float, notify=statusChanged)
    def progress(self):
        return float(self._job().get("progress") or 0)

    @pyqtProperty(str, notify=statusChanged)
    def timeLeft(self):
        left = float(self._job().get("left", -1) or -1)
        if left < 0:
            return ""
        m = int(left // 60)
        return "%d:%02d" % (m // 60, m % 60)

    @pyqtProperty(str, notify=statusChanged)
    def doneAt(self):
        left = float(self._job().get("left", -1) or -1)
        if left < 0:
            return ""
        t = datetime.datetime.now() + datetime.timedelta(seconds=left)
        return t.strftime("%I:%M %p").lstrip("0")

    @pyqtProperty(str, notify=statusChanged)
    def material(self):
        return (self._status or {}).get("material") or ""

    @pyqtProperty(str, notify=statusChanged)
    def rollName(self):
        s = self._spool()
        return s.get("name") or ""

    @pyqtProperty(float, notify=statusChanged)
    def rollLeft(self):
        return float(self._spool().get("left") or 0)

    @pyqtProperty(float, notify=statusChanged)
    def rollFull(self):
        return float(self._spool().get("full") or 0)

    @pyqtProperty(str, notify=statusChanged)
    def snapshotUrl(self):
        """The screen of the deck as a JPEG, or "" when Cura has no connection to a Printdeck."""
        if not self._status or not self._base:
            return ""
        return "%s/webcam/snapshot?apikey=%s" % (self._base.rstrip("/"), self._key)

    @pyqtSlot()
    def pause(self):
        self._send("pause", "pause")

    @pyqtSlot()
    def resume(self):
        self._send("pause", "resume")

    @pyqtSlot()
    def openInPreview(self):
        if self.showPrint:
            self.showPrint()

    @pyqtSlot()
    def cancel(self):
        self._send("cancel", "")

    def _send(self, command, action):
        if self.command:
            self.command(command, action)
