// Speakdeck case, version 11: a cabinet with the size of one speaker.
// The front leans back. The 7 inch touch display is almost flush with the front.
// Units are millimetres. Measure your parts and correct the values in the
// part sections before you print.
//
// Axes: X is the width, Y is the depth (the front is at Y = 0), Z is the height.
//
// Design rules:
// - The width, the height, and the depth agree with the speaker.
// - The front has an inset frame around the display. The frame has a pattern in its top band.
// - The display glass is 1 mm behind the front of the frame.
// - The top has an inset frame around the Inky wHAT. The e-paper is flush with the top.
// - The Inky wHAT is separate from the Pi stack. A ribbon cable connects them.
// - All bolts are M3. No bolt is visible from the front, the top, or the sides.
// - The body prints with the front face on the bed. Only the frame indent needs supports.
// - The two trays slide into grooves. The rear panel keeps them in position.

use <parts.scad>

/* [Output] */
// "exploded", "assembly", "body", "frame", "top_frame", "cap", "pad", "rear", "sled", "tray", or "step".
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
corner_r = 8;
// Clearance between parts.
gap = 0.3;
rear_t = 3;
tray_t = 3;
// Ribs of the tray grooves.
rib_w = 6;
rib_h = 4;

/* [Front] */
// Angle of the front from vertical. The top of the front goes to the rear.
tilt = 10;
// Distance from the front of the frame to the display glass.
glass_in = 1;
// The centre of the visible area is this distance above the centre of the front.
// The plugs below the display need the space.
disp_up = 4;
// The frame opening is larger than the visible area by this value on each side.
view_margin = 0.5;

/* [Front frame] */
// Thickness of the front wall of the body.
front_t = 5;
// Thickness of the frame. The indent in the body has the same depth.
frame_t = 2;
// Distance from the edges of the front to the frame.
frame_in = 6;
frame_r = 5;
// Pattern in the top band of the frame: number of bars, pitch, and bar width.
pattern_n = 15;
pattern_pitch = 6;
pattern_w = 3;

/* [Fasteners] */
// Hole for an M3 heat-set insert. Use the value from the insert datasheet.
insert_d = 4.0;
insert_l = 4;
boss_d = 8.5;

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

/* [Pi stack: Raspberry Pi 5 and HiFiBerry Amp2] */
// From the top face of the Pi board to the bottom face of the Amp2 board.
// Measure this value with your stacking adapter.
hat_gap = 16;
// Height of the Amp2 above the bottom face of its board. From the HiFiBerry model.
amp_h = 15.5;
// Free height above the stack, for the ribbon cable.
stack_clear = 30;
pi_post_h = 4;

/* [Top and Inky wHAT] */
// Outline of the Inky wHAT: [X, Y]. The long side is across the case.
inky_board = [91, 77];
// Total thickness of the board and the e-paper panel, without the header.
inky_t = 2.8;
// Distance from the edges to the screen: [left, right, rear, front].
// The wide border is at the front of the case, which is the bottom of the image.
inky_border = [2, 2, 2, 9];
// Thickness of the frame lip on the border. The e-paper is this distance below the top.
inky_lip = 0.6;
// The Inky wHAT sits in a cavity in the top wall, on a rubber pad.
// The pad pushes the Inky wHAT up against the lip of the top frame.
// Thickness of the pad when it is compressed, and thickness of the floor below it.
inky_pad = 1.5;
inky_floor = 2.4;
// Opening in the floor for the header and the ribbon cable: [X, Y].
inky_header = [60, 16];
// Offset of the centre of the header from the centre of the board: [X, Y].
// Measure this value. The default puts the header 14 mm from the rear long edge.
inky_header_off = [0, 24.5];

/* [Drive tray] */
// Height of the top face of the drive tray. The drive is below the display plugs.
tray_top = 12;
// 2.5 inch SATA drive: [width, length, thickness].
ssd = [69.85, 100.45, 6.8];
// M3 holes in the bottom of the drive: distance across, and distances from the connector end.
ssd_hole_w = 61.72;
ssd_hole_y = [14.0, 90.6];
// The USB adapter board extends past the connector end by this distance. Measured: 36.08 mm.
ssd_adapter = 36.1;
// Width of the adapter board, and length of the USB plug.
ssd_adapter_w = 66;
ssd_plug = 30;
// Height of the pads below the drive. This gives the correct bolt depth in the drive.
ssd_pad = 1;

/* [Rear panel] */
barrel_d = 8;
post_d = 7;
post_pitch = 19;
fan_size = 30;
fan_t = 10;
fan_hole_pitch = 24;
// Offset of the fan from the centre.
fan_dx = 0;
usb_slot = [14, 8];
rear_row_z = 30;
dot_d = 2;
dot_pitch = 4.5;

/* [Hidden] */
$fn = 48;
W = speaker_w;
H = speaker_h;
D = speaker_d;

// Positions on the front: "u" is across and "v" is up the slope.
face_l = H / cos(tilt);
// Centre of the visible area, of the LCD module, and of the display board.
view_c = [0, face_l / 2 + disp_up];
lcd_c = [view_c[0] - disp_view_off[0], view_c[1] - disp_view_off[1]];
pcb_c = [lcd_c[0] - disp_lcd_off[0], lcd_c[1] - disp_lcd_off[1]];
disp_boss_uv = [for (su = [-1, 1], sv = [-1, 1]) [pcb_c[0] + su * disp_holes[0] / 2, pcb_c[1] + sv * disp_holes[1] / 2]];
// The display board sits on bosses on the inner side of the front wall.
disp_boss_h = max(0, glass_in + disp_lcd_t - front_t);
// Space that the display and its plugs need behind the front face.
disp_depth = glass_in + disp_lcd_t + disp_pcb_t + disp_back;
// Bosses on the rear face of the frame: [u, v].
frame_boss_uv = [[-40, 17], [40, 17], [-57, face_l - 11], [57, face_l - 11]];
frame_boss_l = front_t - frame_t + 5;
// Y position of the front face, and of the free space behind the display.
function face_y(z) = z * tan(tilt);
function behind_y(z) = face_y(z) + disp_depth / cos(tilt);

// Thickness of the top wall. It contains the frame lip, the Inky wHAT, the pad, and the floor.
top_t = inky_lip + inky_t + inky_pad + inky_floor;
stack_h = 1.5 + hat_gap + amp_h;
sled_top = H - top_t - stack_clear - stack_h - pi_post_h;
sled_y0 = ceil(behind_y(sled_top));
// The drive tray starts behind the lower caps of the front frame.
tray_y0 = ceil(face_y(tray_top) + (front_t + 9) / cos(tilt));
// The SD card end of the Pi points to the front. The USB ports point to the rear.
pi_y0 = sled_y0 + 3;
pi_c = [W / 2, pi_y0 + 32.5];
// Front edge of the top, centre of the Inky board, and bosses of the top frame: [x, y].
top_y0 = face_y(H);
inky_c = [W / 2, (top_y0 + D) / 2];
top_boss_xy = [for (x = [20.5, W - 20.5], y = [top_y0 + 17, D - 24]) [x, y]];
top_boss_l = top_t - frame_t + 5;
tray_x0 = wall + gap;
tray_w = W - 2 * (wall + gap);
tray_y1 = D - rear_t - 0.5;
post_s = 10;
post_c = wall + 4.5;
// The lower posts are above the drive tray.
post_xz = [for (x = [post_c, W - post_c], z = [tray_top + 16, H - top_t - 4.5]) [x, z]];
fan_x = W / 2 + fan_dx;
fan_z = H - 28;
post_x = [for (s = [-1, 1], i = [0, 1]) [W / 2 + s * (38 + i * post_pitch), i]];
jack_x = W / 2 - 12;

// Drive tray layout. The connector end of the drive points to the rear.
ssd_x0 = (W - ssd[0]) / 2;
ssd_y0 = tray_y0 + 2;
ssd_holes = [for (sx = [-1, 1], hy = ssd_hole_y) [W / 2 + sx * ssd_hole_w / 2, ssd_y0 + ssd[1] - hy]];

echo(str("Space behind the USB plug of the drive: ", tray_y1 - ssd_y0 - ssd[1] - ssd_adapter - ssd_plug, " mm"));
echo(str("Space for the USB plugs behind the Pi: ", D - rear_t - pi_y0 - 85, " mm"));
echo(str("Space for the plugs below the display: ", (pcb_c[1] - disp_pcb[1] / 2) * cos(tilt) - wall, " mm"));

module rrect(w, h, r) {
  translate([r, r]) offset(r = r) square([w - 2 * r, h - 2 * r]);
}

// Extrude a shape in the XZ plane along Y, from y0 to y1.
module ext_y(y0, y1) {
  translate([0, y1, 0]) rotate([90, 0, 0]) linear_extrude(y1 - y0, convexity = 10) children();
}

// Cylinder along Y that starts at the given point.
module cyl_y(x, y, z, d, h, d2 = -1) {
  translate([x, y, z]) rotate([-90, 0, 0]) cylinder(d1 = d, d2 = d2 < 0 ? d : d2, h = h);
}

// Put a part with its +Z axis along +Y.
module along_y(x, y, z) {
  translate([x, y, z]) rotate([-90, 0, 0]) children();
}

// Local frame of the front: X across, Y up the slope, Z out of the case.
module on_face() {
  translate([W / 2, 0, 0]) rotate([90 - tilt, 0, 0]) children();
}

// The cabinet volume, made smaller by "inset" on the four long faces and on the front.
module solid(inset) {
  intersection() {
    ext_y(-1, D + (inset > 0 ? 1 : 0))
      translate([inset, inset]) rrect(W - 2 * inset, H - inset - (inset > 0 ? top_t : 0), corner_r - inset);
    on_face() translate([-W / 2 - 5, -20, -(D + 30)]) cube([W + 10, face_l + 20 + D * sin(tilt) + 10, D + 30 - (inset > 0 ? front_t : 0)]);
  }
}

// ---------------------------------------------------------------- body

// One groove on each side wall. A tray with its top face at z_top slides in.
module ribs(z_top) {
  for (x = [wall - 0.5, W - wall - rib_w]) {
    translate([x, 0, z_top - tray_t - rib_h]) cube([rib_w + 0.5, D - rear_t, rib_h]);
    translate([x, 0, z_top + 0.5]) cube([rib_w + 0.5, D - rear_t, rib_h]);
  }
}

// Opening for the LCD module, and the visible area.
module lcd_rect(g = 0) {
  translate([lcd_c[0] - disp_lcd[0] / 2 - g, lcd_c[1] - disp_lcd[1] / 2 - g]) square([disp_lcd[0] + 2 * g, disp_lcd[1] + 2 * g]);
}

module view_rect() {
  m = view_margin;
  translate([view_c[0] - disp_view[0] / 2 - m, view_c[1] - disp_view[1] / 2 - m]) rrect(disp_view[0] + 2 * m, disp_view[1] + 2 * m, 2);
}

module body() {
  difference() {
    union() {
      difference() {
        solid(0);
        solid(wall);
      }
      // Bosses for the display board.
      on_face() for (p = disp_boss_uv)
        translate([p[0], p[1], -front_t - disp_boss_h]) cylinder(d = 7.4, h = disp_boss_h + 0.01);
      intersection() {
        solid(0);
        union() {
          // Corner posts. The rear panel bolts to these posts.
          ext_y(0, D - rear_t) for (p = post_xz) translate([p[0] - post_s / 2, p[1] - post_s / 2]) square(post_s);
          ribs(sled_top);
          ribs(tray_top);
        }
      }
    }
    // Opening for the LCD module. The module goes through the front wall.
    on_face() translate([0, 0, -front_t - disp_boss_h - 1]) linear_extrude(front_t + disp_boss_h + 2) lcd_rect(gap);
    // Indent for the frame, and holes for the frame bosses.
    on_face() translate([0, 0, -frame_t]) linear_extrude(frame_t + 1) frame_outline(gap);
    on_face() for (b = frame_boss_uv) translate([b[0], b[1], -front_t - 1]) cylinder(d = 7.8, h = front_t + 2);
    // Top: indent for the top frame, and holes for its bosses.
    translate([0, 0, H - frame_t]) linear_extrude(frame_t + 1) top_outline(gap);
    for (b = top_boss_xy) translate([b[0], b[1], H - top_t - 1]) cylinder(d = 7.8, h = top_t + 2);
    // Cavity for the pad and the Inky wHAT, and an opening for the header and the ribbon cable.
    translate([0, 0, H - inky_lip - inky_t - inky_pad]) linear_extrude(top_t) inky_rect(inky_board, gap);
    translate([0, 0, H - top_t - 1]) linear_extrude(top_t + 2) inky_rect(inky_header, 0, inky_header_off);
    // Insert holes: display bosses and corner posts. The frame covers the display holes.
    on_face() for (p = disp_boss_uv)
      translate([p[0], p[1], -front_t - disp_boss_h - 0.01]) cylinder(d = insert_d, h = insert_l + 1);
    for (p = post_xz)
      cyl_y(p[0], D - rear_t - insert_l - 1, p[1], insert_d, insert_l + 1.01);
  }
}

// ---------------------------------------------------------------- front frame

module frame_outline(g = 0) {
  translate([-W / 2 + frame_in - g, frame_in - g]) rrect(W - 2 * (frame_in - g), face_l - 2 * (frame_in - g), frame_r + g);
}

// Row of bars with different heights, like a level meter.
module pattern() {
  band_v0 = lcd_c[1] + disp_lcd[1] / 2 + 3;
  band_v1 = face_l - frame_in - 3;
  max_h = band_v1 - band_v0;
  for (i = [0:pattern_n - 1]) {
    h = max(pattern_w + 1, max_h * (0.25 + 0.75 * abs(sin(i * 47 + 20)) * (0.5 + 0.5 * abs(sin(i * 23)))));
    translate([(i - (pattern_n - 1) / 2) * pattern_pitch, (band_v0 + band_v1) / 2]) hull() {
      translate([0, -h / 2 + pattern_w / 2]) circle(d = pattern_w);
      translate([0, h / 2 - pattern_w / 2]) circle(d = pattern_w);
    }
  }
}

// Frame in the local frame of the front. The front face is at Z = 0.
// The pattern goes through the frame, so you see the body colour in it.
// A pocket in the rear face holds the edge of the LCD module.
module frame() {
  difference() {
    union() {
      translate([0, 0, -frame_t]) linear_extrude(frame_t, convexity = 10) difference() {
        frame_outline();
        view_rect();
        pattern();
      }
      for (b = frame_boss_uv) translate([b[0], b[1], -frame_t - frame_boss_l]) cylinder(d = 7.4, h = frame_boss_l);
    }
    translate([0, 0, -frame_t - 1]) linear_extrude(frame_t - glass_in + 1) lcd_rect(gap);
    for (b = frame_boss_uv)
      translate([b[0], b[1], -frame_t - frame_boss_l - 0.01]) cylinder(d = insert_d, h = insert_l + 1);
  }
}

// ---------------------------------------------------------------- top frame

module top_outline(g = 0) {
  translate([frame_in - g, top_y0 + frame_in - g])
    rrect(W - 2 * (frame_in - g), D - top_y0 - 2 * (frame_in - g), frame_r + g);
}

// Rectangle at the Inky position, larger by "g" on each side.
module inky_rect(size, g = 0, off = [0, 0]) {
  translate([inky_c[0] + off[0] - size[0] / 2 - g, inky_c[1] + off[1] - size[1] / 2 - g])
    square([size[0] + 2 * g, size[1] + 2 * g]);
}

// Screen of the Inky wHAT.
module inky_view_rect() {
  b = inky_border;
  translate([inky_c[0] - inky_board[0] / 2 + b[0], inky_c[1] - inky_board[1] / 2 + b[3]])
    rrect(inky_board[0] - b[0] - b[1], inky_board[1] - b[2] - b[3], 1);
}

// Top frame, in its position in the case. The top face is at Z = H.
// The opening shows the screen. A thin lip covers the border and holds the Inky wHAT down.
module top_frame() {
  difference() {
    union() {
      translate([0, 0, H - frame_t]) linear_extrude(frame_t, convexity = 10) difference() {
        top_outline();
        inky_view_rect();
      }
      for (b = top_boss_xy) translate([b[0], b[1], H - frame_t - top_boss_l]) cylinder(d = 7.4, h = top_boss_l);
    }
    translate([0, 0, H - frame_t - 1]) linear_extrude(frame_t - inky_lip + 1) inky_rect(inky_board, gap);
    for (b = top_boss_xy)
      translate([b[0], b[1], H - frame_t - top_boss_l - 0.01]) cylinder(d = insert_d, h = insert_l + 1);
  }
}

// Pad below the Inky wHAT. Print it in TPU, or use it as a pattern to cut rubber or foam.
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

// ---------------------------------------------------------------- rear panel

module rear_hole(x, z, d) {
  cyl_y(x, D - rear_t - 1, z, d, rear_t + 2);
}

module rear() {
  mid = W / 2;
  difference() {
    ext_y(D - rear_t, D) translate([wall + gap, wall + gap])
      rrect(W - 2 * (wall + gap), H - wall - top_t - 2 * gap, corner_r - wall - gap);
    // Bolts to the corner posts, countersunk.
    for (p = post_xz) {
      rear_hole(p[0], p[1], 3.4);
      cyl_y(p[0], D - 1.7, p[1], 3.4, 1.71, 6.6);
    }
    // Speaker binding posts: left pair and right pair.
    for (p = post_x) rear_hole(p[0], rear_row_z, post_d);
    // Power input.
    rear_hole(jack_x, rear_row_z, barrel_d);
    // Slot for the USB cable to the printer.
    translate([mid + 12 - usb_slot[0] / 2, D - rear_t - 1, rear_row_z - usb_slot[1] / 2])
      cube([usb_slot[0], rear_t + 2, usb_slot[1]]);
    // Air inlet, above the drive tray.
    for (i = [-10:10], j = [0:4])
      rear_hole(mid + i * dot_pitch, tray_top + 12 + j * dot_pitch, dot_d);
    // Fan outlet, adjacent to the Pi stack.
    n = floor(fan_size / 2 / dot_pitch);
    for (i = [-n:n], j = [-n:n])
      if (norm([i, j]) * dot_pitch <= fan_size / 2 - 2)
        rear_hole(fan_x + i * dot_pitch, fan_z + j * dot_pitch, dot_d);
    for (dx = [-1, 1], dz = [-1, 1])
      rear_hole(fan_x + dx * fan_hole_pitch / 2, fan_z + dz * fan_hole_pitch / 2, 3.4);
  }
}

// ---------------------------------------------------------------- trays

module tray_plate(z_top, y0) {
  translate([tray_x0, y0, z_top - tray_t]) cube([tray_w, tray_y1 - y0, tray_t]);
}

// Opening at the rear edge for cables.
module cable_notch(z_top) {
  translate([W / 2 - 35, tray_y1 - 28, z_top - tray_t - 1]) cube([70, 29, tray_t + 2]);
}

// Upper tray. The Pi stack sits on four pins.
module sled() {
  pins = [for (sx = [-1, 1], sy = [-1, 1]) [pi_c[0] + sx * 24.5, pi_c[1] + sy * 29]];
  difference() {
    union() {
      tray_plate(sled_top, sled_y0);
      for (p = pins) translate([p[0], p[1], sled_top - 0.01]) {
        cylinder(d = 6.5, h = pi_post_h);
        cylinder(d = 2.3, h = pi_post_h + 3);
      }
    }
    // Openings for air and cables.
    translate([pi_c[0] - 16, pi_c[1] - 22, sled_top - tray_t - 1]) cube([32, 44, tray_t + 2]);
    for (s = [-1, 1])
      translate([pi_c[0] + s * 41 - 9, pi_c[1] - 25, sled_top - tray_t - 1]) cube([18, 70, tray_t + 2]);
    cable_notch(sled_top);
  }
}

// Drive tray. Four M3 bolts go from below into the holes in the bottom of the drive.
module tray() {
  difference() {
    union() {
      tray_plate(tray_top, tray_y0);
      for (h = ssd_holes) translate([h[0], h[1], tray_top - 0.01]) cylinder(d = 9, h = ssd_pad);
    }
    for (h = ssd_holes) translate([h[0], h[1], tray_top - tray_t - 1]) cylinder(d = 3.4, h = tray_t + ssd_pad + 2);
    // Openings for air below the drive.
    for (i = [0:2]) translate([W / 2 - 22, ssd_y0 + 20 + i * 22, tray_top - tray_t - 1]) cube([44, 14, tray_t + 2]);
    cable_notch(tray_top);
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
module p_rear() { p_part("rear") rear(); }
module p_tray() { p_part("tray") tray(); }
module p_sled() { p_part("sled") sled(); }
module p_frame() { p_part("frame") frame(); }
module p_top_frame() { p_part("top_frame") top_frame(); }

// Frame with its caps and bolts, in the frame of the front.
// "out" moves the frame to the front, and the caps and bolts to the rear.
module frame_asm(out = 0) {
  color(c_dark) translate([0, 0, out]) p_frame();
  for (b = frame_boss_uv) translate([b[0], b[1], -front_t - 7 - out]) {
    color(c_mark) cap();
    translate([0, 0, -0.6 * out]) rotate([180, 0, 0]) bolt(6);
  }
}

// Display with its plugs and bolts, in the frame of the front.
// "lift" moves the display and its bolts to the rear.
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

module g_stack() {
  z0 = sled_top + pi_post_h;
  translate([pi_c[0] + 28, pi_c[1] - 32.5, z0]) rotate([0, 0, 90]) {
    pi5_model();
    translate([0, 0, 1.5]) pi_standoffs(hat_gap);
    translate([0, 0, 1.5 + hat_gap]) amp2_model();
  }
}

module g_inky(lift = 0) {
  translate([inky_c[0], inky_c[1], H - inky_lip - inky_t + lift]) inky_what(inky_board, inky_t, inky_border, inky_header_off);
}

module g_pad(lift = 0) {
  color("#3C3C40") translate([0, 0, H - inky_lip - inky_t - inky_pad + lift]) pad();
}

// Top frame with its caps and bolts. "out" moves the frame up, and the caps and bolts down.
module top_frame_asm(out = 0) {
  color(c_dark) translate([0, 0, out]) p_top_frame();
  for (b = top_boss_xy) translate([b[0], b[1], H - top_t - 7 - 0.4 * out]) {
    color(c_mark) cap();
    translate([0, 0, -0.3 * out]) rotate([180, 0, 0]) bolt(6);
  }
}

module g_drive(lift = 0) {
  translate([ssd_x0, ssd_y0, tray_top + ssd_pad + lift]) ssd_sata(ssd, ssd_adapter, ssd_adapter_w);
}

module g_drive_bolts(drop = 0) {
  for (h = ssd_holes) translate([h[0], h[1], tray_top - tray_t - drop]) rotate([180, 0, 0]) bolt(6);
}

// Parts of the rear panel. "out" moves them away from the panel.
module g_rear_parts(out = 0) {
  along_y(fan_x, D - rear_t - fan_t - out, fan_z) fan30(fan_size, fan_t, fan_hole_pitch);
  for (dx = [-1, 1], dz = [-1, 1]) {
    along_y(fan_x + dx * fan_hole_pitch / 2, D + 1.2 * out, fan_z + dz * fan_hole_pitch / 2) bolt(16);
    along_y(fan_x + dx * fan_hole_pitch / 2, D - rear_t - fan_t - 2.4 - 1.8 * out, fan_z + dz * fan_hole_pitch / 2) nut_m3();
  }
  for (p = post_x) along_y(p[0], D + out, rear_row_z) binding_post(p[1] == 0 ? "#C0392B" : "#1E1E20", rear_t);
  along_y(jack_x, D + out, rear_row_z) dc_jack(rear_t);
}

module g_rear_bolts(out = 0) {
  for (p = post_xz) along_y(p[0], D + out, p[1]) bolt_csk(8);
}

// Arrow along +Z, for the build instructions.
module arrow(len = 40) {
  color(c_mark) { cylinder(d = 3, h = len - 10); translate([0, 0, len - 10]) cylinder(d1 = 9, d2 = 0, h = 10); }
}

module ghost_body() { color([0.86, 0.85, 0.81, 0.3]) p_body(); }

module tray_asm() { color(c_print) p_tray(); g_drive(); g_drive_bolts(); }
module sled_asm() { color(c_print) p_sled(); g_stack(); }
module rear_asm() { color(c_print) p_rear(); g_rear_parts(); }
module front_asm() { on_face() { frame_asm(); display_asm(); } }
module top_asm() { g_pad(); g_inky(); top_frame_asm(); }

// Scenes for the build instructions. The numbers agree with the steps.
module scene(n) {
  if (n == 1) {
    // Inserts in the body.
    color(c_print) p_body();
    on_face() for (p = disp_boss_uv) translate([p[0], p[1], -front_t - disp_boss_h - 14]) insert_m3(insert_l);
    for (p = post_xz) along_y(p[0], D + 8, p[1]) insert_m3(insert_l);
  } else if (n == 2) {
    // Inserts in the two frames. The rear faces of the frames point up.
    rotate([180, 0, 0]) translate([0, -face_l, 0]) {
      color(c_dark) p_frame();
      for (b = frame_boss_uv) translate([b[0], b[1], -frame_t - frame_boss_l - 12]) insert_m3(insert_l);
    }
    translate([W / 2 + 30, 40, 0]) rotate([180, 0, 0]) translate([0, -D, -H]) {
      color(c_dark) p_top_frame();
      for (b = top_boss_xy) translate([b[0], b[1], H - frame_t - top_boss_l - 12]) insert_m3(insert_l);
    }
  } else if (n == 3) {
    // Front frame onto the body.
    color(c_print) p_body();
    on_face() frame_asm(45);
  } else if (n == 4) {
    // Display into the body.
    on_face() frame_asm();
    on_face() display_asm(70);
    translate([W / 2 + 62, 150, H / 2 + 60]) rotate([90 + tilt, 0, 0]) arrow(60);
    ghost_body();
  } else if (n == 5) {
    // Inky wHAT and top frame onto the body.
    front_asm();
    color(c_print) p_body();
    g_pad(25);
    g_inky(55);
    top_frame_asm(100);
    translate([W / 2 + 70, inky_c[1], H + 90]) rotate([180, 0, 0]) arrow(45);
  } else if (n == 6) {
    // Drive onto the tray. The bolts go in from below.
    color(c_print) p_tray();
    g_drive(40);
    g_drive_bolts(35);
    translate([W / 2 + 62, ssd_y0 + ssd[1] / 2, tray_top + 75]) rotate([180, 0, 0]) arrow(45);
  } else if (n == 7) {
    // Drive tray into the body.
    front_asm(); top_asm();
    translate([0, 190, 0]) tray_asm();
    translate([W / 2 + 62, tray_y1 + 180, tray_top + 30]) rotate([90, 0, 0]) arrow(60);
    ghost_body();
  } else if (n == 8) {
    // Pi stack onto the sled.
    color(c_print) p_sled();
    translate([0, 0, 45]) g_stack();
    translate([pi_c[0] + 62, pi_c[1], sled_top + 75]) rotate([180, 0, 0]) arrow(45);
  } else if (n == 9) {
    // Sled into the body.
    front_asm(); top_asm(); tray_asm();
    translate([0, 170, 0]) sled_asm();
    translate([W / 2 + 62, tray_y1 + 160, sled_top + 25]) rotate([90, 0, 0]) arrow(60);
    ghost_body();
  } else if (n == 10) {
    // Parts onto the rear panel.
    color(c_print) p_rear();
    g_rear_parts(30);
  } else if (n == 12) {
    // Rear panel onto the body.
    front_asm(); top_asm(); tray_asm(); sled_asm();
    color(c_print) p_body();
    translate([0, 80, 0]) rear_asm();
    g_rear_bolts(125);
  } else if (n == 13) {
    // Complete unit between the speakers.
    assembly();
    translate([-W - 40, 25, 0]) speaker(W, H, D, corner_r);
    translate([W + 40, 25, 0]) speaker(W, H, D, corner_r);
  }
}

module assembly() {
  front_asm(); top_asm();
  tray_asm(); sled_asm();
  color("#D9D9D6") p_body();
  rear_asm(); g_rear_bolts();
}

// All parts, moved away from the body along the direction of their assembly.
module exploded(e = 1) {
  color("#D9D9D6") p_body();
  // Front: the frame and the display.
  on_face() {
    translate([0, 0, 45 * e]) display_asm();
    frame_asm(95 * e);
  }
  // Top: the Inky wHAT and the top frame.
  g_pad(25 * e);
  g_inky(55 * e);
  top_frame_asm(105 * e);
  // Rear: the drive tray, the sled, and the rear panel.
  translate([0, 205 * e, 0]) {
    color(c_print) p_tray();
    g_drive(22 * e);
    g_drive_bolts(18 * e);
  }
  translate([0, 205 * e, 0]) {
    color(c_print) p_sled();
    translate([0, 0, 22 * e]) g_stack();
  }
  translate([0, 400 * e, 0]) {
    color(c_print) p_rear();
    g_rear_parts(28 * e);
    g_rear_bolts(60 * e);
  }
}

// The printed parts come out in the correct position for the print bed.
// The "raw" parts come out in their position in the case. The images use them.
if (part == "body") translate([W / 2, face_l, 0]) rotate([180, 0, 0]) rotate([tilt - 90, 0, 0]) translate([-W / 2, 0, 0]) p_body();
else if (part == "frame") translate([0, face_l, 0]) rotate([180, 0, 0]) p_frame();
else if (part == "top_frame") translate([0, D, 0]) rotate([180, 0, 0]) translate([0, 0, -H]) p_top_frame();
else if (part == "cap") cap();
else if (part == "pad") translate([-inky_c[0], -inky_c[1], 0]) pad();
else if (part == "rear") translate([0, 0, D]) rotate([-90, 0, 0]) p_rear();
else if (part == "sled") translate([0, 0, -(sled_top - tray_t)]) p_sled();
else if (part == "tray") translate([0, 0, -(tray_top - tray_t)]) p_tray();
else if (part == "raw_body") body();
else if (part == "raw_frame") frame();
else if (part == "raw_top_frame") top_frame();
else if (part == "raw_rear") rear();
else if (part == "raw_sled") sled();
else if (part == "raw_tray") tray();
else if (part == "step") rotate([0, 0, view_rear ? 180 : 0]) scene(step);
else if (part == "assembly") rotate([0, 0, view_rear ? 180 : 0]) assembly();
else rotate([0, 0, view_rear ? 180 : 0]) exploded(explode);
