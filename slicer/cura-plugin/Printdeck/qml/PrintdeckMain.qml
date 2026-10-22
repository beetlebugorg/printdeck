// The Printdeck tab: the deck UI, live, in a dark bezel on the grey chassis of the deck. While Cura
// connects, the bezel shows the start screen of the deck.
import QtQuick 2.7

import UM 1.3 as UM

Rectangle
{
    id: root
    color: "#d3d0c8" // the chassis

    property var deck: UM.Controller.activeStage
    property string view: deck ? deck.view : "connecting"
    property bool busy: deck ? deck.state == "printing" || deck.state == "paused" : false

    FontLoader { id: plex; source: "../fonts/IBMPlexMono-Regular.ttf" }
    FontLoader { id: plexBold; source: "../fonts/IBMPlexMono-SemiBold.ttf" }

    // A key of the deck: a raised cap on a darker edge. It goes down when pressed.
    component Key: Item
    {
        id: key
        property string text: ""
        property color cap: "#f3f1ec"
        property color ink: "#171717"
        signal clicked()
        height: 56
        Rectangle { anchors.fill: parent; anchors.topMargin: 5; radius: 6; color: Qt.darker(key.cap, 1.35) }
        Rectangle
        {
            width: parent.width; height: parent.height - 5; radius: 6; color: key.cap
            y: keyArea.pressed ? 4 : 0
            Text
            {
                anchors.centerIn: parent; text: key.text.toUpperCase(); color: key.ink
                font.family: plexBold.name; font.weight: Font.DemiBold; font.pixelSize: 16; font.letterSpacing: 1
            }
        }
        MouseArea { id: keyArea; anchors.fill: parent; onClicked: key.clicked() }
    }

    // The deck UI, or the start screen while Cura connects: a screen in a dark bezel, as large as
    // the tab allows, with the shape of the deck UI.
    Item
    {
        id: area
        anchors.fill: parent
        anchors.margins: 24
        anchors.bottomMargin: preview.visible ? 24 + preview.height + 16 : 24
        visible: root.view == "live" || root.view == "connecting"
        onWidthChanged: if (root.deck) root.deck.setArea(width - 20, height - 20)
        onHeightChanged: if (root.deck) root.deck.setArea(width - 20, height - 20)

        property real sw: root.deck ? root.deck.screenWidth : 800
        property real sh: root.deck ? root.deck.screenHeight : 480
        property real k: Math.min((width - 20) / sw, (height - 20) / sh)

        Rectangle
        {
            anchors.centerIn: parent
            width: area.sw * area.k + 20; height: area.sh * area.k + 20
            radius: 10; color: "#1b1c1a"

            DeckCanvas
            {
                x: 10; y: 10; width: area.sw * area.k; height: area.sh * area.k
                visible: root.view == "live"
                deck: root.deck
                sw: area.sw; sh: area.sh
            }
            Connecting
            {
                x: 10; y: 10; width: area.sw * area.k; height: area.sh * area.k
                visible: root.view == "connecting"
                running: visible && root.visible
                host: root.deck ? root.deck.host : ""
                family: plexBold.name
            }
        }
    }

    // The file of the print in the Preview of Cura, under the deck UI.
    Key
    {
        id: preview
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.bottom: parent.bottom; anchors.bottomMargin: 24
        width: 300; visible: root.view == "live" && root.busy
        text: "Show in Preview"
        onClicked: root.deck.openInPreview()
    }

    // No Printdeck, or a Printdeck that refused the screen.
    Rectangle
    {
        anchors.centerIn: parent; visible: root.view == "none" || root.view == "refused"
        width: 520; height: message.implicitHeight + 70; radius: 8; color: "#e6e3dc"; border.color: "#a9a59c"
        Column
        {
            anchors.fill: parent; anchors.margins: 20; spacing: 10
            Text
            {
                text: "PRINTDECK"; color: "#4a4742"
                font.family: plexBold.name; font.weight: Font.DemiBold; font.pixelSize: 12; font.letterSpacing: 1
            }
            Text
            {
                id: message
                width: parent.width; wrapMode: Text.WordWrap
                text: root.view == "refused"
                    ? "The Printdeck refused the screen. Update the Printdeck OS, and check the API key of the OctoPrint connection."
                    : "Cura has no connection to a Printdeck. Connect the printer with the OctoPrint connection plugin: Preferences > Printers > Connect OctoPrint."
                color: "#171717"; font.family: plex.name; font.pixelSize: 16
            }
        }
    }
}
