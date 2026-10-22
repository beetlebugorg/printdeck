# Starts a print of the simulator at a layer of its file, so that a take of the film (film.sh) does
# not wait for the layers before it. The deck shows the moves before that layer as printed.
#
#   python3 desktop/film/resume.py GCODE LAYER [SOCKET]
#
# It homes, heats to the temperatures of PRINT_START, selects the file, puts the file position at
# the layer (M26), sets the extruder to its place there (G92), and starts the print (M24).
import json
import os
import re
import socket
import sys

path, layer = sys.argv[1], int(sys.argv[2])
sock = sys.argv[3] if len(sys.argv) > 3 else os.path.expanduser("~/.config/Printdeck Sim/klippy.sock")
data = open(path, "rb").read()
offset = data.index(b";LAYER:%d\n" % layer)
before = data[:offset].decode()

start = re.search(r"PRINT_START.*", before).group(0)
tool = int(re.search(r"EXTRUDER=(\d+)", start).group(1))
bed = int(re.search(r"BED=(\d+)", start).group(1))
e = 0.0
fan = 0
for line in before.splitlines():
    s = line.split(";")[0].strip()
    m = re.search(r"\bE(-?[\d.]+)", s)
    if s.startswith(("G0", "G1", "G92")) and m:
        e = float(m.group(1))
    if s.startswith("M106"):
        m = re.search(r"S(\d+)", s)
        fan = int(m.group(1)) if m else 255
    if s.startswith("M107"):
        fan = 0


def gcode(script):
    c = socket.socket(socket.AF_UNIX)
    c.connect(sock)
    c.sendall(json.dumps({"id": 1, "method": "gcode/script", "params": {"script": script}}).encode() + b"\x03")
    reply = b""
    while not reply.endswith(b"\x03"):
        reply += c.recv(65536)
    r = json.loads(reply[:-1])
    if "error" in r:
        sys.exit("%s: %s" % (script, r["error"]["message"]))


name = os.path.basename(path)
for script in [
    "M220 S100", "SET_VELOCITY_LIMIT VELOCITY=300 ACCEL=3000",
    "M140 S%d" % bed, "M104 S%d" % tool, "G28",
    "M190 S%d" % bed, "M109 S%d" % tool,
    "G90", "M82", "G92 E%.5f" % e, "M106 S%d" % fan,
    "M23 %s" % name, "M26 S%d" % offset, "M24",
]:
    print(script)
    gcode(script)
print("printing %s from layer %d (byte %d)" % (name, layer, offset))
