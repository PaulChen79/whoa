// Command whoa is the hook binary both Claude Code and Codex invoke.
//
// This process is spawned once per Step, so it is deliberately thin: read the
// payload, hand it to the pure core, act on what the core decides. All I/O
// lives out here and none of it lives in the core.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PaulChen79/whoa/internal/config"
	"github.com/PaulChen79/whoa/internal/core"
	"github.com/PaulChen79/whoa/internal/install"
	"github.com/PaulChen79/whoa/internal/store"
)

// version is overridden at build time via -ldflags.
var version = "dev"

const usage = `whoa — stop-loss for coding agents

Usage:
  whoa install     register whoa's hooks with Claude Code and Codex
  whoa uninstall   remove them again
  whoa doctor      check whoa is actually running, per Harness
  whoa wrong       mark the last thing whoa said as a Misjudgment
  whoa digest      print exactly what would be sent to the Judge, and send nothing
  whoa calibrate   report how often whoa has been wrong
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
		return hook(s, harnessFrom(args[1:]))
	case "install":
		return installCmd(s)
	case "judge":
		return judgeCmd(s, args[1:])
	case "digest":
		return digestCmd(s, args[1:])
	case "wrong":
		return wrongCmd(s)
	case "calibrate":
		return calibrateCmd(s)
	case "doctor":
		return doctorCmd(s)
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
// harnessFrom reads the Harness the installed hook was told to report as.
//
// Nothing in a payload says which Harness sent it, so install writes the
// answer into the command it registers. An older handler without the flag is
// Claude Code, which is the only Harness that existed before the flag did.
func harnessFrom(args []string) core.Harness {
	for _, arg := range args {
		if name, ok := strings.CutPrefix(arg, "--harness="); ok {
			for _, known := range []core.Harness{core.ClaudeCode, core.Codex} {
				if core.Harness(name) == known {
					return known
				}
			}
		}
	}
	return core.ClaudeCode
}

func hook(s system, harness core.Harness) error {
	raw, err := io.ReadAll(s.stdin)
	if err != nil {
		warn(s, err)
		return nil
	}

	cfg, err := config.Load(s.home)
	if err != nil {
		// Reported, then carried on with the defaults. A broken config file
		// must be visible, but it must not be the reason whoa stops watching.
		warn(s, err)
	}

	// The Session id decides which log to read, and it is in the payload, so
	// the core is asked for it before it is asked to decide.
	//
	// An empty id means the payload was unreadable or is not about a Session.
	// Loading with it would report "empty session id", which describes whoa's
	// own question rather than the problem; the core is left to make the same
	// judgement and return a no-op.
	var log []core.Entry
	if session := core.SessionID(raw); session != "" {
		loaded, err := store.Load(cfg.StateDir, session)
		if err != nil {
			warn(s, err)
		}
		log = loaded
	}

	ob := core.Observe(core.Input{
		Raw:            raw,
		Harness:        harness,
		Log:            log,
		Config:         cfg,
		Now:            s.now(),
		JudgeAvailable: apiKey() != "",
	})

	if ob.Problem != "" {
		warn(s, errors.New(ob.Problem))
	}
	if ob.AskJudge {
		askJudge(s, core.SessionID(raw))
	}
	if ob.Notice != nil {
		ob.Notice.Session = core.SessionID(raw)
		if err := store.Append(cfg.StateDir, ob.Notice); err != nil {
			warn(s, err)
		}
	}

	if len(ob.Output) > 0 {
		if _, err := s.stdout.Write(append(ob.Output, '\n')); err != nil {
			warn(s, err)
		}
	}
	if ob.Entry == nil {
		return nil
	}
	if err := store.Append(cfg.StateDir, ob.Entry); err != nil {
		warn(s, err)
	}
	sweep(s, cfg)
	return nil
}

// warn reports a problem inside whoa without failing the agent's Step.
//
// It is quiet by design but never silent: a hook that exits 0 has its stderr
// routed to the Harness's debug log, so `claude --debug` surfaces this today
// and `whoa doctor` will surface it without the flag. What must not happen is
// whoa swallowing its own failure while the user believes it is watching.
// sweep expires old Session logs, at most once a day.
//
// It runs from the hook because that is the only thing that runs regularly.
// Doing it on every Step would walk the log directory thousands of times a
// day; doing it only from a command the user has to remember would mean it
// never happened at all, which for a retention promise is the same as lying.
//
// The marker is written before the sweep, so a sweep that fails does not
// retry on every Step for the rest of the day.
func sweep(s system, cfg core.Config) {
	if cfg.RetentionDays <= 0 || !store.SweepDue(cfg.StateDir, s.now()) {
		return
	}
	if err := store.MarkSwept(cfg.StateDir, s.now()); err != nil {
		warn(s, err)
		return
	}
	if _, err := store.Expire(cfg.StateDir, cfg.RetentionDays); err != nil {
		warn(s, err)
	}
}

func warn(s system, err error) {
	fmt.Fprintln(s.stderr, "whoa:", err)
}

func installCmd(s system) error {
	if s.binary == "" {
		return fmt.Errorf("could not find the whoa binary to install")
	}

	installed := 0
	for _, t := range targets(s.home) {
		if !t.present() {
			continue
		}
		installed++
		changed, err := install.Install(t.settings, s.binary, t.harness)
		if err != nil {
			return fmt.Errorf("%s: %w", t.name, err)
		}
		if changed {
			fmt.Fprintf(s.stdout, "Installed whoa for %s in %s\n", t.name, t.settings)
		} else {
			fmt.Fprintf(s.stdout, "whoa is already installed for %s in %s\n", t.name, t.settings)
		}
		// The trust step is printed whether or not anything changed, because
		// an install that "succeeded" earlier and was never trusted is
		// exactly the silent non-protection this is here to prevent.
		if len(t.trust) > 0 {
			fmt.Fprintf(s.stdout, "\n  REQUIRED for %s:\n%s\n", t.name, indent(t.trust, "    "))
		}
	}

	if installed == 0 {
		return fmt.Errorf("found neither Claude Code nor Codex under %s", s.home)
	}

	cfg, err := config.Load(s.home)
	if err != nil {
		return err
	}
	fmt.Fprintf(s.stdout, "Steps are recorded under %s\n", filepath.Join(cfg.StateDir, "sessions"))
	fmt.Fprintln(s.stdout, "Restart the agent, run a tool call, then `whoa doctor` to confirm it is running.")
	return nil
}

func uninstallCmd(s system) error {
	removed := 0
	for _, t := range targets(s.home) {
		changed, err := install.Uninstall(t.settings)
		if err != nil {
			return fmt.Errorf("%s: %w", t.name, err)
		}
		if changed {
			removed++
			fmt.Fprintf(s.stdout, "Removed whoa from %s\n", t.settings)
		}
	}
	if removed == 0 {
		fmt.Fprintln(s.stdout, "whoa was not installed for any Harness")
	}
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

func claudeSettingsPath(s system) string {
	return filepath.Join(s.home, ".claude", "settings.json")
}
