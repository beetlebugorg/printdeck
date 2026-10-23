# The Printdeck plugin for Cura. It reads the roll of filament and the print from the Printdeck,
# through the printer of the OctoPrint connection plugin. It works by itself, and tells the user
# what it did in a message:
#
# - the print button says "Print to Printdeck",
# - when the roll in the Printdeck changes, Cura changes to the material of the roll,
# - after a slice, it warns when the model needs more filament than the roll has,
# - during a print of the file that Cura has open, the Preview shows the layer that prints,
# - it tells when a print ends,
# - it writes the material of Cura into the header of each G-code file, so that the Printdeck asks
#   before it prints a file for another material.
#
# It asks the Printdeck every 10 seconds, on a thread, and changes Cura on the main thread.

import json
import os
import tempfile
import threading
import time
import urllib.parse
import urllib.request

from UM.Extension import Extension
from UM.Logger import Logger
from UM.Message import Message
from cura.CuraApplication import CuraApplication

from . import deckscreen, logic, playsync

try:
    from PyQt6.QtCore import QObject, pyqtProperty, pyqtSignal
except ImportError:  # Cura 4 (Cura LulzBot Edition)
    from PyQt5.QtCore import QObject, pyqtProperty, pyqtSignal


class RollCard(QObject):
    """The roll card next to the slice button (qml/RollCard.qml): the roll in the Printdeck, its
    filament, and the filament of the sliced model. The texts come from logic.roll_card."""

    changed = pyqtSignal()

    def __init__(self, parent=None):
        super().__init__(parent)
        self._card = None
        self._printer = ""

    def set(self, card, printer=""):
        """Sets the texts of the card, and the name of the printer in Cura."""
        if (card, printer) != (self._card, self._printer):
            self._card, self._printer = card, printer
            self.changed.emit()

    @pyqtProperty(str, notify=changed)
    def printer(self):
        return self._printer

    def _get(self, key, empty):
        return self._card[key] if self._card else empty

    @pyqtProperty(bool, notify=changed)
    def shown(self):
        return self._card is not None

    @pyqtProperty(str, notify=changed)
    def name(self):
        return self._get("name", "")

    @pyqtProperty(str, notify=changed)
    def detail(self):
        return self._get("detail", "")

    @pyqtProperty(str, notify=changed)
    def color(self):
        return self._get("color", "")

    @pyqtProperty(float, notify=changed)
    def fraction(self):
        return self._get("fraction", 0.0)

    @pyqtProperty(str, notify=changed)
    def job(self):
        return self._get("job", "")

    @pyqtProperty(bool, notify=changed)
    def short(self):
        return self._get("short", False)

    @pyqtProperty(str, notify=changed)
    def warn(self):
        return self._get("warn", "")

POLL_SECONDS = 10
# The time between the answers while Play in the Preview follows the printer.
FOLLOW_POLL_SECONDS = 1
# The preference with the favorite materials that the plugin added for the rolls, ";" between them.
ROLL_FAVORITES = "printdeck/roll_favorites"
BUTTON = "Print to Printdeck"
SENT_SECONDS = 300  # the time from a write to the change to Monitor, with the upload dialog


def _message(text):
    Message(text, title="Printdeck").show()


class PrintdeckPlugin(Extension):
    def __init__(self, stage=None):
        super().__init__()
        self._stage = stage  # the Printdeck tab, or None
        if stage:
            stage.showPrint = self._showPrint
        self._printerAddr = None  # the base URL and the API key of the last answer
        self._status = None  # the last answer of the Printdeck
        self._connected = False
        self._greeted = False
        self._offer = None  # the message that offers to show the print
        self._loading = None  # the file of the print that Cura loads, and the caution setting to restore
        self._roll = None  # the roll and the material of the last answer, to see a change
        self._materialsSent = None  # the materials of Cura that the Printdeck has
        self._warned = None  # the roll and the grams of the last warning
        self._layer = None  # the job and the layer that the Preview shows
        self._job = None  # the file that prints
        self._sent = 0.0  # the time of the last write, to see the change to Monitor after it
        # Play in the Preview follows the printer (playsync): the moves of the G-code file that Cura
        # loaded, its name, and the move of the last answer with the time of the answer.
        self._playMap = None
        self._playFile = ""
        self._playAt = None  # (layer, start, monotonic time, paused), or None
        self._following = False  # Play follows the printer: the poll is faster
        self._stop = threading.Event()
        self._wake = threading.Event()  # ends the wait of the poll before its time
        self._card = RollCard()
        self._cardView = None  # the QML item of the card; Cura keeps only the reference that we give it

        app = CuraApplication.getInstance()
        app.engineCreatedSignal.connect(self._start)
        app.applicationShuttingDown.connect(self._shutDown)

    def _start(self):
        app = CuraApplication.getInstance()
        # The favorite materials that the plugin added for the rolls, so that it removes only those.
        app.getPreferences().addPreference(ROLL_FAVORITES, "")
        app.getPrintInformation().materialWeightsChanged.connect(self._checkRoll)
        app.getMachineManager().activeMaterialChanged.connect(self._checkRoll)
        app.getOutputDeviceManager().writeStarted.connect(self._tagMaterial)
        app.fileCompleted.connect(self._onFileCompleted)
        app.getController().activeStageChanged.connect(self._onStage)
        threading.Thread(target=self._poll, name="Printdeck", daemon=True).start()
        # The roll card, next to the slice button.
        path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "qml", "RollCard.qml")
        self._cardView = app.createQmlComponent(path, {"printdeckRoll": self._card})
        if self._cardView:
            app.addAdditionalComponent("saveButton", self._cardView)
        Logger.log("i", "Printdeck: started")

    # The connection.

    def _devices(self):
        """Returns the OctoPrint devices: the ones with a base URL and an API key."""
        out = []
        for device in CuraApplication.getInstance().getOutputDeviceManager().getOutputDevices():
            if getattr(device, "_base_url", None) and getattr(device, "_api_key", None):
                out.append(device)
        return out

    def _printer(self):
        """Returns the base URL and the API key of the connected OctoPrint device, or None."""
        for device in self._devices():
            key = device._api_key
            if isinstance(key, bytes):
                key = key.decode()
            return device._base_url, key
        return None

    def _shutDown(self):
        self._stop.set()
        self._wake.set()

    def _poll(self):
        while not self._stop.is_set():
            app = CuraApplication.getInstance()
            printer = self._printer()
            status = None
            if printer:
                base, key = printer
                try:
                    url, headers = deckscreen.direct(base.rstrip("/") + "/api/plugin/printdeck")
                    req = urllib.request.Request(url, headers=dict(headers, **{"X-Api-Key": key}))
                    with urllib.request.urlopen(req, timeout=5) as r:
                        status = logic.parse_status(json.loads(r.read().decode()))
                except Exception as e:  # not a Printdeck, or the Printdeck is off
                    Logger.log("d", "Printdeck: %s", e)
            self._printerAddr = printer
            app.callLater(self._update, status)
            self._wake.wait(FOLLOW_POLL_SECONDS if self._following else POLL_SECONDS)
            self._wake.clear()

    def _update(self, status):
        self._status = status
        if self._stage:
            base, key = self._printerAddr or ("", "")
            self._stage.setStatus(status, base, key)
        if not status:
            self._connected = False
            self._following = False
            self._card.set(None)
            return
        if not self._connected:
            self._connected = True
            Logger.log("i", "Printdeck: connected")
            if not self._greeted:  # once each time Cura starts; the roll card shows the roll
                self._greeted = True
                self._offerPrint()
        self._nameButton()
        self._sendMaterials()
        self._syncRolls()
        self._followRoll()
        self._checkRoll()
        self._anchorPlay()
        self._watchPlay()
        self._checkFollowing()
        self._followPrint()

    def _offerPrint(self):
        """During a print of a file that Cura does not have open, offers to show it in the Preview."""
        job = self._status["job"]
        app = CuraApplication.getInstance()
        if not job or logic.job_matches(app.getPrintInformation().jobName, job.get("file", "")):
            return
        name = job.get("file", "").split("/")[-1]
        message = Message("The Printdeck prints %s. Cura can show it in the Preview, at the layer that prints." % name,
                          title="Printdeck", lifetime=0)
        message.addAction("show", "Show the print", "", "Open the file of the print in the Preview")
        # A method: the signals of Cura keep only a weak reference, so a lambda is gone before the click.
        message.actionTriggered.connect(self._onOfferAction)
        message.show()
        self._offer = message

    def _onOfferAction(self, message, action):
        message.hide()
        self._showPrint()

    def _showPrint(self):
        """Downloads the file of the print from the Printdeck, and opens it in the Preview."""
        printer = self._printer()
        job = self._status["job"] if self._status else None
        if not printer or not job:
            return
        base, key = printer
        path = job.get("file", "")
        url = base.rstrip("/") + "/downloads/files/local/" + urllib.parse.quote(path)
        target = os.path.join(tempfile.gettempdir(), "printdeck", path.split("/")[-1])

        def download():
            try:
                os.makedirs(os.path.dirname(target), exist_ok=True)
                direct, headers = deckscreen.direct(url)
                req = urllib.request.Request(direct, headers=dict(headers, **{"X-Api-Key": key}))
                with urllib.request.urlopen(req, timeout=120) as r, open(target, "wb") as f:
                    while True:
                        chunk = r.read(1 << 20)
                        if not chunk:
                            break
                        f.write(chunk)
            except Exception as e:
                CuraApplication.getInstance().callLater(_message, "The file of the print did not download: %s" % e)
                return
            CuraApplication.getInstance().callLater(self._open, target)

        _message("Downloading %s from the Printdeck…" % path.split("/")[-1])
        threading.Thread(target=download, daemon=True).start()

    def _open(self, path):
        try:
            from PyQt6.QtCore import QUrl
        except ImportError:
            from PyQt5.QtCore import QUrl
        self._layer = None  # the Preview goes to the layer that prints when the file is loaded
        if self._offer:
            self._offer.hide()
            self._offer = None
        # The file comes from the Printdeck that prints it: no caution about the printer.
        prefs = CuraApplication.getInstance().getPreferences()
        self._loading = (path, prefs.getValue("gcodereader/show_caution"))
        prefs.setValue("gcodereader/show_caution", False)
        CuraApplication.getInstance().readLocalFile(QUrl.fromLocalFile(path), add_to_recent_files=False)

    def _onFileCompleted(self, name):
        """After the file of the print loaded, the caution setting comes back. Cura shows no time
        for a G-code file: the Printdeck tab has the time of the print. A G-code file gets the map
        of its moves, so that Play in the Preview can follow the printer."""
        self._mapFile(name)
        if not self._loading or os.path.basename(name) != os.path.basename(self._loading[0]):
            return
        _, caution = self._loading
        self._loading = None
        CuraApplication.getInstance().getPreferences().setValue("gcodereader/show_caution", caution)

    # Play in the Preview follows the printer.

    def _mapFile(self, name):
        """Reads the moves of a G-code file that Cura loaded, in a thread."""
        self._playMap, self._playFile, self._playAt = None, "", None
        if not name or not name.lower().endswith((".gcode", ".gco", ".g")) or not os.path.exists(name):
            return

        def read():
            try:
                with open(name, "rb") as f:
                    gmap = playsync.GcodeMap.parse(f.read())
            except OSError as e:
                Logger.log("w", "Printdeck: the moves of %s: %s", name, e)
                return
            CuraApplication.getInstance().callLater(self._setMap, name, gmap)

        threading.Thread(target=read, name="Printdeck moves", daemon=True).start()

    def _setMap(self, name, gmap):
        self._playMap, self._playFile = gmap, os.path.basename(name)
        Logger.log("i", "Printdeck: %d moves in %s for Play in the Preview", len(gmap.ends), self._playFile)
        self._anchorPlay()

    def _anchorPlay(self):
        """Keeps the move of the printer from the last answer, when the Preview shows the file that
        prints."""
        job = self._status["job"] if self._status else None
        cura_job = CuraApplication.getInstance().getPrintInformation().jobName
        if (not job or not self._playMap or not logic.job_matches(self._playFile, job.get("file", ""))
                or not logic.job_matches(cura_job, job.get("file", ""))):
            self._playAt = None
            return
        at = self._playMap.locate(playsync.file_pos(job, self._playMap.size))
        if at:
            self._playAt = (at[0], at[1], time.monotonic(), job.get("state") == "paused")

    def _watchPlay(self):
        """Puts the advance of the time of Play in the Preview through _advancePlay, once."""
        view = CuraApplication.getInstance().getController().getView("SimulationView")
        if not view or getattr(view, "_printdeckFollow", False) or not hasattr(view, "advanceTime"):
            return
        advance = view.advanceTime

        def follow(duration):
            if not self._advancePlay(view):
                advance(duration)

        # The Play timer of the Preview calls advanceTime of the view 15 times a second.
        view.advanceTime = follow
        view._printdeckFollow = True

    def _checkFollowing(self):
        """The poll is fast only while Play runs on the file that prints. Cura calls no
        advanceTime while Play stops, so the answers check it too."""
        controller = CuraApplication.getInstance().getController()
        view = controller.getView("SimulationView")
        self._following = (bool(self._playAt) and bool(view) and controller.getActiveView() is view
                           and view.isSimulationRunning())

    def _advancePlay(self, view):
        """While Play runs on the file that prints, sets the Preview to the move of the printer, and
        reports True. Else reports False, and Play runs as in Cura."""
        at = self._playAt
        following = bool(at) and view.isSimulationRunning() and time.monotonic() - at[2] < 4 * POLL_SECONDS
        if following and not self._following:
            self._wake.set()  # the next answer now, then one each FOLLOW_POLL_SECONDS
        self._following = following
        if not following:
            return False
        layer, start, when, paused = at
        same = view.getCurrentLayer() == layer
        if not same:
            view.setLayer(layer)
        elapsed = 0.0 if paused else time.monotonic() - when
        factor = getattr(type(view), "SIMULATION_FACTOR", 2) or 2
        current = view._current_time if same and hasattr(view, "_current_time") else None
        view.setTime(playsync.follow_time(view.cumulativeLineDuration(), start, elapsed, current, factor))
        return True

    def _nameButton(self):
        """Names the print button. The OctoPrint plugin makes a new device after a reconnect, so this
        runs at each answer. Cura reads the name of the button when the active device changes."""
        renamed = False
        for device in self._devices():
            if device.getShortDescription() != BUTTON:
                device.setShortDescription(BUTTON)
                device.setDescription(BUTTON)
                renamed = True
        if renamed:
            Logger.log("i", "Printdeck: the print button says %s", BUTTON)
            CuraApplication.getInstance().getOutputDeviceManager().activeDeviceChanged.emit()

    # The material.

    def _extruder(self):
        stack = CuraApplication.getInstance().getGlobalContainerStack()
        if not stack or not stack.extruderList:
            return None, None
        return stack, stack.extruderList[0]

    def _rollMaterial(self):
        """Returns the base file and the name of the Cura material for the roll, or None."""
        from cura.Machines.ContainerTree import ContainerTree

        stack, extruder = self._extruder()
        if not extruder or not self._status:
            return None
        try:
            machine = ContainerTree.getInstance().machines[stack.definition.getId()]
            nodes = list(machine.variants[extruder.variant.getName()].materials.values())
        except (KeyError, AttributeError):
            return None
        base = logic.choose_material(self._status["material"], [(n.base_file, n.material_type, n.brand) for n in nodes])
        if not base:
            return None
        node = next(n for n in nodes if n.base_file == base)
        name = node.container.getName() if getattr(node, "container", None) else base
        return base, name

    def _materialNodes(self):
        """The material nodes of Cura that fit the printer and the nozzle, or []."""
        from cura.Machines.ContainerTree import ContainerTree

        stack, extruder = self._extruder()
        if not extruder:
            return []
        try:
            machine = ContainerTree.getInstance().machines[stack.definition.getId()]
            return list(machine.variants[extruder.variant.getName()].materials.values())
        except (KeyError, AttributeError):
            return []

    def _sendMaterials(self):
        """Sends the materials of Cura to the Printdeck, for its rolls, when they changed."""
        from cura.Settings.CuraContainerRegistry import CuraContainerRegistry

        app = CuraApplication.getInstance()
        registry = CuraContainerRegistry.getInstance()
        _, extruder = self._extruder()
        current = extruder.material.getMetaDataEntry("base_file") if extruder else ""
        found = []
        diameter = str(int(extruder.approximateMaterialDiameter)) if extruder else ""
        for node in self._materialNodes():
            meta = registry.findInstanceContainersMetadata(id=node.container_id)
            if not meta:
                continue
            meta = meta[0]
            if diameter and str(meta.get("approximate_diameter", diameter)) != diameter:
                continue  # a filament of another diameter
            found.append({"id": node.base_file, "name": meta.get("name", node.base_file),
                          "brand": meta.get("brand", ""), "material": meta.get("material", ""),
                          "color": meta.get("color_code", ""), "readonly": registry.isReadOnly(node.base_file),
                          "density": (meta.get("properties") or {}).get("density")})
        favorites = set((app.getPreferences().getValue("cura/favorite_materials") or "").split(";"))
        materials = logic.cura_material_list(found, favorites, current)
        printer = self._printer()
        if not materials or not printer or (printer, materials) == self._materialsSent:
            return
        self._materialsSent = (printer, materials)
        base, key = printer

        def send():
            try:
                url, headers = deckscreen.direct(base.rstrip("/") + "/api/plugin/printdeck/materials")
                req = urllib.request.Request(url, data=json.dumps(materials).encode(), method="POST",
                                             headers=dict(headers, **{"X-Api-Key": key, "Content-Type": "application/json"}))
                urllib.request.urlopen(req, timeout=10).close()
                Logger.log("i", "Printdeck: sent %d materials", len(materials))
            except Exception as e:  # an older Printdeck, or the network
                Logger.log("w", "Printdeck: the materials: %s", e)
                self._materialsSent = None

        threading.Thread(target=send, daemon=True).start()

    def _setSpoolCost(self, base_file, spool):
        """Gives the material the price and the grams of the roll, for the cost of a print in Cura."""
        from cura.Settings.CuraContainerRegistry import CuraContainerRegistry

        meta = CuraContainerRegistry.getInstance().findInstanceContainersMetadata(id=base_file)
        guid = meta[0].get("GUID") if meta else None
        if not guid or not spool:
            return
        prefs = CuraApplication.getInstance().getPreferences()
        old = prefs.getValue("cura/material_settings")
        new = logic.spool_settings(old, guid, spool)
        if new != old:
            prefs.setValue("cura/material_settings", new)

    def _madeMaterials(self):
        """The materials that the plugin made for rolls: a dict from the ID of the roll to the
        metadata of the root material."""
        from cura.Settings.CuraContainerRegistry import CuraContainerRegistry

        made = {}
        for m in CuraContainerRegistry.getInstance().findInstanceContainersMetadata(type="material"):
            rid = m.get("printdeck_roll")
            if rid and m.get("base_file") == m.get("id"):
                made.setdefault(rid, m)
        return made

    def _newRollMaterial(self, spool):
        """Makes a material of Cura for a roll: a copy of the material of Cura now when it has the
        type of the roll, else of the generic one, with the name and the color of the roll. Returns
        the base file of the new material and the name of the material that it copies, or None."""
        import uuid

        from cura.Settings.CuraContainerRegistry import CuraContainerRegistry

        app = CuraApplication.getInstance()
        registry = CuraContainerRegistry.getInstance()
        rid = spool["id"]
        name = logic.spool_name(spool)
        material = spool.get("material") or ""
        _, extruder = self._extruder()
        if not extruder:
            return None
        nodes = self._materialNodes()
        base = logic.choose_base(material, extruder.material.getMetaDataEntry("base_file"),
                                 extruder.material.getMetaDataEntry("material") or "",
                                 [(n.base_file, n.material_type, n.brand) for n in nodes])
        node = next((n for n in nodes if n.base_file == base), None)
        if not node:
            Logger.log("w", "Printdeck: no %s material of Cura for the roll %s", material, name)
            return None
        meta = {"name": name, "brand": logic.roll_brand(node.brand), "GUID": str(uuid.uuid4()), "printdeck_roll": rid}
        color = spool.get("color") or logic.color_from_name(name)
        if color:
            meta["color_code"], meta["color_name"] = color, name
        meta["printdeck_color"] = (spool.get("color") or "").lower()
        base_id = app.getMaterialManagementModel().duplicateMaterialByBaseFile(
            base, registry.uniqueName("printdeck_" + logic.material_slug(name)), meta)
        if not base_id:
            return None
        Logger.log("i", "Printdeck: made the material %s for the roll %s from %s", base_id, rid, base)
        return base_id, node.container.getName()

    def _syncRolls(self):
        """Gives each roll of the Printdeck a material of Cura, keeps the name, the color and the
        cost of the materials that the plugin made in step with the rolls, and puts the materials of
        the rolls in the favorites, at the top of the material menu. An older Printdeck sends no
        list of rolls: then the plugin does nothing here."""
        from cura.Settings.CuraContainerRegistry import CuraContainerRegistry

        spools = self._status.get("spools")
        if spools is None:
            return
        registry = CuraContainerRegistry.getInstance()
        made = self._madeMaterials()
        existing = {m["id"] for m in registry.findInstanceContainersMetadata(type="material")
                    if m.get("base_file") == m.get("id")}
        wanted, new = [], 0
        for sp, action, base_id in logic.roll_plan(spools, existing, {rid: m["id"] for rid, m in made.items()}):
            if action == "make":
                # One new material in each answer of the Printdeck: Cura adds a material to its tree
                # in its event loop, and many at once make it slow.
                if new:
                    continue
                new += 1
                result = self._newRollMaterial(sp)
                if not result:
                    continue
                base_id = result[0]
                meta = (registry.findInstanceContainersMetadata(id=base_id) or [{}])[0]
                self._linkRoll(sp["id"], base_id, meta.get("color_code", ""))
            elif action == "link":
                self._linkRoll(sp["id"], base_id, made[sp["id"]].get("color_code", ""))
            mine = made.get(sp["id"])
            if mine and mine["id"] == base_id:
                self._followRollName(base_id, mine, sp)
            self._setSpoolCost(base_id, sp)
            wanted.append(base_id)
        self._setRollFavorites(wanted)

    def _followRollName(self, base_id, meta, spool):
        """Gives a material that the plugin made the name of its roll, and keeps the color of the
        roll and of the material the same (logic.roll_material_changes)."""
        from cura.Settings.CuraContainerRegistry import CuraContainerRegistry

        changes, to_deck = logic.roll_material_changes(meta, spool)
        if to_deck:
            self._linkRoll(spool["id"], base_id, to_deck)
        if not changes:
            return
        containers = CuraContainerRegistry.getInstance().findContainers(id=base_id)
        if not containers:
            return
        root = containers[0]
        if "name" in changes:
            root.setName(changes.pop("name"))
        for key, value in changes.items():
            root.setMetaDataEntry(key, value)
        Logger.log("i", "Printdeck: the material %s follows its roll %s", base_id, spool.get("id"))

    def _setRollFavorites(self, wanted):
        """Puts the materials of the rolls in the favorites of Cura, and takes out the materials
        that the plugin added for rolls that are gone."""
        prefs = CuraApplication.getInstance().getPreferences()
        favorites = (prefs.getValue("cura/favorite_materials") or "").split(";")
        added = [f for f in (prefs.getValue(ROLL_FAVORITES) or "").split(";") if f]
        new, mine = logic.roll_favorites(favorites, added, wanted)
        if new != [f for f in favorites if f]:
            prefs.setValue("cura/favorite_materials", ";".join(new))
        if mine != sorted(set(added)):
            prefs.setValue(ROLL_FAVORITES, ";".join(mine))

    def _makeRollMaterial(self, spool):
        """Selects the material of Cura of the roll in the printer: the material that the plugin
        made for the roll before, or a new one. The Printdeck then links the roll to it."""
        from cura.Settings.CuraContainerRegistry import CuraContainerRegistry

        app = CuraApplication.getInstance()
        registry = CuraContainerRegistry.getInstance()
        rid = spool["id"]
        name = logic.spool_name(spool)
        material = spool.get("material") or ""
        made = self._madeMaterials().get(rid)
        if made:
            base_id, copied = made["id"], None
        else:
            result = self._newRollMaterial(spool)
            if not result:
                _message("The Printdeck has %s. Cura has no %s material for this printer to start from." % (name, material))
                return
            base_id, copied = result
        self._setSpoolCost(base_id, spool)
        color = (registry.findInstanceContainersMetadata(id=base_id) or [{}])[0].get("color_code", "")
        # Cura adds the new material to its tree in its event loop: select it a moment later.
        from PyQt6.QtCore import QTimer

        def select():
            app.getMachineManager().setMaterialById("0", base_id)
            if copied:
                _message("The Printdeck has the roll %s. Cura made the material %s from %s, and prints with it."
                         % (name, name, copied))
            else:
                _message("The Printdeck has %s. Cura now prints with %s." % (name, name))

        QTimer.singleShot(1000, select)
        self._linkRoll(rid, base_id, color)

    def _linkRoll(self, rid, base_id, color):
        """Tells the Printdeck the material of Cura of a roll."""
        printer = self._printer()
        if not printer:
            return
        base, key = printer
        body = json.dumps({"id": rid, "cura_material": base_id, "color": color}).encode()

        def send():
            try:
                url, headers = deckscreen.direct(base.rstrip("/") + "/api/plugin/printdeck/roll")
                req = urllib.request.Request(url, data=body, method="POST",
                                             headers=dict(headers, **{"X-Api-Key": key, "Content-Type": "application/json"}))
                urllib.request.urlopen(req, timeout=10).close()
            except Exception as e:  # an older Printdeck, or the network
                Logger.log("w", "Printdeck: the material of the roll %s: %s", rid, e)

        threading.Thread(target=send, daemon=True).start()

    def _followRoll(self):
        """When the roll in the Printdeck changes, Cura changes to its material: the material of
        Cura of the roll, or else a material of the same type."""
        material = self._status["material"]
        spool = self._status["spool"]
        cura = (spool.get("cura_material") or "") if spool else ""
        roll = (spool.get("id") if spool else "", material, cura)
        if roll == self._roll or not material:
            self._roll = roll
            return
        self._roll = roll
        _, extruder = self._extruder()
        if not extruder:
            return
        name = logic.spool_name(spool) if spool else material
        node = next((n for n in self._materialNodes() if n.base_file == cura), None) if cura else None
        if spool and spool.get("id") and not node:
            # The roll has no material of Cura yet, or Cura has it no more: the plugin makes one.
            self._makeRollMaterial(spool)
            return
        if node:
            self._setSpoolCost(cura, spool)
            if extruder.material.getMetaDataEntry("base_file") == cura:
                return
            CuraApplication.getInstance().getMachineManager().setMaterialById("0", cura)
            _message("The Printdeck has %s. Cura now prints with %s." % (name, node.container.getName()))
            return
        if name != material:
            name = "%s (%s)" % (name, material)
        current = extruder.material.getMetaDataEntry("material") or ""
        if logic.same_material(current, material):
            return
        found = self._rollMaterial()
        if not found:
            _message("The Printdeck has %s. Cura has no %s material for this printer." % (name, material))
            return
        base, label = found
        CuraApplication.getInstance().getMachineManager().setMaterialById("0", base)
        _message("The Printdeck has %s. Cura now prints with %s." % (name, label))

    def _onStage(self):
        """The OctoPrint plugin opens Monitor when it sends a print. After a write, the Printdeck tab
        opens in its place: it shows the print in the style of the deck."""
        controller = CuraApplication.getInstance().getController()
        stage = controller.getActiveStage()
        if not self._stage or not stage or stage.getPluginId() != "MonitorStage":
            return
        if time.monotonic() - self._sent > SENT_SECONDS:
            return  # the user opened Monitor
        self._sent = 0.0
        CuraApplication.getInstance().callLater(controller.setActiveStage, self._stage.getPluginId())

    def _tagMaterial(self, output_device=None):
        """Adds the material of the first extruder to the header of the G-code that Cura writes."""
        app = CuraApplication.getInstance()
        self._sent = time.monotonic()
        _, extruder = self._extruder()
        scene = app.getController().getScene()
        gcode_dict = getattr(scene, "gcode_dict", None)
        if not extruder or not gcode_dict:
            return
        plate = app.getMultiBuildPlateModel().activeBuildPlate
        gcode_list = gcode_dict.get(plate)
        if not gcode_list or ";PRINTDECK_MATERIAL:" in gcode_list[0]:
            return
        # A print on a part: the pause and the height check (OnPartTool).
        from . import OnPartTool
        if OnPartTool.instance and ";PRINTDECK_ONPART" not in gcode_list[0]:
            if OnPartTool.instance.changeGcode(gcode_list):
                gcode_list[0] += ";PRINTDECK_ONPART\n"
        material = extruder.material
        lines = logic.material_lines(material.getMetaDataEntry("material") or "", material.getName())
        if lines:
            # At the end of the header: some programs read ;FLAVOR on the first line.
            if not gcode_list[0].endswith("\n"):
                gcode_list[0] += "\n"
            gcode_list[0] += lines
            gcode_dict[plate] = gcode_list
            setattr(scene, "gcode_dict", gcode_dict)

    # The roll.

    def _checkRoll(self):
        weights = CuraApplication.getInstance().getPrintInformation().materialWeights
        needed = float(weights[0]) if weights else 0
        stack, extruder = self._extruder()
        cura = None
        if extruder:
            m = extruder.material
            cura = {"base": m.getMetaDataEntry("base_file") or "", "name": m.getName(),
                    "material": m.getMetaDataEntry("material") or ""}
        self._card.set(logic.roll_card(self._status, needed, cura), stack.getName() if stack else "")
        if not self._status or not self._status["spool"]:
            return
        text = logic.short_roll(needed, self._status["spool"])
        key = (self._status["spool"].get("id"), round(needed))
        if text and key != self._warned:
            self._warned = key
            Message(text, title="Printdeck", lifetime=0).show()

    # The print.

    def _followPrint(self):
        app = CuraApplication.getInstance()
        job = self._status["job"]
        if not job:
            if self._job:
                _message("The print of %s ended." % self._job)
            self._job, self._layer = None, None
            return
        self._job = job.get("file", "").split("/")[-1]
        if not logic.job_matches(app.getPrintInformation().jobName, job.get("file", "")):
            return
        layer = logic.preview_layer(job)
        if (self._job, layer) == self._layer:
            return  # the same layer: the user can look at other layers until the next one
        self._layer = (self._job, layer)
        view = app.getController().getView("SimulationView")
        if view and view.getMaxLayers() > 0 and not (self._following and view.isSimulationRunning()):
            view.setLayer(min(view.getMaxLayers(), layer))
