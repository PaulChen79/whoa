package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PaulChen79/whoa/internal/config"
)

const failedStep = `{"session_id":"abc123","prompt_id":"turn-1","hook_event_name":"PostToolUseFailure",
"tool_name":"Bash","tool_input":{"command":"npm test"},"error":"exit 1","tool_use_id":"toolu_1"}`

const beforeNextStep = `{"session_id":"abc123","prompt_id":"turn-1","hook_event_name":"PreToolUse",
"tool_name":"Bash","tool_input":{"command":"npm test"}}`

// feed runs one hook invocation against the same home directory, so a test can
// play a Session through whoa the way the Harness would.
func feed(t *testing.T, home, payload string) (stdout string, stderr string) {
	t.Helper()
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	s := system{
		stdin:  strings.NewReader(payload),
		stdout: out,
		stderr: errOut,
		home:   home,
		binary: "/usr/local/bin/whoa",
		now:    func() time.Time { return time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC) },
	}
	if err := run([]string{"hook"}, s); err != nil {
		t.Fatalf("hook: %v", err)
	}
	return out.String(), errOut.String()
}

func writeConfig(t *testing.T, home, body string) {
	t.Helper()
	dir := filepath.Join(home, ".whoa")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, config.Name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

type hookOut struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
	SystemMessage string `json:"systemMessage"`
}

func parseOut(t *testing.T, s string) hookOut {
	t.Helper()
	var o hookOut
	if err := json.Unmarshal([]byte(s), &o); err != nil {
		t.Fatalf("hook output is not JSON: %v\n%s", err, s)
	}
	return o
}

// The whole of M0, end to end: whoa watches a Loop form and warns the agent
// before its next Step, with no API key and no network.
func TestALoopEarnsANudgeBeforeTheNextStep(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, home, `{"trigger": {"repeated_failures": 3}}`)

	for i := 0; i < 2; i++ {
		if out, _ := feed(t, home, failedStep); out != "" {
			t.Fatalf("Nudged after %d failures: %s", i+1, out)
		}
	}
	// Two failures in: still nothing to say.
	if out, _ := feed(t, home, beforeNextStep); out != "" {
		t.Fatalf("Nudged before the trigger: %s", out)
	}

	feed(t, home, failedStep)
	out, errOut := feed(t, home, beforeNextStep)
	if out == "" {
		t.Fatal("three repeated failures produced no Nudge")
	}
	if errOut != "" {
		t.Errorf("hook wrote %q to stderr", errOut)
	}

	got := parseOut(t, out)
	if got.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Errorf("hookEventName = %q", got.HookSpecificOutput.HookEventName)
	}
	if !strings.Contains(got.HookSpecificOutput.AdditionalContext, "Bash") {
		t.Errorf("the agent's Nudge does not name the tool: %q", got.HookSpecificOutput.AdditionalContext)
	}
	if got.SystemMessage == "" {
		t.Error("the human was told nothing")
	}

	// The Nudge is in the log, next to the Steps that earned it.
	lines := logLines(t, system{home: home}, "abc123")
	if len(lines) != 4 {
		t.Fatalf("log has %d lines, want three Steps and one Nudge", len(lines))
	}
	var nudge map[string]any
	if err := json.Unmarshal([]byte(lines[3]), &nudge); err != nil {
		t.Fatal(err)
	}
	if nudge["kind"] != "nudge" || nudge["counter"] != "repeated_failures" {
		t.Errorf("last line = %v, want a recorded Nudge", nudge)
	}
}

// What the agent ran reaches the log only after Seam 2 has been through it:
// the command with its secrets gone, and nothing else the payload carried.
func TestTheLogLearnsOnlyWhatSeam2Allows(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, home, `{"trigger": {"repeated_failures": 2}}`)
	feed(t, home, failedStep)
	feed(t, home, failedStep)
	feed(t, home, beforeNextStep)

	b, err := os.ReadFile(filepath.Join(home, ".whoa", "sessions", "abc123.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"exit 1", "tool_input", "tool_response", "Cannot find"} {
		if bytes.Contains(b, []byte(leak)) {
			t.Errorf("the Session log contains %q:\n%s", leak, b)
		}
	}
	if !bytes.Contains(b, []byte(`"command":{"text":"npm test"}`)) {
		t.Errorf("the redacted command is missing; ADR 0003 says it earns its place:\n%s", b)
	}
}

func TestNotifyFalseSilencesTheHumanButNotTheAgent(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, home, `{"notify": false, "trigger": {"repeated_failures": 2}}`)
	feed(t, home, failedStep)
	feed(t, home, failedStep)

	got := parseOut(t, mustNudge(t, home))
	if got.SystemMessage != "" {
		t.Errorf("notify = false still spoke to the human: %q", got.SystemMessage)
	}
	if got.HookSpecificOutput.AdditionalContext == "" {
		t.Error("notify = false also silenced the agent's Nudge")
	}
}

func TestStateDirRelocatesTheSessionLog(t *testing.T) {
	home := t.TempDir()
	elsewhere := filepath.Join(t.TempDir(), "logs")
	writeConfig(t, home, `{"state_dir": `+quote(elsewhere)+`}`)
	feed(t, home, failedStep)

	if _, err := os.Stat(filepath.Join(elsewhere, "sessions", "abc123.jsonl")); err != nil {
		t.Errorf("log was not written under state_dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".whoa", "sessions")); !os.IsNotExist(err) {
		t.Error("a log was written to the default directory as well")
	}
}

// A config file that cannot be parsed must be reported, not swallowed, and
// must not stop whoa recording Steps.
func TestABrokenConfigIsReportedAndWhoaKeepsWatching(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, home, `{"window":`)
	_, errOut := feed(t, home, failedStep)
	if errOut == "" {
		t.Error("a broken config file was accepted in silence")
	}
	if _, err := os.Stat(filepath.Join(home, ".whoa", "sessions", "abc123.jsonl")); err != nil {
		t.Errorf("whoa stopped recording Steps over a config error: %v", err)
	}
}

func mustNudge(t *testing.T, home string) string {
	t.Helper()
	out, _ := feed(t, home, beforeNextStep)
	if out == "" {
		t.Fatal("expected a Nudge, got nothing")
	}
	return out
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
