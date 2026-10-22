// The 3D window of the simulated printer: a LulzBot TAZ Workhorse that moves as the simulated board
// moves it, with the part that grows on its bed. The view starts isometric. The left button turns
// it, the right button pans, the middle button walks on the ground, the wheel zooms, and the side
// buttons move down and up. The keys move the point of the view too: W A S D or the arrows forward,
// left, back and right, Q and E down and up, with Shift faster.
//
// The coordinates are mm. A point of the printer (x right, y back, z up) is (x, z, -y) in the
// scene. The tool head moves in X, the gantry in Z, and the bed in Y: the nozzle stays in the plane
// z = 0 of the scene, and the bed moves under it.
import QtQuick
import QtQuick.Window
import QtQuick3D
import Printdeck.Sim

Window
{
    id: win
    width: 900; height: 720
    visible: true
    title: "Printdeck Sim · TAZ Workhorse"
    color: "#1b1c1a"

    property vector3d p: sim.position
    // The glow of a heater: from no glow at the room to full at hot.
    function glow(t, hot) { return Math.max(0, Math.min(1, (t - 30) / (hot - 30))) }

    View3D
    {
        id: view
        anchors.fill: parent
        environment: SceneEnvironment
        {
            clearColor: "#24251f"
            backgroundMode: SceneEnvironment.Color
            antialiasingMode: SceneEnvironment.MSAA
            antialiasingQuality: SceneEnvironment.High
        }

        // The pivot of the view, at the middle of the bed, a little above it.
        Node
        {
            id: pivot
            position: Qt.vector3d(120, 240, -140)
            eulerRotation: Qt.vector3d(view.pitch, view.yaw, 0)
            PerspectiveCamera
            {
                id: camera
                position: Qt.vector3d(0, 0, 3000)
                fieldOfView: 22 // a long lens: close to isometric
                clipNear: 10; clipFar: 20000
            }
        }

        DirectionalLight { eulerRotation: Qt.vector3d(-50, -30, 0); brightness: 1.3; ambientColor: "#7a7a78"; castsShadow: true; shadowFactor: 40 }
        DirectionalLight { eulerRotation: Qt.vector3d(-25, 150, 0); brightness: 0.6 }
        DirectionalLight { eulerRotation: Qt.vector3d(-80, 0, 0); brightness: 0.35 }

        // --- The frame, which does not move.
        component Box: Model
        {
            property vector3d size: Qt.vector3d(10, 10, 10)
            property color tint: "#3a3b39"
            property real metal: 0.2
            property color glowColor: "black"
            source: "#Cube"
            scale: Qt.vector3d(size.x / 100, size.y / 100, size.z / 100)
            materials: PrincipledMaterial { baseColor: tint; metalness: metal * 0.45; roughness: 0.5; emissiveFactor: Qt.vector3d(glowColor.r, glowColor.g, glowColor.b) }
        }
        // A rod, a lead screw, a shaft: a cylinder along an axis of the scene.
        component Rod: Model
        {
            property real radius: 4
            property real length: 100
            property color tint: "#c4c6c8"
            source: "#Cylinder"
            scale: Qt.vector3d(radius / 50, length / 100, radius / 50)
            materials: PrincipledMaterial { baseColor: tint; metalness: 0.35; roughness: 0.35 }
        }
        // A NEMA 17 stepper motor: a black body between two silver end caps, with its axis on Y of
        // the motor.
        component Motor: Node
        {
            property real length: 48
            Box { size: Qt.vector3d(42, 6, 42); y: length / 2 - 3; tint: "#b8babc"; metal: 0.7 }
            Box { size: Qt.vector3d(42, length - 12, 42); tint: "#161616" }
            Box { size: Qt.vector3d(42, 6, 42); y: -length / 2 + 3; tint: "#b8babc"; metal: 0.7 }
            Rod { radius: 2.5; length: 22; y: length / 2 + 11; tint: "#d0d2d4" }
        }
        // The base and the Y rails.
        Box { position: Qt.vector3d(120, -75, -140); size: Qt.vector3d(470, 30, 640) }
        Box { position: Qt.vector3d(40, -48, -140); size: Qt.vector3d(20, 20, 600); tint: "#9a9c9e"; metal: 0.8 }
        Box { position: Qt.vector3d(200, -48, -140); size: Qt.vector3d(20, 20, 600); tint: "#9a9c9e"; metal: 0.8 }
        // The two uprights and the top bar, behind the plane of the nozzle.
        Box { position: Qt.vector3d(-110, 200, -60); size: Qt.vector3d(40, 580, 40) }
        Box { position: Qt.vector3d(355, 200, -60); size: Qt.vector3d(40, 580, 40) }
        Box { position: Qt.vector3d(122, 470, -60); size: Qt.vector3d(505, 40, 40) }
        // The Z motors on the top of the uprights, and their lead screws down to the base.
        Motor { position: Qt.vector3d(-110, 514, -15); eulerRotation: Qt.vector3d(180, 0, 0) }
        Motor { position: Qt.vector3d(355, 514, -15); eulerRotation: Qt.vector3d(180, 0, 0) }
        Rod { position: Qt.vector3d(-110, 205, -15); radius: 4; length: 560 }
        Rod { position: Qt.vector3d(355, 205, -15); radius: 4; length: 560 }
        // The Y motor at the front of the base, and the Y belt under the bed.
        Motor { position: Qt.vector3d(120, -60, 160); eulerRotation: Qt.vector3d(0, 0, 90) }
        Box { position: Qt.vector3d(120, -40, -140); size: Qt.vector3d(6, 2, 600); tint: "#111111" }
        // The electronics: the RAMBo in its box at the back left.
        Box { position: Qt.vector3d(-40, -10, -420); size: Qt.vector3d(160, 100, 50); tint: "#2a2b29" }
        Box { position: Qt.vector3d(-40, -10, -394); size: Qt.vector3d(120, 60, 2); tint: "#1d4f8a" }
        // The spool on its holder over the top bar, with orange filament.
        Rod { position: Qt.vector3d(122, 520, -60); eulerRotation: Qt.vector3d(0, 0, 90); radius: 6; length: 120; tint: "#8a8d90" }
        Model
        {
            source: "#Cylinder"
            position: Qt.vector3d(122, 545, -60); eulerRotation: Qt.vector3d(0, 0, 90)
            scale: Qt.vector3d(1.9, 0.62, 1.9)
            materials: PrincipledMaterial { baseColor: "#1a1a1a"; roughness: 0.6 }
        }
        Model
        {
            source: "#Cylinder"
            position: Qt.vector3d(122, 545, -60); eulerRotation: Qt.vector3d(0, 0, 90)
            scale: Qt.vector3d(1.6, 0.66, 1.6)
            materials: PrincipledMaterial { baseColor: "#ff7a1a"; roughness: 0.5 }
        }

        // --- The filament: from the spool down into the top of the tool head, which it follows. A
        // cylinder between the two points: its Y axis turned onto the direction between them.
        Model
        {
            id: filament
            property vector3d from: Qt.vector3d(122, 470, 10) // where it leaves the spool
            property vector3d to: Qt.vector3d(win.p.x, win.p.z + 140, -10) // the top of the extruder
            property vector3d d: to.minus(from)
            property real length: d.length()
            property vector3d u: d.normalized()
            // The rotation of (0, 1, 0) onto u: about the axis Y × u, by the angle between them.
            property real angle: Math.acos(Math.max(-1, Math.min(1, u.y)))
            property vector3d axis: Qt.vector3d(u.z, 0, -u.x).normalized()
            source: "#Cylinder"
            position: from.plus(d.times(0.5))
            rotation: Qt.quaternion(Math.cos(angle / 2), axis.x * Math.sin(angle / 2), axis.y * Math.sin(angle / 2), axis.z * Math.sin(angle / 2))
            scale: Qt.vector3d(0.0285, length / 100, 0.0285)
            materials: PrincipledMaterial { baseColor: "#ff7a1a"; roughness: 0.4 }
        }

        // --- The gantry, at the height of Z: two rods of X, and the tool head on them.
        Node
        {
            id: gantry
            y: win.p.z
            Box { position: Qt.vector3d(122, 30, -40); size: Qt.vector3d(470, 12, 12); tint: "#c4c6c8"; metal: 0.9 }
            Box { position: Qt.vector3d(122, 75, -40); size: Qt.vector3d(470, 12, 12); tint: "#c4c6c8"; metal: 0.9 }
            Box { position: Qt.vector3d(-110, 52, -40); size: Qt.vector3d(60, 70, 30) } // the Z carriages
            Box { position: Qt.vector3d(355, 52, -40); size: Qt.vector3d(60, 70, 30) }
            // The X motor at the left end, and the X belt to the idler at the right.
            Motor { position: Qt.vector3d(-75, 52, -75); eulerRotation: Qt.vector3d(90, 0, 0) }
            Box { position: Qt.vector3d(122, 52, -32); size: Qt.vector3d(420, 6, 2); tint: "#111111" }

            Node
            {
                id: head
                x: win.p.x
                // The body, the fan duct, the heater block that glows, and the nozzle.
                Box { position: Qt.vector3d(0, 60, -10); size: Qt.vector3d(64, 80, 56); tint: "#202120" }
                Box { position: Qt.vector3d(0, 72, 19); size: Qt.vector3d(56, 50, 4); tint: "#a5c43a" } // the LulzBot face
                Box { position: Qt.vector3d(0, 30, 16); size: Qt.vector3d(40, 16, 14); tint: "#ff5a1f" } // the fan duct
                // The extruder motor on the top of the tool head.
                Motor { position: Qt.vector3d(0, 124, -10); length: 26 }
                Box
                {
                    position: Qt.vector3d(0, 12, 0); size: Qt.vector3d(20, 10, 16); tint: "#8a8d90"; metal: 0.7
                    glowColor: Qt.rgba(0.9 * win.glow(sim.tool, 230), 0.25 * win.glow(sim.tool, 230), 0, 1)
                }
                Model
                {
                    source: "#Cone"
                    position: Qt.vector3d(0, 3.5, 0)
                    eulerRotation: Qt.vector3d(180, 0, 0)
                    scale: Qt.vector3d(0.08, 0.07, 0.08)
                    materials: PrincipledMaterial { baseColor: "#c9a24b"; metalness: 0.9; roughness: 0.3 }
                }
                // The molten plastic at the tip of the nozzle, while the extruder pushes.
                Model
                {
                    source: "#Sphere"
                    visible: sim.extruding
                    position: Qt.vector3d(0, 0.2, 0)
                    scale: Qt.vector3d(0.012, 0.008, 0.012)
                    materials: PrincipledMaterial { baseColor: "#ff7a1a"; emissiveFactor: Qt.vector3d(0.3, 0.1, 0) }
                }
            }
        }

        // --- The bed, which moves in Y, with the part on it.
        Node
        {
            id: bed
            z: win.p.y
            // The plate: PEI on aluminum, which glows when it is hot.
            Box { position: Qt.vector3d(140, -9, -140); size: Qt.vector3d(340, 10, 340); tint: "#55575a"; metal: 0.6 }
            Box
            {
                position: Qt.vector3d(140, -2, -140); size: Qt.vector3d(330, 4, 330); tint: "#b8923f"
                glowColor: Qt.rgba(0.35 * win.glow(sim.bed, 110), 0.08 * win.glow(sim.bed, 110), 0, 1)
            }
            // The 4 washers of the probe, at the corners.
            Repeater3D
            {
                model: [[-10, -10], [290, -10], [290, 290], [-10, 290]]
                Model
                {
                    source: "#Cylinder"
                    position: Qt.vector3d(modelData[0], 0.6, -modelData[1])
                    scale: Qt.vector3d(0.16, 0.012, 0.16)
                    materials: PrincipledMaterial { baseColor: "#d8dadc"; metalness: 1; roughness: 0.25 }
                }
            }
            // The wiper pad at the left of the bed.
            Box { position: Qt.vector3d(-16, 0, -60); size: Qt.vector3d(10, 3, 80); tint: "#7a1f1a" }
            // The part, and the strands of the last moments up to the nozzle.
            Model
            {
                geometry: PathGeometry { id: part }
                materials: PrincipledMaterial { baseColor: "#ff7a1a"; roughness: 0.45; cullMode: Material.NoCulling }
                Component.onCompleted: sim.path = part
            }
            Model
            {
                geometry: PathGeometry { id: recent }
                materials: PrincipledMaterial { baseColor: "#ff7a1a"; roughness: 0.45; cullMode: Material.NoCulling }
                Component.onCompleted: sim.recent = recent
            }
        }

        // The angles of the view: isometric at the start, 45° to the side and 35° from above.
        property real yaw: -45
        property real pitch: -35.264
        // The size of a pixel at the point of the view, in mm.
        function pixel() { return 2 * camera.z * Math.tan(camera.fieldOfView * Math.PI / 360) / height }
        // move moves the point of the view by a vector of the scene.
        function move(d) { pivot.position = pivot.position.plus(d) }
        function ground(v) { return Qt.vector3d(v.x, 0, v.z).normalized() }

        MouseArea
        {
            anchors.fill: parent
            acceptedButtons: Qt.AllButtons
            property point last
            property int button
            onPressed: function(mouse) {
                last = Qt.point(mouse.x, mouse.y)
                button = mouse.button
                view.forceActiveFocus()
                // The side buttons move down (back) and up (forward) while they are down.
                if (button == Qt.BackButton || button == Qt.ForwardButton) {
                    var h = view.held
                    h[button == Qt.BackButton ? Qt.Key_Q : Qt.Key_E] = true
                    view.held = h
                }
            }
            onReleased: function(mouse) {
                if (mouse.button == Qt.BackButton || mouse.button == Qt.ForwardButton) {
                    var h = view.held
                    delete h[mouse.button == Qt.BackButton ? Qt.Key_Q : Qt.Key_E]
                    view.held = h
                }
            }
            onPositionChanged: function(mouse) {
                var dx = mouse.x - last.x, dy = mouse.y - last.y
                last = Qt.point(mouse.x, mouse.y)
                var px = view.pixel()
                switch (button) {
                case Qt.LeftButton: // turn
                    view.yaw -= dx * 0.3
                    view.pitch = Math.max(-89, Math.min(10, view.pitch - dy * 0.3))
                    break
                case Qt.RightButton: // pan in the plane of the screen
                    view.move(pivot.right.times(-dx * px).plus(pivot.up.times(dy * px)))
                    break
                case Qt.MiddleButton: // walk on the ground
                    view.move(view.ground(pivot.right).times(-dx * px).plus(view.ground(pivot.forward).times(-dy * px)))
                    break
                }
            }
            onWheel: function(wheel) {
                camera.z = Math.max(80, Math.min(9000, camera.z * Math.pow(0.88, wheel.angleDelta.y / 120)))
            }
        }

        // The keys move the point of the view, as long as they are down. The speed follows the
        // zoom: close to the part, the view moves slowly.
        property var held: ({})
        focus: true
        Keys.onPressed: function(event) {
            if (event.isAutoRepeat)
                return
            var h = held
            h[event.key] = true
            held = h
            event.accepted = true
        }
        Keys.onReleased: function(event) {
            if (event.isAutoRepeat)
                return
            var h = held
            delete h[event.key]
            held = h
            event.accepted = true
        }
        FrameAnimation
        {
            running: Object.keys(view.held).length > 0
            onTriggered: {
                var h = view.held
                var speed = camera.z * 0.35 * frameTime * (h[Qt.Key_Shift] ? 3 : 1)
                // Forward and right in the ground plane, up straight up.
                var f = pivot.forward, r = pivot.right
                var fwd = Qt.vector3d(f.x, 0, f.z).normalized(), right = Qt.vector3d(r.x, 0, r.z).normalized()
                var d = Qt.vector3d(0, 0, 0)
                if (h[Qt.Key_W] || h[Qt.Key_Up]) d = d.plus(fwd)
                if (h[Qt.Key_S] || h[Qt.Key_Down]) d = d.minus(fwd)
                if (h[Qt.Key_D] || h[Qt.Key_Right]) d = d.plus(right)
                if (h[Qt.Key_A] || h[Qt.Key_Left]) d = d.minus(right)
                if (h[Qt.Key_E]) d = d.plus(Qt.vector3d(0, 1, 0))
                if (h[Qt.Key_Q]) d = d.minus(Qt.vector3d(0, 1, 0))
                pivot.position = pivot.position.plus(d.times(speed))
            }
        }
    }

    // The state of the printer, at the top left.
    Rectangle
    {
        x: 16; y: 16; width: info.implicitWidth + 28; height: info.implicitHeight + 20
        radius: 6; color: "#cc121211"; border.color: "#3a3b39"
        Column
        {
            id: info
            x: 14; y: 10; spacing: 4
            Text { text: sim.connected ? sim.state.toUpperCase() : "CONNECTING…"; color: "#ff7a3a"; font.bold: true; font.pixelSize: 14; font.family: "monospace" }
            Text { text: sim.file; visible: sim.file != ""; color: "#eee9df"; font.pixelSize: 13; font.family: "monospace" }
            Text
            {
                text: "NOZZLE " + sim.tool.toFixed(0) + "° / " + sim.toolTarget.toFixed(0) + "°   BED " + sim.bed.toFixed(0) + "° / " + sim.bedTarget.toFixed(0) + "°"
                color: "#c2bcb0"; font.pixelSize: 12; font.family: "monospace"
            }
            Text
            {
                text: "X " + win.p.x.toFixed(1) + "  Y " + win.p.y.toFixed(1) + "  Z " + win.p.z.toFixed(2) + "   " + part.moves + " MOVES"
                color: "#c2bcb0"; font.pixelSize: 12; font.family: "monospace"
            }
        }
    }
    Text
    {
        anchors.right: parent.right; anchors.bottom: parent.bottom; anchors.margins: 12
        text: "Left turn · right pan · middle walk · wheel zoom · side buttons down/up · W A S D Q E move"
        color: "#6f6b63"; font.pixelSize: 12
    }
}
