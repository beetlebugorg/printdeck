// The sky of the film (film/), behind the 3D view: a dusk gradient, stars, and a faint sun with
// stripes at the horizon. t is the time of the film in seconds.
import QtQuick

Item
{
    id: sky
    property real t: 0

    Rectangle
    {
        anchors.fill: parent
        gradient: Gradient
        {
            GradientStop { position: 0.0; color: "#05060f" }
            GradientStop { position: 0.45; color: "#0e1024" }
            GradientStop { position: 0.62; color: "#2a1d3d" }
            GradientStop { position: 0.74; color: "#5a3248" }
            GradientStop { position: 1.0; color: "#15142a" }
        }
    }

    // The stars: fixed places, each with its own twinkle.
    Repeater
    {
        model: 90
        Rectangle
        {
            property real seed: (index * 7919 % 1000) / 1000
            x: (index * 1237 % 1000) / 1000 * sky.width
            y: Math.pow((index * 3571 % 1000) / 1000, 1.6) * sky.height * 0.5
            width: seed > 0.85 ? 3 : 2; height: width
            color: "#ffe9ff"
            opacity: 0.35 + 0.45 * Math.abs(Math.sin(sky.t * (1 + seed * 2) + seed * 40))
        }
    }

    // The glow of the sun, and the sun: yellow at the top to pink at the bottom, with stripes
    // that grow toward the bottom and move down slowly.
    Rectangle
    {
        width: 1100; height: 1100; radius: 550
        x: (sky.width - width) / 2; y: sky.height * 0.56 - height / 2
        gradient: Gradient
        {
            orientation: Gradient.Vertical
            GradientStop { position: 0.0; color: "#00ff2bd6" }
            GradientStop { position: 0.5; color: "#55ff4f9a" }
            GradientStop { position: 1.0; color: "#00ff2bd6" }
        }
        opacity: 0.25
    }
    Canvas
    {
        id: sun
        width: 620; height: 620
        opacity: 0.45
        x: (sky.width - width) / 2; y: sky.height * 0.56 - height / 2
        property real phase: (sky.t * 6) % 22
        onPhaseChanged: requestPaint()
        onPaint: {
            var c = getContext("2d")
            c.reset()
            var g = c.createLinearGradient(0, 0, 0, height)
            g.addColorStop(0, "#fff36b")
            g.addColorStop(0.45, "#ffb13d")
            g.addColorStop(0.75, "#ff3d7f")
            g.addColorStop(1, "#c4126b")
            c.fillStyle = g
            c.beginPath()
            c.arc(width / 2, height / 2, width / 2, 0, 2 * Math.PI)
            c.fill()
            // The stripes: cut out of the lower half.
            c.globalCompositeOperation = "destination-out"
            c.fillStyle = "black"
            for (var y = height * 0.5 + phase, i = 0; y < height; i++) {
                var h = 2 + i * 1.6
                c.fillRect(0, y, width, h)
                y += h + 22 - i * 1.2
            }
        }
    }
}
