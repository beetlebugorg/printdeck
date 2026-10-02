// Speakerdeck case: the pad behind the Inky wHAT.
// It prints flat.
// Open this file to see the part in its position for the print bed.

include <common.scad>

// Pad behind the Inky wHAT. See "pad_squeeze" for the material.
module pad() {
  linear_extrude(inky_pad + pad_squeeze) difference() {
    inky_rect(inky_board, -0.3);
    inky_rect(inky_header, 1, inky_header_off);
  }
}

translate([-inky_c[0], -inky_c[1], 0]) pad();
