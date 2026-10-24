# The color changes of a print with one nozzle: models with a roll of the Printdeck print in the
# filament of that roll. The file pauses at the first layer of those models, and the deck changes
# the filament to the roll (PRINTDECK_PAUSE ROLL). When other models go on above them, the file
# pauses again at the first layer over them, for the roll of the rest of the print.
#
# For example a blank part, and raised lettering on its top in another color: the part prints, the
# printer parks at the first layer of the letters, and the deck asks for the roll of the letters.

from . import onpart

# A layer is in a change when the middle of the layer is more than this over the bottom of the
# colored models. Cura slices a layer at its middle, and the Z of a layer is its top: with a first
# layer of 0.25 mm, the layer at Z 3.05 is from 2.85 to 3.05 mm, so it is part of a plate of 3 mm.
EPS = 0.01


def plan(groups, rest_top, start):
    """The changes of a print. groups is a list of the colored models, each a dict with roll (the ID
    of the roll), name, bottom and top (mm). rest_top is the top of the models with no roll, or 0.
    start is the roll at the start of the print, a dict with roll and name (the roll in the
    printer), or None.

    It returns a list of (z, roll, name), ordered by z: at the first layer over z, change to the
    roll. A change at the bed is no change: the print starts with that roll. Two changes at one
    height become the last one."""
    events = {}
    for g in groups:
        if g["bottom"] > EPS:
            events[round(g["bottom"], 3)] = (g["roll"], g["name"])
        # Back to the roll of the start, when other models go on above the colored ones.
        higher = rest_top > g["top"] + EPS or any(o["top"] > g["top"] + EPS and o["bottom"] <= g["top"] + EPS
                                                    for o in groups if o is not g)
        if start and higher:
            events.setdefault(round(g["top"], 3), (start["roll"], start["name"]))
    out = []
    last = start["roll"] if start else None
    for z in sorted(events):
        roll, name = events[z]
        if roll == last:
            continue
        out.append((z, roll, name))
        last = roll
    return out


def message(name):
    """The message of the pause for a roll."""
    return "Load %s" % name


def insert_change(gcode_list, z, roll, name, lift=onpart.LIFT):
    """Inserts the pause of a change to a roll at the start of the first layer whose middle is over z,
    after its ;LAYER: line, as onpart.insert_pause does for a part. It returns the index of the
    layer, or -1."""
    zl_last, below = 0.0, 0.0  # the last Z, and the Z of the layer before this one
    for i, chunk in enumerate(gcode_list):
        if chunk.startswith(";LAYER:"):
            zl = onpart.layer_start_z(chunk, zl_last)
            if (zl + below) / 2 > z + EPS:
                xy = None
                if i > 0:
                    gcode_list[i - 1], xy = onpart.take_travel(gcode_list[i - 1])
                head, _, rest = chunk.partition("\n")
                gcode_list[i] = head + "\n" + pause_lines(roll, name, zl + lift, zl, xy) + rest
                return i
            below = zl
        zl_last = onpart.last_z(chunk, zl_last)
    return -1


def pause_lines(roll, name, lift_z, layer_z, xy=None):
    """The G-code of a change: a lift, PRINTDECK_PAUSE with the message and the roll, the travel to
    the first point of the layer, and the move down to the layer."""
    msg = message(name).replace('"', "'")
    out = ";PRINTDECK: change the filament to %s\nG1 Z%.3f F600\n" % (msg[5:], lift_z)
    out += 'PRINTDECK_PAUSE MESSAGE="%s" ROLL=%s\n' % (msg, roll)
    if xy:
        out += "G0 F6000 X%.3f Y%.3f\n" % xy
    return out + "G1 Z%.3f F600\n" % layer_z


def apply(gcode_list, changes):
    """Inserts the changes of plan into the G-code of Cura, from the highest: the lift of a pause
    looks like a higher layer to the search of a change under it. It returns the changes that found
    no layer (a model over the top of the print)."""
    missing = []
    for z, roll, name in sorted(changes, reverse=True):
        if insert_change(gcode_list, z, roll, name) < 0:
            missing.append((z, roll, name))
    return missing
