// Speakerdeck case: the cap for a frame boss.
// The plate contains the eight caps of the case.
// Open this file to see the part in its position for the print bed.

include <common.scad>

// Cap that holds a frame boss from the inner side of the body.
module cap() {
  difference() {
    cylinder(d = 13, h = 7);
    translate([0, 0, 1.8]) cylinder(d = 8.2, h = 6);
    translate([0, 0, -1]) cylinder(d = 3.4, h = 4);
  }
}

for (i = [0:3], j = [0:1]) translate([i * 18, j * 18, 0]) cap();
