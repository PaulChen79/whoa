package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PaulChen79/whoa/internal/core"
)

const bin = "/usr/local/bin/whoa"

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func hookEvents(t *testing.T, p string) map[string][]map[string]any {
	t.Helper()
	var s struct {
		Hooks map[string][]map[string]any `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(read(t, p)), &s); err != nil {
		t.Fatalf("settings is not valid JSON after install: %v\n%s", err, read(t, p))
	}
	return s.Hooks
}

func TestInstallCreatesSettingsWhenMissing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	if _, err := Install(p, bin, core.ClaudeCode); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, ok := hookEvents(t, p)["PostToolUse"]; !ok {
		t.Error("PostToolUse hook not written")
	}
}

// PostToolUse fires only on success. Loop detection counts repeated failures,
// so without the failure event whoa is blind to the Steps that matter most.
func TestInstallRegistersBothSuccessAndFailureEvents(t *testing.T) {
	p := write(t, `{}`)
	if _, err := Install(p, bin, core.ClaudeCode); err != nil {
		t.Fatal(err)
	}
	h := hookEvents(t, p)
	for _, event := range []string{"PostToolUse", "PostToolUseFailure"} {
		if _, ok := h[event]; !ok {
			t.Errorf("%s hook not written", event)
		}
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	p := write(t, `{}`)
	if changed, err := Install(p, bin, core.ClaudeCode); err != nil || !changed {
		t.Fatalf("first install: changed=%v err=%v", changed, err)
	}
	first := read(t, p)

	changed, err := Install(p, bin, core.ClaudeCode)
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if changed {
		t.Error("second install reported a change")
	}
	if got := read(t, p); got != first {
		t.Errorf("second install rewrote the file:\n%s\nwant:\n%s", got, first)
	}
}

func TestInstallKeepsUnrelatedSettings(t *testing.T) {
	p := write(t, `{"model":"opus","permissions":{"allow":["Bash"]},"env":{"FOO":"bar"}}`)
	if _, err := Install(p, bin, core.ClaudeCode); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(read(t, p)), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"model", "permissions", "env"} {
		if _, ok := got[key]; !ok {
			t.Errorf("install dropped %q", key)
		}
	}
}

func TestInstallKeepsOtherToolsHooks(t *testing.T) {
	p := write(t, `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"rtk-rewrite"}]}]}}`)
	if _, err := Install(p, bin, core.ClaudeCode); err != nil {
		t.Fatal(err)
	}
	// whoa now registers a PreToolUse hook of its own for the Nudge, so it
	// joins the event rather than being absent from it. What must survive is
	// the handler that was already there.
	h := hookEvents(t, p)
	if len(h["PreToolUse"]) != 2 {
		t.Fatalf("PreToolUse groups = %d, want whoa's alongside the existing one", len(h["PreToolUse"]))
	}
	if !strings.Contains(read(t, p), "rtk-rewrite") {
		t.Error("install dropped another tool's hook")
	}
}

// Uninstall must leave the other tool's PreToolUse hook exactly as it found it.
func TestUninstallLeavesOtherToolsPreToolUseHookAlone(t *testing.T) {
	before := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"rtk-rewrite"}]}]}}`
	p := write(t, before)
	if _, err := Install(p, bin, core.ClaudeCode); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(p); err != nil {
		t.Fatal(err)
	}
	h := hookEvents(t, p)
	if len(h["PreToolUse"]) != 1 {
		t.Fatalf("PreToolUse groups after uninstall = %d, want 1", len(h["PreToolUse"]))
	}
	if !strings.Contains(read(t, p), "rtk-rewrite") {
		t.Error("uninstall removed another tool's hook")
	}
}

func TestInstallKeepsOtherHandlersOnTheSameEvent(t *testing.T) {
	p := write(t, `{"hooks":{"PostToolUse":[{"matcher":"*","hooks":[{"type":"command","command":"someone-else"}]}]}}`)
	if _, err := Install(p, bin, core.ClaudeCode); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, p), "someone-else") {
		t.Error("install dropped another tool's handler")
	}
	if !strings.Contains(read(t, p), bin) {
		t.Error("install did not add whoa's handler")
	}
}

// Rewriting someone's settings file with the keys shuffled makes a noisy diff
// out of an install.
func TestInstallPreservesTopLevelKeyOrder(t *testing.T) {
	p := write(t, `{"zulu":1,"alpha":2,"model":"opus"}`)
	if _, err := Install(p, bin, core.ClaudeCode); err != nil {
		t.Fatal(err)
	}
	got := read(t, p)
	iz, ia, im := strings.Index(got, `"zulu"`), strings.Index(got, `"alpha"`), strings.Index(got, `"model"`)
	if !(iz < ia && ia < im) {
		t.Errorf("key order not preserved:\n%s", got)
	}
}

func TestInstallRefusesToTouchMalformedSettings(t *testing.T) {
	original := `{"model": "opus",`
	p := write(t, original)
	if _, err := Install(p, bin, core.ClaudeCode); err == nil {
		t.Error("expected an error for malformed settings")
	}
	if got := read(t, p); got != original {
		t.Errorf("malformed settings were modified:\n%s", got)
	}
}

func TestUninstallRemovesOnlyWhoasHandlers(t *testing.T) {
	p := write(t, `{"model":"opus","hooks":{"PostToolUse":[{"matcher":"*","hooks":[{"type":"command","command":"someone-else"}]}]}}`)
	if _, err := Install(p, bin, core.ClaudeCode); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(p); err != nil {
		t.Fatal(err)
	}
	got := read(t, p)
	if strings.Contains(got, bin) {
		t.Error("uninstall left whoa's handler behind")
	}
	if !strings.Contains(got, "someone-else") {
		t.Error("uninstall removed another tool's handler")
	}
	if !strings.Contains(got, `"model"`) {
		t.Error("uninstall dropped unrelated settings")
	}
}

// Nothing documents whether Claude Code tolerates an unknown key inside a hook
// handler, so whoa writes none. An install the Harness silently rejects is the
// exact failure whoa exists to prevent.
func TestInstallWritesOnlyDocumentedHandlerFields(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	if _, err := Install(p, bin, core.ClaudeCode); err != nil {
		t.Fatalf("install: %v", err)
	}
	documented := map[string]bool{"type": true, "command": true, "timeout": true}
	for event, groups := range hookEvents(t, p) {
		for _, g := range groups {
			hooks, ok := g["hooks"].([]any)
			if !ok {
				t.Fatalf("%s: no handlers", event)
			}
			for _, h := range hooks {
				for key := range h.(map[string]any) {
					if !documented[key] {
						t.Errorf("%s: handler carries undocumented key %q", event, key)
					}
				}
			}
		}
	}
}

// The command string is run through a shell, and a home directory with a space
// in it is ordinary on macOS. An unquoted path would install a hook that never
// runs, and uninstall would then not recognise it either.
func TestInstallQuotesABinaryPathWithSpaces(t *testing.T) {
	const spaced = "/Users/some one/Library/Application Support/whoa"
	p := filepath.Join(t.TempDir(), "settings.json")
	if _, err := Install(p, spaced, core.ClaudeCode); err != nil {
		t.Fatalf("install: %v", err)
	}
	command := hookEvents(t, p)["PostToolUse"][0]["hooks"].([]any)[0].(map[string]any)["command"].(string)
	if want := `'` + spaced + `' hook --harness=claude-code`; command != want {
		t.Errorf("command = %q, want %q", command, want)
	}

	changed, err := Uninstall(p)
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !changed {
		t.Error("uninstall did not recognise the handler it installed")
	}
	if strings.Contains(read(t, p), "whoa") {
		t.Errorf("uninstall left whoa behind:\n%s", read(t, p))
	}
}

// The settings file belongs to the user. whoa adds a hook to it; it does not
// get an opinion about who else may read it.
func TestInstallKeepsTheSettingsFilePermissions(t *testing.T) {
	p := write(t, `{"model":"opus"}`)
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(p, bin, core.ClaudeCode); err != nil {
		t.Fatalf("install: %v", err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o644); got != want {
		t.Errorf("mode = %o, want %o", got, want)
	}
}

// A file whoa creates is whoa's to lock down: it did not exist, so nothing is
// being narrowed behind the user's back.
func TestInstallCreatesSettingsPrivate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	if _, err := Install(p, bin, core.ClaudeCode); err != nil {
		t.Fatalf("install: %v", err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Errorf("mode = %o, want %o", got, want)
	}
}

// whoa recognises its own handlers by the command they run. Another tool whose
// command merely ends in "hook" is not one of them.
func TestUninstallLeavesALookalikeHandlerAlone(t *testing.T) {
	p := write(t, `{
  "hooks": {
    "PostToolUse": [
      {"matcher": "*", "hooks": [{"type": "command", "command": "/usr/local/bin/othertool hook"}]}
    ]
  }
}`)
	changed, err := Uninstall(p)
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if changed {
		t.Error("uninstall removed a handler whoa does not own")
	}
	if !strings.Contains(read(t, p), "othertool") {
		t.Errorf("uninstall removed another tool's hook:\n%s", read(t, p))
	}
}

// The two Harnesses fire different events. Installing Claude Code's list into
// Codex would register PostToolUseFailure, which Codex never fires — a hook
// that looks installed and can never run.
func TestEachHarnessGetsOnlyItsOwnEvents(t *testing.T) {
	for _, harness := range []core.Harness{core.ClaudeCode, core.Codex} {
		p := filepath.Join(t.TempDir(), "settings.json")
		if _, err := Install(p, bin, harness); err != nil {
			t.Fatal(err)
		}
		registered, err := Registered(p, harness)
		if err != nil {
			t.Fatal(err)
		}
		if len(registered) != len(core.Events(harness)) {
			t.Errorf("%s: registered %v, want %v", harness, registered, core.Events(harness))
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if harness == core.Codex && strings.Contains(string(b), "PostToolUseFailure") {
			t.Error("Codex settings contain an event Codex never fires")
		}
	}
}

// Uninstall works from every Harness's events, not one's, so a file written by
// an earlier version or by the other Harness is still cleaned up.
func TestUninstallRemovesAnotherHarnessesHandlers(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	if _, err := Install(p, bin, core.ClaudeCode); err != nil {
		t.Fatal(err)
	}
	changed, err := Uninstall(p)
	if err != nil || !changed {
		t.Fatalf("Uninstall = %v, %v", changed, err)
	}
	for _, harness := range []core.Harness{core.ClaudeCode, core.Codex} {
		registered, err := Registered(p, harness)
		if err != nil {
			t.Fatal(err)
		}
		if len(registered) != 0 {
			t.Errorf("%s: %v left behind", harness, registered)
		}
	}
}

// A handler written by a version of whoa that predates a flag is still whoa's,
// or an upgrade would leave a duplicate hook behind and uninstall would miss
// it.
func TestOwnershipSurvivesAChangeOfFlags(t *testing.T) {
	owned := []string{
		`'/usr/local/bin/whoa' hook`,
		`'/usr/local/bin/whoa' hook --harness=codex`,
		`'/usr/local/bin/whoa' hook --harness=claude-code --future-flag`,
		`/usr/local/bin/whoa hook`,
		`'/Users/some one/bin/whoa' hook --harness=codex`,
	}
	for _, command := range owned {
		if !(handler{Type: "command", Command: command}).owned() {
			t.Errorf("did not recognise its own handler: %s", command)
		}
	}
	foreign := []string{
		`'/usr/local/bin/ruler' hook`,
		`'/usr/local/bin/whoa-extra' hook`,
		`'/usr/local/bin/whoa' doctor`,
		`echo hello`,
		``,
	}
	for _, command := range foreign {
		if (handler{Type: "command", Command: command}).owned() {
			t.Errorf("claimed a handler it does not own: %s", command)
		}
	}
}
