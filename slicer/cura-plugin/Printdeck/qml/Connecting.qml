// The start screen of the deck while Cura connects: the blue dot-matrix display with PRINTDECK in
// the 8 x 16 font of the printer display, and a rule under it. The dots of the word print from the
// bottom row up, one layer at a time, then the word starts again.
import QtQuick 2.7

Item
{
    id: screen
    property bool running: false
    property string host: ""
    property string family: ""

    // The word in the 8 x 16 font: the rows 3 to 11 of the glyphs (the others are empty).
    readonly property var word: [
        "######..######....####..##...##..######.#####...#######...####..###..##.",
        ".##..##..##..##....##...###..##..######..##.##...##..##..##..##..##..##.",
        ".##..##..##..##....##...####.##..#.##.#..##..##..##...#.##....#..##.##..",
        ".##..##..##..##....##...#######....##....##..##..##.#...##.......##.##..",
        ".#####...#####.....##...##.####....##....##..##..####...##.......####...",
        ".##......##.##.....##...##..###....##....##..##..##.#...##.......##.##..",
        ".##......##..##....##...##...##....##....##..##..##...#.##....#..##.##..",
        ".##......##..##....##...##...##....##....##.##...##..##..##..##..##..##.",
        "####....###..##...####..##...##...####..#####...#######...####..###..##."]
    readonly property int cols: 72
    readonly property int rows: 9
    // The grid: the word, two empty rows, the rule, and the text under it.
    readonly property int gridRows: rows + 10
    readonly property real pitch: Math.max(3, Math.min((width - 48) / cols, (height - 48) / gridRows))
    readonly property real x0: Math.round((width - cols * pitch) / 2)
    readonly property real y0: Math.round((height - gridRows * pitch) / 2)

    // The layers of the word that are lit: 0 to rows, then a pause with the whole word.
    property int printed: 0
    readonly property int hold: 8

    Timer
    {
        interval: 110; repeat: true; running: screen.running
        onTriggered: { screen.printed = (screen.printed + 1) % (screen.rows + 1 + screen.hold); word.requestPaint() }
    }

    // The empty display: the background and the dots of the grid, drawn when the size changes.
    Canvas
    {
        id: grid
        anchors.fill: parent
        onWidthChanged: requestPaint()
        onHeightChanged: requestPaint()
        onPaint:
        {
            var ctx = getContext("2d")
            var p = screen.pitch
            ctx.fillStyle = "#1f4bd1"
            ctx.fillRect(0, 0, width, height)
            ctx.fillStyle = "#2a58e0"
            var fx = -Math.ceil(screen.x0 / p), fy = -Math.ceil(screen.y0 / p)
            for (var y = fy; screen.y0 + y * p < height; y++)
                for (var x = fx; screen.x0 + x * p < width; x++)
                    ctx.fillRect(screen.x0 + x * p + 1, screen.y0 + y * p + 1, p - 2, p - 2)
        }
    }

    // The word, the layers that are lit.
    Canvas
    {
        id: word
        anchors.fill: parent
        onWidthChanged: requestPaint()
        onHeightChanged: requestPaint()
        onPaint:
        {
            var ctx = getContext("2d")
            ctx.clearRect(0, 0, width, height)
            var p = screen.pitch
            var lit = Math.min(screen.printed, screen.rows) // the layers from the bottom
            ctx.fillStyle = "#f2f6ff"
            for (var y = screen.rows - lit; y < screen.rows; y++) {
                var row = screen.word[y]
                for (var x = 0; x < screen.cols; x++)
                    if (row.charAt(x) == "#")
                        ctx.fillRect(screen.x0 + x * p + 1, screen.y0 + y * p + 1, p - 2, p - 2)
            }
        }
    }

    // The rule, and what Cura does.
    Rectangle
    {
        x: screen.x0 + 1; width: screen.cols * screen.pitch - 2
        y: screen.y0 + (screen.rows + 2) * screen.pitch + screen.pitch / 2; height: Math.max(2, screen.pitch / 3)
        color: "#f2f6ff"
    }
    Text
    {
        anchors.horizontalCenter: parent.horizontalCenter
        y: screen.y0 + (screen.rows + 5) * screen.pitch
        text: screen.host ? "CONNECTING TO " + screen.host.toUpperCase() : "CONNECTING"
        color: "#8fa8ef"
        font.family: screen.family; font.bold: true; font.letterSpacing: 2
        font.pixelSize: Math.max(11, screen.pitch * 2.2)
    }
}
