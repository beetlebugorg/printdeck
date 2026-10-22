# The Printdeck plugin for Cura: an extension that follows the Printdeck, the Printdeck tab, and the
# Print on a part tool. The plugin modules load only in Cura, so the tests can import logic and
# onpart.


def getMetaData():
    return {
        "stage": {"name": "Printdeck", "weight": 35},
        "tool": {
            "name": "Print on a part",
            "description": "Print new models on a printed part, in a pocket on the bed",
            "icon": "PrintTopBottom",
            "tool_panel": "qml/OnPartPanel.qml",
            "weight": 10,
        },
    }


def register(app):
    from . import OnPartTool, PrintdeckPlugin, PrintdeckStage

    stage = PrintdeckStage.PrintdeckStage()
    return {"extension": PrintdeckPlugin.PrintdeckPlugin(stage), "stage": stage, "tool": OnPartTool.OnPartTool()}
