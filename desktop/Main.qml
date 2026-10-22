// The window of Printdeck for the desktop: the deck UI, as large as the window, with the shape of
// the deck UI. While printdeck app starts and connects, the window shows the start screen of the
// deck.
import QtQuick
import QtQuick.Window

Window
{
    id: win
    width: 800; height: 480
    minimumWidth: 400; minimumHeight: 240
    visible: true
    title: "Printdeck"
    color: "#1b1c1a"

    FontLoader { id: plexBold; source: printdeck.fontBase ? printdeck.fontBase + "bold.ttf" : "" }

    Item
    {
        id: area
        anchors.fill: parent
        // The keyboard of the computer types on the deck: in its keyboard, its keypad, its
        // questions; Escape goes back.
        focus: true
        Keys.onPressed: function(event) {
            var name = ""
            switch (event.key) {
            case Qt.Key_Return:
            case Qt.Key_Enter: name = "Enter"; break
            case Qt.Key_Escape: name = "Escape"; break
            case Qt.Key_Backspace: name = "Backspace"; break
            }
            if (name != "") {
                printdeck.key("", name)
                event.accepted = true
            } else if (event.text.length > 0 && event.text.charCodeAt(0) >= 32) {
                printdeck.key(event.text, "")
                event.accepted = true
            }
        }
        onWidthChanged: printdeck.setArea(width, height)
        onHeightChanged: printdeck.setArea(width, height)
        Component.onCompleted: printdeck.setArea(width, height)

        property real k: Math.min(width / printdeck.screenWidth, height / printdeck.screenHeight)

        DeckCanvas
        {
            anchors.centerIn: parent
            width: printdeck.screenWidth * area.k; height: printdeck.screenHeight * area.k
            visible: printdeck.view == "live"
            deck: printdeck
            sw: printdeck.screenWidth; sh: printdeck.screenHeight
        }
        Connecting
        {
            anchors.fill: parent
            visible: printdeck.view == "starting" || printdeck.view == "connecting"
            running: visible
            host: printdeck.host
            family: plexBold.name
        }
        Text
        {
            anchors.centerIn: parent
            width: Math.min(parent.width - 48, 560)
            visible: printdeck.view == "failed"
            wrapMode: Text.WordWrap
            horizontalAlignment: Text.AlignHCenter
            color: "#f3f1ec"; font.pixelSize: 18
            text: "Printdeck stopped.\n" + printdeck.error
        }
    }
}
