# The Print on a part tool of Cura. A part that is printed already (the base) goes into a pocket on
# the bed, and Cura prints new models on it, for example a logo in another color on a lid:
#
# 1. Open the base and the new models. Lay the base on the bed with the face for the new models up.
# 2. Select the base, and tap Set as base. Cura does not print it, and puts no support in it. The
#    tool adds the pocket: a low wall around the base on the bed.
# 3. Select a new model, and tap Place on the base: it goes to the top of the base, and stays there.
#    Move it as usual. Its height in the Move tool can go into a pocket of the base.
# 4. Print. The file prints the pocket, then pauses: put the base in the pocket, check its height on
#    the deck, and the new models print on it.
#
# Place as in the files puts the selected models where they are relative to the base in their files:
# files from one assembly (for example the panel and its logo inlays) share one frame. Cura centers
# each mesh when it loads it, and keeps the offset (MeshData.getCenterPosition).
#
# Add text puts raised lettering on the top of the base, at its middle: move and turn it as usual.
#
# For the next color, the pocket is on the bed already: check "The pocket is on the bed". The file
# then has no pocket and starts with no prime line on the bed (onpart.on_part_start).

import numpy

from UM.Application import Application
from UM.Logger import Logger
from UM.Math.Vector import Vector
from UM.Mesh.MeshBuilder import MeshBuilder
from UM.Operations.AddSceneNodeOperation import AddSceneNodeOperation
from UM.Operations.RemoveSceneNodeOperation import RemoveSceneNodeOperation
from UM.Operations.TranslateOperation import TranslateOperation
from UM.Scene.Selection import Selection
from UM.Scene.SceneNodeSettings import SceneNodeSettings
from UM.Settings.SettingInstance import SettingInstance
from UM.Tool import Tool
from cura.CuraApplication import CuraApplication
from cura.Scene.BuildPlateDecorator import BuildPlateDecorator
from cura.Scene.CuraSceneNode import CuraSceneNode
from cura.Scene.SliceableObjectDecorator import SliceableObjectDecorator

from . import onpart

# The tool of this Cura, for the extension that changes the G-code.
instance = None

BASE = "PrintdeckBase"  # the setting of the scene node of the base
POCKET = "PrintdeckPocket"  # the setting of the scene node of the pocket


class OnPartTool(Tool):
    def __init__(self):
        super().__init__()
        global instance
        instance = self
        self._message = ""
        self._pocketOnBed = False
        self._clearance = onpart.CLEARANCE
        self._wallHeight = onpart.HEIGHT
        self._pushFree = None  # the "Ensure models are kept apart" preference before a base was set
        self._watched = None  # the base whose moves move the pocket
        self._pocketLater = False
        self._text = ""
        self._textSize = 8.0  # mm, the size of the font
        self._textDepth = 0.6  # mm, the height of the letters over the face
        self.setExposedProperties("HasBase", "BaseName", "Message", "PocketOnBed", "Clearance", "WallHeight", "Warning",
                                  "Text", "TextSize", "TextDepth")

    # The scene.

    def _nodes(self):
        root = CuraApplication.getInstance().getController().getScene().getRoot()
        return [n for n in root.getChildren() if isinstance(n, CuraSceneNode) and n.callDecoration("isSliceable")]

    def base(self):
        return next((n for n in self._nodes() if n.getSetting(BASE, False)), None)

    def pocket(self):
        return next((n for n in self._nodes() if n.getSetting(POCKET, False)), None)

    def placed(self):
        """The models on the base: the printed models that do not stand on the bed."""
        base = self.base()
        if not base:
            return []
        return [n for n in self._nodes() if n is not base and not n.getSetting(POCKET, False)
                and n.getBoundingBox() and n.getBoundingBox().bottom > 1.0]

    # The properties of the panel.

    def getHasBase(self):
        return self.base() is not None

    def getBaseName(self):
        b = self.base()
        return b.getName() if b else ""

    def getMessage(self):
        if self._message:
            return self._message
        name = self.getBaseName().rsplit(".", 1)[0] or "part"
        return "Put the %s in the pocket on the bed." % name

    def setMessage(self, value):
        if value != self._message:
            self._message = value
            self.propertyChanged.emit()

    def getPocketOnBed(self):
        return self._pocketOnBed

    def setPocketOnBed(self, value):
        value = bool(value)
        if value == self._pocketOnBed:
            return
        self._pocketOnBed = value
        self._setupSettings()
        self.updatePocket()
        self.propertyChanged.emit()

    def getClearance(self):
        return self._clearance

    def setClearance(self, value):
        self._clearance = max(0.0, float(value))
        self.updatePocket()
        self.propertyChanged.emit()

    def getWallHeight(self):
        return self._wallHeight

    def setWallHeight(self, value):
        self._wallHeight = max(1.0, float(value))
        self.updatePocket()
        self.propertyChanged.emit()

    def getWarning(self):
        """A warning about the place of the base, or "". The start of the printer prints its prime
        line at the front edge, and moves along the left edge at Z 10."""
        base = self.base()
        if not base or self._pocketOnBed:
            return ""
        x0, z0, x1, z1 = self._footprint(base)
        gx, gy, _ = onpart.to_gcode(x0, 0, z1, *self._machine()[:2])
        if gy < 40 or gx < 40:
            return "Move the base away from the front and the left edge: the start of the print primes there."
        return ""

    def getText(self):
        return self._text

    def setText(self, value):
        self._text = str(value)
        self.propertyChanged.emit()

    def getTextSize(self):
        return self._textSize

    def setTextSize(self, value):
        self._textSize = max(2.0, float(value))
        self.propertyChanged.emit()

    def getTextDepth(self):
        return self._textDepth

    def setTextDepth(self, value):
        self._textDepth = max(0.2, float(value))
        self.propertyChanged.emit()

    # The actions of the panel.

    def addText(self):
        """Adds raised lettering on the top of the base, at the middle of the base. Qt draws the
        text at 0.1 mm a pixel, and each run of dark pixels becomes a box."""
        base = self.base()
        text = self._text.strip()
        if not base or not text:
            return
        try:
            from PyQt6.QtGui import QColor, QFont, QFontMetricsF, QImage, QPainter
            gray = QImage.Format.Format_Grayscale8
        except ImportError:
            from PyQt5.QtGui import QColor, QFont, QFontMetricsF, QImage, QPainter
            gray = QImage.Format_Grayscale8
        px = 0.1
        font = QFont("Sans Serif")
        font.setPixelSize(max(8, round(self._textSize / px)))
        font.setBold(True)
        fm = QFontMetricsF(font)
        w, h = int(fm.horizontalAdvance(text)) + 4, int(fm.height()) + 4
        image = QImage(w, h, gray)
        image.fill(QColor(255, 255, 255))
        painter = QPainter(image)
        painter.setFont(font)
        painter.setPen(QColor(0, 0, 0))
        painter.drawText(2, int(fm.ascent()) + 2, text)
        painter.end()
        rows = [[image.pixelColor(x, y).red() < 128 for x in range(w)] for y in range(h)]
        rects = onpart.bitmap_rects(rows)
        if not rects:
            return
        b = base.getBoundingBox()
        # The mesh is around the origin of the node, and the node is at the middle of the top of the
        # base, so the Rotate tool turns the text about its middle.
        boxes = onpart.raised_boxes(rects, px, 0, 0, w, h, 0, self._textDepth)
        verts, tris = [], []
        for box in boxes:
            v, t = onpart.box_mesh_at(box)
            tris += [(a + len(verts), c + len(verts), d + len(verts)) for a, c, d in t]
            verts += v
        mesh = MeshBuilder()
        mesh.setVertices(numpy.asarray(verts, dtype=numpy.float32))
        mesh.setIndices(numpy.asarray(tris, dtype=numpy.int32))
        mesh.calculateNormals()
        node = CuraSceneNode()
        node.setName("Text " + text)
        node.setMeshData(mesh.build())
        node.setSelectable(True)
        node.addDecorator(BuildPlateDecorator(CuraApplication.getInstance().getMultiBuildPlateModel().activeBuildPlate))
        node.addDecorator(SliceableObjectDecorator())
        node.setSetting(SceneNodeSettings.AutoDropDown, False)
        root = CuraApplication.getInstance().getController().getScene().getRoot()
        AddSceneNodeOperation(node, root).push()
        node.setPosition(Vector(b.center.x, b.top, b.center.z))
        Selection.clear()
        Selection.add(node)
        self.propertyChanged.emit()


    def setBase(self):
        node = Selection.getSelectedObject(0)
        if not node:
            return
        for n in self._nodes():
            if n.getSetting(BASE, False):
                n.setSetting(BASE, False)
                self._setMeshSetting(n, "anti_overhang_mesh", False)
        node.setSetting(BASE, True)
        # Cura does not print the base, and puts no support in it.
        self._setMeshSetting(node, "anti_overhang_mesh", True)
        # Cura keeps models apart by their outlines on the bed, so it would push the pocket and the
        # new models away from each other: the new models are over the pocket.
        prefs = CuraApplication.getInstance().getPreferences()
        if self._pushFree is None:
            self._pushFree = prefs.getValue("physics/automatic_push_free")
        prefs.setValue("physics/automatic_push_free", False)
        self._watch(node)
        self._setupSettings()
        self.updatePocket()
        self.propertyChanged.emit()

    def clearBase(self):
        base = self.base()
        if base:
            base.setSetting(BASE, False)
            self._setMeshSetting(base, "anti_overhang_mesh", False)
        pocket = self.pocket()
        if pocket:
            RemoveSceneNodeOperation(pocket).push()
        if self._pushFree is not None:
            CuraApplication.getInstance().getPreferences().setValue("physics/automatic_push_free", self._pushFree)
            self._pushFree = None
        self._watch(None)
        self.propertyChanged.emit()

    def _watch(self, node):
        """The pocket follows the moves of the base."""
        if self._watched:
            self._watched.transformationChanged.disconnect(self._onBaseMoved)
        self._watched = node
        if node:
            node.transformationChanged.connect(self._onBaseMoved)

    def _onBaseMoved(self, node):
        if not self._pocketLater:
            self._pocketLater = True
            CuraApplication.getInstance().callLater(self._movePocket)

    def _movePocket(self):
        self._pocketLater = False
        self._text = ""
        self._textSize = 8.0  # mm, the size of the font
        self._textDepth = 0.6  # mm, the height of the letters over the face
        self.updatePocket()
        self.propertyChanged.emit()

    def placeOnBase(self):
        base = self.base()
        if not base:
            return
        top = base.getBoundingBox().top
        for node in Selection.getAllSelectedObjects():
            if node is base or node.getSetting(POCKET, False):
                continue
            node.setSetting(SceneNodeSettings.AutoDropDown, False)
            dy = top - node.getBoundingBox().bottom
            TranslateOperation(node, Vector(0, dy, 0)).push()
        self.propertyChanged.emit()

    def placeAsInFiles(self):
        """Puts the selected models at their place relative to the base in their files, with the
        turn and the move of the base."""
        from UM.Math.Matrix import Matrix
        base = self.base()
        if not base or not base.getMeshData():
            return
        cb = base.getMeshData().getCenterPosition()
        for node in Selection.getAllSelectedObjects():
            if node is base or node.getSetting(POCKET, False) or not node.getMeshData():
                continue
            cm = node.getMeshData().getCenterPosition()
            if cb is None or cm is None:
                Logger.log("w", "Printdeck: %s has no position of its file", node.getName())
                continue
            offset = Matrix()
            offset.setByTranslation(cm - cb)
            node.setSetting(SceneNodeSettings.AutoDropDown, False)
            node.setTransformation(base.getWorldTransformation().multiply(offset, copy=True))
        self.propertyChanged.emit()

    def updatePocket(self):
        """Makes the pocket around the base again, or removes it when the pocket is on the bed."""
        old = self.pocket()
        if old:
            RemoveSceneNodeOperation(old).push()
        base = self.base()
        if not base or self._pocketOnBed:
            return
        x0, z0, x1, z1 = self._footprint(base)
        mesh = MeshBuilder()
        verts, tris = [], []
        for box in onpart.pocket_boxes(x0, z0, x1, z1, self._clearance, onpart.WALL, self._wallHeight):
            v, t = onpart.box_mesh(box)
            tris += [(a + len(verts), b + len(verts), c + len(verts)) for a, b, c in t]
            verts += v
        mesh.setVertices(numpy.asarray(verts, dtype=numpy.float32))
        mesh.setIndices(numpy.asarray(tris, dtype=numpy.int32))
        mesh.calculateNormals()
        node = CuraSceneNode()
        node.setName("Printdeck pocket")
        node.setMeshData(mesh.build())
        node.setSelectable(True)
        node.addDecorator(BuildPlateDecorator(CuraApplication.getInstance().getMultiBuildPlateModel().activeBuildPlate))
        node.addDecorator(SliceableObjectDecorator())
        node.setSetting(POCKET, True)
        node.setSetting(SceneNodeSettings.AutoDropDown, False)
        node.setSetting(SceneNodeSettings.LockPosition, True)
        root = CuraApplication.getInstance().getController().getScene().getRoot()
        AddSceneNodeOperation(node, root).push()

    # Help.

    def _footprint(self, node):
        b = node.getBoundingBox()
        return b.left, b.back, b.right, b.front

    def _machine(self):
        stack = CuraApplication.getInstance().getGlobalContainerStack()
        w = float(stack.getProperty("machine_width", "value"))
        d = float(stack.getProperty("machine_depth", "value"))
        return w, d, bool(stack.getProperty("machine_center_is_zero", "value"))

    def _setMeshSetting(self, node, key, value):
        stack = node.callDecoration("getStack")
        if not stack:
            return
        settings = stack.getTop()
        if settings.getInstance(key):
            settings.removeInstance(key)
        if value:
            instance = SettingInstance(stack.getSettingDefinition(key), settings)
            instance.setProperty("value", value)
            instance.resetState()
            settings.addInstance(instance)

    def _setupSettings(self):
        """With a base, Cura keeps the empty layers under the new models, so that they print at
        their height. With the pocket on the bed, the first layer is on the base, so it gets no skirt
        or brim in the air."""
        stack = CuraApplication.getInstance().getGlobalContainerStack()
        if not stack:
            return
        user = stack.userChanges
        user.setProperty("remove_empty_first_layers", "value", False)
        if self._pocketOnBed:
            user.setProperty("adhesion_type", "value", "none")
        elif user.getProperty("adhesion_type", "value") == "none":
            user.removeInstance("adhesion_type")

    # The G-code.

    def changeGcode(self, gcode_list):
        """Adds the pause before the first layer of the new models, with the height check at a point
        of the top face of the base, and the start for a pocket on the bed. It returns False when
        the scene has no base or no new models."""
        base, placed = self.base(), self.placed()
        if not base or not placed:
            return False
        w, d, center = self._machine()
        avoid = [self._footprint(n) for n in placed]
        point = onpart.top_point(self._topTriangles(base), avoid)
        check = None
        if point:
            check = onpart.to_gcode(point[0], point[2], point[1], w, d, center)
        z_start = min(n.getBoundingBox().bottom for n in placed)
        if onpart.insert_pause(gcode_list, z_start, self.getMessage(), check, onpart.LIFT) < 0:
            Logger.log("w", "Printdeck: no layer at %.2f mm for the pause", z_start)
            return False
        if self._pocketOnBed and not onpart.on_part_start(gcode_list):
            Logger.log("w", "Printdeck: no PRINT_START in the start G-code")
        return True

    def _topTriangles(self, node):
        """The triangles of the node near its top, in the scene."""
        verts = node.getMeshDataTransformedVertices()
        data = node.getMeshData()
        if verts is None or data is None:
            return []
        idx = data.getIndices()
        tri = verts[idx] if idx is not None else verts.reshape(-1, 3, 3)
        top = tri[:, :, 1].max()
        keep = tri[:, :, 1].min(axis=1) >= top - 0.05
        return [tuple(tuple(float(c) for c in p) for p in t) for t in tri[keep]]
