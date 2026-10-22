// The test print of "Print on a part": a cube, then a cross on its top in another color. The two
// parts are in one frame, so "Place as in the files" puts the cross on the cube.
//
//   part = "cube"   job 1: the cube, a plain print. Take it off the bed.
//   part = "cross"  job 2: the cross. The tool adds a holder (the pocket) around the cube. The print
//                   pauses after the holder: put the cube in it, change the filament, check the
//                   height, and resume.

part = "cube";

SIZE = 20;    // the cube, mm
RAISE = 1;    // the height of the cross over the top of the cube
ARM = 14;     // the length of the cross
WIDE = 3;     // the width of the arms

module cube_part() { translate([-SIZE / 2, -SIZE / 2, 0]) cube(SIZE); }

module cross_part() {
    translate([0, 0, SIZE]) linear_extrude(RAISE) {
        square([ARM, WIDE], center = true);
        square([WIDE, ARM], center = true);
    }
}

if (part == "cube") cube_part();
else if (part == "cross") cross_part();
else if (part == "all") { color("gray") cube_part(); color("red") cross_part(); }
