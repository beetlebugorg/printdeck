# Writes how Klipper's G-code dispatcher (klippy/gcode.py) reads G-code lines, as JSON: for each
# line, the command, the parameters, and the extended parameters (or the error). The tests of the
# Go port of gcode compare their result with it.
#
#   python3 dumpgcode.py LINES.txt OUT.json
import sys, json
sys.path.insert(0, '/klipper/klippy')
import gcode

class Printer:
    def get_start_args(self): return {}
    def register_event_handler(self, e, cb): pass
    def get_reactor(self):
        class R:
            def mutex(self): return None
        return R()

g = gcode.GCodeDispatch(Printer())
res = []
for line in open(sys.argv[1]).read().split('\n'):
    out = {}
    def handler(gcmd):
        out['cmd'] = gcmd.get_command()
        out['params'] = dict(gcmd.get_command_parameters())
        out['raw'] = gcmd.get_raw_command_parameters()
        try:
            out['extended'] = dict(g._get_extended_params(gcmd).get_command_parameters())
        except gcode.CommandError as e:
            out['error'] = str(e)
    g.gcode_handlers = {}
    g.cmd_default = handler
    g._process_commands([line], need_ack=False)
    out['line'] = line
    res.append(out)
with open(sys.argv[2], 'w') as f:
    json.dump(res, f, indent=1)
    f.write('\n')
