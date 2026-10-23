// The roll card next to the slice button: the roll in the Printdeck, a bar of the filament that is
// left on it, and after a slice, the filament of the model. printdeckRoll is the RollCard of the
// plugin, a property of the context of the card.
//
// Cura puts the card in a row left of its action panel (ActionPanelWidget.qml): the row is centered
// on a line two thick margins above the bottom of the panel. The card has the height, the frame and
// the margins of the panel, and moves down so that its bottom is the bottom of the panel.
import QtQuick 2.7

import UM 1.5 as UM

Rectangle
{
    id: card
    property var roll: printdeckRoll
    property real margin: UM.Theme.getSize("thick_margin").width

    // The action panel: the sibling of the area of the card with the property outputAvailable.
    property Item panel:
    {
        var area = card.parent ? card.parent.parent : null
        var base = area ? area.parent : null
        if (!base) return null
        for (var i = 0; i < base.children.length; i++)
        {
            if (base.children[i].outputAvailable !== undefined) return base.children[i]
        }
        return null
    }

    visible: roll ? roll.shown : false
    width: UM.Theme.getSize("action_panel_widget").width * 0.75
    height: Math.max(content.height + 2 * margin, panel ? panel.height : 0)
    y: 2 * UM.Theme.getSize("thick_margin").height - height / 2
    radius: UM.Theme.getSize("default_radius").width
    color: UM.Theme.getColor("main_background")
    border.width: UM.Theme.getSize("default_lining").width
    border.color: roll && roll.short ? UM.Theme.getColor("error") : UM.Theme.getColor("lining")

    Column
    {
        id: content
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.verticalCenter: parent.verticalCenter
        anchors.leftMargin: card.margin
        anchors.rightMargin: card.margin
        spacing: UM.Theme.getSize("default_margin").height

        Row
        {
            width: parent.width
            spacing: UM.Theme.getSize("default_margin").width

            // A dot in the color of the roll. The ring shows a light color on the light panel.
            Rectangle
            {
                id: dot
                width: UM.Theme.getSize("section_icon").width * 1.5
                height: width
                radius: width / 2
                anchors.verticalCenter: parent.verticalCenter
                color: roll && roll.color != "" ? roll.color : "transparent"
                border.width: UM.Theme.getSize("default_lining").width
                border.color: UM.Theme.getColor("text_inactive")
            }
            Column
            {
                width: parent.width - dot.width - parent.spacing
                UM.Label
                {
                    width: parent.width
                    text: roll ? roll.name : ""
                    font: UM.Theme.getFont("large_bold")
                    color: UM.Theme.getColor("text")
                    elide: Text.ElideRight
                }
                UM.Label
                {
                    width: parent.width
                    text: roll ? roll.detail : ""
                    font: UM.Theme.getFont("default")
                    color: UM.Theme.getColor("text_medium")
                    elide: Text.ElideRight
                }
            }
        }

        // The filament on the roll.
        Rectangle
        {
            width: parent.width
            height: UM.Theme.getSize("progressbar").height
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
            width: parent.width
            text: roll ? roll.job : ""
            font: UM.Theme.getFont("default")
            wrapMode: Text.WordWrap
            color: roll && roll.short ? UM.Theme.getColor("error") : UM.Theme.getColor("text")
        }
    }
}
