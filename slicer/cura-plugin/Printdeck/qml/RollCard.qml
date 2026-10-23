// The roll card next to the slice button: the roll in the Printdeck, a bar of the filament that is
// left on it, and after a slice, the filament of the model. printdeckRoll is the RollCard of the
// plugin, a property of the context of the card.
import QtQuick 2.7

import UM 1.5 as UM

Rectangle
{
    id: card
    property var roll: printdeckRoll

    visible: roll ? roll.shown : false
    width: UM.Theme.getSize("default_margin").width * 18
    height: content.height + UM.Theme.getSize("default_margin").height * 2
    radius: UM.Theme.getSize("default_radius").width
    color: UM.Theme.getColor("main_background")
    border.width: UM.Theme.getSize("default_lining").width
    border.color: roll && roll.short ? UM.Theme.getColor("error") : UM.Theme.getColor("lining")

    Column
    {
        id: content
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        anchors.margins: UM.Theme.getSize("default_margin").width
        spacing: UM.Theme.getSize("narrow_margin").height

        Row
        {
            spacing: UM.Theme.getSize("default_margin").width

            // A dot in the color of the roll. The ring shows a light color on the light panel.
            Rectangle
            {
                width: UM.Theme.getSize("section_icon").width
                height: width
                radius: width / 2
                anchors.verticalCenter: parent.verticalCenter
                color: roll && roll.color != "" ? roll.color : "transparent"
                border.width: UM.Theme.getSize("default_lining").width
                border.color: UM.Theme.getColor("text_inactive")
            }
            Column
            {
                UM.Label
                {
                    text: roll ? roll.name : ""
                    font: UM.Theme.getFont("default_bold")
                    width: content.width - UM.Theme.getSize("section_icon").width - UM.Theme.getSize("default_margin").width
                    elide: Text.ElideRight
                }
                UM.Label
                {
                    text: roll ? roll.detail : ""
                    color: UM.Theme.getColor("text_medium")
                }
            }
        }

        // The filament on the roll.
        Rectangle
        {
            width: parent.width
            height: UM.Theme.getSize("narrow_margin").height * 1.5
            radius: height / 2
            color: UM.Theme.getColor("progressbar_background")
            Rectangle
            {
                width: parent.width * (roll ? roll.fraction : 0)
                height: parent.height
                radius: parent.radius
                color: roll && roll.short ? UM.Theme.getColor("error") : UM.Theme.getColor("progressbar_control")
            }
        }

        UM.Label
        {
            visible: text != ""
            text: roll ? roll.job : ""
            width: parent.width
            wrapMode: Text.WordWrap
            color: roll && roll.short ? UM.Theme.getColor("error") : UM.Theme.getColor("text")
        }
    }
}
