//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detach puts the Judge call in its own session, so that a Harness tidying up
// after the hook does not take the call down with it. Without this the child
// is in the hook's process group and a group signal reaches it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
