# The Printdeck plugin for Cura: an extension that follows the Printdeck, the Printdeck tab, and the
# Print on a part and Colors tools. The plugin modules load only in Cura, so the tests can import logic and
# onpart.


def getMetaData():
    return {
        "stage": {"name": "Printdeck", "weight": 35},
        "tool": [
            {
                "name": "Print on a part",
                "description": "Print new models on a printed part, in a pocket on the bed",
                "icon": "PrintTopBottom",
                "tool_panel": "qml/OnPartPanel.qml",
                "weight": 10,
            },
            {
                "name": "Colors",
                "description": "Print the selected models with another roll: the printer pauses to change the filament",
                "icon": "Spool",
                "tool_panel": "qml/ColorPanel.qml",
                "weight": 11,
            },
        ],
    }


def register(app):
    from . import ColorTool, OnPartTool, PrintdeckPlugin, PrintdeckStage

    stage = PrintdeckStage.PrintdeckStage()
    return {"extension": PrintdeckPlugin.PrintdeckPlugin(stage), "stage": stage,
            "tool": [OnPartTool.OnPartTool(), ColorTool.ColorTool()]}
