package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testSystem(t *testing.T, stdin string) (system, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	return system{
		stdin:  strings.NewReader(stdin),
		stdout: out,
		stderr: errOut,
		home:   t.TempDir(),
		binary: "/usr/local/bin/whoa",
		now:    func() time.Time { return time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC) },
	}, out, errOut
}

const step = `{"session_id":"abc123","prompt_id":"turn-1","hook_event_name":"PostToolUse",
"tool_name":"Bash","tool_input":{"command":"ls"},"tool_response":{},"tool_use_id":"toolu_1"}`

func logLines(t *testing.T, s system, session string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(s.home, ".whoa", "sessions", session+".jsonl"))
	if err != nil {
		t.Fatalf("reading session log: %v", err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

func TestHookRecordsAStepAndEmitsNothing(t *testing.T) {
	s, out, errOut := testSystem(t, step)
	if err := run([]string{"hook"}, s); err != nil {
		t.Fatalf("hook: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("hook wrote %q to stdout, want nothing", out)
	}
	if errOut.Len() != 0 {
		t.Errorf("hook wrote %q to stderr, want nothing", errOut)
	}

	lines := logLines(t, s, "abc123")
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("log line is not JSON: %v", err)
	}
	for k, want := range map[string]any{"tool": "Bash", "turn": "turn-1", "outcome": "ok"} {
		if got[k] != want {
			t.Errorf("%s = %v, want %v", k, got[k], want)
		}
	}
}

// Whatever goes wrong inside whoa, the agent's Step must not be affected.
func TestHookNeverFailsTheAgentsStep(t *testing.T) {
	for name, stdin := range map[string]string{
		"empty":     "",
		"garbage":   "not json at all",
		"truncated": `{"session_id":"ab`,
		"unknown":   `{"session_id":"a","hook_event_name":"SessionStart"}`,
	} {
		s, out, _ := testSystem(t, stdin)
		if err := run([]string{"hook"}, s); err != nil {
			t.Errorf("%s: hook returned %v, want nil", name, err)
		}
		if out.Len() != 0 {
			t.Errorf("%s: wrote %q to stdout, want nothing", name, out)
		}
	}
}

func TestHookRecordsRepeatedStepsInOrder(t *testing.T) {
	s, _, _ := testSystem(t, "")
	for i := 0; i < 5; i++ {
		s.stdin = strings.NewReader(step)
		if err := run([]string{"hook"}, s); err != nil {
			t.Fatal(err)
		}
	}
	if lines := logLines(t, s, "abc123"); len(lines) != 5 {
		t.Fatalf("got %d lines, want 5", len(lines))
	}
}

func TestInstallThenHookThenUninstall(t *testing.T) {
	s, out, _ := testSystem(t, "")
	settings := filepath.Join(s.home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := run([]string{"install"}, s); err != nil {
		t.Fatalf("install: %v", err)
	}
	b, err := os.ReadFile(settings)
	if err != nil {
		t.Fatalf("settings not written: %v", err)
	}
	for _, event := range []string{"PostToolUse", "PostToolUseFailure"} {
		if !strings.Contains(string(b), event) {
			t.Errorf("settings missing %s", event)
		}
	}

	out.Reset()
	if err := run([]string{"install"}, s); err != nil {
		t.Fatalf("second install: %v", err)
	}
	if !strings.Contains(out.String(), "already installed") {
		t.Errorf("second install said %q", out)
	}

	if err := run([]string{"uninstall"}, s); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	b, _ = os.ReadFile(settings)
	if strings.Contains(string(b), "whoa") {
		t.Errorf("uninstall left whoa behind:\n%s", b)
	}
}

func TestVersionIsPrinted(t *testing.T) {
	s, out, _ := testSystem(t, "")
	if err := run([]string{"version"}, s); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) == "" {
		t.Error("version printed nothing")
	}
}

func TestUnknownCommandIsAnError(t *testing.T) {
	s, _, _ := testSystem(t, "")
	if err := run([]string{"nope"}, s); err == nil {
		t.Fatal("expected an error for an unknown command")
	}
}

func TestNoArgumentsPrintsUsage(t *testing.T) {
	s, out, _ := testSystem(t, "")
	if err := run(nil, s); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Errorf("printed %q", out)
	}
}

// failingReader stands in for a stdin that dies mid-read.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("stdin went away") }

// whoa may fail, but it may not fail invisibly: a user who installed it
// believes it is watching, so a broken read has to leave a trace somewhere.
func TestHookReportsItsOwnFailures(t *testing.T) {
	s, out, errOut := testSystem(t, "")
	s.stdin = failingReader{}

	if err := run([]string{"hook"}, s); err != nil {
		t.Fatalf("hook returned %v, want nil", err)
	}
	if out.Len() != 0 {
		t.Errorf("wrote %q to stdout, want nothing", out)
	}
	if !strings.Contains(errOut.String(), "stdin went away") {
		t.Errorf("stderr = %q, want the cause reported", errOut)
	}
}

func TestInstallWithoutAKnownBinaryIsAnError(t *testing.T) {
	s, _, _ := testSystem(t, "")
	s.binary = ""
	if err := run([]string{"install"}, s); err == nil {
		t.Fatal("expected an error when the binary path is unknown")
	}
}
