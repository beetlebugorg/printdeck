# The Printdeck plugin for Cura. The plugin module loads only in Cura, so the tests can import logic.


def getMetaData():
    return {}


def register(app):
    from . import PrintdeckPlugin

    return {"extension": PrintdeckPlugin.PrintdeckPlugin()}
