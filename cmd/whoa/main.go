// Command whoa is the hook binary that both Claude Code and Codex invoke.
//
// This process is spawned once per Step, so everything here is deliberately
// thin: read the payload, hand it to the pure core, write the core's decision
// back out. All I/O lives in this package and none of it lives in the core.
package main

import (
	"fmt"
	"os"
)

// version is overridden at build time via -ldflags.
var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "whoa:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin *os.File, stdout, stderr *os.File) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: whoa <command>")
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, version)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
