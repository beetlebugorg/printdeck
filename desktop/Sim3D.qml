// The 3D window of the simulated printer: a LulzBot TAZ Workhorse that moves as the simulated board
// moves it, with the part that grows on its bed. The view starts isometric. The left button turns
// it, the right button pans, the middle button walks on the ground, the wheel zooms, and the side
// buttons move down and up. The keys move the point of the view too: W A S D or the arrows forward,
// left, back and right, Q and E down and up, with Shift faster. T makes the tool head see-through
// or solid.
//
// With PRINTDECK_FILM (filmOn), the window is the film of desktop/film/: a 1920 x 1080 stage with
// a scripted camera, the black printer on a neon floor, the Printdeck that the parts assemble on
// the printer, and the titles. film records it (FilmRecorder).
//
// The coordinates are mm. A point of the printer (x right, y back, z up) is (x, z, -y) in the
// scene. The tool head moves in X, the gantry in Z, and the bed in Y: the nozzle stays in the plane
// z = 0 of the scene, and the bed moves under it.
import QtQuick
import QtQuick.Window
import QtQuick3D
import QtQuick3D.Helpers
import Printdeck.Sim

Window
{
    id: win
    width: 900; height: 720
    visible: true
    title: "Printdeck Sim · TAZ Workhorse"
    color: "#1b1c1a"

    property vector3d p: sim.position
    // The tool head is see-through, so the part under it shows. T turns it solid and back.
    property bool seeThrough: true
    // The film: the time (s), the take that runs, and the point that follows the nozzle a little
    // behind it, so the camera does not shake.
    property real ft: 0
    property bool filming: false
    property vector3d noz: Qt.vector3d(140, 40, 0)
    function nozzleNow() { return Qt.vector3d(p.x, p.z + 8, 0) }
    function startFilm() {
        ft = film.from
        noz = nozzleNow()
        view.applyCam(ft)
        if (!film.start(1920, 1080, film.fps))
            return
        filming = true
        Qt.callLater(step)
    }
    // step records the frame of the time ft, and goes to the next frame.
    function step() {
        stage.grabToImage(function(r) {
            film.frame(r.image)
            var dt = 1 / film.fps
            if (ft + dt > film.to + 1e-6) {
                filming = false
                film.finish()
                return
            }
            ft += dt
            noz = noz.plus(nozzleNow().minus(noz).times(1 - Math.exp(-dt / 0.35)))
            view.applyCam(ft)
            Qt.callLater(step)
        }, Qt.size(1920, 1080))
    }
    Connections
    {
        target: filmOn ? film : null
        function onArmedChanged() {
            if (film.armed && !win.filming)
                win.startFilm()
        }
    }
    Component.onCompleted: if (filmOn) view.applyCam(0)

    // The glow of a heater: from no glow at the room to full at hot.
    function glow(t, hot) { return Math.max(0, Math.min(1, (t - 30) / (hot - 30))) }

    // The stage: the window, or the frame of the film (1920 x 1080), as large as the window fits.
    Item
    {
        id: stageBox
        width: filmOn ? 1920 : win.width; height: filmOn ? 1080 : win.height
        scale: filmOn ? Math.min(win.width / 1920, win.height / 1080) : 1
        transformOrigin: Item.TopLeft
        Item
        {
            id: stage
            anchors.fill: parent
            FilmSky { anchors.fill: parent; visible: filmOn; t: win.ft }
            View3D
            {
                id: view
                anchors.fill: parent
                environment: filmOn ? filmEnv : plainEnv
                property SceneEnvironment plainEnv: SceneEnvironment
                {
                    clearColor: "#24251f"
                    backgroundMode: SceneEnvironment.Color
                    antialiasingMode: SceneEnvironment.MSAA
                    antialiasingQuality: SceneEnvironment.High
                }
                // The film: the sky shows behind, with glow on the neon, fog toward the horizon, and a
                // vignette.
                property ExtendedSceneEnvironment filmEnv: ExtendedSceneEnvironment
                {
                    backgroundMode: SceneEnvironment.Transparent
                    antialiasingMode: SceneEnvironment.MSAA
                    antialiasingQuality: SceneEnvironment.High
                    tonemapMode: SceneEnvironment.TonemapModeFilmic
                    exposure: 2.6
                    glowEnabled: true
                    glowIntensity: 0.6
                    glowStrength: 0.8
                    glowBloom: 0.15
                    glowBlendMode: ExtendedSceneEnvironment.GlowBlendMode.Additive
                    vignetteEnabled: true
                    vignetteStrength: 0.35
                    vignetteRadius: 0.4
                    fog: Fog
                    {
                        enabled: true
                        color: "#15142a"
                        depthEnabled: true
                        depthNear: 2500; depthFar: 12000
                    }
                }

                // The pivot of the view, at the middle of the bed, a little above it.
                Node
                {
                    id: pivot
                    position: Qt.vector3d(120, 230, 0)
                    eulerRotation: Qt.vector3d(view.pitch, view.yaw, 0)
                    PerspectiveCamera
                    {
                        id: camera
                        position: Qt.vector3d(0, 0, 3000)
                        fieldOfView: 22 // a long lens: close to isometric
                        clipNear: 10; clipFar: 20000
                    }
                }

                DirectionalLight { visible: !filmOn; eulerRotation: Qt.vector3d(-50, -30, 0); brightness: 1.3; ambientColor: "#7a7a78"; castsShadow: true; shadowFactor: 40 }
                DirectionalLight { visible: !filmOn; eulerRotation: Qt.vector3d(-25, 150, 0); brightness: 0.6 }
                DirectionalLight { visible: !filmOn; eulerRotation: Qt.vector3d(-80, 0, 0); brightness: 0.35 }
                // The film: a white key light and a fill from the front, and a soft pink and blue light from
                // behind on the edges.
                DirectionalLight { visible: filmOn; eulerRotation: Qt.vector3d(-35, -30, 0); brightness: 2.4; color: "#f2f0ff"; ambientColor: "#45435a"; castsShadow: true; shadowFactor: 45 }
                DirectionalLight { visible: filmOn; eulerRotation: Qt.vector3d(-10, 10, 0); brightness: 0.8; color: "#d8e2ff" }
                DirectionalLight { visible: filmOn; eulerRotation: Qt.vector3d(-15, 150, 0); brightness: 1.5; color: "#ff7ab8" }
                DirectionalLight { visible: filmOn; eulerRotation: Qt.vector3d(-15, -150, 0); brightness: 1.3; color: "#7fd6ff" }

                // The film: the floor, a faint grid of 200 mm under the feet of the printer.
                property Item gridCell: Item
                {
                    width: 256; height: 256
                    Rectangle { anchors.fill: parent; color: "#05000a" }
                    Rectangle { width: 256; height: 3; color: "#7a5cff" }
                    Rectangle { width: 3; height: 256; color: "#7a5cff" }
                }
                Model
                {
                    visible: filmOn
                    source: "#Rectangle"
                    position: Qt.vector3d(120, -92, 0)
                    eulerRotation.x: -90
                    scale: Qt.vector3d(300, 300, 1)
                    materials: PrincipledMaterial
                    {
                        baseColor: "#07070d"; roughness: 0.95; metalness: 0
                        emissiveMap: Texture
                        {
                            sourceItem: view.gridCell
                            scaleU: 150; scaleV: 150
                            tilingModeHorizontal: Texture.Repeat; tilingModeVertical: Texture.Repeat
                            generateMipmaps: true; mipFilter: Texture.Linear
                        }
                        emissiveFactor: Qt.vector3d(0.45, 0.45, 0.45)
                    }
                }

                // A part of the printer: a mesh of the CAD of LulzBot (cad/export.py), in one material.
                component Part: Model
                {
                    property string mesh
                    property color tint: "#2a2b29"
                    property real metal: 0
                    property real rough: 0.65
                    property color glowColor: "black"
                    geometry: MeshGeometry { source: ":/meshes/" + mesh + ".bin" }
                    materials: PrincipledMaterial
                    {
                        baseColor: tint; metalness: metal; roughness: rough; cullMode: Material.NoCulling
                        emissiveFactor: Qt.vector3d(glowColor.r, glowColor.g, glowColor.b)
                    }
                }
                // A screen: a picture on a rectangle that faces +Z, with no light on it.
                component Screen: Model
                {
                    property size size: Qt.size(100, 100)
                    property alias image: tex.image
                    source: "#Rectangle"
                    scale: Qt.vector3d(size.width / 100, size.height / 100, 1)
                    materials: PrincipledMaterial
                    {
                        lighting: PrincipledMaterial.NoLighting
                        // The rows of the texture data go from the bottom up.
                        baseColorMap: Texture { flipV: true; textureData: ImageTexture { id: tex } }
                    }
                }
                // The materials of the kinds of parts. The film has a black printer, with steel rods and
                // screws.
                component Metal: Part { tint: filmOn ? "#17181b" : "#c4c6c8"; metal: filmOn ? 0.55 : 0.35; rough: filmOn ? 0.32 : 0.35 }
                component Steel: Part { tint: "#c4c6c8"; metal: filmOn ? 0.8 : 0.35; rough: filmOn ? 0.22 : 0.35 }
                component Printed: Part { tint: filmOn ? "#0c0c0e" : "#2a2b29" }
                component Motor: Part { tint: "#161616"; rough: 0.5 }
                component Belt: Part { tint: "#0e0e0e"; rough: 0.8 }

                // --- The printer. The meshes are at the pose of the CAD (cad/export.py). The nodes move
                // them to the places of the printer: the place (x, y, z) of the nozzle is (x, z, -y) in
                // the scene, and the bed moves under the nozzle, so the place (x, y) of the bed is at
                // (x, 0, p.y - y). The part that is printed uses the same coordinates, so the nozzle is
                // exactly at its strands.
                //
                // At the pose of the CAD the center of the nozzle tip is at (137.02, 26.60, 0.50), the top
                // of the bed at Y 0.03, and the center of the front left washer at X -9.97 and Z 142.15.
                // The washers are around the middle of the bed, so the front left one is at -14, -14 of
                // the printer (simmcu/machine.go), and the printer is 4.03 mm to the left of its pose.
                //
                // The X of the CAD does not agree with printer.cfg: at X -50 (position_endstop of
                // stepper_x) the carriage is 52 mm past the X switch of the CAD, in the X motor mount.
                // The bed mounts of the frame and the Y base agree with each other, so the frame and the
                // bed stay at the places of the CAD, and the nozzle goes where printer.cfg puts it.
                property real frameX: -14 - -9.97
                property real bedX: frameX
                // The X of the carriage from its pose.
                property real carriageX: win.p.x - 137.02 - frameX
                Node
                {
                    id: machine
                    position: Qt.vector3d(view.frameX, 0, -0.50)
                    Metal { mesh: "frame_metal" }
                    Steel { mesh: "frame_rod" }
                    Printed { mesh: "frame_printed" }
                    Belt { mesh: "frame_belt" }

                    // The display of the printer, in the window of its bezel (72 x 40 mm): the picture of
                    // the simulated display, as the touch screen draws it.
                    Screen { position: Qt.vector3d(-165.0, 343.8, 61.5); size: Qt.size(72, 40); image: sim.lcd }

                    // The Printdeck on its mount (cad/deck.py): the head under the logo, the pod on the
                    // side of the chassis, and the deck UI on its display. Each group of the build booklet
                    // has its own node, so the film can assemble them (view.place). The face leans back
                    // 15°; the opening is 155.1 x 86.9 mm, and the active area of the display
                    // 154.21 x 85.92 mm.
                    Repeater3D
                    {
                        model: sim.resource(":/meshes/deck.json")
                        Node
                        {
                            property var a: view.place(modelData.group, win.ft)
                            position: a.offset
                            visible: a.shown
                            opacity: a.alpha
                            Part
                            {
                                property var m: view.materials[modelData.material] || ["#808080", 0, 0.5]
                                mesh: modelData.mesh
                                tint: m[0]; metal: m[1]; rough: m[2]
                            }
                        }
                    }
                    Node
                    {
                        property var a: view.place("display", win.ft)
                        position: a.offset
                        visible: a.shown
                        Node
                        {
                            position: Qt.vector3d(-137.45, 114.16, 102.07)
                            eulerRotation.x: -15
                            // The deck UI, as large as it fits, when the display is on.
                            Screen
                            {
                                property real aspect: sim.screen.width > 0 ? sim.screen.width / sim.screen.height : 1024 / 600
                                size: aspect > 154.21 / 85.92 ? Qt.size(154.21, 154.21 / aspect) : Qt.size(85.92 * aspect, 85.92)
                                image: sim.screen
                                opacity: view.power(win.ft)
                            }
                            // The flash of the display when it turns on.
                            Model
                            {
                                source: "#Rectangle"
                                z: 0.05
                                scale: Qt.vector3d(1.5421, 0.8592, 1)
                                opacity: view.flash(win.ft)
                                visible: opacity > 0
                                materials: PrincipledMaterial { lighting: PrincipledMaterial.NoLighting; baseColor: "#e8f6ff" }
                            }
                        }
                    }

                    // The gantry, at the height of Z, with the tool head on it.
                    Node
                    {
                        y: win.p.z - 26.60
                        Metal { mesh: "gantry_metal" }
                        Steel { mesh: "gantry_rod" }
                        Printed { mesh: "gantry_printed" }
                        Motor { mesh: "gantry_motor" }
                        Belt { mesh: "gantry_belt" }
                        // The top run of the X belt: from the motor pulley (X -51.5) to the clamp of the
                        // carriage (95 at the pose), and from the tensioner of the carriage (184) to the
                        // idler (405). Each part stretches between its two ends. Past the switch of the CAD
                        // (see frameX) the left part has no length.
                        Node
                        {
                            x: -51.5
                            scale.x: Math.max(0.001, (95 + view.carriageX - -51.5) / (95 - -51.5))
                            Belt { mesh: "xbeltl_belt"; x: 51.5 }
                        }
                        Node
                        {
                            x: 405
                            scale.x: Math.max(0.001, (405 - (184 + view.carriageX)) / (405 - 184))
                            Belt { mesh: "xbeltr_belt"; x: -405 }
                        }
                        Node
                        {
                            x: view.carriageX
                            Belt { mesh: "carriage_belt" }
                            // The body of the tool head; the nozzle and the heater block stay solid.
                            Node
                            {
                                opacity: win.seeThrough ? (filmOn ? 0.7 : 0.3) : 1
                                Metal { mesh: "carriage_metal" }
                                Steel { mesh: "carriage_hardware" }
                                Printed { mesh: "carriage_printed" }
                                Motor { mesh: "carriage_motor" }
                            }
                            Part { mesh: "carriage_nozzle"; tint: "#c9a24b"; metal: 0.6; rough: 0.3 }
                            // The heater block glows when it is hot.
                            Steel
                            {
                                mesh: "carriage_heater"
                                glowColor: Qt.rgba(0.9 * win.glow(sim.tool, 230), 0.25 * win.glow(sim.tool, 230), 0, 1)
                            }
                        }
                    }
                }
                // The base of the Y axis: its rails, its motor and its belt.
                Node
                {
                    position: Qt.vector3d(view.bedX, 0, -0.50)
                    Metal { mesh: "ybase_metal" }
                    Steel { mesh: "ybase_rod" }
                    Printed { mesh: "ybase_printed" }
                    Motor { mesh: "ybase_motor" }
                    Belt { mesh: "ybase_belt" }
                }
                // The bed, which moves in Y.
                Node
                {
                    position: Qt.vector3d(view.bedX, -0.03, win.p.y + 14 - 142.15)
                    Metal { mesh: "bed_metal" }
                    Printed { mesh: "bed_printed" }
                    // The washers of the probe. The simulated board puts their tops at the z_offset of the
                    // probe (0.86 in simprinter.cfg, simmcu/machine.go); the CAD has them at 1.78.
                    Steel { mesh: "bed_washer"; y: 0.86 - 1.78 }
                    // The PEI sheet glows when it is hot.
                    Part
                    {
                        mesh: "bed_surface"; tint: "#b8923f"; rough: 0.5
                        glowColor: Qt.rgba(0.35 * win.glow(sim.bed, 110), 0.08 * win.glow(sim.bed, 110), 0, 1)
                    }
                }
                // The part, and the strands of the last moments up to the nozzle.
                Node
                {
                    z: win.p.y
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
                // The molten plastic at the tip of the nozzle, while the extruder pushes.
                Model
                {
                    source: "#Sphere"
                    visible: sim.extruding
                    position: Qt.vector3d(win.p.x, win.p.z + 0.2, 0)
                    scale: Qt.vector3d(0.012, 0.008, 0.012)
                    materials: PrincipledMaterial { baseColor: "#ff7a1a"; emissiveFactor: Qt.vector3d(0.3, 0.1, 0) }
                }

                // --- The spool, on the top of the frame over the back bar: the CAD has no spool holder.
                Model
                {
                    source: "#Cylinder"
                    position: Qt.vector3d(170 + view.frameX, 440, -95); eulerRotation: Qt.vector3d(0, 0, 90)
                    scale: Qt.vector3d(0.12, 1.2, 0.12)
                    materials: PrincipledMaterial { baseColor: "#8a8d90"; metalness: 0.35; roughness: 0.35 }
                }
                Model
                {
                    source: "#Cylinder"
                    position: Qt.vector3d(170 + view.frameX, 520, -95); eulerRotation: Qt.vector3d(0, 0, 90)
                    scale: Qt.vector3d(1.9, 0.62, 1.9)
                    materials: PrincipledMaterial { baseColor: "#1a1a1a"; roughness: 0.6 }
                }
                Model
                {
                    source: "#Cylinder"
                    position: Qt.vector3d(170 + view.frameX, 520, -95); eulerRotation: Qt.vector3d(0, 0, 90)
                    scale: Qt.vector3d(1.6, 0.66, 1.6)
                    materials: PrincipledMaterial { baseColor: "#ff7a1a"; roughness: 0.5 }
                }

                // --- The filament: from the spool down into the top of the extruder, 113 mm over the
                // nozzle tip, which it follows. A cylinder between the two points: its Y axis turned onto
                // the direction between them.
                Model
                {
                    id: filament
                    property vector3d from: Qt.vector3d(170 + view.frameX, 520, -15) // where it leaves the spool
                    property vector3d to: Qt.vector3d(win.p.x, win.p.z + 113, 0) // the top of the extruder
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

                // The materials of the deck (lulzbot/build/manifest.json): the color, the metalness and
                // the roughness.
                property var materials: ({
                    pla_navy: ["#1c2541", 0, 0.6], pla_orange: ["#f26b1d", 0, 0.6], pla_white: ["#e8e6e1", 0, 0.6],
                    black_glass: ["#0a0a0c", 0.2, 0.08], black_plastic: ["#151515", 0, 0.5], black_steel: ["#2a2a2c", 0.7, 0.35],
                    steel: ["#c9ccd0", 0.7, 0.25], alu_dark: ["#3a3d42", 0.7, 0.35], pcb_green: ["#1f5a32", 0, 0.5],
                    lcd_metal: ["#9a9ea3", 0.6, 0.3], brass: ["#c9a24b", 0.7, 0.3], nickel: ["#b9bcc0", 0.7, 0.25]
                })
                // The assembly of the film, in the order of the build booklet (lulzbot/lineart/render.mjs):
                // each group comes from v (mm of the scene) at the time t0, and is in place d seconds later.
                // The display turns on at screenOn.
                property real screenOn: 17.4
                property var steps: {
                    var r = Math.sin(15 * Math.PI / 180), c = Math.cos(15 * Math.PI / 180)
                    var head = { t0: 12.6, d: 1.6, v: [0, 90, 220] }
                    var display = { t0: 13.8, d: 1.1, v: [0, 220 * r, 220 * c] }
                    return {
                        plate: { t0: 8.0, d: 1.2, v: [-140, 0, 0] }, ins_block: { t0: 8.0, d: 1.2, v: [-140, 0, 0] },
                        mount_screws: { t0: 9.0, d: 0.8, v: [-90, 0, 0] },
                        pi: { t0: 9.8, d: 1.0, v: [-120, 0, 0] }, pi_screws: { t0: 10.6, d: 0.6, v: [-70, 0, 0] },
                        clips: { t0: 11.2, d: 0.8, v: [0, 0, 120] }, ins_nut: { t0: 11.2, d: 0.8, v: [0, 0, 120] },
                        clip_screws: { t0: 11.8, d: 0.5, v: [0, 0, 75] },
                        head: head, ins_head: head, back: head, ins_back: head, drive: head, drive_screws: head, head_screws: head,
                        display: display, display_screws: display,
                        flange_screws: { t0: 14.8, d: 0.5, v: [0, 0, -60] },
                        pod: { t0: 15.2, d: 1.0, v: [-150, -25, 0] }, grille: { t0: 16.0, d: 0.8, v: [-200, -25, 0] },
                        bottom_screws: { t0: 16.6, d: 0.5, v: [0, -70, 0] }
                    }
                }
                // place is the offset, the visibility and the opacity of a group of the deck at the time t
                // of the film: out of the picture before t0, then a move in that ends with a small bounce.
                function place(group, t) {
                    var st = filmOn ? steps[group] : undefined
                    if (!st)
                        return { offset: Qt.vector3d(0, 0, 0), shown: true, alpha: 1 }
                    var p = (t - st.t0) / st.d
                    if (p <= 0)
                        return { offset: Qt.vector3d(0, 0, 0), shown: false, alpha: 0 }
                    p = Math.min(1, p)
                    var k = 1.6, e = 1 + (k + 1) * Math.pow(p - 1, 3) + k * Math.pow(p - 1, 2)
                    return { offset: Qt.vector3d(st.v[0], st.v[1], st.v[2]).times(1 - e), shown: true, alpha: Math.min(1, p * 4) }
                }
                // power is the brightness of the deck UI: off until screenOn in the film, then on.
                function power(t) { return filmOn ? Math.max(0, Math.min(1, (t - screenOn - 0.15) / 0.5)) : 1 }
                // flash is the white of the display as it turns on.
                function flash(t) { return filmOn && t > screenOn && t < screenOn + 0.6 ? 1 - (t - screenOn) / 0.6 : 0 }

                // The keys of the camera of the film: the time, the point of the view ("nozzle" follows
                // the nozzle), the yaw, the pitch, the distance and the field of view. The camera goes
                // through them on a Catmull-Rom spline.
                property var shots: [
                    { t: 0, at: [120, 160, 0], yaw: -25, pitch: -3, dist: 3800, fov: 26 },
                    { t: 5, at: [100, 200, 0], yaw: -30, pitch: -10, dist: 2300, fov: 26 },
                    { t: 7.5, at: [-150, 130, 20], yaw: -55, pitch: -14, dist: 900, fov: 26 },
                    { t: 11, at: [-150, 130, 20], yaw: -72, pitch: -12, dist: 760, fov: 26 },
                    { t: 14, at: [-140, 125, 40], yaw: -40, pitch: -10, dist: 700, fov: 26 },
                    { t: 17, at: [-141.5, 114.2, 101.6], yaw: -15, pitch: -15, dist: 520, fov: 26 },
                    { t: 21, at: [-141.5, 114.2, 101.6], yaw: -2, pitch: -15, dist: 300, fov: 24 },
                    { t: 24, at: [-160, 260, 80], yaw: -8, pitch: -10, dist: 520, fov: 25 },
                    { t: 27, at: [-169, 343.8, 61], yaw: -10, pitch: -6, dist: 300, fov: 25 },
                    { t: 30, at: [-169, 343.8, 61], yaw: 4, pitch: -4, dist: 240, fov: 25 },
                    { t: 32.5, at: [60, 120, 40], yaw: 25, pitch: -14, dist: 900, fov: 24 },
                    { t: 35, at: "nozzle", yaw: 55, pitch: -10, dist: 420, fov: 22 },
                    { t: 40, at: "nozzle", yaw: 35, pitch: -6, dist: 340, fov: 22 },
                    { t: 44, at: "nozzle", yaw: -20, pitch: -18, dist: 520, fov: 22 },
                    { t: 48, at: [120, 230, 0], yaw: 35, pitch: -8, dist: 1900, fov: 26 },
                    { t: 53, at: [120, 230, 0], yaw: -10, pitch: -12, dist: 2000, fov: 26 },
                    { t: 57, at: [120, 220, 0], yaw: -20, pitch: -4, dist: 3000, fov: 26 },
                    { t: 60, at: [120, 220, 0], yaw: -22, pitch: -3, dist: 3400, fov: 26 }
                ]
                // camAt is the camera of the film at the time t: the point, the angles, the distance and
                // the field of view. nozzle is the point that follows the nozzle.
                function camAt(t, nozzle) {
                    var k = shots, n = k.length
                    var i = 0
                    while (i < n - 2 && t >= k[i + 1].t)
                        i++
                    var u = Math.max(0, Math.min(1, (t - k[i].t) / (k[i + 1].t - k[i].t)))
                    var p0 = k[Math.max(0, i - 1)], p1 = k[i], p2 = k[i + 1], p3 = k[Math.min(n - 1, i + 2)]
                    function cr(a, b, c, d) {
                        return 0.5 * (2 * b + (c - a) * u + (2 * a - 5 * b + 4 * c - d) * u * u + (3 * b - a - 3 * c + d) * u * u * u)
                    }
                    function at(key) { return key.at === "nozzle" ? [nozzle.x, nozzle.y, nozzle.z] : key.at }
                    var a0 = at(p0), a1 = at(p1), a2 = at(p2), a3 = at(p3)
                    return {
                        at: Qt.vector3d(cr(a0[0], a1[0], a2[0], a3[0]), cr(a0[1], a1[1], a2[1], a3[1]), cr(a0[2], a1[2], a2[2], a3[2])),
                        yaw: cr(p0.yaw, p1.yaw, p2.yaw, p3.yaw), pitch: cr(p0.pitch, p1.pitch, p2.pitch, p3.pitch),
                        dist: cr(p0.dist, p1.dist, p2.dist, p3.dist), fov: cr(p0.fov, p1.fov, p2.fov, p3.fov)
                    }
                }
                function applyCam(t) {
                    var c = camAt(t, win.noz)
                    pivot.position = c.at
                    yaw = c.yaw
                    pitch = c.pitch
                    camera.z = c.dist
                    camera.fieldOfView = c.fov
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
                    if (event.key == Qt.Key_T) {
                        win.seeThrough = !win.seeThrough
                        event.accepted = true
                        return
                    }
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
            FilmTitles { anchors.fill: parent; visible: filmOn; t: win.ft }
        }
    }

    // The state of the printer, at the top left.
    Rectangle
    {
        visible: !filmOn
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
        visible: !filmOn
        anchors.right: parent.right; anchors.bottom: parent.bottom; anchors.margins: 12
        text: "Left turn · right pan · middle walk · wheel zoom · side buttons down/up · W A S D Q E move · T see-through"
        color: "#6f6b63"; font.pixelSize: 12
    }
}
