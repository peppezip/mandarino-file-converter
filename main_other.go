//go:build !windows && !linux

package main

import (
	"fmt"
	"os"
)

// Mandarino has a user interface for Windows and Linux only. On other systems
// (for example macOS) the engine can still be built and tested: go test ./engine
func main() {
	fmt.Fprintln(os.Stderr, "Mandarino supports Windows and Linux. On macOS, have a look at Tangerine.")
	os.Exit(1)
}
