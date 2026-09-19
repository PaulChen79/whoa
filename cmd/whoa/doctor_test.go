package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withHarness creates the directory that makes a Harness look installed.
func withHarness(t *testing.T, home, dir string) string {
	t.Helper()
	path := filepath.Join(home, dir)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func doctor(t *testing.T, s system) (string, error) {
	t.Helper()
	err := run([]string{"doctor"}, s)
	return s.stdout.(interface{ String() string }).String(), err
}

// The failure doctor exists to catch: everything configured, nothing running.
func TestDoctorFailsWhenNothingHasEverRun(t *testing.T) {
	s, _, _ := testSystem(t, "")
	withHarness(t, s.home, ".claude")
	if err := run([]string{"install"}, s); err != nil {
		t.Fatal(err)
	}

	out, err := doctor(t, s)
	if err == nil {
		t.Error("doctor exited zero with no Step ever recorded; a script could not tell whoa was dead")
	}
	if !strings.Contains(out, "NOT OBSERVING") {
		t.Errorf("doctor did not say it is not observing:\n%s", out)
	}
	if !strings.Contains(out, "registered on") {
		t.Errorf("doctor did not report the registration it can see:\n%s", out)
	}
}

func TestDoctorReportsObservingOnceAStepIsRecorded(t *testing.T) {
	s, _, _ := testSystem(t, "")
	withHarness(t, s.home, ".claude")
	if err := run([]string{"install"}, s); err != nil {
		t.Fatal(err)
	}
	feed(t, s.home, failedStep)

	out, err := doctor(t, s)
	if err != nil {
		t.Errorf("doctor failed though a Step was recorded: %v\n%s", err, out)
	}
	if !strings.Contains(out, "observing: last Step") {
		t.Errorf("doctor did not report the evidence:\n%s", out)
	}
}

func TestDoctorExplainsManagedHooksOnly(t *testing.T) {
	s, _, _ := testSystem(t, "")
	codex := withHarness(t, s.home, ".codex")
	if err := os.WriteFile(filepath.Join(codex, "requirements.toml"),
		[]byte("# managed policy\nallow_managed_hooks_only = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"install"}, s); err != nil {
		t.Fatal(err)
	}

	out, _ := doctor(t, s)
	if !strings.Contains(out, "BLOCKED") || !strings.Contains(out, "allow_managed_hooks_only") {
		t.Errorf("doctor did not explain the one setting that silently disables it:\n%s", out)
	}
}

// A commented-out setting is not a setting.
func TestDoctorIgnoresACommentedOutPolicy(t *testing.T) {
	s, _, _ := testSystem(t, "")
	codex := withHarness(t, s.home, ".codex")
	if err := os.WriteFile(filepath.Join(codex, "requirements.toml"),
		[]byte("# allow_managed_hooks_only = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := doctor(t, s)
	if strings.Contains(out, "BLOCKED") {
		t.Errorf("doctor read a comment as policy:\n%s", out)
	}
}

func TestDoctorReportsAHarnessThatIsNotInstalled(t *testing.T) {
	s, _, _ := testSystem(t, "")
	withHarness(t, s.home, ".claude")
	out, _ := doctor(t, s)
	if !strings.Contains(out, "not installed on this machine") {
		t.Errorf("doctor did not distinguish a missing Harness from a broken one:\n%s", out)
	}
}

// Codex will not run an untrusted command hook, so an install that did not say
// so would be reporting a protection the user does not have.
func TestInstallPrintsTheCodexTrustStep(t *testing.T) {
	s, out, _ := testSystem(t, "")
	withHarness(t, s.home, ".codex")
	if err := run([]string{"install"}, s); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "REQUIRED") || !strings.Contains(text, "/hooks") {
		t.Errorf("install did not print the trust step as required:\n%s", text)
	}
}

func TestInstallFailsWhenNoHarnessIsPresent(t *testing.T) {
	s, _, _ := testSystem(t, "")
	if err := run([]string{"install"}, s); err == nil {
		t.Error("install reported success with no Harness to install into")
	}
}

// Codex registers different events from Claude Code, and installing one
// Harness's event list into the other would be a hook that never fires.
func TestInstallWritesEachHarnessItsOwnEvents(t *testing.T) {
	s, _, _ := testSystem(t, "")
	withHarness(t, s.home, ".claude")
	withHarness(t, s.home, ".codex")
	if err := run([]string{"install"}, s); err != nil {
		t.Fatal(err)
	}
	claude, err := os.ReadFile(filepath.Join(s.home, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	codex, err := os.ReadFile(filepath.Join(s.home, ".codex", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(claude), "PostToolUseFailure") {
		t.Error("Claude Code settings missing PostToolUseFailure")
	}
	if strings.Contains(string(codex), "PostToolUseFailure") {
		t.Error("Codex has no PostToolUseFailure; whoa registered a hook that can never fire")
	}
	for _, event := range []string{"PreToolUse", "PostToolUse", "UserPromptSubmit"} {
		if !strings.Contains(string(codex), event) {
			t.Errorf("Codex settings missing %s", event)
		}
	}
}

// Nothing in a payload says which Harness sent it. If the hook guessed, a
// Codex Step would be filed under Claude Code and doctor would report a
// Harness as observing when it is not.
func TestTheHookReportsTheHarnessItWasInstalledFor(t *testing.T) {
	s, _, _ := testSystem(t, "")
	withHarness(t, s.home, ".codex")
	raw, err := os.ReadFile(filepath.Join("..", "..", "internal", "core", "testdata", "codex_posttooluse_failed.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.stdin = strings.NewReader(string(raw))
	if err := run([]string{"hook", "--harness=codex"}, s); err != nil {
		t.Fatal(err)
	}

	out, _ := doctor(t, s)
	if !strings.Contains(out, "Codex\n  ") || !strings.Contains(out, "observing: last Step") {
		t.Errorf("the Codex Step was not attributed to Codex:\n%s", out)
	}
	if strings.Count(out, "observing: last Step") != 1 {
		t.Errorf("a Codex Step was counted for more than one Harness:\n%s", out)
	}
}

func TestAHookWithNoHarnessFlagIsClaudeCode(t *testing.T) {
	if got := harnessFrom(nil); got != "claude-code" {
		t.Errorf("harnessFrom(nil) = %q, want claude-code", got)
	}
	if got := harnessFrom([]string{"--harness=nonsense"}); got != "claude-code" {
		t.Errorf("an unknown Harness must fall back, got %q", got)
	}
}
