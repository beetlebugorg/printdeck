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

from . import logic

POLL_SECONDS = 10
BUTTON = "Print to Printdeck"
SENT_SECONDS = 300  # the time from a write to the change to Monitor, with the upload dialog


def _message(text):
    Message(text, title="Printdeck").show()


class PrintdeckPlugin(Extension):
    def __init__(self, stage=None):
        super().__init__()
        self._stage = stage  # the Printdeck tab, or None
        if stage:
            stage.command = self._command
            stage.showPrint = self._showPrint
        self._printerAddr = None  # the base URL and the API key of the last answer
        self._status = None  # the last answer of the Printdeck
        self._connected = False
        self._greeted = False
        self._offer = None  # the message that offers to show the print
        self._loading = None  # the file of the print that Cura loads, and the caution setting to restore
        self._roll = None  # the roll and the material of the last answer, to see a change
        self._warned = None  # the roll and the grams of the last warning
        self._layer = None  # the job and the layer that the Preview shows
        self._job = None  # the file that prints
        self._sent = 0.0  # the time of the last write, to see the change to Monitor after it
        self._stop = threading.Event()

        app = CuraApplication.getInstance()
        app.engineCreatedSignal.connect(self._start)
        app.applicationShuttingDown.connect(self._stop.set)

    def _start(self):
        app = CuraApplication.getInstance()
        app.getPrintInformation().materialWeightsChanged.connect(self._checkRoll)
        app.getOutputDeviceManager().writeStarted.connect(self._tagMaterial)
        app.fileCompleted.connect(self._onFileCompleted)
        app.getController().activeStageChanged.connect(self._onStage)
        threading.Thread(target=self._poll, name="Printdeck", daemon=True).start()
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

    def _poll(self):
        while not self._stop.wait(POLL_SECONDS):
            app = CuraApplication.getInstance()
            printer = self._printer()
            status = None
            if printer:
                base, key = printer
                try:
                    req = urllib.request.Request(base.rstrip("/") + "/api/plugin/printdeck", headers={"X-Api-Key": key})
                    with urllib.request.urlopen(req, timeout=5) as r:
                        status = logic.parse_status(json.loads(r.read().decode()))
                except Exception as e:  # not a Printdeck, or the Printdeck is off
                    Logger.log("d", "Printdeck: %s", e)
            self._printerAddr = printer
            app.callLater(self._update, status)

    def _update(self, status):
        self._status = status
        if self._stage:
            base, key = self._printerAddr or ("", "")
            self._stage.setStatus(status, base, key)
        if not status:
            self._connected = False
            return
        if not self._connected:
            self._connected = True
            Logger.log("i", "Printdeck: connected")
            if not self._greeted:  # once each time Cura starts
                self._greeted = True
                _message(logic.connected_text(status))
                self._offerPrint()
        self._nameButton()
        self._followRoll()
        self._checkRoll()
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
                req = urllib.request.Request(url, headers={"X-Api-Key": key})
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
        for a G-code file: the Printdeck tab has the time of the print."""
        if not self._loading or os.path.basename(name) != os.path.basename(self._loading[0]):
            return
        _, caution = self._loading
        self._loading = None
        CuraApplication.getInstance().getPreferences().setValue("gcodereader/show_caution", caution)

    def _command(self, command, action):
        """Sends a job command of the OctoPrint API (pause, resume, cancel) on a thread."""
        printer = self._printer()
        if not printer:
            return
        base, key = printer
        body = {"command": command}
        if action:
            body["action"] = action

        def send():
            try:
                req = urllib.request.Request(base.rstrip("/") + "/api/job", data=json.dumps(body).encode(), method="POST",
                                             headers={"X-Api-Key": key, "Content-Type": "application/json"})
                urllib.request.urlopen(req, timeout=10).close()
            except Exception as e:
                Logger.log("w", "Printdeck: %s %s: %s", command, action, e)
                CuraApplication.getInstance().callLater(_message, "The Printdeck did not take the command: %s" % e)

        threading.Thread(target=send, daemon=True).start()

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

    def _followRoll(self):
        """When the roll in the Printdeck changes, Cura changes to its material."""
        material = self._status["material"]
        spool = self._status["spool"]
        roll = (spool.get("id") if spool else "", material)
        if roll == self._roll or not material:
            self._roll = roll
            return
        self._roll = roll
        _, extruder = self._extruder()
        if not extruder:
            return
        name = logic.spool_name(spool) if spool else material
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
        if not self._status or not self._status["spool"]:
            return
        weights = CuraApplication.getInstance().getPrintInformation().materialWeights
        needed = float(weights[0]) if weights else 0
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
        first = self._layer is None or self._layer[0] != self._job
        self._layer = (self._job, layer)
        view = app.getController().getView("SimulationView")
        if view and view.getMaxLayers() > 0:
            view.setLayer(min(view.getMaxLayers(), layer))
            if first:
                _message("The Preview shows the layer that prints on the Printdeck.")
