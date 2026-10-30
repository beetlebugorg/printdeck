# Camera check of the prime line

Status: plan. The build has not started.

At the start of a print, the deck primes the nozzle and checks the prime line with a camera. A
line with gaps, or no line, gets another try. After a good line, the deck can skip the skirt of
the file and print. The user decides about the skirt with a setting.

## Why

The nozzle can be empty at the start of a print: the filament oozed out during the heat-up, the
wipe pulled it back, or a small clog stops it. The skirt of the slicer primes the nozzle, but
nobody looks at it. A first layer with no plastic for the first centimetres fails, or starts
with a gap in the first wall. The camera sees the result of the prime line, so the deck can try
again before the part starts.

## What the user sees

1. The print starts as now: heat, wipe, probe, heat to the print temperatures.
2. The status panel shows "Priming · checking the line".
3. The deck draws a prime line at the front of the bed, and moves the nozzle away from the line.
4. The deck compares two pictures of the line: before and after.
   - A good line: the print goes on. With the setting on, the deck skips the skirt.
   - A bad line: the status panel shows "Priming · try 2". The deck draws a new line behind the
     first one.
   - An unsure result (low contrast, for example white filament on a light bed): the deck pauses
     and shows the picture of the line, with the keys **Good line**, **Prime again** and
     **Cancel**.
5. After the last try (4 by default), the deck pauses and shows the picture of the last line,
   with the same keys.

The deck makes no sound and sends no notification for a good line. A pause after the last try
sends the notification of a pause.

## Settings

The settings are on Settings > Camera > Prime check:

- **Check the prime line**: on or off. Off by default. It needs a camera with a calibration (the
  bed is in its picture). With no calibration, the setting says "Calibrate the camera first".
- **Tries**: 1 to 8, 4 by default.
- **Skip the skirt after a good line**: on or off. Off by default. The text under it: "Cura
  marks a brim as a skirt too. Turn this off for prints with a brim."
- **Show the place of the line**: the live view of the camera with the place of the line on it.

The Cura plugin can get a check box for the skirt of one print later (open question 2).

## The parts that exist

- **The start macro** `PRINT_START` (printer/printer.cfg) draws a prime strip of 2 lines along
  the front edge (Y 2 mm, X 20 to 260 mm).
- **`[printdeck_pause]`** draws a prime line for a color change (`prime_x`, `prime_y`,
  `prime_step`, `prime_length`), and pauses for the user. Its height check of a print on a part
  is the pattern for a pause that the deck answers: a status for the deck, and a command that
  ends the pause.
- **The camera calibration** (`camera.Calibration.ProjectAt`) gives the place in the picture of a
  point on the bed.
- **The PTZ map and the follow of the part** (`Hub.FollowPart`, `boxTarget`) zoom a camera to a
  box on the bed.
- **The deck runs the G-code file** (`virtual_sdcard`, `printdeck_sdcard`, `exclude_object` of
  the Go host), so it can leave out a part of the file.
- **The G-code parse** of the deck knows the kind of each move (`gcode.KindSkirt`).

## The design

### 1. The command `PRINTDECK_PRIME_CHECK`

A new command of `[printdeck_pause]` (or a new section `[printdeck_prime]`). `PRINT_START` calls
it in place of its loop of prime lines:

```
PRINTDECK_PRIME_CHECK LAYER={layer} SPEED={prime_speed}
```

- With the check off, or with no calibrated camera, the command draws the strip of now: 2 lines
  along the front edge. A deck with no camera prints as now.
- With the check on, the command draws one line, retracts, lifts, and moves the nozzle to the
  park position. It then pauses the print with the state `prime_check` and the place of the
  line.

The command is the last command of `PRINT_START`. The pause stops the file after the macro, so
no move of the file runs before the answer of the deck.

New options of the section:

- `check_x`, `check_y`: the start of the first line, in the view of the camera. The default is
  the start of the strip of now.
- `check_length`: 60 mm. A shorter line than the strip: the bed has space for 8 tries.
- `check_width`, `check_height`: 1.0 mm and 0.4 mm. A wide, high line is easier to see.
- `check_step`: 4 mm. Each try is behind the try before it.

The status of the section gives the deck: `state` (`prime_check`), `try`, and the start and the
end of the line on the bed.

### 2. The check on the deck

The deck program runs the check, because it has the cameras. Klipper waits in the pause.

1. **Before.** Before the line, while the nozzle heats at the park position, the deck makes a
   picture of the place of the line. The nozzle is away from the line, so the picture shows the
   whole place.
2. **Zoom.** With a PTZ map, the camera zooms to the box of the line with `Follow`, as the follow
   of the part does. The zoom gives more pixels for each millimetre of the line. Without a map,
   the check uses the wide picture.
3. **After.** After the line, at the park position, the deck makes a picture with the same zoom.
4. **Compare.** The calibration gives the line in the picture: a band along the line, as wide as
   the line plus 1 mm on each side. The deck compares the two pictures in the band:
   - It reads a sample each 1 mm along the line. A sample is filled when the color changes more
     than the noise.
   - The noise comes from two bands beside the line, 3 mm away, which the line does not touch.
   - The color of the roll helps: a change toward the color of the roll counts more.
5. **Result.**
   - **Good**: 90 % of the samples or more are filled, and no gap is longer than 5 mm.
   - **Bad**: fewer than 30 % of the samples are filled, or a gap is longer than 15 mm.
   - **Unsure**: the rest, or a noise that is too high (the light changed), or a contrast of the
     roll color and the bed that is too low.
6. **Answer.**
   - Good: `PRINTDECK_PRIME_CHECK_DONE`, with `SKIP_SKIRT=1` when the setting is on. The command
     ends the follow of the camera, pushes back the filament of the retract, and resumes.
   - Bad, with tries left: `PRINTDECK_PRIME_CHECK_AGAIN`. The command draws the next line and
     pauses again.
   - Unsure, or bad after the last try: the deck shows the picture and asks the user.

The pictures stay in memory. The check writes no file to the SD card.

The check of one line is one crop of some thousand pixels, once for each try. The CPU cost is
small. The deck measures it on the Pi 5 before and after, as for each new feature.

**Exposure.** The camera can change its exposure between the two pictures. The deck locks the
exposure for the check, or compares the two bands beside the line to correct the brightness.
The first build measures which way works.

### 3. The skip of the skirt

With `SKIP_SKIRT=1`, the deck leaves out the skirt of the first layer:

- At the start of the print, the deck finds the skirt in the file: the moves of the kind
  `KindSkirt` of layer 1, from the line `;TYPE:SKIRT` to the next `;TYPE:` line. It keeps the
  byte offsets of this part of the file.
- `printdeck_sdcard` jumps over the part. It runs the moves of the part that change no position
  of the plastic: the moves of Z, and the end position of the part as one travel move.
- With absolute extrusion (M82), the deck runs `G92 E<the E value at the end of the part>`. The
  E values of the file after the part then stay correct.
- The retract at the end of the skirt stays, so the next move starts with the unretract of the
  file.

The deck does not check that the part is a skirt. A brim in Cura has the same `;TYPE:SKIRT`
label. The setting is the choice of the user.

### 4. The history

The record of the print gets the count of the tries, and the result of the check. The Statistics
page can show the prints that needed more than one try. A good line on the first try is the
normal case, and the record has no field for it.

## Phases

1. **Show and ask.** `PRINTDECK_PRIME_CHECK` with one line and a pause. The deck shows the
   picture of the line and asks: Good line, Prime again, Cancel. No automatic check. This phase
   tests the pause, the park position, the zoom, and the place of the line in the picture.
2. **Collect pictures.** The deck keeps the last 20 pairs of pictures in memory. The web page of
   the deck downloads them as a ZIP, for the tests of the check. The pairs need different
   filaments: black, white, clear, and a color.
3. **Automatic check.** The compare of step 2.4, with tests on the collected pairs. Good and bad
   go on by themselves. Unsure asks.
4. **Skip the skirt.** The setting and the jump in `printdeck_sdcard`, with tests on Cura files
   with a skirt, with a brim, and with no adhesion.
5. **Settings and the manual.** The Prime check page, the history field, the manual and the
   Cura booklet.

## Tests

- The test camera of the deck draws a bed and a line from the G-code moves. The tests of the
  check use it for a good line, a line with a gap, no line, and a low contrast.
- The collected pairs of phase 2 are test data for the compare.
- The tests of the skip use small Cura files: the moves after the skip must match a file with no
  skirt, with the same E values.
- The Go host tests check that the pause after `PRINTDECK_PRIME_CHECK` stops the file before its
  first move.

## Open questions

1. **The place of the line.** The strip of now is at Y 2 mm, at the front edge. The bed frame
   or the angle of the camera can hide it. Is a place 10 to 20 mm from the front edge good, or
   do you want to pick the place on the live view?
2. **The skirt for one print.** Do you want a check box in the Cura plugin for the skirt of one
   print, in addition to the setting of the deck?
3. **The tries.** Is 4 a good default?
4. **The wipe between tries.** A bad line can come from a clog. Do you want a wipe of the nozzle
   before the second try?
