# The Printdeck plugin for Cura. It reads the roll of filament and the print from the Printdeck,
# through the printer of the OctoPrint connection plugin, and:
#
# - offers to use the material of the roll when Cura uses another material,
# - warns when the sliced model needs more filament than the roll has,
# - moves the layer of the Preview to the layer that prints, when Cura has the same file,
# - writes the material of Cura into the header of each G-code file, so that the Printdeck asks
#   before it prints a file for another material.
#
# It asks the Printdeck every 10 seconds, on a thread, and changes Cura on the main thread.

import json
import threading
import urllib.request

from UM.Extension import Extension
from UM.Logger import Logger
from UM.Message import Message
from cura.CuraApplication import CuraApplication

from . import logic

POLL_SECONDS = 10


class PrintdeckPlugin(Extension):
    def __init__(self):
        super().__init__()
        self.setMenuName("Printdeck")
        self.addMenuItem("Use the material of the roll", self._useRollMaterial)
        self.addMenuItem("Follow the print in Preview", self._toggleFollow)

        self._status = None  # the last answer of the Printdeck
        self._follow = False
        self._asked = None  # the roll and the material of the last question, so it is asked once
        self._warned = None  # the roll and the grams of the last warning
        self._stop = threading.Event()

        app = CuraApplication.getInstance()
        app.engineCreatedSignal.connect(self._start)
        app.applicationShuttingDown.connect(self._stop.set)

    def _start(self):
        app = CuraApplication.getInstance()
        app.getPrintInformation().materialWeightsChanged.connect(self._checkRoll)
        app.getOutputDeviceManager().writeStarted.connect(self._tagMaterial)
        threading.Thread(target=self._poll, name="Printdeck", daemon=True).start()

    # The connection.

    def _printer(self):
        """Returns the base URL and the API key of the connected OctoPrint device, or None."""
        for device in CuraApplication.getInstance().getOutputDeviceManager().getOutputDevices():
            base = getattr(device, "_base_url", None)
            key = getattr(device, "_api_key", None)
            if base and key:
                if isinstance(key, bytes):
                    key = key.decode()
                return base, key
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
            app.callLater(self._update, status)

    def _update(self, status):
        self._status = status
        if not status:
            return
        self._askMaterial()
        self._checkRoll()
        self._followPrint()

    # The material.

    def _extruder(self):
        stack = CuraApplication.getInstance().getGlobalContainerStack()
        if not stack or not stack.extruderList:
            return None, None
        return stack, stack.extruderList[0]

    def _rollMaterial(self):
        """Returns the base file of the Cura material for the roll, or None."""
        from cura.Machines.ContainerTree import ContainerTree

        stack, extruder = self._extruder()
        if not extruder or not self._status:
            return None
        try:
            machine = ContainerTree.getInstance().machines[stack.definition.getId()]
            nodes = machine.variants[extruder.variant.getName()].materials.values()
        except (KeyError, AttributeError):
            return None
        return logic.choose_material(self._status["material"], [(n.base_file, n.material_type, n.brand) for n in nodes])

    def _askMaterial(self):
        stack, extruder = self._extruder()
        material = self._status["material"]
        if not extruder or not material:
            return
        current = extruder.material.getMetaDataEntry("material") or ""
        spool = self._status["spool"]
        key = (spool.get("id") if spool else "", material, current)
        if logic.same_material(current, material) or key == self._asked:
            return
        self._asked = key
        name = logic.spool_name(spool) if spool else material
        message = Message("The Printdeck has %s (%s). Cura prints with %s." % (name, material, current or "no material"),
                          lifetime=0, title="Printdeck")
        message.addAction("use", "Use %s" % material, "", "Choose the material of the roll")
        message.actionTriggered.connect(lambda m, action: (m.hide(), self._useRollMaterial()))
        message.show()

    def _useRollMaterial(self):
        base_file = self._rollMaterial()
        if not base_file:
            material = self._status["material"] if self._status else ""
            Message("Cura has no %s material for this printer." % (material or "matching"), title="Printdeck").show()
            return
        CuraApplication.getInstance().getMachineManager().setMaterialById("0", base_file)

    def _tagMaterial(self, output_device=None):
        """Adds the material of the first extruder to the header of the G-code that Cura writes."""
        app = CuraApplication.getInstance()
        _, extruder = self._extruder()
        scene = app.getController().getScene()
        gcode_dict = getattr(scene, "gcode_dict", None)
        if not extruder or not gcode_dict:
            return
        plate = app.getMultiBuildPlateModel().activeBuildPlate
        gcode_list = gcode_dict.get(plate)
        if not gcode_list or ";PRINTDECK_MATERIAL:" in gcode_list[0]:
            return
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

    def _toggleFollow(self):
        self._follow = not self._follow
        if self._follow:
            Message("The Preview follows the layer that prints when Cura has the same file.", title="Printdeck").show()
            self._followPrint()

    def _followPrint(self):
        if not self._follow or not self._status or not self._status["job"]:
            return
        app = CuraApplication.getInstance()
        job = self._status["job"]
        if not logic.job_matches(app.getPrintInformation().jobName, job.get("file", "")):
            return
        view = app.getController().getView("SimulationView")
        if view and view.getMaxLayers() > 0:
            view.setLayer(min(view.getMaxLayers(), logic.preview_layer(job)))
