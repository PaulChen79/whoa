//go:build windows

package main

import "os/exec"

// detach is a no-op on Windows, where a child process does not share the
// parent's process group in the way that makes this necessary elsewhere.
func detach(cmd *exec.Cmd) {}
