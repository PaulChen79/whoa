// Command whoa is the hook binary both Claude Code and Codex invoke.
//
// This process is spawned once per Step, so it is deliberately thin: read the
// payload, hand it to the pure core, act on what the core decides. All I/O
// lives out here and none of it lives in the core.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/PaulChen79/whoa/internal/core"
	"github.com/PaulChen79/whoa/internal/install"
	"github.com/PaulChen79/whoa/internal/store"
)

// version is overridden at build time via -ldflags.
var version = "dev"

const usage = `whoa — stop-loss for coding agents

Usage:
  whoa install     register whoa's hooks with Claude Code
  whoa uninstall   remove them again
  whoa hook        observe one Step (invoked by the Harness, reads stdin)
  whoa version     print the version
`

// system is the outside world the shell talks to: the streams, the user's home
// directory, and the clock. Everything impure whoa touches arrives through it,
// which is what lets the commands be tested without touching the real home
// directory.
type system struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	home   string
	binary string
	now    func() time.Time
}

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "whoa:", err)
		os.Exit(1)
	}
	s := system{
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
		home:   home,
		binary: whoaBinary(),
		now:    time.Now,
	}
	if err := run(os.Args[1:], s); err != nil {
		fmt.Fprintln(os.Stderr, "whoa:", err)
		os.Exit(1)
	}
}

func run(args []string, s system) error {
	if len(args) == 0 {
		fmt.Fprint(s.stdout, usage)
		return nil
	}
	switch args[0] {
	case "hook":
		return hook(s)
	case "install":
		return installCmd(s)
	case "uninstall":
		return uninstallCmd(s)
	case "version", "--version", "-v":
		fmt.Fprintln(s.stdout, version)
		return nil
	case "help", "--help", "-h":
		fmt.Fprint(s.stdout, usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
	}
}

// hook observes one Step.
//
// It never returns an error and never writes to stdout. whoa sits on the path
// of every tool call the agent makes, so a bug in here must cost the user
// nothing: a Step whoa cannot record is a Step it forgets, not a Step the
// agent is told failed. Silence is the documented no-op for a Claude Code
// hook, so emitting nothing is also the safest thing to emit.
func hook(s system) error {
	raw, err := io.ReadAll(s.stdin)
	if err != nil {
		warn(s, err)
		return nil
	}
	ob := core.Observe(core.Input{
		Raw:     raw,
		Harness: core.ClaudeCode,
		Now:     s.now(),
	})
	if ob.Step == nil {
		return nil
	}
	if err := store.Append(stateDir(s), ob.Step); err != nil {
		warn(s, err)
	}
	return nil
}

// warn reports a problem inside whoa without failing the agent's Step.
//
// It is quiet by design but never silent: a hook that exits 0 has its stderr
// routed to the Harness's debug log, so `claude --debug` surfaces this today
// and `whoa doctor` will surface it without the flag. What must not happen is
// whoa swallowing its own failure while the user believes it is watching.
func warn(s system, err error) {
	fmt.Fprintln(s.stderr, "whoa:", err)
}

func installCmd(s system) error {
	if s.binary == "" {
		return fmt.Errorf("could not find the whoa binary to install")
	}
	path := claudeSettingsPath(s)
	changed, err := install.Install(path, s.binary)
	if err != nil {
		return err
	}
	if !changed {
		fmt.Fprintf(s.stdout, "whoa is already installed in %s\n", path)
		return nil
	}
	fmt.Fprintf(s.stdout, "Installed whoa in %s\n", path)
	fmt.Fprintln(s.stdout, "Restart Claude Code, then run a tool call. Steps are recorded under", filepath.Join(stateDir(s), "sessions"))
	return nil
}

func uninstallCmd(s system) error {
	path := claudeSettingsPath(s)
	changed, err := install.Uninstall(path)
	if err != nil {
		return err
	}
	if !changed {
		fmt.Fprintf(s.stdout, "whoa was not installed in %s\n", path)
		return nil
	}
	fmt.Fprintf(s.stdout, "Removed whoa from %s\n", path)
	return nil
}

// whoaBinary is the path the installed hook will invoke. Resolving it is I/O,
// so it happens here once rather than inside the command.
func whoaBinary() string {
	binary, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(binary); err == nil {
		return resolved
	}
	return binary
}

// stateDir is where Session logs live. It becomes the state_dir Parameter in
// a later ticket; today it has one value.
func stateDir(s system) string { return filepath.Join(s.home, ".whoa") }

func claudeSettingsPath(s system) string {
	return filepath.Join(s.home, ".claude", "settings.json")
}
