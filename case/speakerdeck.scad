// Speakerdeck case: assembly, exploded view, and the scenes for the build instructions.
//
// The case is a cabinet with the size of one speaker.
// The lower front is vertical and holds the Inky wHAT.
// The upper front leans back 40 degrees and holds the 7 inch touch display.
//
// Each printed part has its own file: body.scad, base.scad, frame.scad,
// inky_frame.scad, cap.scad, and pad.scad. The parameters are in common.scad.
//
// Design rules:
// - The width, the height, and the depth agree with the speaker.
// - Each display has an inset frame. The display glass is 1 mm behind its frame,
//   and the e-paper is 0.6 mm behind its frame.
// - All bolts are M3. No bolt is visible from the front, the top, or the sides.
// - The body prints upside down, with the top on the bed. The rear wall is part of the body.
// - The base plate carries the drive and the Pi stack. It closes the body from below.

include <common.scad>
use <hardware.scad>
use <body.scad>
use <base.scad>
use <frame.scad>
use <inky_frame.scad>
use <cap.scad>
use <pad.scad>

/* [Output] */
// "exploded", "assembly", or "step".
part = "exploded";
// Distance between the parts in the exploded view. 1 is the default, 0 is the closed case.
explode = 1;
// Scene number for the build instructions. Used when part is "step".
step = 1;
// Turn the scene by 180 degrees. The rear of the case then gets the light.
view_rear = false;
// Use the mesh files in build/mesh for the printed parts. This makes the images fast.
mesh = false;

echo(str("Upper front: ", slope_l, " mm long. Top of the case: ", D - run, " mm deep."));
echo(str("Space behind the USB plug of the drive: ", D - wall - ssd_y0 - ssd[1] - ssd_adapter - ssd_plug, " mm"));
echo(str("Space for the USB plugs behind the Pi: ", D - wall - pi_pos[1] - 85, " mm"));

// ---------------------------------------------------------------- preview
// The detailed models of the purchased parts are in hardware.scad.

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

// The "raw" parts come out in their position in the case. The images use them.
if (part == "raw_body") body();
else if (part == "raw_base") base();
else if (part == "raw_frame") frame();
else if (part == "raw_inky_frame") inky_frame();
else if (part == "step") rotate([0, 0, view_rear ? 180 : 0]) scene(step);
else if (part == "assembly") rotate([0, 0, view_rear ? 180 : 0]) assembly();
else rotate([0, 0, view_rear ? 180 : 0]) exploded(explode);
