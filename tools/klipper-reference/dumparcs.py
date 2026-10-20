# Writes the G1 moves that Klipper's gcode_arcs.py makes for a set of arc commands, as JSON. The
# tests of the Go port of gcode_arcs compare their result with it.
#
#   python3 dumparcs.py OUT.json
import sys, json
sys.path.insert(0, '/klipper/klippy')
sys.path.insert(0, '/klipper/klippy/extras')
import gcode, gcode_arcs

class Printer:
    def get_start_args(self): return {}
    def register_event_handler(self, e, cb): pass
    def get_reactor(self):
        class R:
            def mutex(self): return None
        return R()

class GCodeMove:
    def __init__(self):
        self.moves = []
        self.status = {}
    def get_status(self, eventtime=None): return self.status
    def cmd_G1(self, gcmd): self.moves.append(dict(gcmd.get_command_parameters()))

g = gcode.GCodeDispatch(Printer())
gm = GCodeMove()
class Config:
    def get_printer(self):
        class P:
            def load_object(self, config, name): return gm
            def lookup_object(self, name): return g
        return P()
    def getfloat(self, name, default, above=None): return 0.5

# Each case: the plane command, the start position (X, Y, Z, E), absolute extrude, and the arc
# command.
CASES = [
    ("G17", [10., 10., 0.3, 5.], True, "G2 X20 Y10 I5 J0 E6 F1200"),
    ("G17", [10., 10., 0.3, 5.], True, "G3 X20 Y10 I5 J0"),
    ("G17", [0., 0., 0., 0.], False, "G2 X0 Y0 I7.5 J2.5 E3"),
    ("G17", [100.2, 50.7, 1.1, 0.], True, "G3 X90.3 Y61.9 Z2.3 I-4.4 J6.1 E1.25"),
    ("G18", [10., 10., 5., 0.], True, "G2 X20 Z5 I5 K0"),
    ("G19", [10., 10., 5., 0.], True, "G3 Y10 Z15 J0 K5 F600"),
    ("G17", [33.3, 44.4, 0.2, 2.], True, "G2 X35.1 Y46.2 I0.9 J0.9 E2.05"),
    ("G17", [1., 1., 1., 1.], True, "G2 X3 Y1"),
    ("G17", [1., 1., 1., 1.], True, "G2 X3 Y1 I1 R5"),
]
arcs = gcode_arcs.ArcSupport(Config())
res = []
for plane, pos, absext, line in CASES:
    gm.moves = []
    gm.status = {'absolute_coordinates': True, 'gcode_position': list(pos),
                 'absolute_extrude': absext}
    out = {'plane': plane, 'pos': pos, 'absolute_extrude': absext, 'line': line}
    def run(cmdline):
        parts = cmdline.split()
        params = {p[0]: p[1:] for p in parts[1:]}
        gcmd = g.create_gcode_command(parts[0], cmdline, params)
        getattr(arcs, 'cmd_' + parts[0])(gcmd)
    try:
        run(plane)
        run(line)
    except gcode.CommandError as e:
        out['error'] = str(e)
    out['moves'] = gm.moves
    res.append(out)
with open(sys.argv[1], 'w') as f:
    json.dump(res, f, indent=1)
    f.write('\n')
