// Package config loads whoa's Parameters from the user's config file.
//
// The set of Parameters and their defaults live in the core, which is where
// they are read; this package only turns a file on disk into that struct. See
// docs/adr/0004-parameters-and-invariants.md for what is a Parameter and what
// is deliberately not.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/PaulChen79/whoa/internal/core"
)

// Name is the config file, which lives at a fixed path under the home
// directory rather than under state_dir: state_dir is itself a Parameter, and
// a config file that moved with it could never be found in order to be read.
const Name = "config.json"

// Load reads the config file and layers it over the defaults.
//
// A missing file is not an error: whoa is meant to work the moment it is
// installed, and every Parameter has a default. A file that exists but cannot
// be parsed *is* an error, because silently falling back to defaults would
// leave someone believing a setting had taken effect when it had not.
func Load(home string) (core.Config, error) {
	// Every return below carries a usable state_dir, including the error
	// returns. A caller that reports the error and carries on must not end up
	// writing Session logs into whatever directory the agent happened to be
	// working in.
	fallback := core.Defaults()
	fallback.StateDir = filepath.Join(home, ".whoa")
	cfg := fallback

	path := filepath.Join(home, ".whoa", Name)
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("reading %s: %w", path, err)
	}

	// Unmarshalling over the defaults leaves anything the file does not
	// mention alone, so a file setting one Parameter does not silently reset
	// the other eleven.
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fallback, fmt.Errorf("parsing %s: %w", path, err)
	}

	cfg.StateDir = expandHome(cfg.StateDir, home)
	if cfg.StateDir == "" {
		cfg.StateDir = filepath.Join(home, ".whoa")
	}
	return cfg, validate(cfg)
}

// validate rejects values that would make whoa behave in a way the user
// plainly did not intend, rather than quietly clamping them.
func validate(c core.Config) error {
	switch c.Mode {
	case core.ModeCounters, core.ModeShadow, core.ModeFull:
	default:
		return fmt.Errorf("mode %q: want one of counters, shadow, full", c.Mode)
	}
	switch c.OnMissingKey {
	case core.OnMissingKeyDegrade, core.OnMissingKeyError:
	default:
		return fmt.Errorf("on_missing_key %q: want degrade or error", c.OnMissingKey)
	}
	if c.Window <= 0 {
		return fmt.Errorf("window %d: want a positive number of Steps", c.Window)
	}
	if c.RetentionDays < 0 {
		return fmt.Errorf("retention_days %d: want zero or more days", c.RetentionDays)
	}
	return nil
}

// expandHome resolves a leading ~ so that a hand-written state_dir behaves the
// way its author expects.
func expandHome(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}
