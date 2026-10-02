// Speakerdeck case: the body.
// It prints upside down, with the top on the bed. The rear wall is part of the body.
// Open this file to see the part in its position for the print bed.

include <common.scad>

module outer() {
  intersection() {
    ext_x(W) polygon([[0, 0], [D, 0], [D, H], [run, H], [0, lip_h]]);
    linear_extrude(H) rrect(W, D, corner_r);
  }
}

module cavity() {
  intersection() {
    translate([wall, inky_wall, -1]) cube([W - 2 * wall, D - wall - inky_wall, H - wall + 1]);
    slope_half(front_t);
  }
  // Space for the display plugs, above the Inky wHAT.
  intersection() {
    translate([wall, 0, 0]) ext_x(W - 2 * wall)
      polygon([[wall, relief_z + inky_wall - wall], [inky_wall + 0.1, relief_z], [inky_wall + 0.1, lip_h + 40], [wall, lip_h + 40]]);
    slope_half(front_t);
  }
}

// Post for a base plate bolt. The sloped part makes it printable with no supports.
module base_post(p) {
  hull() {
    translate([p[0] - 5, p[1] - 5, base_t]) cube([10, 10, 12]);
    translate([p[0] < W / 2 ? wall - 0.5 : W - wall - 0.5, p[1] - 5, base_t + 22]) cube([1, 10, 0.1]);
  }
}

module rear_hole(x, z, d) {
  cyl_y(x, D - wall - 1, z, d, wall + 2);
}

module rear_cutouts() {
  mid = W / 2;
  // Speaker binding posts: left pair and right pair.
  for (p = post_x) rear_hole(p[0], rear_row_z, post_d);
  // Power input.
  rear_hole(jack_x, rear_row_z, barrel_d);
  // Notch for the USB cable to the printer.
  translate([mid + 12 - usb_notch[0] / 2, D - wall - 1, -1]) cube([usb_notch[0], wall + 2, usb_notch[1] + 1]);
  // Air inlet.
  for (i = [-10:10], j = [0:4]) rear_hole(mid + i * dot_pitch, 55 + j * dot_pitch, dot_d);
  // Fan outlet.
  n = floor(fan_size / 2 / dot_pitch);
  for (i = [-n:n], j = [-n:n])
    if (norm([i, j]) * dot_pitch <= fan_size / 2 - 2)
      rear_hole(fan_x + i * dot_pitch, fan_z + j * dot_pitch, dot_d);
  for (dx = [-1, 1], dz = [-1, 1])
    rear_hole(fan_x + dx * fan_hole_pitch / 2, fan_z + dz * fan_hole_pitch / 2, 3.4);
}

module body() {
  difference() {
    union() {
      difference() {
        outer();
        cavity();
      }
      // Bosses for the display board.
      on_slope() for (p = disp_boss_uv)
        translate([p[0], p[1], -front_t - disp_boss_h]) cylinder(d = 7.4, h = disp_boss_h + 0.01);
      intersection() {
        outer();
        for (p = post_xy) base_post(p);
      }
    }
    // Upper front: LCD opening, frame indent, holes for the frame bosses, display inserts.
    on_slope() {
      translate([0, 0, -front_t - disp_boss_h - 1]) linear_extrude(front_t + disp_boss_h + 2) lcd_rect(gap);
      translate([0, 0, -frame_t]) linear_extrude(frame_t + 1) frame_outline(slope_l, gap);
      for (b = frame_boss_uv) translate([b[0], b[1], -front_t - 1]) cylinder(d = 7.8, h = front_t + 2);
      for (p = disp_boss_uv)
        translate([p[0], p[1], -front_t - disp_boss_h - 0.01]) cylinder(d = insert_d, h = insert_l + 1);
    }
    // Lower front: frame indent, cavity for the pad and the Inky wHAT, header opening, boss holes.
    on_front() {
      translate([0, 0, -frame_t]) linear_extrude(frame_t + 1) frame_outline(lip_h, gap);
      translate([0, 0, -(inky_lip + inky_t + inky_pad)]) linear_extrude(inky_wall) inky_rect(inky_board, gap);
      translate([0, 0, -inky_wall - 1]) linear_extrude(inky_wall + 2) inky_rect(inky_header, 0, inky_header_off);
      for (b = inky_boss_xz) translate([b[0], b[1], -inky_wall - 1]) cylinder(d = 7.8, h = inky_wall + 2);
    }
    // Inserts for the base plate bolts.
    for (p = post_xy) translate([p[0], p[1], base_t - 0.01]) cylinder(d = insert_d, h = insert_l + 1);
    rear_cutouts();
  }
}

translate([0, D, H]) rotate([180, 0, 0]) body();
