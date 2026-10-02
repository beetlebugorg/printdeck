# Vendor models

These meshes are models of purchased parts. The case images use them. Do not print them.

| File | Part | Source |
|---|---|---|
| `hifiberry_amp2.stl` | HiFiBerry Amp2 | https://www.hifiberry.com/docs/hardware/3d-models-of-hifiberry-boards/ |
| `raspberry_pi_5.stl` | Raspberry Pi 5 | Not in the repository (30 MB). Download `rpi5-step.zip` to `src/rpi5/`, then run `freecadcmd convert_pi5.py`. Without it, set `use_pi_mesh = false` in `hardware.scad`. |

The original downloads go in `src/`. That directory is not in the repository.

| File in `src/` | Source |
|---|---|
| `amp2.zip` | https://www.hifiberry.com/wp-content/uploads/2019/07/hifiberry-amp2-1.0.1.step.zip |
| `rpi5-step.zip` | https://datasheets.raspberrypi.com/rpi5/RaspberryPi5-step.zip |
| `lcd7_adafruit_2354.*` | https://github.com/adafruit/Adafruit_CAD_Parts, directory `2354 7in TFT Touchscreen` |

The display board data comes from https://github.com/adafruit/Adafruit-7-HDMI-Backpack-PCB.
