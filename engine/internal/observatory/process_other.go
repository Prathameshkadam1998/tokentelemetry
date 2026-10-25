//go:build !windows

package observatory

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
