package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PaulChen79/whoa/internal/config"
	"github.com/PaulChen79/whoa/internal/core"
	"github.com/PaulChen79/whoa/internal/install"
)

// notRunning is returned when no Harness can be shown to be running whoa.
//
// It is an error rather than a report because the whole point of doctor is to
// be usable from a script: a protection tool that is not running should fail a
// check, not print a cheerful summary that nobody reads.
var notRunning = fmt.Errorf("whoa is not running on any Harness")

// doctorCmd answers one question per Harness: is whoa actually observing?
//
// Every other check here exists because it is a way for the answer to be "no"
// while everything looks fine. A hook can be registered and blocked by policy,
// registered and never trusted, or registered and simply never fired because
// the agent was not restarted. None of those announce themselves.
func doctorCmd(s system) error {
	cfg, err := config.Load(s.home)
	if err != nil {
		warn(s, err)
	}
	seen := lastSeen(cfg.StateDir)

	running := false
	for _, t := range targets(s.home) {
		fmt.Fprintf(s.stdout, "%s\n", t.name)

		if !t.present() {
			fmt.Fprintf(s.stdout, "  not installed on this machine (%s does not exist)\n\n", t.dir)
			continue
		}

		events, err := install.Registered(t.settings, t.harness)
		switch {
		case err != nil:
			fmt.Fprintf(s.stdout, "  cannot read %s: %v\n", t.settings, err)
		case len(events) == 0:
			fmt.Fprintf(s.stdout, "  not registered in %s — run `whoa install`\n", t.settings)
		case len(events) < len(core.Events(t.harness)):
			fmt.Fprintf(s.stdout, "  registered on %s, but not on %s — run `whoa install`\n",
				strings.Join(events, ", "), strings.Join(missing(events, core.Events(t.harness)), ", "))
		default:
			fmt.Fprintf(s.stdout, "  registered on %s\n", strings.Join(events, ", "))
		}

		if install.DisableAllHooks(t.settings) {
			fmt.Fprintf(s.stdout, "  BLOCKED: disableAllHooks is true in %s; no hook runs at all\n", t.settings)
		}
		if path, ok := managedHooksOnly(t); ok {
			fmt.Fprintf(s.stdout, "  BLOCKED: %s is true in %s\n", t.key, path)
			fmt.Fprintf(s.stdout, "           Your administrator has restricted %s to managed hooks.\n", t.name)
			fmt.Fprintf(s.stdout, "           whoa is installed in your own settings, so it will never run.\n")
		}

		if when, ok := seen[t.harness]; ok {
			running = true
			fmt.Fprintf(s.stdout, "  observing: last Step %s (%s)\n", humanAge(s.now().Sub(when)), when.Format(time.RFC3339))
		} else {
			fmt.Fprintf(s.stdout, "  NOT OBSERVING: no Step has ever been recorded for this Harness\n")
			if len(t.trust) > 0 {
				fmt.Fprint(s.stdout, indent(t.trust, "           "))
			} else {
				fmt.Fprintf(s.stdout, "           Restart %s and run one tool call, then check again.\n", t.name)
			}
		}
		fmt.Fprintln(s.stdout)
	}

	if !running {
		return notRunning
	}
	return nil
}

// missing reports which of want is absent from have.
func missing(have, want []string) []string {
	present := make(map[string]bool, len(have))
	for _, h := range have {
		present[h] = true
	}
	var gone []string
	for _, w := range want {
		if !present[w] {
			gone = append(gone, w)
		}
	}
	return gone
}

// managedHooksOnly looks for the one administrator setting that disables every
// hook a user installs, in whichever file that Harness keeps it in.
//
// The Codex file is TOML and the Claude Code file is JSON. whoa has no
// dependencies, so the TOML is scanned for the assignment rather than parsed.
// A scan can be fooled by a commented-out line; reporting a block that is not
// there costs the user one confused minute, and missing one costs them the
// belief that they are protected.
func managedHooksOnly(t target) (string, bool) {
	for _, path := range t.managed {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if strings.HasSuffix(path, ".json") {
			var settings map[string]json.RawMessage
			if json.Unmarshal(raw, &settings) != nil {
				continue
			}
			var on bool
			if v, ok := settings[t.key]; ok && json.Unmarshal(v, &on) == nil && on {
				return path, true
			}
			continue
		}
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") {
				continue
			}
			name, value, ok := strings.Cut(line, "=")
			if ok && strings.TrimSpace(name) == t.key && strings.TrimSpace(value) == "true" {
				return path, true
			}
		}
	}
	return "", false
}

// lastSeen is the evidence half of doctor: not what the settings say, but when
// each Harness last actually caused whoa to run.
func lastSeen(stateDir string) map[core.Harness]time.Time {
	seen := map[core.Harness]time.Time{}
	logs, err := filepath.Glob(filepath.Join(stateDir, "sessions", "*.jsonl"))
	if err != nil {
		return seen
	}
	for _, path := range logs {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if line == "" {
				continue
			}
			var e core.Entry
			if json.Unmarshal([]byte(line), &e) != nil || e.Harness == "" {
				continue
			}
			if e.Timestamp.After(seen[e.Harness]) {
				seen[e.Harness] = e.Timestamp
			}
		}
	}
	return seen
}

func humanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}
