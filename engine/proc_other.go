//go:build !windows

package engine

import "os/exec"

const isWindows = false

// hideConsole does nothing outside Windows (no console windows to hide).
func hideConsole(cmd *exec.Cmd) {}
