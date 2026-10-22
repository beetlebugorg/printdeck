// The titles of the film (film/), over the 3D view, in the style of the printer display: each card
// is a blue dot-matrix display in a dark bezel, and its dots light up from the bottom row, as on
// the start screen of the deck. The cards are in film/cards.json; filmcards (deck/cmd/filmcards)
// draws their pictures into filmCards. t is the time of the film in seconds.
import QtQuick
import QtQuick.Effects

Item
{
    id: titles
    property real t: 0
    property var cards: sim.resource(":/film/cards.json") || []

    // rise is 0 before t0 and 1 after t0 + d.
    function rise(t0, d) { return Math.max(0, Math.min(1, (titles.t - t0) / d)) }

    // A display: the blank dots, and the lit dots row by row from the bottom over d seconds from
    // t0, in a dark bezel with a soft blue glow.
    component Panel: Item
    {
        id: panel
        property string name
        property real t0
        property real d: 0.9
        property int pitch: 7 // the pixels of a dot
        width: blank.implicitWidth + 24; height: blank.implicitHeight + 24
        Rectangle
        {
            anchors.fill: parent
            radius: 14
            color: "#0b1236"
            border.color: "#24346f"; border.width: 2
            layer.enabled: true
            layer.effect: MultiEffect { shadowEnabled: true; shadowColor: "#3a6bff"; shadowBlur: 0.7; shadowOpacity: 0.6; shadowHorizontalOffset: 0; shadowVerticalOffset: 0 }
        }
        Image { id: blank; x: 12; y: 12; source: "file://" + filmCards + "/" + panel.name + "-blank.png"; smooth: false }
        Item
        {
            property int rows: Math.round(blank.implicitHeight / panel.pitch)
            property int lit: Math.floor(rows * titles.rise(panel.t0, panel.d))
            x: 12; width: blank.implicitWidth
            height: lit * panel.pitch
            y: 12 + blank.implicitHeight - height
            clip: true
            Image { y: -(blank.implicitHeight - parent.height); source: "file://" + filmCards + "/" + panel.name + ".png"; smooth: false }
        }
    }

    Repeater
    {
        model: titles.cards
        Item
        {
            anchors.fill: parent
            property var c: modelData
            // The display turns on at t0 and off at t1.
            property real a: Math.min(titles.rise(c.t0, 0.15), 1 - titles.rise(c.t1 - 0.25, 0.25))
            visible: a > 0
            opacity: a

            // The title and the end card: PRINTDECK in the middle, and the line under it.
            Panel
            {
                visible: c.kind != "card"
                name: String(index)
                t0: c.t0 + 0.1
                d: 1.2
                pitch: 11
                anchors.horizontalCenter: parent.horizontalCenter
                y: parent.height * 0.30 - height / 2
            }
            Panel
            {
                visible: c.kind != "card" && (c.sub || "") != ""
                name: String(index) + "-sub"
                t0: c.t0 + 1.0
                d: 0.6
                pitch: 6
                anchors.horizontalCenter: parent.horizontalCenter
                y: parent.height * 0.30 + 190
            }
            // A card: a small display at the bottom.
            Panel
            {
                visible: c.kind == "card"
                name: String(index)
                t0: c.t0 + 0.05
                d: 0.6
                pitch: 7
                anchors.horizontalCenter: parent.horizontalCenter
                y: parent.height * 0.78
            }
        }
    }
}
