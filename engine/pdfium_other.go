//go:build !windows && !(linux && cgo)

package engine

// No PDF backend on this platform/build (for example a Linux build without cgo):
// PDF actions report errNoPDF, everything else works.
