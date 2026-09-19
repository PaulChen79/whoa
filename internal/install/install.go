// Package install writes whoa's hook configuration into a Harness's settings
// file, and takes it out again.
//
// The settings file belongs to the user and to every other tool that writes
// hooks into it, so install is additive and reversible: it adds whoa's handler
// beside whatever is already there, changes nothing else, preserves key order,
// and refuses outright rather than risk a file it cannot parse.
package install

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/PaulChen79/whoa/internal/core"
)

// harness is the Harness whose settings file this package writes. Codex has a
// different settings file and different event names; it arrives with its own
// Harness value rather than by widening this one.
const harness = core.ClaudeCode

// handler is one hook handler entry in a settings file.
//
// It carries only the fields Claude Code documents. whoa would rather identify
// its own handlers by a marker key of its own, but nothing documents whether
// an unknown key inside a handler is tolerated or rejected, and an install
// that half-succeeds silently is the exact failure whoa exists to prevent.
type handler struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

// group is one matcher group: a filter plus the handlers that run when it
// matches.
type group struct {
	Matcher string    `json:"matcher"`
	Hooks   []handler `json:"hooks"`
}

// binaryName is what whoa's own handlers invoke. Uninstall recognises its
// handlers by their command, so a user who renames the binary keeps the hook
// but has to remove it by hand.
const binaryName = "whoa"

// subcommand is the argument whoa's hook handlers pass.
const subcommand = "hook"

// timeoutSeconds is far more than the binary needs. It exists so that a
// pathological filesystem stall cannot hold up the agent indefinitely; the
// observed cost of a Step is a couple of milliseconds.
const timeoutSeconds = 10

// Install adds whoa's hook handlers to the settings file at path, creating the
// file if it does not exist. It reports whether it changed anything, so
// running it twice is a no-op the second time.
func Install(path, binary string) (bool, error) {
	settings, err := load(path)
	if err != nil {
		return false, err
	}

	hooks, err := hooksObject(settings)
	if err != nil {
		return false, err
	}

	changed := false
	for _, event := range core.Events(harness) {
		groups, err := groupsFor(hooks, event)
		if err != nil {
			return false, err
		}
		if hasWhoa(groups) {
			continue
		}
		groups = append(groups, group{
			Matcher: "*",
			Hooks: []handler{{
				Type:    "command",
				Command: fmt.Sprintf("%s %s", shellQuote(binary), subcommand),
				Timeout: timeoutSeconds,
			}},
		})
		if err := hooks.set(event, groups); err != nil {
			return false, err
		}
		changed = true
	}
	if !changed {
		return false, nil
	}
	if err := settings.set("hooks", hooks); err != nil {
		return false, err
	}
	return true, save(path, settings)
}

// Uninstall removes the handlers whoa owns and leaves everything else in
// place, including other tools' handlers on the same events.
func Uninstall(path string) (bool, error) {
	settings, err := load(path)
	if err != nil {
		return false, err
	}
	hooks, err := hooksObject(settings)
	if err != nil {
		return false, err
	}

	changed := false
	for _, event := range core.Events(harness) {
		groups, err := groupsFor(hooks, event)
		if err != nil {
			return false, err
		}
		kept := make([]group, 0, len(groups))
		for _, g := range groups {
			handlers := make([]handler, 0, len(g.Hooks))
			for _, h := range g.Hooks {
				if h.owned() {
					changed = true
					continue
				}
				handlers = append(handlers, h)
			}
			if len(handlers) == 0 {
				continue
			}
			g.Hooks = handlers
			kept = append(kept, g)
		}
		if len(kept) == 0 {
			hooks.delete(event)
			continue
		}
		if err := hooks.set(event, kept); err != nil {
			return false, err
		}
	}
	if !changed {
		return false, nil
	}
	if hooks.empty() {
		settings.delete("hooks")
	} else if err := settings.set("hooks", hooks); err != nil {
		return false, err
	}
	return true, save(path, settings)
}

func load(path string) (*object, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return newObject(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return newObject(), nil
	}
	settings := newObject()
	if err := json.Unmarshal(b, settings); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON, so whoa left it alone: %w", path, err)
	}
	return settings, nil
}

// save writes the settings atomically, so an interrupted install cannot leave
// the user with a truncated settings file and a Harness that will not start.
//
// The file belongs to the user, so an existing one keeps its permissions; only
// a file whoa creates gets whoa's choice of mode.
func save(path string, settings *object) error {
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	var pretty []byte
	if pretty, err = indent(raw); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating settings directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".whoa-settings-*")
	if err != nil {
		return fmt.Errorf("creating temporary settings file: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(pretty); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func hooksObject(settings *object) (*object, error) {
	hooks := newObject()
	raw, ok := settings.get("hooks")
	if !ok {
		return hooks, nil
	}
	if err := json.Unmarshal(raw, hooks); err != nil {
		return nil, fmt.Errorf(`the "hooks" section is not a JSON object, so whoa left it alone: %w`, err)
	}
	return hooks, nil
}

func groupsFor(hooks *object, event string) ([]group, error) {
	raw, ok := hooks.get(event)
	if !ok {
		return nil, nil
	}
	var groups []group
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, fmt.Errorf("the %s hooks are not in the expected shape, so whoa left them alone: %w", event, err)
	}
	return groups, nil
}

func hasWhoa(groups []group) bool {
	for _, g := range groups {
		for _, h := range g.Hooks {
			if h.owned() {
				return true
			}
		}
	}
	return false
}

// owned reports whether this handler is one whoa installed, by reading the
// command it runs: a binary called whoa, invoked with whoa's own subcommand.
func (h handler) owned() bool {
	cmd := strings.TrimSpace(h.Command)
	if !strings.HasSuffix(cmd, " "+subcommand) {
		return false
	}
	binary := shellUnquote(strings.TrimSpace(strings.TrimSuffix(cmd, subcommand)))
	name := filepath.Base(binary)
	return name == binaryName || name == binaryName+".exe"
}

// shellQuote makes a path safe to embed in the command string, which the
// Harness runs through a shell. Paths with spaces in them are ordinary on
// macOS, and an unquoted one would install a hook that never runs.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func shellUnquote(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], `'\''`, "'")
	}
	return s
}

func indent(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}
