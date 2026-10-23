# Play in the Preview of Cura follows the printer. The Printdeck sends the bytes of the file that the
# printer has read (job "pos"). This module finds the move at that position in the G-code file that
# the Preview shows, and the time of the Preview for it. It needs no Cura, so the tests can use it.
#
# Cura reads a G-code file with its FlavorParser (plugins/GCodeReader). The Preview counts the lines
# of a layer from it: each G0 or G1 adds a point to the path of the layer, and a line joins two
# points. A ";LAYER:" comment or a T code ends the path, and a new path starts from the last point.
# The list of the line durations of a layer (SimulationView.cumulativeLineDuration) has one more
# entry after each path. GcodeMap counts the moves in the same way.

import bisect
import re

# A number after the first "G" of a line, as FlavorParser._getInt(line, "G") reads it.
_G = re.compile(r"G([^;\s]*)")
_LAYER = ";LAYER:"

# The Preview steps back to the printer only when it is ahead by more than this, in the seconds of
# the Preview. When it is ahead by less, it waits for the printer: a small step back stutters.
BACK_LIMIT = 3.0
# The longest time that the Preview moves on from the last answer of the Printdeck, in seconds.
EXTRAPOLATE = 5.0


class GcodeMap:
    """The moves of a G-code file: for each G0 or G1, the byte after its line, its layer in the
    Preview, and the index of the start of its line in the line durations of the layer."""

    def __init__(self, ends, layers, starts, size):
        self.ends, self.layers, self.starts, self.size = ends, layers, starts, size

    @classmethod
    def parse(cls, data):
        """Reads the bytes of a G-code file."""
        ends, layers, starts = [], [], []
        layers_in_file = (b"\n" + _LAYER.encode()) in data or data.startswith(_LAYER.encode())
        layer = 0  # the layer of the Preview, as FlavorParser._layer_number
        points = 0  # the points of the path that is open
        base = {}  # the entries of the closed paths of each layer in the line durations
        min_layer, negative = 0, 0

        def close():
            # FlavorParser._createPolygon: a path of 2 points or more is a polygon of the layer.
            # Its lines and one more entry are in the line durations of the layer.
            if points >= 2:
                base[layer] = base.get(layer, 0) + (points - 1) + 1

        offset = 0
        for raw in data.split(b"\n"):
            end = offset + len(raw) + 1
            line = raw.decode("utf-8", "replace")
            offset = end
            if not line:
                continue
            if layers_in_file and line.startswith(_LAYER):
                try:
                    n = int(line[len(_LAYER):])
                except ValueError:
                    continue
                close()
                points = 1  # the new path starts at the end of the last one
                if n < min_layer:
                    min_layer = n
                if n < 0:
                    n += abs(min_layer)
                    negative += 1
                else:
                    n += negative
                layer = n
                continue
            if line.startswith(";"):
                continue
            m = _G.search(line)
            if m:
                try:
                    g = int(m.group(1))
                except ValueError:
                    g = None
                if g is not None:
                    if g in (0, 1):
                        points += 1
                        ends.append(end)
                        layers.append(layer)
                        # The line of this move is the line before the new point.
                        starts.append(base.get(layer, 0) + max(0, points - 2))
                    continue
            if line.startswith("T"):
                try:
                    int(line[1:].split(";")[0].split()[0])
                except (ValueError, IndexError):
                    continue
                close()
                points = 2  # the point before the tool change and the point after it
        return cls(ends, layers, starts, len(data))

    def locate(self, pos):
        """The layer and the start index of the move that the printer does when it has read pos
        bytes: the last move that it has read. None before the first move."""
        i = bisect.bisect_right(self.ends, pos) - 1
        if i < 0:
            return None
        return self.layers[i], self.starts[i]


def file_pos(job, size):
    """The bytes of the file that the printer has read: the "pos" of the job, or else the progress
    times the size of the file, from an older Printdeck."""
    if job.get("pos") is not None:
        return int(job["pos"])
    return int(float(job.get("progress") or 0) * size)


def follow_time(durations, start, elapsed, current, factor=2.0):
    """The time of the Preview on its layer for the printer.

    durations is the list of the line durations of the layer (cumulative, in the seconds of the
    Preview). start is the index of the line of the move from the last answer, and elapsed the
    seconds since that answer. The Preview plays factor times faster than the feed rates, so the
    printer moves 1 / factor of a Preview second in each second. current is the time of the Preview
    on the same layer, or None. The time stops at the end of the layer: the next answer gives the
    next layer."""
    if not durations:
        return 0.0
    base = durations[start - 1] if 0 < start <= len(durations) else (0.0 if start <= 0 else durations[-1])
    target = min(base + max(0.0, min(elapsed, EXTRAPOLATE)) / factor, durations[-1])
    if current is not None and 0.0 < current - target < BACK_LIMIT:
        return current  # a little ahead of the printer: wait for it
    return target
