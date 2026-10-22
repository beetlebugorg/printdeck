// The Printdeck tab: the screen of the deck, the print on a blue display, the roll, and the keys,
// in the style of the deck: a grey chassis, plates, dark windows, raised keys, and orange.
import QtQuick 2.7

import UM 1.3 as UM

Rectangle
{
    id: root
    color: "#d3d0c8" // the chassis

    property var deck: UM.Controller.activeStage
    property bool connected: deck ? deck.connected : false
    property string state: deck ? deck.state : "ready"
    property bool busy: state == "printing" || state == "paused"
    property bool askCancel: false

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
            y: area.pressed ? 4 : 0
            Text
            {
                anchors.centerIn: parent; text: key.text.toUpperCase(); color: key.ink
                font.family: plexBold.name; font.weight: Font.DemiBold; font.pixelSize: 16; font.letterSpacing: 1
            }
        }
        MouseArea { id: area; anchors.fill: parent; onClicked: key.clicked() }
    }

    // A label of the deck: small grey capitals.
    component Label: Text
    {
        color: "#4a4742"; font.family: plexBold.name; font.weight: Font.DemiBold; font.pixelSize: 12; font.letterSpacing: 1
    }

    // A meter of lit segments, in a window.
    component Meter: Rectangle
    {
        id: meter
        property real value: 0
        property color on: "#ff7a30"
        property color off: "#2a211a"
        height: 18; radius: 3; color: "#1b1c1a"
        Row
        {
            id: cells
            anchors.fill: parent; anchors.margins: 4; spacing: 3
            property int n: Math.max(1, Math.floor((width + spacing) / 9))
            Repeater
            {
                model: cells.n
                Rectangle
                {
                    width: 6; height: cells.height; radius: 1
                    color: index < Math.round(meter.value * cells.n) ? meter.on : meter.off
                }
            }
        }
    }

    Row
    {
        anchors.centerIn: parent
        spacing: 24
        visible: root.connected

        // The screen of the deck, in a dark bezel. Two images take turns, so that a new frame
        // shows only when it is loaded.
        Rectangle
        {
            width: Math.min(820, root.width - 460); height: width * 480 / 800 + 20
            radius: 10; color: "#1b1c1a"
            property int frame: 0
            Timer
            {
                interval: 1000; running: root.connected && root.visible; repeat: true; triggeredOnStart: true
                onTriggered:
                {
                    var url = root.deck ? root.deck.snapshotUrl : "";
                    if (url == "") return;
                    parent.frame++;
                    // Load the next frame into the image that does not show.
                    if (shotA.visible) shotB.source = url + "&f=" + parent.frame;
                    else shotA.source = url + "&f=" + parent.frame;
                }
            }
            Image
            {
                id: shotA
                anchors.fill: parent; anchors.margins: 10; fillMode: Image.PreserveAspectFit; cache: false
                visible: true
                onStatusChanged: if (status == Image.Ready) { shotA.visible = true; shotB.visible = false }
            }
            Image
            {
                id: shotB
                anchors.fill: parent; anchors.margins: 10; fillMode: Image.PreserveAspectFit; cache: false
                visible: false
                onStatusChanged: if (status == Image.Ready) { shotB.visible = true; shotA.visible = false }
            }
        }

        Column
        {
            width: 380
            spacing: 16

            // The print, on the blue display of the deck.
            Rectangle
            {
                width: parent.width; height: 196; radius: 8; color: "#1f4bd1"; border.color: "#2a58e0"; border.width: 2
                Column
                {
                    anchors.fill: parent; anchors.margins: 18; spacing: 8
                    Row
                    {
                        width: parent.width
                        Text
                        {
                            width: parent.width - pct.width
                            text: root.state == "paused" ? "PAUSED" : root.busy ? "PRINTING" : "READY"
                            color: "#f2f6ff"; font.family: plexBold.name; font.weight: Font.DemiBold; font.pixelSize: 26
                        }
                        Text
                        {
                            id: pct
                            text: root.busy ? Math.floor(root.deck.progress * 100) + "%" : ""
                            color: "#f2f6ff"; font.family: plexBold.name; font.weight: Font.DemiBold; font.pixelSize: 26
                        }
                    }
                    Text
                    {
                        width: parent.width; elide: Text.ElideMiddle
                        text: root.busy ? root.deck.file : "No print"
                        color: "#8fa8ef"; font.family: plex.name; font.pixelSize: 15
                    }
                    Meter { width: parent.width; value: root.busy ? root.deck.progress : 0; on: "#ff7a1a"; off: "#2a58e0"; color: "#173aa8" }
                    Text
                    {
                        visible: root.busy && root.deck.layers > 0
                        text: "LAYER " + (root.deck ? root.deck.layer : 0) + " OF " + (root.deck ? root.deck.layers : 0)
                        color: "#f2f6ff"; font.family: plexBold.name; font.weight: Font.DemiBold; font.pixelSize: 16
                    }
                    Text
                    {
                        visible: root.busy && root.deck.timeLeft != ""
                        text: root.deck ? root.deck.timeLeft + " LEFT · DONE " + root.deck.doneAt : ""
                        color: "#f2f6ff"; font.family: plexBold.name; font.weight: Font.DemiBold; font.pixelSize: 16
                    }
                }
            }

            // The roll in the printer, on a plate.
            Rectangle
            {
                width: parent.width; height: 128; radius: 8; color: "#e6e3dc"; border.color: "#a9a59c"
                Column
                {
                    anchors.fill: parent; anchors.margins: 16; spacing: 6
                    Label { text: "ROLL" }
                    Text
                    {
                        width: parent.width; elide: Text.ElideRight
                        text: root.deck && root.deck.rollName ? root.deck.rollName + " · " + root.deck.material
                            : root.deck && root.deck.material ? root.deck.material + " · no roll" : "No filament set"
                        color: "#171717"; font.family: plexBold.name; font.weight: Font.DemiBold; font.pixelSize: 18
                    }
                    Text
                    {
                        visible: root.deck && root.deck.rollFull > 0
                        text: root.deck ? Math.round(root.deck.rollLeft) + " g left of " + Math.round(root.deck.rollFull) + " g" : ""
                        color: "#4a4742"; font.family: plex.name; font.pixelSize: 14
                    }
                    Meter
                    {
                        width: parent.width; visible: root.deck && root.deck.rollFull > 0
                        value: root.deck && root.deck.rollFull > 0 ? root.deck.rollLeft / root.deck.rollFull : 0
                    }
                }
            }

            // The file of the print in the Preview of Cura.
            Key
            {
                width: parent.width; visible: root.busy
                text: "Show in Preview"
                onClicked: root.deck.openInPreview()
            }

            // The keys of the print.
            Row
            {
                width: parent.width; spacing: 12; visible: root.busy
                Key
                {
                    width: (parent.width - 12) / 2
                    text: root.state == "paused" ? "Resume" : "Pause"
                    cap: "#ff5a1f"; ink: "#121212"
                    onClicked: root.state == "paused" ? root.deck.resume() : root.deck.pause()
                }
                Key
                {
                    width: (parent.width - 12) / 2
                    text: root.askCancel ? "Tap to cancel" : "Cancel"
                    cap: root.askCancel ? "#c0291b" : "#f3f1ec"; ink: root.askCancel ? "#fbfaf7" : "#d12b1d"
                    onClicked:
                    {
                        if (root.askCancel) { root.askCancel = false; root.deck.cancel() }
                        else { root.askCancel = true; cancelTimer.restart() }
                    }
                    Timer { id: cancelTimer; interval: 3000; onTriggered: root.askCancel = false }
                }
            }
        }
    }

    // No Printdeck: how to connect one.
    Rectangle
    {
        anchors.centerIn: parent; visible: !root.connected
        width: 520; height: 150; radius: 8; color: "#e6e3dc"; border.color: "#a9a59c"
        Column
        {
            anchors.fill: parent; anchors.margins: 20; spacing: 10
            Label { text: "PRINTDECK" }
            Text
            {
                width: parent.width; wrapMode: Text.WordWrap
                text: "Cura has no connection to a Printdeck. Connect the printer with the OctoPrint connection plugin: Preferences > Printers > Connect OctoPrint."
                color: "#171717"; font.family: plex.name; font.pixelSize: 16
            }
        }
    }
}
