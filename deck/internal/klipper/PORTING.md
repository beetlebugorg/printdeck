# The Go port of the Klipper host

The packages in this folder are a port of klippy, the host program of
[Klipper](https://github.com/Klipper3d/klipper), to Go. The source is Klipper
commit 7bc4d09465d31cd30fc0822e8d0abe02cc8c547f (`os/package/klipper`). The
port follows the Python code closely, so a change in Klipper is easy to find
and copy.

## Layout

| Klipper file | Go package |
| --- | --- |
| `klippy/klippy.py` (Printer) | `host` |
| `klippy/configfile.py` | `configfile` |
| `klippy/pins.py` | `pins` |
| `klippy/mcu.py` | `mcu` |
| `klippy/gcode.py` | `gcode` |
| `klippy/extras/NAME.py` | `extras/NAME` |
| `klippy/kinematics/NAME.py` | `kinematics/NAME` |
| `klippy/serialhdl.py`, `msgproto.py`, `clocksync.py`, `reactor.py` | same names |
| `klippy/chelper/*.c` | `serialqueue`, and later packages for the motion code |

`extras/all` imports each module package. `batch` runs the host in klippy's
batch mode for the tests.

## Rules

- Each Go file starts with the Klipper file name, the commit, the copyright of
  the Klipper file, and the GPLv3 line. Copy the header of `mcu/mcu.go`.
- Keep the names and the order of the Python code. A Python method
  `get_status` is `Status`, `get_name` is `Name`, `cmd_FOO` is `cmdFoo`.
- Klipper's exceptions are panics with a typed error: `*configfile.Error` for
  `config.error` and `printer.config_error`, `*pins.Error`, `*mcu.Error`,
  `*gcode.CommandError` for `gcmd.error` and `printer.command_error`. Use
  `configfile.Raise` and `gcode.Errorf`. The error messages are the same text
  as in Klipper.
- A module registers itself in an init function:
  `host.RegisterModule("fan", load, loadPrefix)`. `load` makes the object of
  `[fan]`, and `loadPrefix` makes the object of `[fan name]`.
- The order of the calls that make oids, config callbacks, and config
  commands is the order of the Python code. The board config and its CRC
  depend on it.
- A module of another package is found as in Klipper, with
  `printer.LoadObject` or `printer.LookupObject`, and a small interface in the
  package that uses it. This keeps the packages independent.
- Callbacks of the board (sensor reports) run on the reading goroutine. Use a
  `sync.Mutex` where Klipper uses a `threading.Lock`.
- Numbers in text that Klipper writes with `%s` or `str()` use
  `pyfmt.Float`. `%.3f` and the other fixed formats are the same in Go.
- Python's `math` functions call the C library. Go's `math.Sin`, `math.Cos`,
  `math.Atan2`, `math.Exp` and `math.Log` can differ from it in the last bit,
  so the tests of these modules have a small tolerance. Use
  `mathutil.Hypot` for `math.hypot`: `math.Hypot` differs from Python in about
  one third of the values.

## Tests

Each package compares its result with the result of the real Klipper code
where this is possible. `tools/klipper-reference` runs Klipper in Docker and
writes the reference files. `batch` compares the board commands for
`printer/printer.cfg` with the commands of the real host.
