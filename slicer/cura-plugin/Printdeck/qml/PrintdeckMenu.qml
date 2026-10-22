// The header of the Printdeck tab: the printer selector, as on the Monitor tab of Cura.
import QtQuick 2.7
import QtQuick.Controls 2.3

import UM 1.3 as UM
import Cura 1.1 as Cura

Item
{
    signal showTooltip(Item item, point location, string text)
    signal hideTooltip()

    Cura.MachineSelector
    {
        id: machineSelection
        headerCornerSide: Cura.RoundedRectangle.Direction.All
        width: UM.Theme.getSize("machine_selector_widget").width
        height: parent.height
        anchors.centerIn: parent
        machineListModel: Cura.MachineListModel {}
        machineManager: Cura.MachineManager
        onSelectPrinter: function(machine)
        {
            toggleContent();
            Cura.MachineManager.setActiveMachine(machine.id);
        }
    }
}
