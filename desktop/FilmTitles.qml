// The titles of the film (film/), over the 3D view: the chrome title and end cards, and the
// cards with a soft glow. t is the time of the film in seconds. The words of the cards are in
// cards.
import QtQuick
import QtQuick.Effects

Item
{
    id: titles
    property real t: 0

    // kind: "title" (chrome, with a line under it), "card" (neon, at the bottom), or "end".
    // Each card starts and ends with its shot (the shots of Sim3D.qml), and names what the shot
    // shows.
    property var cards: [
        { t0: 0.6, t1: 7.4, kind: "title", text: "PRINTDECK", sub: "A TOUCH SCREEN FOR THE TAZ WORKHORSE" },
        { t0: 8.0, t1: 11.8, kind: "card", text: "PRINTED ON THE PRINTER IT MOUNTS ON" },
        { t0: 17.9, t1: 22.3, kind: "card", text: "A 27 MB SYSTEM" },
        { t0: 25.4, t1: 33.4, kind: "card", text: "YOUR ROLLS, THEIR COST, THEIR PRINTS" },
        { t0: 34.4, t1: 40.0, kind: "card", text: "THE OLD LCD IS THE SECOND DISPLAY" },
        { t0: 40.6, t1: 48.4, kind: "card", text: "THE CURA PLUGIN" },
        { t0: 60.9, t1: 65.6, kind: "card", text: "THE KLIPPER HOST, IN GO" },
        { t0: 66.2, t1: 72.2, kind: "card", text: "ONE RASPBERRY PI 5. ONE PROGRAM." },
        { t0: 72.6, t1: 80.5, kind: "end", text: "PRINTDECK", sub: "BEETLEBUG.ORG" }
    ]

    FontLoader { id: plex; source: "qrc:/film/plex.ttf" }

    // in is 0 before t0 and 1 after t0 + d; out is the same at t1.
    function rise(t0, d) { return Math.max(0, Math.min(1, (titles.t - t0) / d)) }
    function shown(c) { return Math.min(rise(c.t0, 0.35), 1 - rise(c.t1 - 0.35, 0.35)) }

    // Chrome letters: a sky-to-sunset gradient in the shape of the text, with a pink glow, and
    // leaning forward.
    component Chrome: Item
    {
        property alias text: mask.text
        property int size: 200
        width: mask.implicitWidth; height: mask.implicitHeight
        transform: Matrix4x4 { matrix: Qt.matrix4x4(1, -0.18, 0, 0.18 * height, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1) }
        Text
        {
            id: mask
            visible: false
            layer.enabled: true
            font.family: plex.name; font.pixelSize: parent.size; font.weight: Font.DemiBold
            font.letterSpacing: parent.size * 0.04
            color: "white"
        }
        Rectangle
        {
            id: metal
            anchors.fill: mask
            visible: false
            layer.enabled: true
            gradient: Gradient
            {
                GradientStop { position: 0.0; color: "#ffffff" }
                GradientStop { position: 0.40; color: "#9fd8ff" }
                GradientStop { position: 0.50; color: "#24124f" }
                GradientStop { position: 0.53; color: "#7a2a7a" }
                GradientStop { position: 0.75; color: "#ff9a52" }
                GradientStop { position: 1.0; color: "#fff4c9" }
            }
        }
        MultiEffect
        {
            anchors.fill: mask
            source: metal
            maskEnabled: true
            maskSource: mask
            shadowEnabled: true
            shadowColor: "#8a6cff"
            shadowBlur: 0.5
            shadowScale: 1.0
            shadowHorizontalOffset: 0; shadowVerticalOffset: 0
        }
    }

    // Neon letters: a bright core and a colored glow.
    component Neon: Item
    {
        property alias text: core.text
        property int size: 64
        property color glow: "#00e5ff"
        width: core.implicitWidth; height: core.implicitHeight
        Text
        {
            id: core
            font.family: plex.name; font.pixelSize: parent.size; font.weight: Font.DemiBold
            font.letterSpacing: parent.size * 0.08
            color: "#f4fdff"
            layer.enabled: true
            layer.effect: MultiEffect
            {
                shadowEnabled: true
                shadowColor: parent.glow
                shadowBlur: 0.5
                shadowScale: 1.0
                shadowHorizontalOffset: 0; shadowVerticalOffset: 0
            }
        }
    }

    Repeater
    {
        model: titles.cards
        Item
        {
            anchors.fill: parent
            property var c: modelData
            property real a: titles.shown(c)
            visible: a > 0
            opacity: a

            // The title and the end card: chrome in the middle, a neon line, and the line under it.
            Item
            {
                visible: c.kind != "card"
                anchors.fill: parent
                Chrome
                {
                    id: chrome
                    text: c.text
                    size: 230
                    anchors.horizontalCenter: parent.horizontalCenter
                    y: parent.height * 0.30 - height / 2
                    scale: 1.25 - 0.25 * Math.min(1, (titles.t - c.t0) / 1.2)
                }
                Rectangle
                {
                    anchors.horizontalCenter: parent.horizontalCenter
                    y: chrome.y + chrome.height + 10
                    height: 4; radius: 2
                    width: 1200 * titles.rise(c.t0 + 0.3, 0.8)
                    color: "#8a6cff"
                    layer.enabled: true
                    layer.effect: MultiEffect { shadowEnabled: true; shadowColor: "#ff2bd6"; shadowBlur: 0.8; shadowHorizontalOffset: 0; shadowVerticalOffset: 0 }
                }
                Neon
                {
                    text: c.sub || ""
                    size: 54
                    glow: "#6a5cff"
                    anchors.horizontalCenter: parent.horizontalCenter
                    y: chrome.y + chrome.height + 40
                    opacity: titles.rise(c.t0 + 0.8, 0.5)
                }
            }
            // A card: neon words at the bottom, which type in, with a line that grows under them.
            Item
            {
                visible: c.kind == "card"
                anchors.fill: parent
                Neon
                {
                    id: neon
                    property int shownChars: Math.ceil(c.text.length * titles.rise(c.t0, 0.9))
                    text: c.text.substring(0, shownChars) + (shownChars < c.text.length ? "▌" : "")
                    size: 66
                    glow: "#6a5cff"
                    x: (parent.width - fullWidth.width) / 2
                    y: parent.height * 0.80
                    Text { id: fullWidth; visible: false; text: c.text; font.family: plex.name; font.pixelSize: 66; font.weight: Font.DemiBold; font.letterSpacing: 66 * 0.08 }
                }
                Rectangle
                {
                    x: neon.x; y: neon.y + neon.height + 8
                    height: 3
                    width: fullWidth.width * titles.rise(c.t0 + 0.4, 0.8)
                    color: "#8a6cff"
                }
            }
        }
    }
}
