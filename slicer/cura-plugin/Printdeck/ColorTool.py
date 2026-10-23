# The Colors tool of Cura: a roll of the Printdeck for the selected models. The file pauses at the
# first layer of those models, and the deck changes the filament to the roll (colors.py). For
# example raised lettering on a part: select the letters, pick the roll of the letters, and print.
#
# The rolls come from the Printdeck that Cura prints to. The roll in the printer at the start is the
# roll of the models with no roll.

from UM.Logger import Logger
from UM.Scene.Selection import Selection
from UM.Tool import Tool
from cura.CuraApplication import CuraApplication
from cura.Scene.CuraSceneNode import CuraSceneNode

from . import colors

# The tool of this Cura, for the extension that changes the G-code.
instance = None

ROLL = "PrintdeckRoll"  # the setting of a scene node: the ID of its roll
ROLL_NAME = "PrintdeckRollName"  # and the name of the roll, as "#3 Navy"


def roll_name(spool):
    """The name of a roll of the status: its number and its name."""
    return "#%s %s" % (spool.get("id", "?"), spool.get("name") or spool.get("material") or "")


class ColorTool(Tool):
    def __init__(self):
        super().__init__()
        global instance
        instance = self
        self.setExposedProperties("Rolls", "Selected", "Loaded", "Plan", "HasSelection")
        Selection.selectionChanged.connect(self.propertyChanged)

    def _status(self):
        from . import PrintdeckPlugin
        p = PrintdeckPlugin.instance
        return p._status if p else None

    def _nodes(self):
        root = CuraApplication.getInstance().getController().getScene().getRoot()
        return [n for n in root.getChildren() if isinstance(n, CuraSceneNode) and n.callDecoration("isSliceable")]

    # The properties of the panel.

    def getRolls(self):
        """The rolls of the Printdeck, for the list: id, name and color."""
        st = self._status() or {}
        return [{"id": str(s.get("id", "")), "name": roll_name(s), "color": s.get("color") or ""}
                for s in st.get("spools") or [] if s.get("id")]

    def getHasSelection(self):
        return Selection.hasSelection()

    def getSelected(self):
        """The roll of the selected models: its name, "Several rolls", or ""."""
        names = {n.getSetting(ROLL_NAME, "") for n in Selection.getAllSelectedObjects()}
        if len(names) > 1:
            return "Several rolls"
        return names.pop() if names else ""

    def getLoaded(self):
        st = self._status() or {}
        sp = st.get("spool")
        return roll_name(sp) if sp else ""

    def getPlan(self):
        """The changes of the print, one line each, for the panel."""
        changes = self.plan()
        if not changes:
            return ""
        return "\n".join("At %.2f mm: load %s" % (z, name) for z, _, name in changes)

    # The actions of the panel.

    def setRoll(self, roll_id):
        """Gives the selected models the roll with the ID; "" removes their roll."""
        name = next((r["name"] for r in self.getRolls() if r["id"] == str(roll_id)), "")
        for node in Selection.getAllSelectedObjects():
            node.setSetting(ROLL, str(roll_id) if name else "")
            node.setSetting(ROLL_NAME, name)
        self.propertyChanged.emit()

    def clearRoll(self):
        self.setRoll("")

    # The G-code.

    def plan(self):
        """The changes of the scene (colors.plan)."""
        groups, rest_top = [], 0.0
        for n in self._nodes():
            box = n.getBoundingBox()
            if not box:
                continue
            roll = n.getSetting(ROLL, "")
            if roll:
                groups.append({"roll": roll, "name": n.getSetting(ROLL_NAME, ""), "bottom": box.bottom, "top": box.top})
            else:
                rest_top = max(rest_top, box.top)
        if not groups:
            return []
        st = self._status() or {}
        sp = st.get("spool")
        start = {"roll": str(sp.get("id")), "name": roll_name(sp)} if sp else None
        return colors.plan(groups, rest_top, start)

    def changeGcode(self, gcode_list):
        """Inserts the color changes. It returns False when the scene has none."""
        changes = self.plan()
        if not changes:
            return False
        for z, roll, name in colors.apply(gcode_list, changes):
            Logger.log("w", "Printdeck: no layer over %.2f mm for the change to roll %s", z, roll)
        return True
