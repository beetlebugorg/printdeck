// Speakerdeck case: parameters and shared modules.
// All part files include this file. Change a dimension here, and all parts follow.
// Units are millimetres. Measure your parts and correct the values before you print.
//
// Axes: X is the width, Y is the depth (the front is at Y = 0), Z is the height.

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
// The printed pad is thicker than its space by this value. Use 0 for a rigid PLA pad,
// and 0.5 for TPU, rubber, or foam, so that the pad pushes the Inky wHAT against the frame.
pad_squeeze = 0;
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
// Height of the connector row. It is above the USB plugs of the Pi.
rear_row_z = 42;
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

// ---------------------------------------------------------------- shared modules

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

// Screen of the Inky wHAT.
module inky_view_rect() {
  b = inky_border;
  translate([inky_c[0] - inky_board[0] / 2 + b[0], inky_c[1] - inky_board[1] / 2 + b[3]])
    rrect(inky_board[0] - b[0] - b[1], inky_board[1] - b[2] - b[3], 1);
}
