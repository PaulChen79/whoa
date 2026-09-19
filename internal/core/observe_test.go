package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var at = time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)

func payload(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatalf("reading payload: %v", err)
	}
	return b
}

func observe(t *testing.T, name string) Observation {
	t.Helper()
	return Observe(Input{Raw: payload(t, name), Harness: ClaudeCode, Now: at})
}

func TestSuccessfulStepIsRecorded(t *testing.T) {
	ob := observe(t, "posttooluse_write")

	if ob.Entry == nil {
		t.Fatal("expected a Step to record")
	}
	if got, want := ob.Entry.Tool, "Write"; got != want {
		t.Errorf("Tool = %q, want %q", got, want)
	}
	if got, want := ob.Entry.Turn, "550e8400-e29b-41d4-a716-446655440000"; got != want {
		t.Errorf("Turn = %q, want %q", got, want)
	}
	if got, want := ob.Entry.Session, "abc123"; got != want {
		t.Errorf("Session = %q, want %q", got, want)
	}
	if got, want := ob.Entry.Outcome, OutcomeOK; got != want {
		t.Errorf("Outcome = %q, want %q", got, want)
	}
	if got, want := ob.Entry.DurationMS, 12; got != want {
		t.Errorf("DurationMS = %d, want %d", got, want)
	}
}

func TestFailedStepIsRecordedAsAFailure(t *testing.T) {
	ob := observe(t, "posttoolusefailure_bash")

	if ob.Entry == nil {
		t.Fatal("expected a Step to record")
	}
	if got, want := ob.Entry.Outcome, OutcomeError; got != want {
		t.Errorf("Outcome = %q, want %q", got, want)
	}
	if got, want := ob.Entry.Tool, "Bash"; got != want {
		t.Errorf("Tool = %q, want %q", got, want)
	}
}

// Loop detection counts repeated failures, so a Harness that reports failures
// on a separate event must not leave whoa blind to them.
func TestFailuresAndSuccessesAreBothObserved(t *testing.T) {
	for _, name := range []string{"posttooluse_write", "posttoolusefailure_bash"} {
		if ob := observe(t, name); ob.Entry == nil {
			t.Errorf("%s: expected a Step to record", name)
		}
	}
}

func TestSubagentStepRecordsWhichAgentTookIt(t *testing.T) {
	ob := observe(t, "posttooluse_subagent")

	if ob.Entry == nil {
		t.Fatal("expected a Step to record")
	}
	if got, want := ob.Entry.AgentID, "agent_01"; got != want {
		t.Errorf("AgentID = %q, want %q", got, want)
	}
}

func TestMissingTurnKeyIsNotAnError(t *testing.T) {
	ob := observe(t, "posttooluse_no_prompt_id")

	if ob.Entry == nil {
		t.Fatal("a Step before the first user prompt is still a Step")
	}
	if ob.Entry.Turn != "" {
		t.Errorf("Turn = %q, want empty", ob.Entry.Turn)
	}
}

// The agent's behaviour must never change, so whoa emits nothing at all.
func TestObservationEmitsNoOutput(t *testing.T) {
	for _, name := range []string{"posttooluse_write", "posttoolusefailure_bash", "posttooluse_subagent"} {
		if ob := observe(t, name); len(ob.Output) != 0 {
			t.Errorf("%s: emitted %q, want nothing", name, ob.Output)
		}
	}
}

// A broken payload must cost the user nothing: no output, no recorded Step,
// and above all no failed Step for the agent.
func TestMalformedPayloadIsSilentlyIgnored(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":         "",
		"not json":      "this is not json",
		"truncated":     `{"session_id": "abc`,
		"json array":    `[]`,
		"json null":     `null`,
		"no session id": `{"hook_event_name":"PostToolUse","tool_name":"Bash"}`,
		"no tool name":  `{"session_id":"abc","hook_event_name":"PostToolUse"}`,
		"unknown event": `{"session_id":"abc","hook_event_name":"SessionStart"}`,
	} {
		ob := Observe(Input{Raw: []byte(raw), Harness: ClaudeCode, Now: at})
		if ob.Entry != nil {
			t.Errorf("%s: recorded a Step, want none", name)
		}
		if len(ob.Output) != 0 {
			t.Errorf("%s: emitted %q, want nothing", name, ob.Output)
		}
	}
}

// Nothing free-form reaches disk in this ticket: command text and error text
// can carry secrets, and Redaction does not exist yet.
func TestRecordedStepCarriesNoFreeText(t *testing.T) {
	ob := observe(t, "posttoolusefailure_bash")
	if ob.Entry == nil {
		t.Fatal("expected a Step to record")
	}
	line, err := json.Marshal(ob.Entry)
	if err != nil {
		t.Fatalf("marshalling Step: %v", err)
	}
	for _, secret := range []string{"npm test", "Cannot find module", "Run test suite", "/Users/u/proj"} {
		if strings.Contains(string(line), secret) {
			t.Errorf("Step contains free text %q:\n%s", secret, line)
		}
	}
}

func TestStepIsOneJSONLine(t *testing.T) {
	ob := observe(t, "posttooluse_write")
	line, err := json.Marshal(ob.Entry)
	if err != nil {
		t.Fatalf("marshalling Step: %v", err)
	}
	if strings.ContainsAny(string(line), "\n\r") {
		t.Errorf("Step marshalled across lines:\n%s", line)
	}
}

// Install registers whatever the core says it handles, so the two can never
// drift apart into a Harness whose failures are never seen, or a Nudge with no
// event to be delivered on.
func TestEventsCoverBothOutcomesAndTheNudgeChannel(t *testing.T) {
	events := Events(ClaudeCode)
	seen := map[Outcome]bool{}
	for _, name := range events {
		event, ok := hookPayload{Event: name}.event(ClaudeCode)
		if !ok {
			t.Errorf("registered %q but do not handle it", name)
		}
		seen[event.Records] = true
	}
	if !seen[OutcomeOK] || !seen[OutcomeError] {
		t.Errorf("events cover %v, want both ok and error", seen)
	}
	if !seen[""] {
		t.Error("no event fires before a Step, so a Nudge has no way to reach the agent")
	}
}

// An unknown Harness must fall through to the no-op, not panic on a nil map
// entry.
func TestUnknownHarnessObservesNothing(t *testing.T) {
	if got := Events("nope"); len(got) != 0 {
		t.Errorf("Events(nope) = %v, want none", got)
	}
	ob := Observe(Input{Raw: payload(t, "posttooluse_write"), Harness: "nope", Now: at})
	if ob.Entry != nil {
		t.Errorf("recorded a Step for an unknown Harness: %+v", ob.Entry)
	}
}
