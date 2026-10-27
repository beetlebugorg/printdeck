// The Camera window: the picture of the camera of the deck, from /webcam/snapshot with the token
// of the window (Factory). The window asks for the next picture when the last one has loaded, at
// most 15 times a second, at the width of the window. Two images take turns, so the picture does
// not flicker while the next one loads. With no camera, the deck sends a picture of its screen.
// The window shows the main camera of the deck. A click shows the next camera (the camera
// parameter of the webcam), and a camera that the deck does not have goes back to the main camera.
import QtQuick
import QtQuick.Window

Window
{
    id: cam
    width: 800; height: 450
    minimumWidth: 320; minimumHeight: 180
    title: "Printdeck camera"
    color: "#1b1c1a"

    property int n: 0
    property int camera: 0 // 0 for the main camera, or the number of a camera
    property bool showA: true // a shows and b loads, or the other way

    function next() {
        if (!visible)
            return
        if (printdeck.snapshotUrl == "") {
            retry.start() // the window does not know the deck yet
            return
        }
        n++
        var w = Math.min(1920, Math.ceil(width * Screen.devicePixelRatio))
        var url = printdeck.snapshotUrl + "?w=" + w + "&n=" + n
        if (camera > 0)
            url += "&camera=" + camera
        if (showA)
            b.source = url
        else
            a.source = url
    }
    function loaded(img) {
        if (img.status == Image.Ready) {
            showA = img === a
            wait.start()
        } else if (img.status == Image.Error) {
            if (camera > 0) {
                camera = 0 // no camera of that number: the main camera again
                next()
                return
            }
            retry.start()
        }
    }
    onVisibleChanged: if (visible) next()
    Component.onCompleted: next()

    Image
    {
        id: a
        anchors.fill: parent
        fillMode: Image.PreserveAspectFit
        cache: false; asynchronous: true
        visible: cam.showA
        onStatusChanged: cam.loaded(a)
    }
    Image
    {
        id: b
        anchors.fill: parent
        fillMode: Image.PreserveAspectFit
        cache: false; asynchronous: true
        visible: !cam.showA
        onStatusChanged: cam.loaded(b)
    }
    MouseArea
    {
        anchors.fill: parent
        onClicked: cam.camera = cam.camera == 0 ? 2 : cam.camera + 1
    }
    Text
    {
        anchors { left: parent.left; top: parent.top; margins: 10 }
        visible: cam.camera > 0
        color: "#f3f1ec"; font.pixelSize: 14
        text: "Camera " + cam.camera
    }
    Timer { id: wait; interval: 66; onTriggered: cam.next() }
    Timer { id: retry; interval: 1000; onTriggered: cam.next() }
    Text
    {
        anchors.centerIn: parent
        visible: a.status != Image.Ready && b.status != Image.Ready
        color: "#f3f1ec"; font.pixelSize: 18
        text: "Waiting for the camera…"
    }
}
