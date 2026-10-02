// Speakerdeck case: the display frame.
// It prints with the front face on the bed.
// Open this file to see the part in its position for the print bed.

include <common.scad>

// Frame of the display, in the local frame of the upper front. The front face is at Z = 0.
// A pocket in the rear face holds the edge of the LCD module.
module frame() {
  difference() {
    union() {
      translate([0, 0, -frame_t]) linear_extrude(frame_t, convexity = 10) difference() {
        frame_outline(slope_l);
        view_rect();
      }
      for (b = frame_boss_uv) translate([b[0], b[1], -frame_t - frame_boss_l]) cylinder(d = 7.4, h = frame_boss_l);
    }
    translate([0, 0, -frame_t - 1]) linear_extrude(frame_t - glass_in + 1) lcd_rect(gap);
    for (b = frame_boss_uv)
      translate([b[0], b[1], -frame_t - frame_boss_l - 0.01]) cylinder(d = insert_d, h = insert_l + 1);
  }
}

translate([0, slope_l, 0]) rotate([180, 0, 0]) frame();
