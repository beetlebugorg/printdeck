//go:build !zslice

// Package zsview is the binding of the print view of zslice. With no tag zslice, the deck has no
// zslice: Available is false, and the deck draws its own 3D view.
package zsview

// Available reports that the deck has the print view of zslice.
const Available = false
