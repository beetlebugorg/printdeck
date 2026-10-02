// Speakerdeck case: the base plate.
// It carries the drive and the Pi stack, and it closes the body from below.
// Open this file to see the part in its position for the print bed.

include <common.scad>

// Hole for a countersunk M3 bolt from below.
module csk_hole(x, y, h) {
  translate([x, y, -1]) cylinder(d = 3.4, h = h + 2);
  translate([x, y, -0.01]) cylinder(d1 = 6.6, d2 = 3.4, h = 1.7);
}

// Base plate. The drive bolts on from below. The Pi stack sits on four pins.
module base() {
  difference() {
    union() {
      translate([wall + gap, inky_wall + gap, 0]) cube([W - 2 * (wall + gap), D - wall - inky_wall - 2 * gap, base_t]);
      for (h = ssd_holes) translate([h[0], h[1], base_t - 0.01]) cylinder(d = 9, h = ssd_pad);
      for (p = pi_holes_xy) translate([p[0], p[1], base_t - 0.01]) {
        cylinder(d1 = 9, d2 = 6.5, h = pi_post_h);
        cylinder(d = 2.3, h = pi_post_h + 3);
      }
    }
    for (p = post_xy) csk_hole(p[0], p[1], base_t);
    for (h = ssd_holes) csk_hole(h[0], h[1], base_t + ssd_pad);
    // Openings for air, below the drive and below the Pi.
    for (i = [0:3]) translate([ssd_x0 + 14, ssd_y0 + 22 + i * 18, -1]) cube([ssd[0] - 28, 10, base_t + 2]);
    for (i = [0:2]) translate([pi_pos[0] + 16, pi_pos[1] + 14 + i * 18, -1]) cube([24, 10, base_t + 2]);
  }
}

base();
