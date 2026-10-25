# Pod print files

`pod.py export` writes these files. Each part is in its print orientation, centered on the bed,
with its lowest point at z = 0. Load a file and print it as it is. Do not turn it.

## FDM (`fdm/`)

All parts fit on the TAZ Workhorse and the Mini 2 (0.5 mm nozzle). The dovetail slot of each
bracket has 0.35 mm of clearance on each side.

| File | Print | On the bed | Supports |
|---|---|---|---|
| `pod-case_fdm.stl` | 1 | The bottom end (it stands up) | Built in: a thin wall inside the open front holds up the inside of the top wall. Turn Cura's supports off. Use a brim: the part is 132 mm tall. |
| `pod-bracket-taz_fdm.stl` | 1, for the TAZ | The top end of the plate | Block the supports in the two slots for the rail clips. Their roofs are 12 mm bridges, and support in a slot spoils the fit of the clip. Use a brim. |
| `pod-bracket-mini2_fdm.stl` | 1, for the Mini 2 | The side of the plate that goes on the frame | None |
| `cable-clip-single_fdm.stl` | 5, for the TAZ | The base | None |
| `cable-clip-double_fdm.stl` | 2, for the Mini 2 | The base | None |

After the print, snap the built-in wall out of the front of the case: bend it at the notch along
its foot. It is not part of the case.

Print the clips in PETG. Their arms bend across the layer lines, and PLA can crack there.

## Resin (`resin/`)

The dovetail slot of each bracket has 0.15 mm of clearance on each side. The back of the case and
the backs of the brackets face the build plate, so the open front of the case faces away from it.
Each part leans 30 degrees. Add the supports in the resin slicer, on the back faces.

| File | Print |
|---|---|
| `pod-case_resin.stl` | 1 |
| `pod-bracket-taz_resin.stl` | 1, for the TAZ |
| `pod-bracket-mini2_resin.stl` | 1, for the Mini 2 |
| `cable-clip-single_resin.stl` | 5, for the TAZ |
| `cable-clip-double_resin.stl` | 2, for the Mini 2 |
