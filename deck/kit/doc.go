// Package kit is a small UI framework for a touch screen that a program draws itself, one frame
// at a time. It has two parts:
//
//   - Layout: functions that divide a rectangle into rows, columns, grids and stacks. A screen
//     gets its rectangles from the layout, and then the same code fits a tall and a wide screen.
//   - Input: the touch areas of a frame (taps, sliders, scroll areas, swipes, pads), and the
//     gestures of the finger on them.
//
// The package draws nothing. The program draws its frame, and gives each touch area to Input
// while it draws. kit uses only the standard library, so that it can become a project of its own.
package kit
