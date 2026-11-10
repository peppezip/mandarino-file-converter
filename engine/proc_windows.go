//go:build windows

package engine

import (
	"os/exec"
	"syscall"
)

const isWindows = true

// hideConsole keeps ffmpeg's black console window from flashing on screen.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
