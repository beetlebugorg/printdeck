// Detailed models of the purchased parts.
// These models are for the previews and the build instructions. Do not print them.

$fn = 48;

c_steel = "#B9BCC0";
c_black = "#1E1E20";
c_brass = "#C9A24A";
c_gold = "#D4B03A";
c_pcb = "#2F6B4F";
c_alu = "#9EA3A8";

module p_rrect(w, h, r) {
  translate([r, r]) offset(r = r) square([w - 2 * r, h - 2 * r]);
}

module hex(af, h) {
  cylinder(d = af / cos(30), h = h, $fn = 6);
}

module threads(d, len, pitch = 0.9) {
  cylinder(d = d - 0.7, h = len);
  for (i = [0:max(0, floor(len / pitch) - 1)]) translate([0, 0, i * pitch]) {
    cylinder(d1 = d - 0.7, d2 = d, h = pitch / 2);
    translate([0, 0, pitch / 2]) cylinder(d1 = d, d2 = d - 0.7, h = pitch / 2);
  }
}

module knurl(d, h, n = 20) {
  difference() {
    cylinder(d = d, h = h);
    for (i = [0:n - 1]) rotate(i * 360 / n) translate([d / 2, 0, -1]) cylinder(d = d * 0.1, h = h + 2, $fn = 6);
  }
}

// ---------------------------------------------------------------- fasteners
// The head is above Z = 0 and the thread is below.

module bolt(len) {
  color(c_black) {
    difference() {
      cylinder(d = 5.5, h = 3);
      translate([0, 0, 1.2]) hex(2.5, 2);
    }
    translate([0, 0, -len]) threads(3, len);
  }
}

// Countersunk bolt. The top of the head is at Z = 0.
module bolt_csk(len) {
  color(c_black) {
    difference() {
      translate([0, 0, -1.7]) cylinder(d1 = 3, d2 = 6, h = 1.7);
      translate([0, 0, -1.1]) hex(2, 2);
    }
    translate([0, 0, -len]) threads(3, len - 1.7);
  }
}

module nut_m3() {
  color(c_steel) difference() {
    hex(5.5, 2.4);
    translate([0, 0, -1]) cylinder(d = 2.6, h = 5);
  }
}

// Heat-set insert, from Z = 0 to Z = l.
module insert_m3(l = 4) {
  color(c_brass) difference() {
    union() {
      knurl(4.6, l * 0.4);
      translate([0, 0, l * 0.4]) cylinder(d = 3.8, h = l * 0.2);
      translate([0, 0, l * 0.6]) knurl(4.6, l * 0.4);
    }
    translate([0, 0, -1]) cylinder(d = 2.5, h = l + 2);
  }
}

// ---------------------------------------------------------------- panel parts
// The outer face of the panel is at Z = 0. The outer side is +Z.

module binding_post(c = "#C0392B", panel = 3) {
  color(c_steel) hex(11, 1.5);
  color(c) translate([0, 0, 1.5]) cylinder(d = 10.5, h = 2.5);
  color(c_gold) translate([0, 0, 4]) difference() {
    cylinder(d = 6.5, h = 5);
    translate([0, 0, 2.4]) rotate([90, 0, 0]) cylinder(d = 2.2, h = 10, center = true);
  }
  color(c) translate([0, 0, 8.5]) difference() {
    union() {
      knurl(11.5, 8, 16);
      translate([0, 0, 8]) cylinder(d1 = 11.5, d2 = 9.5, h = 1.5);
    }
    translate([0, 0, 4]) cylinder(d = 5.6, h = 8);
  }
  color(c_gold) translate([0, 0, 8.5]) difference() {
    cylinder(d = 5.5, h = 9);
    translate([0, 0, 2]) cylinder(d = 4, h = 8);
  }
  // Inner side.
  color(c) translate([0, 0, -panel - 1.5]) cylinder(d = 10.5, h = 1.5);
  color(c_gold) translate([0, 0, -panel - 17]) threads(4, 17);
  color(c_steel) translate([0, 0, -panel - 3.9]) hex(7, 2.4);
  color(c_gold) translate([-2.5, -2.5, -panel - 7.5]) difference() {
    cube([13, 5, 0.6]);
    translate([10, 2.5, -1]) cylinder(d = 2, h = 3);
  }
  color(c_steel) translate([0, 0, -panel - 10.5]) hex(7, 2.4);
}

module dc_jack(panel = 3) {
  color(c_black) difference() {
    union() {
      cylinder(d = 11.5, h = 2);
      translate([0, 0, -13]) threads(7.8, 13, 1);
      translate([0, 0, -17]) cylinder(d = 9, h = 4);
    }
    translate([0, 0, -10]) cylinder(d = 6.3, h = 13);
  }
  color(c_steel) translate([0, 0, -9]) cylinder(d = 2, h = 10);
  color(c_steel) translate([0, 0, -panel - 2.2]) difference() {
    hex(10, 2.2);
    translate([0, 0, -1]) cylinder(d = 7.9, h = 5);
  }
  color(c_steel) for (a = [0, 120, 240]) rotate(a) translate([2.6, -0.9, -21.5]) cube([0.5, 1.8, 4.6]);
}

// Fan. The air goes along +Z. The frame is from Z = 0 to Z = t.
module fan30(s = 30, t = 10, pitch = 24) {
  color(c_black) difference() {
    translate([-s / 2, -s / 2, 0]) linear_extrude(t, convexity = 6) p_rrect(s, s, 3);
    translate([0, 0, -1]) cylinder(d = s - 2, h = t + 2);
    for (x = [-1, 1], y = [-1, 1]) translate([x * pitch / 2, y * pitch / 2, -1]) cylinder(d = 3.2, h = t + 2);
  }
  color(c_black) for (a = [45, 135, 225, 315]) rotate(a) translate([0, -0.8, 0]) cube([s / 2 - 1, 1.6, 1.2]);
  color("#2B2B2E") cylinder(d = 12.5, h = t - 0.8);
  color("#3A3A3E") for (i = [0:6]) rotate(i * 360 / 7) translate([9.6, 0, t / 2]) rotate([42, 0, 0])
    cube([6.8, 7.6, 0.7], center = true);
  color("#C9CCD1") translate([0, 0, t - 0.8]) cylinder(d = 10, h = 0.15);
  color("#C0392B") translate([s / 2 - 3, -s / 2 - 12, 1]) cube([1, 12, 1]);
  color(c_black) translate([s / 2 - 4.4, -s / 2 - 12, 1]) cube([1, 12, 1]);
}

// ---------------------------------------------------------------- Pi stack
// The origin is the board corner near the SD card. X is the long side. The USB ports are at +X.

pi_holes = [[3.5, 3.5], [61.5, 3.5], [3.5, 52.5], [61.5, 52.5]];

module hat_pcb(len, c) {
  color(c) linear_extrude(1.5, convexity = 6) difference() {
    p_rrect(len, 56, 3);
    for (p = pi_holes) translate(p) circle(d = 2.7);
  }
}

module usb_stack() {
  color(c_steel) difference() {
    cube([17.5, 13.2, 15.6]);
    for (z = [1.8, 9]) translate([12, 1.2, z]) cube([6, 10.8, 5]);
  }
  color("#2A5DA8") for (z = [3.4, 10.6]) translate([8, 2.2, z]) cube([8.5, 8.8, 1.6]);
}

module pin_header(h = 8.5) {
  color(c_black) translate([7.1, 49.9, 0]) cube([50.8, 5.1, h]);
}

module pi5() {
  hat_pcb(85, c_pcb);
  translate([0, 0, 1.5]) {
    pin_header(2.5);
    color(c_gold) for (i = [0:19], j = [0, 1]) translate([8.37 + i * 2.54 - 0.3, 51.2 + j * 2.54 - 0.3, 2.5]) cube([0.6, 0.6, 6]);
    translate([69.5, 22.5, 0]) usb_stack();
    translate([69.5, 40.4, 0]) usb_stack();
    // Ethernet.
    color(c_steel) translate([66, 2.2, 0]) difference() {
      cube([21, 16, 13.5]);
      translate([14, 2, 2]) cube([8, 12, 9]);
    }
    // USB-C and two micro HDMI ports.
    color(c_steel) translate([6.7, -1.2, 0]) cube([9, 7.4, 3.2]);
    color(c_steel) for (x = [22.2, 35.7]) translate([x, -1.2, 0]) cube([7, 7.6, 3]);
    // Processor, RP1, power chip, radio.
    color("#C4C7CB") translate([22, 22, 0]) cube([17, 17, 2.2]);
    color(c_black) translate([47, 24, 0]) cube([12, 12, 1.2]);
    color(c_black) translate([13, 13, 0]) cube([7, 7, 1]);
    color("#C4C7CB") translate([5, 28, 0]) cube([12, 14, 1.6]);
    // Fan connector.
    color("#EDEDED") translate([60.5, 45, 0]) cube([4.5, 6, 3.6]);
  }
}

// Models from the manufacturers, in the same frame as pi5() and amp_hat().
// If a mesh file is not in the vendor directory, set the flag to false to use the simple model.
use_pi_mesh = false;
use_amp_mesh = true;

module pi5_model() {
  if (use_pi_mesh) color(c_pcb) import("vendor/raspberry_pi_5.stl", convexity = 10);
  else pi5();
}

module amp2_model() {
  if (use_amp_mesh) color("#3A6EA5") translate([-99.82, 156.2, 0]) import("vendor/hifiberry_amp2.stl", convexity = 10);
  else amp_hat();
}

// Four hex standoffs at the Pi hole pattern.
module pi_standoffs(h) {
  color(c_brass) for (p = pi_holes) translate([p[0], p[1], 0]) hex(5, h);
}

module amp_hat() {
  hat_pcb(65, "#1F3A5C");
  // Header socket below the board.
  color(c_black) translate([7.1, 49.9, -8.5]) cube([50.8, 5.1, 8.5]);
  translate([0, 0, 1.5]) {
    // Speaker terminal block.
    color("#3E8E5A") translate([22, 1, 0]) difference() {
      cube([20.5, 8, 10]);
      for (i = [0:3]) translate([2.6 + i * 5.08, 4, 6]) cylinder(d = 3, h = 5);
      for (i = [0:3]) translate([1 + i * 5.08, -1, 1.5]) cube([3.2, 4, 3.5]);
    }
    // Power input.
    color(c_black) translate([3, 1, 0]) cube([9, 14, 11]);
    color(c_steel) translate([7.5, 0.9, 6]) rotate([90, 0, 0]) cylinder(d = 6.3, h = 1);
    // Output filter and capacitors.
    color("#55585C") for (x = [22, 34], y = [16, 29]) translate([x, y, 0]) cube([10, 10, 5]);
    color("#2B2B2E") for (x = [51, 60]) translate([x, 22, 0]) cylinder(d = 8, h = 10.5);
    color(c_alu) for (x = [51, 60]) translate([x, 22, 10.5]) cylinder(d = 7.4, h = 0.2);
    color(c_black) translate([47, 34, 0]) cube([9, 9, 1.2]);
  }
}

// Inky wHAT. The origin is the centre of the bottom face. The screen is on top.
// "border" is the distance from the edges to the screen: [-X, +X, +Y, -Y].
module inky_what(size = [91, 77], t = 2.8, border = [2, 2, 2, 9], header_off = [0, 24.5]) {
  view = [size[0] - border[0] - border[1], size[1] - border[2] - border[3]];
  color("#17171A") translate([-size[0] / 2, -size[1] / 2, 0]) linear_extrude(1.6) p_rrect(size[0], size[1], 1.5);
  color("#DADBD3") translate([-size[0] / 2, -size[1] / 2, 1.6]) cube([size[0], size[1], t - 1.7]);
  // Screen, with an example image: album art and three text lines.
  translate([-size[0] / 2 + border[0], -size[1] / 2 + border[3], t - 0.1]) {
    color("#F1F1EA") cube([view[0], view[1], 0.1]);
    s = view[1] - 12;
    color("#1B1B1B") translate([6, 6, 0.1]) cube([s, s, 0.05]);
    color("#1B1B1B") for (i = [0:2])
      translate([s + 12, view[1] - 10 - i * 9, 0.1]) cube([(view[0] - s - 18) * (1 - i * 0.22), i == 0 ? 3.4 : 2, 0.05]);
  }
  // Header socket below the board.
  color(c_black) translate([header_off[0] - 25.4, header_off[1] - 2.55, -8.5]) cube([50.8, 5.1, 8.5]);
}

// ---------------------------------------------------------------- 7 inch display
// The origin is the centre of the board. The long side is along Y. The glass is at Z = 0
// and points to +Z. The HDMI and USB sockets are at the -Y edge, on the rear side.

module display7(pcb = [116.2, 165.1], holes = [108.2, 157.1], lcd = [100, 165], lcd_t = 6.2,
                view = [86.5, 154.5], view_off = [0, 0], pcb_t = 1.6, lcd_off = [0, 0]) {
  // LCD module: metal frame and glass.
  translate([lcd_off[0], lcd_off[1], 0]) {
  color(c_alu) translate([-lcd[0] / 2, -lcd[1] / 2, -lcd_t]) cube([lcd[0], lcd[1], lcd_t - 0.6]);
  color("#101012") translate([-lcd[0] / 2 + 0.6, -lcd[1] / 2 + 0.6, -0.6]) cube([lcd[0] - 1.2, lcd[1] - 1.2, 0.6]);
  color("#050506") translate([view_off[0] - view[0] / 2, view_off[1] - view[1] / 2, 0]) cube([view[0], view[1], 0.05]);
  }
  // Driver board.
  translate([0, 0, -lcd_t - pcb_t]) {
    // The board has the size of the LCD, with one ear for each mounting hole.
    color("#1D4E9E") linear_extrude(pcb_t, convexity = 6) difference() {
      union() {
        translate([lcd_off[0] - lcd[0] / 2, -pcb[1] / 2]) square([lcd[0], pcb[1]]);
        for (x = [-1, 1], y = [-1, 1]) translate([x * (pcb[0] / 2 - 4.1), y * (pcb[1] / 2 - 3.45)]) square([8.2, 6.9], center = true);
      }
      for (x = [-1, 1], y = [-1, 1]) translate([x * holes[0] / 2, y * holes[1] / 2]) circle(d = 3.2);
    }
    color(c_gold) for (x = [-1, 1], y = [-1, 1]) translate([x * holes[0] / 2, y * holes[1] / 2, -0.05])
      difference() { cylinder(d = 5.6, h = pcb_t + 0.1); translate([0, 0, -1]) cylinder(d = 3.2, h = 4); }
    // Rear side of the board.
    color(c_steel) translate([-7.5, -pcb[1] / 2 - 1, -6.2]) difference() {
      cube([15, 12, 6.2]);
      translate([1.2, -1, 1.2]) cube([12.6, 6, 3.6]);
    }
    color(c_steel) translate([-24.3, -pcb[1] / 2 - 0.8, -2.8]) cube([7.6, 5.6, 2.8]);
    color(c_black) translate([-7, -pcb[1] / 2 + 22, -1.4]) cube([14, 14, 1.4]);
    color(c_black) translate([-27, -pcb[1] / 2 + 43, -1.2]) cube([6.5, 5, 1.2]);
    color("#55585C") translate([8, -pcb[1] / 2 + 39, -3]) cube([5, 5, 3]);
    color("#2B2B2E") translate([-14, -pcb[1] / 2 + 9, -5.5]) cylinder(d = 5, h = 5.5);
    // Flex cable of the LCD at the top edge.
    color("#C77B30") translate([-22, pcb[1] / 2 - 9, -0.4]) cube([44, 9.6, 0.4]);
    color("#EDEDED") translate([-23, pcb[1] / 2 - 14, -2]) cube([46, 5, 2]);
  }
}

// Right-angle plug at the origin. The cable goes to -Z.
module plug_ra(w = 21, c = c_black) {
  color(c) translate([-w / 2, -16, -9]) cube([w, 16, 9]);
  color(c) translate([0, -9, -9]) rotate([180, 0, 0]) cylinder(d = min(w * 0.45, 6), h = 22);
  color(c_steel) translate([-w / 2 + 3, -1, -6.5]) cube([w - 6, 6, 4]);
}

// ---------------------------------------------------------------- drive
// 2.5 inch SATA drive with a USB adapter board. The origin is a bottom corner.
// Y is the long side. The SATA connector and the adapter are at the +Y end.
// The bottom face has the label and the four M3 holes.

module ssd_sata(size = [69.85, 100.45, 6.8], adapter = 36.1, adapter_w = 66) {
  color("#141416") difference() {
    linear_extrude(size[2], convexity = 4) p_rrect(size[0], size[1], 2.5);
    translate([8, size[1] - 5, 0.8]) cube([42, 6, 4.2]);
  }
  color("#D8D8D8") translate([size[0] / 2 - 19, size[1] * 0.42, size[2]]) cube([38, 5, 0.05]);
  color("#8A8D91") translate([size[0] / 2 - 15, size[1] * 0.42 - 6, size[2]]) cube([30, 2.4, 0.05]);
  color("#8A8D91") translate([size[0] - 16, 8, size[2]]) cube([8, 8, 0.05]);
  // Adapter board, with the SATA plug, the USB socket, and parts.
  translate([(size[0] - adapter_w) / 2, size[1] - 2, 2.2]) {
    color("#2E8B3D") cube([adapter_w, adapter - 2, 1.6]);
    color(c_black) translate([adapter_w - 50, -3.5, -1.4]) cube([42, 9, 4.4]);
    color(c_steel) translate([4, adapter - 8, 1.6]) cube([12.5, 8, 3]);
    color("#2B2B2E") translate([adapter_w - 30, 16, 1.6]) cylinder(d = 5, h = 7);
    color(c_black) translate([adapter_w - 22, 18, 1.6]) cube([7, 7, 1.2]);
    color(c_gold) for (p = [[4, 4], [adapter_w - 4, adapter - 7], [adapter_w - 4, 4]]) translate([p[0], p[1], 1.6]) difference() {
      cylinder(d = 5.5, h = 0.05);
      translate([0, 0, -1]) cylinder(d = 3, h = 2);
    }
  }
}

// ---------------------------------------------------------------- speaker
// The front is at Y = 0. The origin is the bottom left corner of the front.

module woofer(r) {
  color("#1B1B1D") difference() {
    cylinder(r = r + 7, h = 1.5);
    translate([0, 0, -1]) cylinder(r = r, h = 4);
  }
  color("#0E0E0F") rotate_extrude() translate([r - 4.5, 0]) circle(r = 4.5, $fn = 24);
  color("#3B3D40") translate([0, 0, -r * 0.42]) difference() {
    cylinder(r1 = r * 0.3, r2 = r - 8, h = r * 0.42);
    translate([0, 0, 0.8]) cylinder(r1 = r * 0.3 - 0.5, r2 = r - 8, h = r * 0.42);
  }
  color("#232426") translate([0, 0, -r * 0.42]) scale([1, 1, 0.45]) sphere(r = r * 0.33);
}

module tweeter(r) {
  color("#1B1B1D") difference() {
    cylinder(r = r + 12, h = 1.5);
    translate([0, 0, 0.4]) cylinder(r1 = r + 1, r2 = r + 7, h = 1.2);
  }
  color("#4A4C50") translate([0, 0, 0.2]) scale([1, 1, 0.5]) sphere(r = r);
}

module speaker(w = 153, h = 233, d = 196, r = 8) {
  wz = h * 0.36;
  tz = h * 0.78;
  wr = w * 0.33;
  color("#2C2C2E") difference() {
    translate([0, d, 0]) rotate([90, 0, 0]) linear_extrude(d, convexity = 6) p_rrect(w, h, r);
    translate([w / 2, -1, wz]) rotate([-90, 0, 0]) cylinder(r = wr, h = wr * 0.5 + 1);
    translate([w / 2, 0.4, tz]) rotate([90, 0, 0]) cylinder(r = 30, h = 2);
  }
  translate([w / 2, 0, wz]) rotate([90, 0, 0]) woofer(wr);
  translate([w / 2, 0.4, tz]) rotate([90, 0, 0]) tweeter(13);
  color("#1B1B1D") for (x = [14, w - 14], y = [18, d - 18]) translate([x, y, -3]) cylinder(d = 14, h = 3);
}
