// The deck UI on a canvas: the drawing commands of each frame of the deck (gfx.Recording), as in
// page.html of the deck. The clicks go to the deck as touches. The deck object gives the frames, and
// the addresses of the images (imageBase + "N.png") and of the fonts (fontBase + "regular.ttf").
// The Cura plugin and the desktop app use this file.
import QtQuick 2.7

Canvas
{
    id: canvas
    property var deck
    property real sw: 800 // the size of the deck UI
    property real sh: 480
    property string family: ""

    FontLoader { id: regular; source: canvas.deck && canvas.deck.fontsReady ? canvas.deck.fontBase + "regular.ttf" : "" }
    FontLoader { id: bold; source: canvas.deck && canvas.deck.fontsReady ? canvas.deck.fontBase + "bold.ttf" : "" }

    Connections
    {
        target: canvas.deck
        function onFrameChanged() { canvas.requestPaint() }
    }
    onWidthChanged: requestPaint()
    onHeightChanged: requestPaint()
    onImageLoaded: requestPaint()

    function hex(n) { return "#" + ("000000" + n.toString(16)).slice(-6) }

    // The last image that each rectangle showed. A new image (for example the next picture of the
    // printer display) loads first, so the rectangle shows the old image until then, with no
    // flicker.
    property var shown: ({})

    // The URL of the image of a command, or of the last image of its rectangle while it loads, or "".
    function image(op) {
        var key = op.slice(1, 5).join(",")
        var url = deck.imageBase + op[5] + ".png"
        if (isImageLoaded(url)) {
            if (shown[key] && shown[key] != url) unloadImage(shown[key])
            shown[key] = url
            return url
        }
        if (!isImageLoading(url)) loadImage(url)
        return shown[key] || ""
    }

    onPaint:
    {
        var ctx = getContext("2d")
        ctx.reset()
        if (!deck || deck.frame == "") return
        var frame = JSON.parse(deck.frame)
        var k = width / sw
        var face = '"' + (regular.name || "monospace") + '"'
        ctx.setTransform(k, 0, 0, k, 0, 0)
        ctx.textBaseline = "alphabetic"
        for (var i = 0; i < frame.length; i++) {
            // A command that fails does not stop the others: the rest of the frame still shows.
            try { drawOp(ctx, frame[i], face) } catch (e) { console.warn("DeckCanvas: " + frame[i][0] + ": " + e) }
        }
    }

    // drawOp draws one drawing command of a frame.
    function drawOp(ctx, op, face) {
        switch (op[0]) {
        case "f":
            ctx.fillStyle = hex(op[5])
            ctx.fillRect(op[1], op[2], op[3] - op[1], op[4] - op[2])
            break
        case "r":
            ctx.fillStyle = hex(op[6])
            ctx.beginPath()
            ctx.roundedRect(op[1], op[2], op[3] - op[1], op[4] - op[2], op[5], op[5])
            ctx.fill()
            break
        case "p":
            var p = op[1]
            ctx.fillStyle = hex(op[2])
            ctx.beginPath()
            ctx.moveTo(p[0], p[1])
            for (var j = 2; j < p.length; j += 2) ctx.lineTo(p[j], p[j + 1])
            ctx.closePath()
            ctx.fill()
            break
        case "t":
            ctx.fillStyle = hex(op[6])
            ctx.font = (op[4] ? "bold " : "normal ") + op[5] + "px " + (op[4] && bold.name ? '"' + bold.name + '"' : face)
            ctx.fillText(op[3], op[1], op[2])
            break
        case "c": {
            // Fill the rectangle with the middle of the image.
            var url = image(op)
            if (url == "") break
            var w = op[3] - op[1], h = op[4] - op[2]
            var img = ctx.createImageData(url)
            var iw = img.width, ih = img.height
            var sx = 0, sy = 0, sw2 = iw, sh2 = ih
            if (iw / ih > w / h) { sw2 = ih * w / h; sx = (iw - sw2) / 2 } else { sh2 = iw * h / w; sy = (ih - sh2) / 2 }
            ctx.drawImage(url, sx, sy, sw2, sh2, op[1], op[2], w, h)
            break
        }
        case "i": {
            var u = image(op)
            if (u == "") break
            ctx.save()
            ctx.beginPath()
            ctx.rect(op[1], op[2], op[3] - op[1], op[4] - op[2])
            ctx.clip()
            ctx.drawImage(u, op[1], op[2])
            ctx.restore()
            break
        }
        case "d":
            ctx.fillStyle = Qt.rgba(0, 0, 0, 1 - 1 / op[1])
            ctx.fillRect(0, 0, sw, sh)
            break
        case "clip":
            ctx.save()
            ctx.beginPath()
            ctx.rect(op[1], op[2], op[3] - op[1], op[4] - op[2])
            ctx.clip()
            break
        case "unclip":
            ctx.restore()
            break
        }
    }

    // The clicks are the touches of the deck.
    MouseArea
    {
        anchors.fill: parent
        function send(kind, mouse) { if (canvas.deck) canvas.deck.touch(kind, mouse.x / width, mouse.y / height) }
        onPressed: function(mouse) { send("down", mouse) }
        onPositionChanged: function(mouse) { if (pressed) send("move", mouse) }
        onReleased: function(mouse) { send("up", mouse) }
        onCanceled: if (canvas.deck) canvas.deck.touch("up", 0, 0)
    }
}
