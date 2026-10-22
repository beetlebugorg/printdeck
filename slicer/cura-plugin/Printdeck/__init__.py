# The Printdeck plugin for Cura: an extension that follows the Printdeck, and the Printdeck tab.
# The plugin modules load only in Cura, so the tests can import logic.


def getMetaData():
    return {"stage": {"name": "Printdeck", "weight": 35}}


def register(app):
    from . import PrintdeckPlugin, PrintdeckStage

    stage = PrintdeckStage.PrintdeckStage()
    return {"extension": PrintdeckPlugin.PrintdeckPlugin(stage), "stage": stage}
