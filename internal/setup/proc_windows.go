//go:build windows

package setup

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps child processes from flashing a console window.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
