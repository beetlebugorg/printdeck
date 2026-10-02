// Speakerdeck case: the Inky frame.
// It prints with the front face on the bed. Print it slowly, so that the details stay sharp.
// Open this file to see the part in its position for the print bed.

include <common.scad>

// Bars at the left and the right of the Inky wHAT, like a level meter.
// They go through the frame, so you see the body colour in them.
module pattern() {
  max_h = 2 * (inky_boss_xz[3][1] - lip_h / 2) - 16;
  for (s = [-1, 1], i = [0:pattern_n - 1]) {
    h = max(pattern_w + 1, max_h * (1 - i * 0.7 / pattern_n));
    translate([s * (inky_board[0] / 2 + 6 + i * pattern_pitch), lip_h / 2]) hull() {
      translate([0, -h / 2 + pattern_w / 2]) circle(d = pattern_w);
      translate([0, h / 2 - pattern_w / 2]) circle(d = pattern_w);
    }
  }
}

// Frame of the Inky wHAT, in the local frame of the lower front. The front face is at Z = 0.
// A thin lip covers the border of the Inky wHAT and holds it on the pad.
module inky_frame() {
  difference() {
    union() {
      translate([0, 0, -frame_t]) linear_extrude(frame_t, convexity = 10) difference() {
        frame_outline(lip_h);
        inky_view_rect();
        pattern();
      }
      for (b = inky_boss_xz) translate([b[0], b[1], -frame_t - inky_boss_l]) cylinder(d = 7.4, h = inky_boss_l);
    }
    translate([0, 0, -frame_t - 1]) linear_extrude(frame_t - inky_lip + 1) inky_rect(inky_board, gap);
    for (b = inky_boss_xz)
      translate([b[0], b[1], -frame_t - inky_boss_l - 0.01]) cylinder(d = insert_d, h = insert_l + 1);
  }
}

translate([0, lip_h, 0]) rotate([180, 0, 0]) inky_frame();
