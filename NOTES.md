# Notes: print on a part (2026-10-22)

Work log and open questions for the "print on a part" feature. The first test case is the MJ logo
on the bittlx Pi addon panel (`~/Projects/3d/bittlx/bittle_pi_addon/stl/print/pod_panel.stl` and
`pod_logo_white.stl`, `pod_logo_red.stl`).

## What is done

- Deck (`045051f`): the `printdeck_pause` module of the Klipper host.
  - `PRINTDECK_PAUSE [MESSAGE=] [CHECK_X= CHECK_Y= CHECK_Z=]`: pause, retract 2 mm, lift 10 mm,
    park at the back left.
  - `PRINTDECK_CHECK_HEIGHT`: a paper test over the check point. The difference goes into the
    Z offset of the saved pause state.
  - `PRINTDECK_RESUME`: prime 2 mm and resume.
  - `PRINTDECK_PURGE [LENGTH=15]`: purge at the park position, then run `wipe_gcode`.
  - The Paused screen shows the message and a **Check the height** key. Resume asks while the
    height is not checked.
  - The Pause key of the deck now parks too. A filament change in a pause no longer purges on the
    print.
- Cura plugin (`4fe928c`, `a75c139`): the **Print on a part** tool in the left toolbar of Prepare.
  See `slicer/cura-plugin/README.md`.
- Tests:
  - A batch test of the Klipper host runs pause, height check, purge and resume. It checks that
    the print goes on 0.6 mm higher.
  - Unit tests in `slicer/cura-plugin/test_onpart.py`.
  - A test in a throwaway copy of the Cura profile sliced the bittlx panel and the white logo, in
    both jobs. The pocket is around the panel, the logo is on its top at 28.7 mm, and the check point
    is on the top face.

## To try it

1. Install the local bundle: `os/out/printdeck-update.tar.xz` (`v0.2.0-beta.1-9-ga75c139`) on
   `http://printdeck.local/system`.
2. Add the wipe to the `printer.cfg` of the deck (the repo copy has it):

   ```
   [printdeck_pause]
   wipe_gcode: NOZZLE_WIPE
   ```

3. Restart Cura. The plugin is in `~/.local/share/cura/5.13/plugins/Printdeck`.
4. Open `stl/installed/pod_panel.stl` and `stl/installed/pod_logo_white.stl` (the assembly frame,
   so the logo has its exact place). The panel loads on its side: turn it 90 degrees about X, top
   up. Put it at the back middle of the bed.
5. Select the panel, open the Print on a part tool, and tap **Set as base**.
6. Select the logo, and tap **Place as in the files**. In the test, the logo went 1.0 mm into the
   logo pocket of the panel (27.7 to 28.5 mm, the top of the panel at 28.7 mm).
7. (Optional) Add text with **Add text**.
8. Print with white. On the deck: put the panel in the pocket, tap **Check the height**, do the
   paper test on the panel, then Resume.
9. For red: change the filament. In Cura, delete the white logo, open
   `stl/installed/pod_logo_red.stl`, and tap **Place as in the files**. Check **The pocket is on
   the bed**, and print. The deck asks for the height check again.

## My guesses (please check)

- The park position is the back left corner (X -45, Y 303). It is off the bed.
- The pocket: 3 mm wall, 0.3 mm clearance, 2 mm thick, open corners of 8 mm.
- The check point is on the top face of the base, beside the new models (3 mm margin). A face with a
  hole or a pocket there gives a bad check. The tool picks the center of the largest flat triangle
  at the top.
- The second job does not heat the bed: the pocket holds the part, and a hot bed softens PLA.
- The second job skips the bed probe (`BED_TILT_CALIBRATE`) and the prime strip. The Workhorse
  homes Z at the top switch, so its Z zero is rough. The paper test on the part corrects that, so
  the height check is important in the second job.
- With "The pocket is on the bed", the plugin sets adhesion to none (a skirt would print in the air).
  When you uncheck it, it removes that change again.
- The tool sets `remove_empty_first_layers` to False in the user settings of Cura. It does not set
  it back.
- While a part is the base, the tool turns off Cura's "Ensure models are kept apart". Clear the base
  to turn it on again.

## Open points

- Place as in the files uses the offset that Cura removes when it loads a file. It works for files
  of one frame (the `stl/installed/` STLs of bittlx), not for the `stl/print/` STLs, which each
  have their own print position.
- The first layer on plastic prints with the normal layer settings. It can need a hotter nozzle or
  a slower first layer on the part.
- The panel of the tool uses `UM 1.5` controls: Cura 5 only. Cura LulzBot Edition (4.13) needs a
  Qt 5 panel.
- Lettering: **Add text** in the tool. Qt draws the text (bold Sans Serif) at 0.1 mm a pixel, and
  each run of dark pixels becomes a box; CuraEngine joins the boxes. A font choice is not in the
  panel yet. The same method can turn an image (for example the LineChef avatar) into a model.
- The deck manual has no figure of the Paused screen with a message.
- The Printdeck tab: the time and the filament of a loaded G-code do not show in Cura's panel (a
  limit of Cura).
- Max mode is on main and in `v0.2.0-beta.1`. It is off until `[max_mode]` is in printer.cfg.
  Run a flow test before you turn it on.
