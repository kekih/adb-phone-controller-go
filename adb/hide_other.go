//go:build !windows

package adb

import "os/exec"

func hideWindow(cmd *exec.Cmd) {
	// no-op on non-Windows
}
