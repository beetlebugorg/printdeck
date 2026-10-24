# The Colors tool of Cura: a roll of the Printdeck for the selected models. The file pauses at the
# first layer of those models, and the deck changes the filament to the roll (colors.py). For
# example raised lettering on a part: select the letters, pick the roll of the letters, and print.
#
# The rolls come from the Printdeck that Cura prints to. The roll in the printer at the start is the
# roll of the models with no roll.

from UM.Application import Application
from UM.Logger import Logger
from UM.Math.Color import Color
from UM.Math.Vector import Vector
from UM.Operations.TranslateOperation import TranslateOperation
from UM.Resources import Resources
from UM.Scene.SceneNode import SceneNode
from UM.Scene.SceneNodeSettings import SceneNodeSettings
from UM.Scene.Selection import Selection
from UM.Tool import Tool
from UM.View.GL.OpenGL import OpenGL
from cura.CuraApplication import CuraApplication
from cura.Scene.CuraSceneNode import CuraSceneNode

from . import colors

# The tool of this Cura, for the extension that changes the G-code.
instance = None

ROLL = "PrintdeckRoll"  # the setting of a scene node: the ID of its roll
ROLL_NAME = "PrintdeckRollName"  # and the name of the roll, as "#3 Navy"


# The tint is this much larger than its model, so that it is drawn over the model.
TINT_SCALE = 1.004


class Tint(SceneNode):
    """The color of the roll of a model in Prepare: a copy of the mesh of the model, a little larger,
    in the color of the roll. Cura draws each model in the color of its extruder, and the printer
    has one extruder. The tint is not sliced (it has no decorators), and it draws nothing in the
    other views."""

    _shader = None

    def __init__(self, parent, color):
        super().__init__(parent)
        self.setName("Printdeck tint")
        self.setSelectable(False)
        self.setCalculateBoundingBox(False)
        data = parent.getMeshData()
        self.setMeshData(data)
        self.color = color
        # Larger around the middle of the mesh: x' = c + s (x - c).
        c = data.getExtents().center
        self.setScale(Vector(TINT_SCALE, TINT_SCALE, TINT_SCALE))
        self.setPosition(c * (1 - TINT_SCALE))

    def render(self, renderer):
        view = Application.getInstance().getController().getActiveView()
        if not view or view.getPluginId() != "SolidView" or not self.getMeshData():
            return True  # drawn in Prepare only
        if not Tint._shader:
            Tint._shader = OpenGL.getInstance().createShaderProgram(Resources.getPath(Resources.Shaders, "overhang.shader"))
            theme = Application.getInstance().getTheme()
            Tint._shader.setUniformValue("u_overhangColor", Color(*theme.getColor("model_overhang").getRgb()))
            Tint._shader.setUniformValue("u_renderError", 0.0)
        c = self.color.lstrip("#")
        rgb = [int(c[i:i + 2], 16) / 255 for i in (0, 2, 4)]
        renderer.queueNode(self, shader=Tint._shader, uniforms={"diffuse_color": rgb + [1.0], "hover_face": -1})
        return True


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
        """Gives the selected models the roll with the ID; "" removes their roll. A model with a roll
        stays at its height, and a model on the plate over another model goes onto its top."""
        roll = next((r for r in self.getRolls() if r["id"] == str(roll_id)), None)
        for node in Selection.getAllSelectedObjects():
            node.setSetting(ROLL, roll["id"] if roll else "")
            node.setSetting(ROLL_NAME, roll["name"] if roll else "")
            if roll:
                node.setSetting(SceneNodeSettings.AutoDropDown, False)
                self._restOnModel(node)
            self._tint(node, roll["color"] if roll else "")
        self.propertyChanged.emit()

    def _tint(self, node, color):
        """Shows the model in the color of its roll, or removes the tint for "" or no color."""
        for child in list(node.getChildren()):
            if isinstance(child, Tint):
                child.setParent(None)
        if color.startswith("#") and len(color) == 7 and node.getMeshData():
            Tint(node, color)

    def _restOnModel(self, node):
        """Puts a model that stands on the plate onto the top of the highest model under it: Cura
        drops each model of a file onto the plate, also letters that were on a part."""
        box = node.getBoundingBox()
        if not box or box.bottom > 0.05:
            return
        under = []
        for other in self._nodes():
            ob = other.getBoundingBox()
            if other is node or not ob or other.getSetting(ROLL, ""):
                continue
            # The footprint of the model is inside the footprint of the other one (x and z of Cura).
            if ob.left <= box.left and box.right <= ob.right and ob.back <= box.back and box.front <= ob.front:
                under.append(ob.top)
        if under:
            TranslateOperation(node, Vector(0, max(under) - box.bottom, 0)).push()

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
