// Speakerdeck case, version 12: a cabinet with the size of one speaker.
// The lower front is vertical and holds the Inky wHAT.
// The upper front leans back 40 degrees and holds the 7 inch touch display.
// Units are millimetres. Measure your parts and correct the values in the
// part sections before you print.
//
// Axes: X is the width, Y is the depth (the front is at Y = 0), Z is the height.
//
// Design rules:
// - The width, the height, and the depth agree with the speaker.
// - Each display has an inset frame. The display glass is 1 mm behind its frame,
//   and the e-paper is 0.6 mm behind its frame.
// - All bolts are M3. No bolt is visible from the front, the top, or the sides.
// - The body prints upside down, with the top on the bed. The rear wall is part of the body.
// - The base plate carries the drive and the Pi stack. It closes the body from below.

use <parts.scad>

/* [Output] */
// "exploded", "assembly", "body", "base", "frame", "inky_frame", "cap", "pad", or "step".
part = "exploded";
// Distance between the parts in the exploded view. 1 is the default, 0 is the closed case.
explode = 1;
// Scene number for the build instructions. Used when part is "step".
step = 1;
// Turn the scene by 180 degrees. The rear of the case then gets the light.
view_rear = false;
// Use the mesh files in build/mesh for the printed parts. This makes the images fast.
mesh = false;

/* [Case] */
speaker_w = 153;
speaker_h = 233;
speaker_d = 196;
wall = 3;
// Radius of the four vertical edges.
corner_r = 6;
// Clearance between parts.
gap = 0.3;
base_t = 3;

/* [Front] */
// Height of the vertical lower front.
lip_h = 95;
// Angle of the upper front from vertical.
tilt = 40;
// Thickness of the wall of the upper front.
front_t = 5;
// Distance from the front of the frame to the display glass.
glass_in = 1;
// The frame opening is larger than the visible area by this value on each side.
view_margin = 0.5;
// The centre of the visible area is this distance above the centre of the upper front.
disp_up = 0;

/* [Frames] */
frame_t = 2;
// Distance from the side edges of the front to the frames.
frame_in = 6;
// Distance from the top and bottom edges of each front face to its frame.
frame_end = 4;
frame_r = 4;
// Pattern at the left and the right of the Inky wHAT: bars on each side, pitch, and bar width.
pattern_n = 3;
pattern_pitch = 6;
pattern_w = 3;

/* [Fasteners] */
// Hole for an M3 heat-set insert. Use the value from the insert datasheet.
insert_d = 4.0;
insert_l = 4;

/* [7 inch display: Adafruit HDMI backpack, portrait] */
// Board: [short side, long side]. From the Adafruit board file.
disp_pcb = [116.2, 165.1];
// Distance between the centres of the four M3 holes: [short side, long side].
disp_holes = [108.2, 157.1];
// LCD module on the front of the board: [short side, long side]. Measured on the display.
disp_lcd = [100, 166];
// From the front glass to the front face of the board.
// The measured total thickness is 9.35 mm, and the board is 2 mm.
disp_lcd_t = 7.35;
disp_pcb_t = 2;
// Height of the sockets and the plugs behind the board.
disp_back = 8;
// Visible area: [short side, long side]. Measured on the display.
disp_view = [89, 157];
// Offset of the LCD centre from the centre of the four holes: [across, up the slope].
// "Across" is positive to the right when you look at the front. The Adafruit board file
// gives 0.1 mm to the left: the holes are 4.0 mm from the LCD on the left and 4.2 mm on the right.
disp_lcd_off = [-0.1, 0];
// Offset of the visible area from the centre of the LCD: [across, up the slope].
disp_view_off = [0, 0];

/* [Inky wHAT, in the lower front] */
// Outline of the Inky wHAT: [width, height].
inky_board = [91, 77];
// Total thickness of the board and the e-paper panel, without the header.
inky_t = 2.8;
// Distance from the edges to the screen: [left, right, top, bottom].
inky_border = [2, 2, 2, 9];
// Thickness of the frame lip on the border. The e-paper is this distance behind the frame.
inky_lip = 0.6;
// The Inky wHAT sits in a cavity in the front wall, on a rubber pad.
// Thickness of the pad when it is compressed, and thickness of the wall behind it.
inky_pad = 1.5;
inky_floor = 2.4;
// Height of the board centre above the centre of the lower front.
inky_dz = -1.5;
// Opening in the wall for the header and the ribbon cable: [width, height].
inky_header = [60, 16];
// Offset of the centre of the header from the centre of the board: [across, up].
// Measure this value. The default puts the header 14 mm from the top edge.
inky_header_off = [0, 24.5];

/* [Pi stack: Raspberry Pi 5 and HiFiBerry Amp2] */
// From the top face of the Pi board to the bottom face of the Amp2 board.
hat_gap = 16;
// Height of the Amp2 above the bottom face of its board. From the HiFiBerry model.
amp_h = 15.5;
// Height of the posts below the Pi. The HDMI plug must be above the drive.
pi_post_h = 12;
// Position of the corner of the Pi board near the SD card and the header: [x, y].
// The USB ports point to the rear. The HDMI ports point to the drive.
pi_pos = [8, 48];

/* [Drive] */
// 2.5 inch SATA drive: [width, length, thickness].
ssd = [69.85, 100.45, 6.8];
// M3 holes in the bottom of the drive: distance across, and distances from the connector end.
ssd_hole_w = 61.72;
ssd_hole_y = [14.0, 90.6];
// The USB adapter board extends past the connector end by this distance. Measured: 36.08 mm.
ssd_adapter = 36.1;
ssd_adapter_w = 66;
ssd_plug = 30;
// Height of the pads below the drive. This gives the correct bolt depth in the drive.
ssd_pad = 1;
// Distance from the front of the case to the front end of the drive.
ssd_y0 = 22;

/* [Rear wall] */
barrel_d = 8;
post_d = 7;
post_pitch = 19;
fan_size = 30;
fan_t = 10;
fan_hole_pitch = 24;
// Notch in the bottom edge for the USB cable to the printer: [width, height].
usb_notch = [14, 12];
rear_row_z = 30;
dot_d = 2;
dot_pitch = 4.5;

/* [Hidden] */
$fn = 48;
W = speaker_w;
H = speaker_h;
D = speaker_d;
rise = H - lip_h;
run = rise * tan(tilt);
slope_l = rise / cos(tilt);
// Thickness of the lower front wall. It contains the frame lip, the Inky wHAT, the pad, and the wall behind.
inky_wall = inky_lip + inky_t + inky_pad + inky_floor;

// Upper front: "u" is across and "v" is up the slope.
view_c = [0, slope_l / 2 + disp_up];
lcd_c = [view_c[0] - disp_view_off[0], view_c[1] - disp_view_off[1]];
pcb_c = [lcd_c[0] - disp_lcd_off[0], lcd_c[1] - disp_lcd_off[1]];
disp_boss_uv = [for (su = [-1, 1], sv = [-1, 1]) [pcb_c[0] + su * disp_holes[0] / 2, pcb_c[1] + sv * disp_holes[1] / 2]];
// The display board sits on bosses on the inner side of the wall.
disp_boss_h = max(0, glass_in + disp_lcd_t - front_t);
frame_boss_uv = [for (su = [-1, 1], sv = [-1, 1]) [su * 60, slope_l / 2 + sv * 40]];
frame_boss_l = front_t - frame_t + 5;

// Lower front: "x" is across and "z" is up.
inky_c = [0, lip_h / 2 + inky_dz];
inky_boss_xz = [for (sx = [-1, 1], sz = [-1, 1]) [sx * 56, lip_h / 2 + sz * 33]];
inky_boss_l = inky_wall - frame_t + 5;
// The wall above the Inky wHAT is thin, to make space for the display plugs.
relief_z = inky_c[1] + inky_board[1] / 2 + gap + 2.5;

// Base plate.
post_xy = [for (y = [inky_wall + 5, D - wall - 5], x = [wall + 5, W - wall - 5]) [x, y]];
pi_holes_xy = [for (h = [[3.5, 3.5], [61.5, 3.5], [3.5, 52.5], [61.5, 52.5]]) [pi_pos[0] + 56 - h[1], pi_pos[1] + h[0]]];
ssd_x0 = W - wall - 8 - ssd[0];
ssd_holes = [for (sx = [-1, 1], hy = ssd_hole_y) [ssd_x0 + ssd[0] / 2 + sx * ssd_hole_w / 2, ssd_y0 + ssd[1] - hy]];

fan_x = W / 2;
fan_z = H - 28;
post_x = [for (s = [-1, 1], i = [0, 1]) [W / 2 + s * (38 + i * post_pitch), i]];
jack_x = W / 2 - 12;

echo(str("Upper front: ", slope_l, " mm long. Top of the case: ", D - run, " mm deep."));
echo(str("Space behind the USB plug of the drive: ", D - wall - ssd_y0 - ssd[1] - ssd_adapter - ssd_plug, " mm"));
echo(str("Space for the USB plugs behind the Pi: ", D - wall - pi_pos[1] - 85, " mm"));

module rrect(w, h, r) {
  translate([r, r]) offset(r = r) square([w - 2 * r, h - 2 * r]);
}

// Extrude a shape in the YZ plane along X.
module ext_x(w) {
  rotate([90, 0, 90]) linear_extrude(w, convexity = 10) children();
}

// Cylinder along Y that starts at the given point.
module cyl_y(x, y, z, d, h, d2 = -1) {
  translate([x, y, z]) rotate([-90, 0, 0]) cylinder(d1 = d, d2 = d2 < 0 ? d : d2, h = h);
}

// Put a part with its +Z axis along +Y.
module along_y(x, y, z) {
  translate([x, y, z]) rotate([-90, 0, 0]) children();
}

// Local frame of the lower front: X across from the centre, Y up, Z out of the case.
module on_front() {
  translate([W / 2, 0, 0]) rotate([90, 0, 0]) children();
}

// Local frame of the upper front: X across from the centre, Y up the slope, Z out of the case.
module on_slope() {
  translate([W / 2, 0, lip_h]) rotate([90 - tilt, 0, 0]) children();
}

// The space behind the upper front, at "inset" below its surface.
module slope_half(inset) {
  on_slope() translate([-W / 2 - 5, -130, -260]) cube([W + 10, 380, 260 - inset]);
}

// ---------------------------------------------------------------- body

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

module lcd_rect(g = 0) {
  translate([lcd_c[0] - disp_lcd[0] / 2 - g, lcd_c[1] - disp_lcd[1] / 2 - g]) square([disp_lcd[0] + 2 * g, disp_lcd[1] + 2 * g]);
}

module view_rect() {
  m = view_margin;
  translate([view_c[0] - disp_view[0] / 2 - m, view_c[1] - disp_view[1] / 2 - m]) rrect(disp_view[0] + 2 * m, disp_view[1] + 2 * m, 2);
}

// Outline of a frame on a front face with the given length.
module frame_outline(len, g = 0) {
  translate([-W / 2 + frame_in - g, frame_end - g]) rrect(W - 2 * (frame_in - g), len - 2 * (frame_end - g), frame_r + g);
}

// Rectangle at the Inky position, larger by "g" on each side.
module inky_rect(size, g = 0, off = [0, 0]) {
  translate([inky_c[0] + off[0] - size[0] / 2 - g, inky_c[1] + off[1] - size[1] / 2 - g])
    square([size[0] + 2 * g, size[1] + 2 * g]);
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

// ---------------------------------------------------------------- base plate

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

// ---------------------------------------------------------------- frames

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

// Screen of the Inky wHAT.
module inky_view_rect() {
  b = inky_border;
  translate([inky_c[0] - inky_board[0] / 2 + b[0], inky_c[1] - inky_board[1] / 2 + b[3]])
    rrect(inky_board[0] - b[0] - b[1], inky_board[1] - b[2] - b[3], 1);
}

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

// Pad behind the Inky wHAT. Print it in TPU, or use it as a pattern to cut rubber or foam.
// It is 0.5 mm thicker than the space, so that it pushes the Inky wHAT against the frame.
module pad() {
  linear_extrude(inky_pad + 0.5) difference() {
    inky_rect(inky_board, -0.3);
    inky_rect(inky_header, 1, inky_header_off);
  }
}

// Cap that holds a frame boss from the inner side of the body.
module cap() {
  difference() {
    cylinder(d = 13, h = 7);
    translate([0, 0, 1.8]) cylinder(d = 8.2, h = 6);
    translate([0, 0, -1]) cylinder(d = 3.4, h = 4);
  }
}

// ---------------------------------------------------------------- preview
// The detailed models of the purchased parts are in parts.scad.

c_print = "#E6E4DE";
c_dark = "#2A2A2C";
c_mark = "#E4572E";

// The printed parts for the images. With "mesh", they come from the files in build/mesh.
module p_part(name) {
  if (mesh) import(str("build/mesh/", name, ".stl"), convexity = 10);
  else children();
}
module p_body() { p_part("body") body(); }
module p_base() { p_part("base") base(); }
module p_frame() { p_part("frame") frame(); }
module p_inky_frame() { p_part("inky_frame") inky_frame(); }

// Caps and bolts for four frame bosses, in the local frame of a front face.
module boss_caps(positions, wall_t, out = 0) {
  for (b = positions) translate([b[0], b[1], -wall_t - 7 - 0.5 * out]) {
    color(c_mark) cap();
    translate([0, 0, -0.4 * out]) rotate([180, 0, 0]) bolt(6);
  }
}

// Display frame with its caps and bolts, in the local frame of the upper front.
module frame_asm(out = 0) {
  color(c_dark) translate([0, 0, out]) p_frame();
  boss_caps(frame_boss_uv, front_t, out);
}

// Display with its plugs and bolts, in the local frame of the upper front.
// "lift" moves the display and its bolts into the case.
module display_asm(lift = 0, plugs = true) {
  translate([pcb_c[0], pcb_c[1], -glass_in - lift]) {
    display7(disp_pcb, disp_holes, disp_lcd, disp_lcd_t, disp_view, disp_view_off, disp_pcb_t, disp_lcd_off);
    if (plugs) {
      translate([0, -disp_pcb[1] / 2, -disp_lcd_t - disp_pcb_t - 1]) plug_ra(21);
      translate([-20.5, -disp_pcb[1] / 2, -disp_lcd_t - disp_pcb_t - 0.5]) plug_ra(11);
    }
  }
  for (p = disp_boss_uv) translate([p[0], p[1], -glass_in - disp_lcd_t - disp_pcb_t - 1.8 * lift]) rotate([180, 0, 0]) bolt(6);
}

// Pad, Inky wHAT, and Inky frame, in the local frame of the lower front.
// "out" moves the parts to the front.
module inky_asm(out = 0) {
  color("#3C3C40") translate([0, 0, -(inky_lip + inky_t + inky_pad) + 0.3 * out]) pad();
  translate([inky_c[0], inky_c[1], -(inky_lip + inky_t) + 0.6 * out]) inky_what(inky_board, inky_t, inky_border, inky_header_off);
  color(c_dark) translate([0, 0, out]) p_inky_frame();
  boss_caps(inky_boss_xz, inky_wall, out);
}

module g_stack() {
  translate([pi_pos[0] + 56, pi_pos[1], base_t + pi_post_h]) rotate([0, 0, 90]) {
    pi5_model();
    translate([0, 0, 1.5]) pi_standoffs(hat_gap);
    translate([0, 0, 1.5 + hat_gap]) amp2_model();
  }
}

module g_drive(lift = 0) {
  translate([ssd_x0, ssd_y0, base_t + ssd_pad + lift]) ssd_sata(ssd, ssd_adapter, ssd_adapter_w);
}

// Countersunk bolts from below, at the given positions.
module bolts_below(positions, len, drop = 0) {
  for (p = positions) translate([p[0], p[1], -drop]) rotate([180, 0, 0]) bolt_csk(len);
}

// Parts of the rear wall. "out" moves them away from the wall.
module g_rear_parts(out = 0) {
  along_y(fan_x, D - wall - fan_t - out, fan_z) fan30(fan_size, fan_t, fan_hole_pitch);
  for (dx = [-1, 1], dz = [-1, 1]) {
    along_y(fan_x + dx * fan_hole_pitch / 2, D + 1.2 * out, fan_z + dz * fan_hole_pitch / 2) bolt(16);
    along_y(fan_x + dx * fan_hole_pitch / 2, D - wall - fan_t - 2.4 - 1.8 * out, fan_z + dz * fan_hole_pitch / 2) nut_m3();
  }
  for (p = post_x) along_y(p[0], D + out, rear_row_z) binding_post(p[1] == 0 ? "#C0392B" : "#1E1E20", wall);
  along_y(jack_x, D + out, rear_row_z) dc_jack(wall);
}

// Arrow along +Z, for the build instructions.
module arrow(len = 40) {
  color(c_mark) { cylinder(d = 3, h = len - 10); translate([0, 0, len - 10]) cylinder(d1 = 9, d2 = 0, h = 10); }
}

module ghost_body() { color([0.86, 0.85, 0.81, 0.3]) p_body(); }

// Base plate with the drive and the Pi stack. "lift" moves the drive and the stack up.
module base_asm(lift = 0) {
  color(c_print) p_base();
  g_drive(lift);
  bolts_below(ssd_holes, 6, lift);
  translate([0, 0, lift]) g_stack();
}

// Body with all the parts that mount on it.
module body_asm(ghost = false) {
  on_slope() { frame_asm(); display_asm(); }
  on_front() inky_asm();
  g_rear_parts();
  if (ghost) ghost_body();
  else color("#D9D9D6") p_body();
}

// Scenes for the build instructions. The numbers agree with the steps.
module scene(n) {
  if (n == 1) {
    // Inserts in the body. The view is from below.
    color(c_print) p_body();
    on_slope() for (p = disp_boss_uv) translate([p[0], p[1], -front_t - disp_boss_h - 14]) insert_m3(insert_l);
    for (p = post_xy) translate([p[0], p[1], base_t - 14]) insert_m3(insert_l);
  } else if (n == 2) {
    // Inserts in the two frames. The rear faces point up.
    rotate([180, 0, 0]) translate([0, -slope_l, 0]) {
      color(c_dark) p_frame();
      for (b = frame_boss_uv) translate([b[0], b[1], -frame_t - frame_boss_l - 12]) insert_m3(insert_l);
    }
    translate([W + 20, 40, 0]) rotate([180, 0, 0]) translate([0, -lip_h, 0]) {
      color(c_dark) p_inky_frame();
      for (b = inky_boss_xz) translate([b[0], b[1], -frame_t - inky_boss_l - 12]) insert_m3(insert_l);
    }
  } else if (n == 3) {
    // Parts onto the rear wall.
    color(c_print) p_body();
    g_rear_parts(30);
  } else if (n == 4) {
    // Display frame onto the body.
    color(c_print) p_body();
    on_slope() frame_asm(45);
  } else if (n == 5) {
    // Display into the body, from below.
    on_slope() { frame_asm(); display_asm(60); }
    ghost_body();
  } else if (n == 6) {
    // Pad, Inky wHAT, and Inky frame onto the body.
    on_slope() { frame_asm(); display_asm(); }
    color(c_print) p_body();
    on_front() inky_asm(85);
  } else if (n == 7) {
    // Drive onto the base plate. The bolts go in from below.
    color(c_print) p_base();
    g_drive(40);
    bolts_below(ssd_holes, 6, 35);
    translate([ssd_x0 - 10, ssd_y0 + ssd[1] / 2, base_t + 75]) rotate([180, 0, 0]) arrow(45);
  } else if (n == 8) {
    // Pi stack onto the base plate.
    color(c_print) p_base();
    g_drive(); bolts_below(ssd_holes, 6);
    translate([0, 0, 50]) g_stack();
    translate([pi_pos[0] + 70, pi_pos[1] + 100, base_t + 95]) rotate([180, 0, 0]) arrow(45);
  } else if (n == 10) {
    // Body onto the base plate.
    base_asm();
    bolts_below(post_xy, 8, 40);
    translate([0, 0, 150]) body_asm(true);
  } else if (n == 11) {
    // Complete unit between the speakers.
    assembly();
    translate([-W - 40, 25, 0]) speaker(W, H, D, 8);
    translate([W + 40, 25, 0]) speaker(W, H, D, 8);
  }
}

module assembly() {
  base_asm();
  bolts_below(post_xy, 8);
  body_asm();
}

// All parts, moved away from the body along the direction of their assembly.
module exploded(e = 1) {
  color("#D9D9D6") p_body();
  on_slope() {
    translate([0, 0, 40 * e]) display_asm();
    frame_asm(90 * e);
  }
  on_front() inky_asm(90 * e);
  g_rear_parts(35 * e);
  translate([0, 0, -150 * e]) {
    base_asm(25 * e);
    bolts_below(post_xy, 8, 45 * e);
  }
}

// The printed parts come out in the correct position for the print bed.
// The "raw" parts come out in their position in the case. The images use them.
if (part == "body") translate([0, D, H]) rotate([180, 0, 0]) p_body();
else if (part == "base") p_base();
else if (part == "frame") translate([0, slope_l, 0]) rotate([180, 0, 0]) p_frame();
else if (part == "inky_frame") translate([0, lip_h, 0]) rotate([180, 0, 0]) p_inky_frame();
else if (part == "cap") cap();
else if (part == "pad") translate([-inky_c[0], -inky_c[1], 0]) pad();
else if (part == "raw_body") body();
else if (part == "raw_base") base();
else if (part == "raw_frame") frame();
else if (part == "raw_inky_frame") inky_frame();
else if (part == "step") rotate([0, 0, view_rear ? 180 : 0]) scene(step);
else if (part == "assembly") rotate([0, 0, view_rear ? 180 : 0]) assembly();
else rotate([0, 0, view_rear ? 180 : 0]) exploded(explode);
