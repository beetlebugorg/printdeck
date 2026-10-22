// The panel of the Print on a part tool: the base, the new models on it, the pocket and the message
// of the pause.
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
    UM.I18nCatalog { id: catalog; name: "cura" }

    property bool hasBase: UM.Controller.properties.getValue("HasBase")

    ColumnLayout
    {
        spacing: UM.Theme.getSize("default_margin").height
        width: 300 * screenScaleFactor

        UM.Label
        {
            Layout.fillWidth: true
            wrapMode: Text.WordWrap
            text: hasBase ? "Base: " + UM.Controller.properties.getValue("BaseName")
                : "Lay the printed part on the bed, face up. Select it, and set it as the base."
        }

        Cura.SecondaryButton
        {
            text: "Set as base"
            onClicked: UM.Controller.triggerAction("setBase")
        }

        Cura.SecondaryButton
        {
            visible: hasBase
            text: "Place on the base"
            onClicked: UM.Controller.triggerAction("placeOnBase")
        }

        UM.CheckBox
        {
            visible: hasBase
            text: "The pocket is on the bed"
            checked: UM.Controller.properties.getValue("PocketOnBed")
            onClicked: UM.Controller.setProperty("PocketOnBed", checked)
        }

        RowLayout
        {
            visible: hasBase && !UM.Controller.properties.getValue("PocketOnBed")
            spacing: UM.Theme.getSize("default_margin").width
            UM.Label { text: "Clearance" }
            UM.TextFieldWithUnit
            {
                Layout.preferredWidth: 80 * screenScaleFactor
                height: UM.Theme.getSize("setting_control").height
                unit: "mm"
                text: UM.Controller.properties.getValue("Clearance")
                validator: UM.FloatValidator { maxBeforeDecimal: 1; maxAfterDecimal: 2 }
                onEditingFinished: UM.Controller.setProperty("Clearance", parseFloat(text))
            }
            UM.Label { text: "Wall" }
            UM.TextFieldWithUnit
            {
                Layout.preferredWidth: 80 * screenScaleFactor
                height: UM.Theme.getSize("setting_control").height
                unit: "mm"
                text: UM.Controller.properties.getValue("WallHeight")
                validator: UM.FloatValidator { maxBeforeDecimal: 2; maxAfterDecimal: 1 }
                onEditingFinished: UM.Controller.setProperty("WallHeight", parseFloat(text))
            }
        }

        UM.Label { visible: hasBase; text: "Message on the deck" }
        TextField
        {
            visible: hasBase
            Layout.fillWidth: true
            text: UM.Controller.properties.getValue("Message")
            onEditingFinished: UM.Controller.setProperty("Message", text)
        }

        UM.Label
        {
            visible: text != ""
            Layout.fillWidth: true
            wrapMode: Text.WordWrap
            color: UM.Theme.getColor("warning")
            text: UM.Controller.properties.getValue("Warning")
        }

        Cura.SecondaryButton
        {
            visible: hasBase
            text: "Clear the base"
            onClicked: UM.Controller.triggerAction("clearBase")
        }
    }
}
