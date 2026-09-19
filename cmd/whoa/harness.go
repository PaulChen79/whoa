package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/PaulChen79/whoa/internal/core"
)

// target is one Harness and the places on disk that decide whether whoa runs
// under it.
//
// Both Harnesses use the same hook configuration shape, so only the paths and
// the event names differ; that is why there is no adapter layer here, just a
// table.
type target struct {
	harness core.Harness
	name    string
	// dir is the Harness's own configuration directory. Its absence is how
	// whoa tells that a Harness is not installed on this machine.
	dir string
	// settings is the file whoa writes its handlers into.
	settings string
	// managed is the administrator-controlled file that can disable hooks
	// wholesale, and the key inside it that does so.
	managed []string
	key     string
	// trust, when set, is the step the user must take themselves before the
	// Harness will run the hook at all. It is a list of lines so that every
	// place that prints it can indent it to its own context.
	trust []string
}

func targets(home string) []target {
	return []target{
		{
			harness:  core.ClaudeCode,
			name:     "Claude Code",
			dir:      filepath.Join(home, ".claude"),
			settings: filepath.Join(home, ".claude", "settings.json"),
			managed:  claudeManagedPaths(),
			key:      "allowManagedHooksOnly",
		},
		{
			harness:  core.Codex,
			name:     "Codex",
			dir:      filepath.Join(home, ".codex"),
			settings: filepath.Join(home, ".codex", "hooks.json"),
			managed:  []string{filepath.Join(home, ".codex", "requirements.toml"), "/etc/codex/requirements.toml"},
			key:      "allow_managed_hooks_only",
			trust: []string{
				"Codex will not run a command hook until you review and trust it.",
				"Run /hooks inside Codex and approve whoa, or it will never fire.",
				"Trust is recorded against the hook's contents, so upgrading whoa",
				"means approving it again.",
			},
		},
	}
}

// claudeManagedPaths are the administrator-controlled settings files, which
// differ by platform.
func claudeManagedPaths() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"/Library/Application Support/ClaudeCode/managed-settings.json"}
	case "windows":
		return []string{filepath.Join(os.Getenv("PROGRAMDATA"), "ClaudeCode", "managed-settings.json")}
	default:
		return []string{"/etc/claude-code/managed-settings.json"}
	}
}

// present reports whether this Harness looks installed for this user.
func (t target) present() bool {
	info, err := os.Stat(t.dir)
	return err == nil && info.IsDir()
}

// indent renders the trust step under a heading, each line at the same depth.
func indent(lines []string, prefix string) string {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(prefix)
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
