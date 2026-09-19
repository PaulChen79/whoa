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

// harnessFlag tells the running hook which Harness invoked it.
//
// The binary cannot work this out for itself: both Harnesses send a payload on
// stdin and neither says who it is. Guessing from which fields are present
// would silently mislabel Steps, and a Step attributed to the wrong Harness is
// worse than no Step, because `whoa doctor` would then report a Harness as
// observing when it is not. Install knows, so install writes it down.
const harnessFlag = "--harness="

// timeoutSeconds is far more than the binary needs. It exists so that a
// pathological filesystem stall cannot hold up the agent indefinitely; the
// observed cost of a Step is a couple of milliseconds.
const timeoutSeconds = 10

// Install reports whether it changed anything, so running it twice is a no-op
// the second time.
//
// Install adds whoa's hook handlers for one Harness to the settings file at
// path, creating the file if it does not exist. Both Harnesses use the same
// settings shape, so only the path and the event names differ.
func Install(path, binary string, harness core.Harness) (bool, error) {
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
				Command: fmt.Sprintf("%s %s %s%s", shellQuote(binary), subcommand, harnessFlag, harness),
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
	for _, event := range core.AllEvents() {
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
	// Trailing flags are whoa's own and may change between versions, so
	// ownership is decided by the binary and the subcommand alone; otherwise
	// an upgrade would strand every handler an older version wrote.
	//
	// The command is trimmed from the right rather than split into fields,
	// because the binary path is quoted and routinely contains spaces.
	for {
		i := strings.LastIndex(cmd, " ")
		if i < 0 || !strings.HasPrefix(cmd[i+1:], "-") {
			break
		}
		cmd = strings.TrimSpace(cmd[:i])
	}
	if !strings.HasSuffix(cmd, " "+subcommand) {
		return false
	}
	name := filepath.Base(shellUnquote(strings.TrimSpace(strings.TrimSuffix(cmd, subcommand))))
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

// Registered reports whether whoa's handlers are present in the settings file
// at path, and on which events.
//
// `whoa doctor` asks this rather than assuming an install that returned no
// error is still in place: a settings file is a file the user also edits.
func Registered(path string, harness core.Harness) ([]string, error) {
	settings, err := load(path)
	if err != nil {
		return nil, err
	}
	hooks, err := hooksObject(settings)
	if err != nil {
		return nil, err
	}
	var found []string
	for _, event := range core.Events(harness) {
		groups, err := groupsFor(hooks, event)
		if err != nil {
			return nil, err
		}
		if hasWhoa(groups) {
			found = append(found, event)
		}
	}
	return found, nil
}

// DisableAllHooks reports whether the settings file turns every hook off. It
// is one key, it is silent, and it makes whoa a no-op.
func DisableAllHooks(path string) bool {
	settings, err := load(path)
	if err != nil {
		return false
	}
	raw, ok := settings.get("disableAllHooks")
	if !ok {
		return false
	}
	var disabled bool
	return json.Unmarshal(raw, &disabled) == nil && disabled
}
