// The panel of the Colors tool: the rolls of the Printdeck for the selected models, and the color
// changes of the print.
import QtQuick 2.15
import QtQuick.Controls 2.2
import QtQuick.Layouts 1.3

import UM 1.5 as UM
import Cura 1.0 as Cura

Item
{
    id: base
    width: childrenRect.width
    height: childrenRect.height

    property var rolls: UM.Controller.properties.getValue("Rolls") || []
    property string selected: UM.Controller.properties.getValue("Selected") || ""
    property string loaded: UM.Controller.properties.getValue("Loaded") || ""
    property string plan: UM.Controller.properties.getValue("Plan") || ""
    property bool hasSelection: UM.Controller.properties.getValue("HasSelection")

    ColumnLayout
    {
        spacing: UM.Theme.getSize("default_margin").height
        width: 300 * screenScaleFactor

        UM.Label
        {
            Layout.fillWidth: true
            wrapMode: Text.WordWrap
            text: rolls.length == 0 ? "Connect to the Printdeck for its rolls."
                : !hasSelection ? "Select the models of another color, for example the letters on a part."
                : selected != "" ? "Selected models: " + selected
                : "Pick the roll of the selected models."
        }

        // The rolls: a key for each, with a dot in its color.
        Repeater
        {
            model: hasSelection ? rolls : []
            delegate: Cura.SecondaryButton
            {
                Layout.fillWidth: true
                text: modelData.name
                iconSource: ""
                onClicked: UM.Controller.triggerActionWithData("setRoll", modelData.id)
                Rectangle
                {
                    width: 14 * screenScaleFactor
                    height: width
                    radius: width / 2
                    anchors.right: parent.right
                    anchors.rightMargin: 10 * screenScaleFactor
                    anchors.verticalCenter: parent.verticalCenter
                    color: modelData.color != "" ? modelData.color : "transparent"
                    border.width: 1
                    border.color: UM.Theme.getColor("lining")
                }
            }
        }

        Cura.SecondaryButton
        {
            visible: hasSelection && selected != ""
            text: "No roll: the roll of the start"
            onClicked: UM.Controller.triggerAction("clearRoll")
        }

        UM.Label
        {
            Layout.fillWidth: true
            wrapMode: Text.WordWrap
            visible: loaded != ""
            color: UM.Theme.getColor("text_medium")
            text: "The print starts with " + loaded + ", the roll in the printer."
        }

        UM.Label
        {
            Layout.fillWidth: true
            wrapMode: Text.WordWrap
            visible: plan != ""
            text: "The printer pauses for each change:\n" + plan
        }
    }
}
